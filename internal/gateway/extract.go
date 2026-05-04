package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// Extraction limits. The output cap is intentionally tight — a 256 KiB
// dump translates to roughly 65 k tokens, already a meaningful chunk
// of any context window, and anything bigger should be retrieved via
// the agent's tool flow rather than inlined as one prompt block.
const (
	// maxAttachmentInputBytes caps the per-attachment payload size for
	// text and document attachments coming in from chat.send. Larger
	// than the image cap because PDFs carry more wrapper data per
	// extracted character.
	maxAttachmentInputBytes = 25 * 1024 * 1024

	// maxExtractedOutputBytes caps the post-extraction text per
	// attachment. Text past this point is dropped with a truncation
	// notice — keeps the LLM prompt finite even on hostile input.
	maxExtractedOutputBytes = 256 * 1024

	// extractionTimeout bounds the wall-clock cost of a single shell
	// extractor invocation.
	extractionTimeout = 30 * time.Second

	// maxConcurrentExtractions caps the number of pdftotext / pandoc
	// processes running simultaneously across the whole gateway. With
	// the per-message attachment count cap of 20, a single chat.send
	// could otherwise spawn 20 extractor processes at once, and
	// concurrent messages would multiply that. 4 is roughly aligned
	// with typical CPU cores on a developer laptop and bounds local
	// resource use without serialising attachments meaningfully (the
	// extractors are I/O-bound for typical PDFs).
	maxConcurrentExtractions = 4
)

// extractorSem is a buffered-channel semaphore that gates concurrent
// invocations of runExtractor. Capacity = maxConcurrentExtractions.
// Each runExtractor call sends an empty struct before spawning the
// child process and drains it on return; a 5th concurrent call
// blocks on the send until one of the running extractions finishes.
// Bounded by the parent decode timeout (60 s in handleChatSend) so a
// stuck queue can't hang the WebSocket forever.
var extractorSem = make(chan struct{}, maxConcurrentExtractions)

// allowedTextMimes lists application/* and other non-image MIMEs whose
// payload is safe to UTF-8 decode and inline directly. text/* is
// handled by prefix; this map covers the application/* exceptions.
var allowedTextMimes = map[string]struct{}{
	"application/json":          {},
	"application/xml":           {},
	"application/x-yaml":        {},
	"application/yaml":          {},
	"application/javascript":    {},
	"application/x-javascript":  {},
	"application/x-typescript":  {},
	"application/typescript":    {},
	"application/x-python":      {},
	"application/x-shellscript": {},
	"application/x-sh":          {},
	"application/x-ruby":        {},
	"application/x-go":          {},
	"application/x-rust":        {},
	"application/x-toml":        {},
	"application/toml":          {},
	"application/sql":           {},
	"application/x-sql":         {},
	"application/x-tex":         {},
}

// extractorBinary names the shell tool used to extract plain text from
// a particular MIME. Empty value means the bytes can be UTF-8 decoded
// directly (or matched via isPlainTextMime for prefix-based MIMEs).
type extractorBinary struct {
	bin     string   // executable name; looked up via $PATH
	args    []string // args excluding the input/output positionals
	pkgHint string  // human-readable install hint surfaced on missing-bin errors
}

// extractorByMime maps a binary document MIME to the shell extractor
// that converts it to plain text on stdout. Both extractors here read
// from stdin and write to stdout when given "-" as the input/output.
var extractorByMime = map[string]extractorBinary{
	"application/pdf": {
		bin:     "pdftotext",
		args:    []string{"-layout", "-enc", "UTF-8", "-", "-"},
		pkgHint: "install poppler (brew install poppler  /  apt install poppler-utils)",
	},
	// .docx — Office Open XML word processing
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {
		bin:     "pandoc",
		args:    []string{"-f", "docx", "-t", "plain", "--wrap=none"},
		pkgHint: "install pandoc (brew install pandoc  /  apt install pandoc)",
	},
}

// isImageMime returns true for the MIME types that decodeChatAttachments
// routes to []llm.ImageContent.
func isImageMime(mime string) bool {
	_, ok := allowedAttachmentMimes[mime]
	return ok
}

// isPlainTextMime returns true if the bytes for this MIME can be safely
// decoded as UTF-8 and inlined verbatim, without invoking an extractor.
func isPlainTextMime(mime string) bool {
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	_, ok := allowedTextMimes[mime]
	return ok
}

// allowedAudioMimes is the set of audio MIMEs the chat UI is willing
// to upload. Whether they can actually be sent depends on the active
// agent's provider Capabilities() — Gemini accepts these natively;
// Anthropic / OpenAI / others reject audio entirely.
var allowedAudioMimes = map[string]struct{}{
	"audio/mpeg":   {}, // .mp3
	"audio/mp4":    {}, // .m4a (sometimes)
	"audio/wav":    {},
	"audio/x-wav":  {},
	"audio/webm":   {},
	"audio/ogg":    {},
	"audio/flac":   {},
	"audio/aac":    {},
	"audio/x-m4a":  {},
}

// nativePDFMime returns true for MIMEs that map to a native document
// content block (Anthropic) / inline_data PDF part (Gemini) when the
// active provider's Capabilities advertises NativePDF. Anything else
// matching isExtractableDocMime falls through to text extraction.
func nativePDFMime(mime string) bool {
	return mime == "application/pdf"
}

// isAudioMime returns true if the MIME is in the audio allowlist.
// Audio uploads are gated at the gateway boundary by the active
// provider's NativeAudio capability.
func isAudioMime(mime string) bool {
	_, ok := allowedAudioMimes[mime]
	return ok
}

// isExtractableDocMime returns true if the MIME is a binary document
// type that needs a shell extractor (pdftotext, pandoc, …).
func isExtractableDocMime(mime string) bool {
	_, ok := extractorByMime[mime]
	return ok
}

// extractAttachmentText turns the bytes of one chat.send attachment
// into the plain-text representation that gets inlined into the user
// message. Returns ("", error) for unsupported types or extractor
// failures; the caller decides whether to surface the error to the
// chat client or fall through to a different handling path.
func extractAttachmentText(ctx context.Context, mime string, data []byte) (string, error) {
	mime = strings.ToLower(strings.TrimSpace(mime))

	if isPlainTextMime(mime) {
		text, err := decodeTextToUTF8(data)
		if err != nil {
			return "", err
		}
		if len(text) > maxExtractedOutputBytes {
			text = text[:maxExtractedOutputBytes] + "\n\n[... truncated]"
		}
		return text, nil
	}

	if ex, ok := extractorByMime[mime]; ok {
		return runExtractor(ctx, ex, data)
	}

	return "", fmt.Errorf("no extractor for mime %q", mime)
}

// runExtractor pipes data through a shell extractor and returns the
// captured stdout, capped at maxExtractedOutputBytes. Wrapped errors
// distinguish missing-binary, timeout, and non-zero-exit cases so the
// chat client can surface a useful hint to the user.
//
// Bounded by extractorSem to maxConcurrentExtractions concurrent
// processes globally — a chat.send with 20 PDFs spawns at most 4
// pdftotext invocations at once; the rest queue. The queue wait is
// itself bounded by parentCtx, which the gateway sets to a 60 s
// decode budget.
func runExtractor(parentCtx context.Context, ex extractorBinary, data []byte) (string, error) {
	if _, err := exec.LookPath(ex.bin); err != nil {
		return "", fmt.Errorf("%s not found on PATH (%s)", ex.bin, ex.pkgHint)
	}

	// Acquire semaphore slot or fail fast on parent cancel.
	select {
	case extractorSem <- struct{}{}:
		defer func() { <-extractorSem }()
	case <-parentCtx.Done():
		return "", fmt.Errorf("extraction queue: %w", parentCtx.Err())
	}

	ctx, cancel := context.WithTimeout(parentCtx, extractionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ex.bin, ex.args...)
	cmd.Stdin = bytes.NewReader(data)
	// Cap stdout via an io.LimitWriter; cap stderr separately at a small
	// size so a malicious input that drives the extractor to an
	// extractor-internal panic can't fill memory before we time out.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &capWriter{w: &stdout, max: maxExtractedOutputBytes + 1}
	cmd.Stderr = &capWriter{w: &stderr, max: 8 * 1024}

	err := cmd.Run()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s timed out after %s", ex.bin, extractionTimeout)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = ee.String()
			}
			return "", fmt.Errorf("%s failed: %s", ex.bin, msg)
		}
		return "", fmt.Errorf("%s: %w", ex.bin, err)
	}

	out := stdout.Bytes()
	truncated := false
	if len(out) > maxExtractedOutputBytes {
		out = out[:maxExtractedOutputBytes]
		truncated = true
	}
	text := string(out)
	if truncated {
		text += "\n\n[... truncated]"
	}
	return text, nil
}

// decodeTextToUTF8 turns the raw bytes of a plain-text attachment into
// a Go UTF-8 string, handling the common multilingual cases:
//
//   - UTF-8 BOM (EF BB BF) — stripped, treated as UTF-8.
//   - UTF-16 LE BOM (FF FE) — transcoded to UTF-8.
//   - UTF-16 BE BOM (FE FF) — transcoded to UTF-8.
//   - No BOM, valid UTF-8 — passed through verbatim. ASCII-only files
//     are valid UTF-8 by construction, so legacy text from any locale
//     that's already ASCII works unchanged.
//   - Heuristic UTF-16 LE/BE without BOM — when more than 30 % of the
//     stream is NUL bytes at fixed parity, transcode. Catches the
//     "Notepad saved as Unicode without BOM" case without false-
//     flagging arbitrary binary as UTF-16.
//
// Returns a wrapped error for non-UTF-8/UTF-16 streams (e.g., Latin-1,
// Shift-JIS, GBK) so the chat client can surface a "save as UTF-8" hint
// to the user instead of silently mangling the bytes.
func decodeTextToUTF8(data []byte) (string, error) {
	const (
		bomUTF8  = "\xEF\xBB\xBF"
		bomUTF16 = "\xFF\xFE"
		bomUTF16BE = "\xFE\xFF"
	)
	switch {
	case bytes.HasPrefix(data, []byte(bomUTF8)):
		data = data[len(bomUTF8):]
	case bytes.HasPrefix(data, []byte(bomUTF16)):
		dec := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
		out, _, err := transform.Bytes(dec, data)
		if err != nil {
			return "", fmt.Errorf("decode UTF-16 LE: %w", err)
		}
		return string(out), nil
	case bytes.HasPrefix(data, []byte(bomUTF16BE)):
		dec := unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder()
		out, _, err := transform.Bytes(dec, data)
		if err != nil {
			return "", fmt.Errorf("decode UTF-16 BE: %w", err)
		}
		return string(out), nil
	}
	// Try the BOM-less UTF-16 heuristic before accepting as UTF-8: NUL
	// is a valid UTF-8 byte (U+0000), so a stream of "ASCII text encoded
	// as UTF-16 LE" passes utf8.Valid and would otherwise leak through
	// as a string riddled with embedded NULs. The heuristic only fires
	// when at least 30 % of the bytes at one parity are NUL — pure
	// ASCII text and binary files both fall through harmlessly.
	sample := data
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	if order, ok := guessUTF16(sample); ok {
		dec := unicode.UTF16(order, unicode.IgnoreBOM).NewDecoder()
		out, _, err := transform.Bytes(dec, data)
		if err == nil && utf8.Valid(out) {
			return string(out), nil
		}
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	return "", fmt.Errorf("not valid UTF-8 or UTF-16 (try saving the file as UTF-8)")
}

// guessUTF16 looks at byte parity in the sample and returns the
// detected endianness if at least 30 % of the bytes at one parity are
// NUL — the signature of mostly-ASCII text encoded as UTF-16. Returns
// (_, false) when the data doesn't fit the pattern strongly enough,
// leaving the caller to fall through to a clear error rather than
// silently transcoding arbitrary binary.
func guessUTF16(sample []byte) (unicode.Endianness, bool) {
	if len(sample) < 2 {
		return unicode.LittleEndian, false
	}
	pairs := len(sample) / 2
	zerosLE, zerosBE := 0, 0 // byte at the high half of each code unit
	for i := 0; i+1 < len(sample); i += 2 {
		if sample[i+1] == 0 {
			zerosLE++ // little-endian: low byte first, high byte (zero for ASCII) second
		}
		if sample[i] == 0 {
			zerosBE++
		}
	}
	const threshold = 0.30
	if float64(zerosLE)/float64(pairs) >= threshold {
		return unicode.LittleEndian, true
	}
	if float64(zerosBE)/float64(pairs) >= threshold {
		return unicode.BigEndian, true
	}
	return unicode.LittleEndian, false
}

// capWriter is an io.Writer that drops everything past max bytes
// without erroring. Used so a misbehaving extractor that emits
// gigabytes of stdout won't OOM the gateway.
type capWriter struct {
	w   io.Writer
	max int
	n   int
}

func (c *capWriter) Write(p []byte) (int, error) {
	if c.n >= c.max {
		// Pretend we accepted the bytes so the child process keeps
		// running to natural completion. The cap is enforced after
		// command exit by the caller.
		return len(p), nil
	}
	remaining := c.max - c.n
	if len(p) > remaining {
		_, err := c.w.Write(p[:remaining])
		c.n += remaining
		if err != nil {
			return 0, err
		}
		return len(p), nil
	}
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}
