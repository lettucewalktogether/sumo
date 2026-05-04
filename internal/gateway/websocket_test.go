package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/sausheong/felix/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalPDF returns the bytes of a tiny single-page PDF whose visible
// content is `body`. Only the structural pieces pdftotext needs are
// included — used so the extraction tests don't carry an opaque binary
// fixture.
func minimalPDF(t *testing.T, body string) []byte {
	t.Helper()
	stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", body)
	const obj1 = "1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n"
	const obj2 = "2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n"
	obj3 := "3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >> endobj\n"
	obj4 := fmt.Sprintf("4 0 obj << /Length %d >> stream\n%s\nendstream endobj\n", len(stream)+1, stream)
	const obj5 = "5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\n"

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for _, obj := range []string{obj1, obj2, obj3, obj4, obj5} {
		offsets = append(offsets, buf.Len())
		buf.WriteString(obj)
	}
	xrefStart := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer << /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefStart)
	return buf.Bytes()
}

// TestSafeRawMessage verifies the WS layer's RawMessage guard.
// Empty or invalid input must become nil (marshals to null) instead
// of triggering "unexpected end of JSON input" at marshal time —
// which would abort the entire WebSocket write and leave the chat
// client's tool_call entry stuck in a pending state.
func TestSafeRawMessage(t *testing.T) {
	tests := []struct {
		name string
		in   json.RawMessage
		want any
	}{
		{"nil", nil, nil},
		{"empty", json.RawMessage{}, nil},
		{"whitespace_only_invalid", json.RawMessage(`   `), nil},
		{"truncated_object", json.RawMessage(`{"a":`), nil},
		{"plain_text_invalid", json.RawMessage(`hello world`), nil},
		{"valid_object", json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":1}`)},
		{"valid_null", json.RawMessage(`null`), json.RawMessage(`null`)},
		{"valid_array", json.RawMessage(`[1,2,3]`), json.RawMessage(`[1,2,3]`)},
		{"valid_string", json.RawMessage(`"hi"`), json.RawMessage(`"hi"`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safeRawMessage(tc.in)
			assert.Equal(t, tc.want, got)

			// And — load-bearing — the result must round-trip through
			// json.Marshal without error. That's the regression we're
			// guarding: a valid `null` is fine, valid JSON is fine,
			// invalid bytes become null, never an error.
			_, err := json.Marshal(map[string]any{"input": got})
			assert.NoError(t, err)
		})
	}
}

// TestWriteJSONIsGoroutineSafe is the regression guard for the
// "panic: concurrent write to websocket connection" crash. The
// gateway has multiple paths that write to the same WS conn from
// different goroutines (main agent-event drain loop, trace SetOnMark
// callbacks, mid-stream chat.compact responses). gorilla/websocket
// panics if two of those races into Conn.NextWriter at once. This
// test fans 200 writes across 50 goroutines through writeJSON and
// asserts no panic and that the receiving end can decode every frame.
func TestWriteJSONIsGoroutineSafe(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		defer releaseConnMutex(conn)

		const goroutines = 50
		const writesPerG = 4
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for g := 0; g < goroutines; g++ {
			g := g
			go func() {
				defer wg.Done()
				for i := 0; i < writesPerG; i++ {
					writeJSON(conn, map[string]any{
						"goroutine": g,
						"index":     i,
					})
				}
			}()
		}
		wg.Wait()
		// One sentinel so the client knows when to stop reading.
		writeJSON(conn, map[string]any{"done": true})
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	got := 0
	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read after %d messages: %v", got, err)
		}
		if done, _ := msg["done"].(bool); done {
			break
		}
		got++
	}
	const expected = 50 * 4
	assert.Equal(t, expected, got, "every write should arrive intact, none dropped")
}

// TestDecodeChatAttachments covers the chat.send attachment guard. The
// decoder is the only place malformed UI input is rejected before the
// bytes flow into the runtime, so each branch needs explicit coverage:
// MIME allowlist (images, plain text, extractable docs), base64
// validity, per-image and per-doc size caps, per-message count cap,
// plain-text UTF-8 validation, and BOM-aware UTF-16 transcode.
func TestDecodeChatAttachments(t *testing.T) {
	ctx := context.Background()
	pngBytes := []byte("\x89PNG\r\n\x1a\nfake-png-data")
	pngB64 := base64.StdEncoding.EncodeToString(pngBytes)

	t.Run("nil_input", func(t *testing.T) {
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, nil)
		require.NoError(t, err)
		assert.Nil(t, out.Images)
		assert.Nil(t, out.Docs)
	})

	t.Run("empty_slice", func(t *testing.T) {
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{})
		require.NoError(t, err)
		assert.Nil(t, out.Images)
		assert.Nil(t, out.Docs)
	})

	t.Run("single_valid_png", func(t *testing.T) {
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "image/png", Data: pngB64, Name: "x.png"},
		})
		require.NoError(t, err)
		require.Len(t, out.Images, 1)
		assert.Equal(t, "image/png", out.Images[0].MimeType)
		assert.Equal(t, pngBytes, out.Images[0].Data)
		assert.Empty(t, out.Docs)
	})

	t.Run("mime_normalised", func(t *testing.T) {
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "  IMAGE/JPEG  ", Data: pngB64},
		})
		require.NoError(t, err)
		require.Len(t, out.Images, 1)
		assert.Equal(t, "image/jpeg", out.Images[0].MimeType)
	})

	t.Run("multiple_mixed_mimes", func(t *testing.T) {
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "image/png", Data: pngB64},
			{MimeType: "image/jpeg", Data: pngB64},
			{MimeType: "image/gif", Data: pngB64},
			{MimeType: "image/webp", Data: pngB64},
			{MimeType: "image/bmp", Data: pngB64},
		})
		require.NoError(t, err)
		assert.Len(t, out.Images, 5)
	})

	t.Run("rejects_unsupported_mime", func(t *testing.T) {
		// PR 4 added audio MIMEs to the allowlist (gated by caps), so
		// the previous "audio/mpeg" choice now yields a different error
		// (caps-gating). Use a genuinely-unsupported MIME — Windows
		// executables aren't in any of the four allowlists.
		_, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "application/x-msdownload", Data: pngB64},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported mime")
	})

	t.Run("rejects_unsupported_mime_at_index", func(t *testing.T) {
		_, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "image/png", Data: pngB64},
			{MimeType: "application/x-msdownload", Data: pngB64},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "attachment 1")
	})

	t.Run("rejects_invalid_base64", func(t *testing.T) {
		_, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "image/png", Data: "not!!!base64@@@"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "base64")
	})

	t.Run("rejects_empty_data", func(t *testing.T) {
		_, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "image/png", Data: ""},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("rejects_oversized_image", func(t *testing.T) {
		big := make([]byte, maxAttachmentBytes+1)
		_, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(big)},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too large")
	})

	t.Run("accepts_image_at_size_cap", func(t *testing.T) {
		atCap := make([]byte, maxAttachmentBytes)
		atCap[0] = 0xFF
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(atCap)},
		})
		require.NoError(t, err)
		require.Len(t, out.Images, 1)
		assert.Equal(t, maxAttachmentBytes, len(out.Images[0].Data))
	})

	t.Run("rejects_too_many_attachments", func(t *testing.T) {
		atts := make([]chatAttachmentParam, maxAttachmentCount+1)
		for i := range atts {
			atts[i] = chatAttachmentParam{MimeType: "image/png", Data: pngB64}
		}
		_, err := decodeChatAttachments(ctx, llm.Capabilities{}, atts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too many")
	})

	t.Run("accepts_at_count_cap", func(t *testing.T) {
		atts := make([]chatAttachmentParam, maxAttachmentCount)
		for i := range atts {
			atts[i] = chatAttachmentParam{MimeType: "image/png", Data: pngB64}
		}
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, atts)
		require.NoError(t, err)
		assert.Len(t, out.Images, maxAttachmentCount)
	})

	// --- Plain text ---

	t.Run("plain_text_utf8", func(t *testing.T) {
		body := "Hello — 世界! مرحبا."
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "text/markdown", Data: base64.StdEncoding.EncodeToString([]byte(body)), Name: "note.md"},
		})
		require.NoError(t, err)
		require.Empty(t, out.Images)
		require.Len(t, out.Docs, 1)
		assert.Equal(t, "note.md", out.Docs[0].Name)
		assert.Equal(t, body, out.Docs[0].Text)
	})

	t.Run("plain_text_unnamed_falls_back", func(t *testing.T) {
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("hi"))},
		})
		require.NoError(t, err)
		require.Len(t, out.Docs, 1)
		assert.Equal(t, "attachment-1", out.Docs[0].Name)
	})

	t.Run("strips_utf8_bom", func(t *testing.T) {
		body := []byte("\xEF\xBB\xBFhello")
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString(body)},
		})
		require.NoError(t, err)
		require.Len(t, out.Docs, 1)
		assert.Equal(t, "hello", out.Docs[0].Text)
	})

	t.Run("decodes_utf16_le_with_bom", func(t *testing.T) {
		// "héllo" in UTF-16 LE with BOM: FF FE 'h' 00 'é'(00E9) low/high 00 'l' 00 'l' 00 'o' 00
		body := []byte{0xFF, 0xFE,
			0x68, 0x00,
			0xE9, 0x00,
			0x6C, 0x00,
			0x6C, 0x00,
			0x6F, 0x00}
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString(body)},
		})
		require.NoError(t, err)
		require.Len(t, out.Docs, 1)
		assert.Equal(t, "héllo", out.Docs[0].Text)
	})

	t.Run("decodes_utf16_be_with_bom", func(t *testing.T) {
		body := []byte{0xFE, 0xFF,
			0x00, 0x68,
			0x00, 0xE9,
			0x00, 0x6C,
			0x00, 0x6C,
			0x00, 0x6F}
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString(body)},
		})
		require.NoError(t, err)
		require.Len(t, out.Docs, 1)
		assert.Equal(t, "héllo", out.Docs[0].Text)
	})

	t.Run("guesses_utf16_le_without_bom", func(t *testing.T) {
		// Notepad-without-BOM case — pad with enough text that the
		// 30 % NUL heuristic fires, since the test sample needs to be
		// decisively UTF-16-shaped.
		body := []byte{}
		for _, r := range []byte("Hello world this is a longer ASCII test string.") {
			body = append(body, r, 0x00)
		}
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString(body)},
		})
		require.NoError(t, err)
		require.Len(t, out.Docs, 1)
		assert.Contains(t, out.Docs[0].Text, "Hello world")
	})

	t.Run("rejects_latin1_with_clear_error", func(t *testing.T) {
		// 0xE9 alone is invalid UTF-8 (start of a multibyte sequence
		// without continuation), and the parity heuristic doesn't fire
		// — so the decoder must return a "save as UTF-8" hint rather
		// than silently mangle bytes.
		body := []byte("caf\xE9 — not utf-8 here")
		_, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString(body)},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "UTF-8")
	})

	// --- Document extraction (skipped if extractor binaries not on PATH) ---

	t.Run("extracts_pdf", func(t *testing.T) {
		if _, err := exec.LookPath("pdftotext"); err != nil {
			t.Skip("pdftotext not installed")
		}
		// A minimal PDF generated inline — simpler than carrying a fixture.
		pdf := minimalPDF(t, "Hello world from PDF.")
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "application/pdf", Data: base64.StdEncoding.EncodeToString(pdf), Name: "test.pdf"},
		})
		require.NoError(t, err)
		require.Len(t, out.Docs, 1)
		assert.Contains(t, out.Docs[0].Text, "Hello world from PDF")
	})

	t.Run("missing_extractor_binary_yields_install_hint", func(t *testing.T) {
		// Stub the extractor map so the lookup fails predictably.
		orig := extractorByMime
		t.Cleanup(func() { extractorByMime = orig })
		extractorByMime = map[string]extractorBinary{
			"application/pdf": {bin: "this-binary-does-not-exist-xyz", args: nil, pkgHint: "install xyz"},
		}
		body := []byte("not a real pdf")
		_, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "application/pdf", Data: base64.StdEncoding.EncodeToString(body)},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "install xyz")
	})

	// --- Capability routing (PR 4) -------------------------------

	t.Run("pdf_routes_native_when_caps_advertise_NativePDF", func(t *testing.T) {
		body := []byte("%PDF-1.4 not actually parsed for this branch")
		out, err := decodeChatAttachments(ctx, llm.Capabilities{NativePDF: true}, []chatAttachmentParam{
			{MimeType: "application/pdf", Data: base64.StdEncoding.EncodeToString(body), Name: "report.pdf"},
		})
		require.NoError(t, err)
		// Bytes go to NativeDocs verbatim — no extraction, no
		// pdftotext shell-out at all.
		require.Len(t, out.NativeDocs, 1)
		assert.Equal(t, "application/pdf", out.NativeDocs[0].MimeType)
		assert.Equal(t, body, out.NativeDocs[0].Data)
		assert.Equal(t, "report.pdf", out.NativeDocs[0].Name)
		// And nothing leaked into Docs (which is the extraction path).
		assert.Empty(t, out.Docs)
	})

	t.Run("audio_accepted_when_caps_advertise_NativeAudio", func(t *testing.T) {
		mp3Body := []byte{0xFF, 0xFB, 0x90, 0x00, 'h', 'i'} // fake MP3 header bytes
		out, err := decodeChatAttachments(ctx, llm.Capabilities{NativeAudio: true}, []chatAttachmentParam{
			{MimeType: "audio/mpeg", Data: base64.StdEncoding.EncodeToString(mp3Body), Name: "voice.mp3"},
		})
		require.NoError(t, err)
		require.Len(t, out.Audio, 1)
		assert.Equal(t, "audio/mpeg", out.Audio[0].MimeType)
		assert.Equal(t, mp3Body, out.Audio[0].Data)
		assert.Equal(t, "voice.mp3", out.Audio[0].Name)
	})

	t.Run("audio_rejected_when_provider_lacks_NativeAudio", func(t *testing.T) {
		// Same payload as the accepted-when-capable test, but caps
		// has NativeAudio=false (e.g. Anthropic / OpenAI / Qwen).
		mp3Body := []byte{0xFF, 0xFB, 0x90, 0x00, 'h', 'i'}
		_, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "audio/mpeg", Data: base64.StdEncoding.EncodeToString(mp3Body), Name: "voice.mp3"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "audio not supported")
		// Error must surface the offending attachment's display name
		// so the user can identify which file to remove.
		assert.Contains(t, err.Error(), "voice.mp3")
	})

	t.Run("pdf_falls_back_to_extraction_when_caps_lack_NativePDF", func(t *testing.T) {
		// When caps say no native PDF, the decoder routes through
		// the PR 2 extractor path. We need a real PDF here for
		// pdftotext to succeed; reuse the minimalPDF helper.
		if _, err := exec.LookPath("pdftotext"); err != nil {
			t.Skip("pdftotext not installed")
		}
		pdf := minimalPDF(t, "Capability fallback test.")
		out, err := decodeChatAttachments(ctx, llm.Capabilities{}, []chatAttachmentParam{
			{MimeType: "application/pdf", Data: base64.StdEncoding.EncodeToString(pdf), Name: "fallback.pdf"},
		})
		require.NoError(t, err)
		// Falls through to Docs (extracted text), NOT NativeDocs.
		assert.Empty(t, out.NativeDocs, "no native route when caps lack NativePDF")
		require.Len(t, out.Docs, 1)
		assert.Contains(t, out.Docs[0].Text, "Capability fallback test")
	})
}

// TestComposeUserText pins the format of the inlined doc blocks so a
// future tweak doesn't accidentally change the prompt the model sees.
func TestComposeUserText(t *testing.T) {
	t.Run("no_docs_returns_text_unchanged", func(t *testing.T) {
		assert.Equal(t, "hello", composeUserText("hello", nil))
	})

	t.Run("appends_each_doc_as_fenced_block", func(t *testing.T) {
		got := composeUserText("review please", []extractedDoc{
			{Name: "a.md", Text: "first body"},
			{Name: "b.txt", Text: "second body\n"},
		})
		want := "review please" +
			"\n\n--- attached: a.md ---\nfirst body\n--- end ---" +
			"\n\n--- attached: b.txt ---\nsecond body\n--- end ---"
		assert.Equal(t, want, got)
	})

	t.Run("empty_user_text_still_emits_block", func(t *testing.T) {
		got := composeUserText("", []extractedDoc{{Name: "x", Text: "y"}})
		assert.Equal(t, "--- attached: x ---\ny\n--- end ---", got)
	})
}

// TestChatSendParamsJSON locks down the wire format the chat UI emits
// against the params struct. If a future field rename or tag tweak
// breaks deserialisation, the chat.send path silently drops attachments
// — this ensures the contract stays intact.
func TestChatSendParamsJSON(t *testing.T) {
	raw := []byte(`{
		"agentId": "default",
		"text": "what's this?",
		"sessionKey": "ws_default",
		"attachments": [
			{"mimeType": "image/png", "data": "AAAA", "name": "shot.png"},
			{"mimeType": "image/jpeg", "data": "////"}
		]
	}`)
	var p chatSendParams
	require.NoError(t, json.Unmarshal(raw, &p))
	assert.Equal(t, "default", p.AgentID)
	assert.Equal(t, "what's this?", p.Text)
	assert.Equal(t, "ws_default", p.SessionKey)
	require.Len(t, p.Attachments, 2)
	assert.Equal(t, "image/png", p.Attachments[0].MimeType)
	assert.Equal(t, "shot.png", p.Attachments[0].Name)
	assert.Equal(t, "AAAA", p.Attachments[0].Data)
	assert.Equal(t, "image/jpeg", p.Attachments[1].MimeType)
	assert.Equal(t, "", p.Attachments[1].Name) // omitted ⇒ empty
}
