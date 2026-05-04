package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sausheong/felix/internal/session"
)

// ExportFormat is one of the supported output formats for a session
// export. The HTTP handler routes on the format query string.
type ExportFormat string

const (
	ExportMarkdown ExportFormat = "md"
	ExportText     ExportFormat = "txt"
	ExportHTML     ExportFormat = "html"
	ExportDocx     ExportFormat = "docx"
	ExportJSON     ExportFormat = "json"
	ExportPDF      ExportFormat = "pdf"
)

// ExportHandlers is the handler set for /api/session/export.
type ExportHandlers struct {
	Export http.HandlerFunc
}

// NewExportHandlers wires up a session-export endpoint backed by the
// supplied session store. The endpoint accepts:
//
//	GET /api/session/export
//	    ?agentId=<id>
//	    &sessionKey=<key>
//	    &format=md|txt|html|docx|json
//	    &includeTools=true|false  (default: false)
//	    &messageIndex=<N>         (optional — Nth assistant message,
//	                               0-based; exports just that turn
//	                               plus its preceding user prompt)
//
// On success it returns the rendered body with Content-Disposition
// set to attachment so browsers download rather than navigate.
//
// docx is rendered by piping the markdown body through `pandoc -f
// markdown -t docx` — the same shell-out pattern PR 2 uses for
// inbound document extraction. PDF is handled UI-side (the chat
// opens the html export in a new window and triggers
// window.print()) so no LaTeX install is required.
func NewExportHandlers(store *session.Store) *ExportHandlers {
	return &ExportHandlers{
		Export: func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			agentID := strings.TrimSpace(q.Get("agentId"))
			sessionKey := strings.TrimSpace(q.Get("sessionKey"))
			format := ExportFormat(strings.TrimSpace(q.Get("format")))
			includeTools := q.Get("includeTools") == "true" || q.Get("includeTools") == "1"

			// messageIndex is optional. Absent / empty / negative means
			// "export the whole session". A non-negative integer scopes
			// the export to the Nth (0-based) assistant message and the
			// preceding user prompt (a self-contained Q&A pair).
			messageIndex := -1
			if raw := strings.TrimSpace(q.Get("messageIndex")); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil {
					http.Error(w, "messageIndex must be an integer", http.StatusBadRequest)
					return
				}
				messageIndex = n
			}

			if agentID == "" {
				agentID = "default"
			}
			if sessionKey == "" {
				http.Error(w, "sessionKey required", http.StatusBadRequest)
				return
			}
			// Inherits the path-traversal guard from PR 4 follow-up:
			// sessionKey is validated before being composed into a
			// filesystem path by the store.
			if err := session.ValidateSessionKey(sessionKey); err != nil {
				http.Error(w, "invalid sessionKey: "+err.Error(), http.StatusBadRequest)
				return
			}

			sess, err := store.Load(agentID, sessionKey)
			if err != nil {
				http.Error(w, "load session: "+err.Error(), http.StatusInternalServerError)
				return
			}

			// Resolve a friendly filename from the session metadata
			// sidecar (PR 3) — falls back to the bare key.
			displayName := sessionKey
			if infos, err := store.List(agentID); err == nil {
				for _, info := range infos {
					if info.Key == sessionKey && info.Name != "" {
						displayName = info.Name
						break
					}
				}
			}

			entries := selectExportEntries(sess.Entries(), messageIndex)
			if messageIndex >= 0 && len(entries) == 0 {
				http.Error(w, "messageIndex out of range", http.StatusBadRequest)
				return
			}

			// Per-message exports get a "-msgN" filename suffix so a
			// flurry of single-response exports doesn't collide on disk.
			filenameStem := sanitizeExportFilename(displayName)
			if messageIndex >= 0 {
				filenameStem = fmt.Sprintf("%s-msg%d", filenameStem, messageIndex+1)
			}

			var body []byte
			var contentType, ext string
			switch format {
			case ExportMarkdown:
				body = []byte(renderConversationMarkdown(entries, displayName, includeTools))
				contentType = "text/markdown; charset=utf-8"
				ext = "md"
			case ExportText:
				body = []byte(renderConversationText(entries, includeTools))
				contentType = "text/plain; charset=utf-8"
				ext = "txt"
			case ExportHTML:
				body = []byte(renderConversationHTML(entries, displayName, includeTools))
				contentType = "text/html; charset=utf-8"
				ext = "html"
			case ExportJSON:
				out, err := json.MarshalIndent(entries, "", "  ")
				if err != nil {
					http.Error(w, "marshal: "+err.Error(), http.StatusInternalServerError)
					return
				}
				body = out
				contentType = "application/json; charset=utf-8"
				ext = "json"
			case ExportDocx:
				md := renderConversationMarkdown(entries, displayName, includeTools)
				out, err := mdToDocx(r.Context(), md)
				if err != nil {
					// Surface the install-hint path so the chat UI
					// can render the same friendly-error pattern
					// already used for missing pdftotext / pandoc.
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				body = out
				contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
				ext = "docx"
			case ExportPDF:
				// Real .pdf via chromedp's PrintToPDF — Felix already
				// depends on chromedp for the browser tool, so no new
				// system dependency. The HTML render is the same one
				// the html/print fallback used; chromedp prints it
				// with a real page layout instead of relying on the
				// user's browser print dialog.
				html := renderConversationHTML(entries, displayName, includeTools)
				out, err := renderHTMLToPDF(r.Context(), html)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				body = out
				contentType = "application/pdf"
				ext = "pdf"
			default:
				http.Error(w, "unsupported format: "+string(format)+
					" (md, txt, html, docx, pdf, json)", http.StatusBadRequest)
				return
			}

			filename := filenameStem + "." + ext
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Content-Disposition",
				`attachment; filename="`+filename+`"`)
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(body)
		},
	}
}

// selectExportEntries narrows a session's entries to what should be
// rendered. messageIndex < 0 means "the whole session"; messageIndex
// >= 0 scopes the export to the Nth (0-based) assistant message and
// the user message immediately preceding it. Tool-call / tool-result
// entries between the user prompt and the target assistant message
// are kept — the renderer's includeTools flag still decides whether
// they appear in the rendered output.
//
// Returns nil when messageIndex is out of range so the handler can
// return 400.
func selectExportEntries(entries []session.SessionEntry, messageIndex int) []session.SessionEntry {
	if messageIndex < 0 {
		return entries
	}
	targetIdx := -1
	seen := 0
	for i, e := range entries {
		if e.Type == session.EntryTypeMessage && e.Role == "assistant" {
			if seen == messageIndex {
				targetIdx = i
				break
			}
			seen++
		}
	}
	if targetIdx == -1 {
		return nil
	}
	// Walk back until we hit the user message that drove this turn.
	userIdx := -1
	for j := targetIdx - 1; j >= 0; j-- {
		if entries[j].Type == session.EntryTypeMessage && entries[j].Role == "user" {
			userIdx = j
			break
		}
	}
	if userIdx == -1 {
		// Conversation opened with an assistant message (rare — system-
		// seeded greetings, etc.). Export just the assistant entry.
		return []session.SessionEntry{entries[targetIdx]}
	}
	return entries[userIdx : targetIdx+1]
}

// sanitizeExportFilename strips characters that would be illegal or
// awkward in a Content-Disposition filename. Leaves alphanumerics,
// space, dash, underscore, and dot. Collapses runs of unsafe chars
// to a single hyphen so a friendly name like "Q3 review (final)"
// becomes "Q3-review-final".
var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9 _.\-]+`)

func sanitizeExportFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "conversation"
	}
	clean := unsafeFilenameChars.ReplaceAllString(name, "-")
	clean = strings.Trim(clean, "-")
	if clean == "" {
		return "conversation"
	}
	const maxLen = 80
	if len(clean) > maxLen {
		clean = clean[:maxLen]
	}
	return clean
}

// renderConversationMarkdown is the canonical export format. The
// other formats are derived from this:
//   - txt: strip headings + emphasis runes
//   - html: pre-rendered with a simple style
//   - docx: piped through pandoc
func renderConversationMarkdown(entries []session.SessionEntry, title string, includeTools bool) string {
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(title)
	sb.WriteString("\n\n")
	sb.WriteString("_Exported ")
	sb.WriteString(time.Now().Format(time.RFC3339))
	sb.WriteString("_\n\n---\n\n")

	for _, e := range entries {
		switch e.Type {
		case session.EntryTypeMessage:
			var md session.MessageData
			if err := json.Unmarshal(e.Data, &md); err != nil {
				continue
			}
			switch e.Role {
			case "user":
				sb.WriteString("## You\n\n")
			case "assistant":
				sb.WriteString("## Assistant\n\n")
			default:
				continue
			}
			sb.WriteString(strings.TrimSpace(md.Text))
			sb.WriteString("\n\n")
		case session.EntryTypeToolCall:
			if !includeTools {
				continue
			}
			var td session.ToolCallData
			if err := json.Unmarshal(e.Data, &td); err != nil {
				continue
			}
			fmt.Fprintf(&sb, "**Tool call:** `%s`\n\n```json\n%s\n```\n\n",
				td.Tool, prettyJSON(td.Input))
		case session.EntryTypeToolResult:
			if !includeTools {
				continue
			}
			var tr session.ToolResultData
			if err := json.Unmarshal(e.Data, &tr); err != nil {
				continue
			}
			label := "**Tool result:**"
			if tr.IsError {
				label = "**Tool error:**"
			} else if tr.Aborted {
				label = "**Tool result (aborted):**"
			}
			out := tr.Output
			if tr.Error != "" {
				out = tr.Error
			}
			fmt.Fprintf(&sb, "%s\n\n```\n%s\n```\n\n",
				label, strings.TrimSpace(out))
		case session.EntryTypeCompaction:
			// Compaction entries are session-internal summaries; not
			// useful in a user-facing export. Skip them — the entries
			// they summarise are still in the session, so the export
			// is lossless even with the summary omitted.
			continue
		}
	}
	return sb.String()
}

// renderConversationText is the markdown export with all formatting
// runes stripped — heading prefixes become plain "You:" / "Assistant:"
// labels and code-fence backticks drop. Suitable for opening in any
// editor that doesn't render markdown.
func renderConversationText(entries []session.SessionEntry, includeTools bool) string {
	var sb strings.Builder
	for _, e := range entries {
		switch e.Type {
		case session.EntryTypeMessage:
			var md session.MessageData
			if err := json.Unmarshal(e.Data, &md); err != nil {
				continue
			}
			switch e.Role {
			case "user":
				sb.WriteString("You:\n")
			case "assistant":
				sb.WriteString("Assistant:\n")
			default:
				continue
			}
			sb.WriteString(strings.TrimSpace(md.Text))
			sb.WriteString("\n\n")
		case session.EntryTypeToolCall:
			if !includeTools {
				continue
			}
			var td session.ToolCallData
			if err := json.Unmarshal(e.Data, &td); err != nil {
				continue
			}
			fmt.Fprintf(&sb, "[Tool call: %s]\n%s\n\n", td.Tool, prettyJSON(td.Input))
		case session.EntryTypeToolResult:
			if !includeTools {
				continue
			}
			var tr session.ToolResultData
			if err := json.Unmarshal(e.Data, &tr); err != nil {
				continue
			}
			out := tr.Output
			if tr.Error != "" {
				out = tr.Error
			}
			fmt.Fprintf(&sb, "[Tool result]\n%s\n\n", strings.TrimSpace(out))
		}
	}
	return sb.String()
}

// renderConversationHTML produces a print-friendly HTML document.
// The chat UI's PDF export route opens this in a new window and
// calls window.print() — the user gets the OS's native "Save as
// PDF" dialog without the gateway needing LaTeX or wkhtmltopdf
// installed.
func renderConversationHTML(entries []session.SessionEntry, title string, includeTools bool) string {
	var sb strings.Builder
	sb.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>`)
	sb.WriteString(htmlEscape(title))
	sb.WriteString(`</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
       max-width: 7.5in; margin: 1in auto; line-height: 1.5; color: #1a1a1a; }
h1 { border-bottom: 2px solid #ccc; padding-bottom: 0.4em; }
h2 { margin-top: 2em; color: #2a4d8f; font-size: 1.1em; }
h2.you { color: #1f6f3f; }
.meta { color: #666; font-size: 0.85em; margin-bottom: 2em; }
.msg { margin: 0.4em 0 1.4em; white-space: pre-wrap; }
.msg .bubble { unicode-bidi: plaintext; }
pre { background: #f4f4f6; padding: 0.6em 0.8em; border-radius: 4px; overflow-x: auto;
      font-size: 0.85em; }
code { font-family: "SF Mono", "Fira Code", Menlo, monospace; }
.tool-call, .tool-result {
    background: #fafafa; border-left: 3px solid #ccc; padding: 0.4em 0.8em; margin: 0.6em 0;
    font-size: 0.9em;
}
.tool-call { border-color: #2a4d8f; }
.tool-result.error { border-color: #c33; }
@media print {
    body { margin: 0.6in; max-width: none; }
    h2 { page-break-after: avoid; }
}
</style>
</head>
<body>
`)
	fmt.Fprintf(&sb, "<h1>%s</h1>\n", htmlEscape(title))
	fmt.Fprintf(&sb, "<p class=\"meta\">Exported %s</p>\n",
		htmlEscape(time.Now().Format(time.RFC1123)))

	for _, e := range entries {
		switch e.Type {
		case session.EntryTypeMessage:
			var md session.MessageData
			if err := json.Unmarshal(e.Data, &md); err != nil {
				continue
			}
			switch e.Role {
			case "user":
				sb.WriteString(`<h2 class="you">You</h2>`)
			case "assistant":
				sb.WriteString(`<h2>Assistant</h2>`)
			default:
				continue
			}
			fmt.Fprintf(&sb, "\n<div class=\"msg\"><div class=\"bubble\" dir=\"auto\">%s</div></div>\n",
				htmlEscape(strings.TrimSpace(md.Text)))
		case session.EntryTypeToolCall:
			if !includeTools {
				continue
			}
			var td session.ToolCallData
			if err := json.Unmarshal(e.Data, &td); err != nil {
				continue
			}
			fmt.Fprintf(&sb,
				"<div class=\"tool-call\"><strong>Tool call:</strong> <code>%s</code><pre>%s</pre></div>\n",
				htmlEscape(td.Tool), htmlEscape(prettyJSON(td.Input)))
		case session.EntryTypeToolResult:
			if !includeTools {
				continue
			}
			var tr session.ToolResultData
			if err := json.Unmarshal(e.Data, &tr); err != nil {
				continue
			}
			cls := "tool-result"
			label := "Tool result"
			if tr.IsError {
				cls = "tool-result error"
				label = "Tool error"
			}
			out := tr.Output
			if tr.Error != "" {
				out = tr.Error
			}
			fmt.Fprintf(&sb,
				"<div class=\"%s\"><strong>%s:</strong><pre>%s</pre></div>\n",
				cls, label, htmlEscape(strings.TrimSpace(out)))
		}
	}
	sb.WriteString(`</body>
</html>
`)
	return sb.String()
}

// mdToDocx pipes the markdown body through `pandoc -f markdown -t docx`
// and returns the binary payload. Reuses the bounded shell-out pattern
// from extract.go's runExtractor — same 30 s timeout, same install-
// hint error if pandoc is missing.
func mdToDocx(parentCtx context.Context, md string) ([]byte, error) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		return nil, fmt.Errorf("pandoc not found on PATH (install with: brew install pandoc / apt install pandoc)")
	}
	ctx, cancel := context.WithTimeout(parentCtx, extractionTimeout)
	defer cancel()

	// Acquire the same extractor semaphore as the inbound path —
	// keeps total concurrent shell-outs bounded across export AND
	// extraction so a flurry of exports can't starve uploads.
	select {
	case extractorSem <- struct{}{}:
		defer func() { <-extractorSem }()
	case <-parentCtx.Done():
		return nil, fmt.Errorf("docx render queue: %w", parentCtx.Err())
	}

	cmd := exec.CommandContext(ctx, "pandoc",
		"-f", "markdown",
		"-t", "docx",
		"--standalone",
	)
	cmd.Stdin = strings.NewReader(md)
	var stdout, stderr bytes.Buffer
	// docx output can be reasonably large for long conversations —
	// keep the cap higher than the inbound 256 KiB text cap. 8 MiB
	// is more than enough for hundreds of turns.
	cmd.Stdout = &capWriter{w: &stdout, max: 8 * 1024 * 1024}
	cmd.Stderr = &capWriter{w: &stderr, max: 8 * 1024}
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("pandoc failed: %s", msg)
	}
	return stdout.Bytes(), nil
}

// prettyJSON re-formats raw JSON for inclusion in an export body. If
// the input isn't valid JSON, returns it unchanged so a malformed
// tool input still appears in the export rather than being dropped.
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return string(raw)
	}
	return pretty.String()
}

// htmlEscape is a tiny replacement for html.EscapeString that we
// hand-roll to keep the export-renderer dependency-free. Escapes
// the five characters that have semantic meaning in HTML.
func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return r.Replace(s)
}
