package gateway

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/sausheong/felix/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedTestSession creates a small session with one user turn, one
// assistant turn, and (optionally) a tool-call/tool-result pair so
// the export tests have realistic content to walk.
func seedTestSession(t *testing.T, store *session.Store, agentID, key string, withTools bool) *session.Session {
	t.Helper()
	sess, err := store.Load(agentID, key)
	require.NoError(t, err)
	sess.Append(session.UserMessageEntry("How does Felix handle PDFs?"))
	if withTools {
		// ToolCallEntry signature is (toolCallID, toolName, input).
		sess.Append(session.ToolCallEntry("tc-1", "read_file",
			json.RawMessage(`{"path":"./report.pdf"}`)))
		sess.Append(session.ToolResultEntry("tc-1", "PDF content here.", "", nil))
	}
	sess.Append(session.AssistantMessageEntry(
		"Felix routes PDFs through native document blocks on Anthropic and Gemini. " +
			"Other providers fall back to text extraction via pdftotext."))
	return sess
}

func TestExport_RequiresSessionKey(t *testing.T) {
	store := session.NewStore(t.TempDir())
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET", "/api/session/export?agentId=default&format=md", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "sessionKey")
}

func TestExport_RejectsTraversalKey(t *testing.T) {
	store := session.NewStore(t.TempDir())
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=../escape&format=md", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid sessionKey")
}

func TestExport_UnsupportedFormat(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedTestSession(t, store, "default", "k1", false)
	h := NewExportHandlers(store)

	// rtf isn't supported — use it to verify the unsupported-format
	// branch (pdf used to belong here before v0.1.5 made it a real
	// server-side renderer).
	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=rtf", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unsupported format")
}

func TestExport_Markdown(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedTestSession(t, store, "default", "k1", false)
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=md", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/markdown; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `filename="`)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `.md"`)
	body := rec.Body.String()
	assert.Contains(t, body, "## You")
	assert.Contains(t, body, "## Assistant")
	assert.Contains(t, body, "How does Felix handle PDFs?")
	assert.Contains(t, body, "native document blocks")
	// includeTools default is false — the tool call/result that wasn't
	// seeded into THIS session shouldn't appear, and the tool-call
	// fences shouldn't appear either.
	assert.NotContains(t, body, "Tool call")
}

func TestExport_TextStripsFormatting(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedTestSession(t, store, "default", "k1", false)
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=txt", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	// Plain-text format uses "You:" / "Assistant:" labels, NOT
	// markdown headings.
	assert.Contains(t, body, "You:")
	assert.Contains(t, body, "Assistant:")
	assert.NotContains(t, body, "## You")
	assert.NotContains(t, body, "## Assistant")
}

func TestExport_HTMLIsValid(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedTestSession(t, store, "default", "k1", false)
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=html", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.True(t, strings.HasPrefix(body, "<!doctype html>"),
		"HTML export should start with doctype")
	assert.Contains(t, body, "<title>")
	// Print-friendly @media print is a key feature for the
	// HTML→browser PDF flow.
	assert.Contains(t, body, "@media print")
	// XSS guard: payload-side raw HTML in the markdown source is
	// stripped. goldmark runs in safe mode (Unsafe NOT enabled), so
	// `<script>` tags inside a message are dropped and replaced with
	// the standard "raw HTML omitted" marker rather than passed
	// through. The active-payload form (executable script) must
	// never appear in the output.
	store2 := session.NewStore(t.TempDir())
	xss, _ := store2.Load("a", "x")
	xss.Append(session.UserMessageEntry(`<script>alert(1)</script> attempt`))
	h2 := NewExportHandlers(store2)
	req2 := httptest.NewRequest("GET", "/api/session/export?agentId=a&sessionKey=x&format=html", nil)
	rec2 := httptest.NewRecorder()
	h2.Export(rec2, req2)
	assert.NotContains(t, rec2.Body.String(), "<script>alert(1)</script>",
		"executable script must not survive into HTML export")
}

func TestExport_JSONIsLossless(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedTestSession(t, store, "default", "k1", true)
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=json", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	// Round-trips through json.Unmarshal cleanly and contains every
	// entry kind we seeded — JSON export is for archival and must
	// be lossless.
	var entries []session.SessionEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &entries))
	require.GreaterOrEqual(t, len(entries), 4) // user + tool_call + tool_result + assistant
	kinds := map[session.EntryType]int{}
	for _, e := range entries {
		kinds[e.Type]++
	}
	assert.Equal(t, 2, kinds[session.EntryTypeMessage])
	assert.Equal(t, 1, kinds[session.EntryTypeToolCall])
	assert.Equal(t, 1, kinds[session.EntryTypeToolResult])
}

func TestExport_IncludeToolsToggle(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedTestSession(t, store, "default", "k1", true) // with tools

	h := NewExportHandlers(store)

	t.Run("excluded_by_default", func(t *testing.T) {
		req := httptest.NewRequest("GET",
			"/api/session/export?agentId=default&sessionKey=k1&format=md", nil)
		rec := httptest.NewRecorder()
		h.Export(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.NotContains(t, body, "Tool call",
			"includeTools defaults to false — tool entries should NOT appear")
		assert.NotContains(t, body, "Tool result")
	})

	t.Run("included_when_true", func(t *testing.T) {
		req := httptest.NewRequest("GET",
			"/api/session/export?agentId=default&sessionKey=k1&format=md&includeTools=true", nil)
		rec := httptest.NewRecorder()
		h.Export(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, "Tool call")
		assert.Contains(t, body, "read_file")
		assert.Contains(t, body, "Tool result")
		assert.Contains(t, body, "PDF content here.")
	})
}

func TestExport_FilenameUsesFriendlyName(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedTestSession(t, store, "default", "k1", false)
	require.NoError(t, store.SetName("default", "k1", "Q3 review (final)"))
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=md", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	disp := rec.Header().Get("Content-Disposition")
	// Sanitisation collapses unsafe chars: "Q3 review (final)" → "Q3 review -final"
	// — the parens hyphen-replaced because they're not on the safe-char list.
	assert.Contains(t, disp, "filename=")
	// Either way: must NOT contain the raw parens (would be unsafe in a header).
	assert.NotContains(t, disp, "(")
	assert.NotContains(t, disp, ")")
	// And must NOT contain the bare key (we set a friendly name).
	assert.NotContains(t, disp, "k1")
}

func TestSanitizeExportFilename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"simple", "simple"},
		{"  trimmed  ", "trimmed"},
		{"Has Spaces OK", "Has Spaces OK"},
		{"With/Slash", "With-Slash"},
		{"With\\Backslash", "With-Backslash"},
		{"Q3 review (final)", "Q3 review -final"},
		{"   ", "conversation"},
		{"", "conversation"},
		{"!@#$%^&*", "conversation"},
		{strings.Repeat("a", 200), strings.Repeat("a", 80)},
	}
	for _, tc := range cases {
		got := sanitizeExportFilename(tc.in)
		assert.Equal(t, tc.want, got, "input=%q", tc.in)
	}
}

func TestExport_DocxViaPandoc(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not installed")
	}
	store := session.NewStore(t.TempDir())
	seedTestSession(t, store, "default", "k1", false)
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=docx", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code,
		"body=%s", rec.Body.String())
	assert.Equal(t,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		rec.Header().Get("Content-Type"))
	body := rec.Body.Bytes()
	require.NotEmpty(t, body)

	// A .docx is a ZIP archive — minimal validity check: the first
	// bytes are the ZIP magic, and we can list entries with
	// archive/zip.
	assert.Equal(t, "PK", string(body[:2]),
		".docx must start with the ZIP magic 'PK'")
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err, "produced docx must parse as a valid ZIP")
	// Office Open XML packages always contain a [Content_Types].xml.
	var hasContentTypes bool
	for _, f := range zr.File {
		if f.Name == "[Content_Types].xml" {
			hasContentTypes = true
			break
		}
	}
	assert.True(t, hasContentTypes, ".docx must contain [Content_Types].xml")
}

// TestExport_ContextCancelStopsDocxQueue exercises the docx queue
// path in mdToDocx — when the extractor semaphore is full and the
// request context cancels, the handler returns rather than hanging.
func TestExport_ContextCancelStopsDocxQueue(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not installed")
	}
	// Saturate the extractor semaphore so the docx render queues.
	for i := 0; i < maxConcurrentExtractions; i++ {
		extractorSem <- struct{}{}
	}
	t.Cleanup(func() {
		for {
			select {
			case <-extractorSem:
			default:
				return
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := mdToDocx(ctx, "# hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue")
}

// Helper: round-trip a query through url.QueryEscape so test URLs
// stay readable while still handling tricky characters cleanly.
func _ /*urlAware*/ (key string) string { return url.QueryEscape(key) }

// seedMultiTurnSession builds a session with three Q&A pairs so the
// per-message export tests have multiple assistant turns to address.
// Pair 0: weather  · Pair 1: PDFs (with a tool call)  · Pair 2: docs
func seedMultiTurnSession(t *testing.T, store *session.Store, agentID, key string) *session.Session {
	t.Helper()
	sess, err := store.Load(agentID, key)
	require.NoError(t, err)
	sess.Append(session.UserMessageEntry("What's the weather?"))
	sess.Append(session.AssistantMessageEntry("I can't check live weather."))
	sess.Append(session.UserMessageEntry("How does Felix handle PDFs?"))
	sess.Append(session.ToolCallEntry("tc-1", "read_file",
		json.RawMessage(`{"path":"./report.pdf"}`)))
	sess.Append(session.ToolResultEntry("tc-1", "PDF content here.", "", nil))
	sess.Append(session.AssistantMessageEntry(
		"Felix routes PDFs through native document blocks where the provider supports them."))
	sess.Append(session.UserMessageEntry("Where do exported docs go?"))
	sess.Append(session.AssistantMessageEntry(
		"The download location depends on your browser's settings."))
	return sess
}

// TestExport_MessageIndexPicksOnePair exercises the per-message
// export path. messageIndex=1 should include only the second
// assistant turn and the user prompt that drove it — not the first
// or third turns. The tool call between the user prompt and the
// assistant reply is included in the entry slice but only emitted
// when includeTools is on.
func TestExport_MessageIndexPicksOnePair(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedMultiTurnSession(t, store, "default", "k1")
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=md&messageIndex=1", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	body := rec.Body.String()
	assert.Contains(t, body, "How does Felix handle PDFs?")
	assert.Contains(t, body, "native document blocks")
	// The other turns must NOT appear.
	assert.NotContains(t, body, "weather")
	assert.NotContains(t, body, "exported docs")
	// includeTools defaults to false — tool call shouldn't render.
	assert.NotContains(t, body, "Tool call")

	// Filename must encode the message index so a flurry of single-
	// response exports doesn't collide on the user's filesystem.
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "msg2")
}

// TestExport_MessageIndexZeroAndLast covers the boundary cases —
// the very first assistant turn (idx 0) and the last one (idx 2).
func TestExport_MessageIndexZeroAndLast(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedMultiTurnSession(t, store, "default", "k1")
	h := NewExportHandlers(store)

	for _, tc := range []struct {
		idx     int
		mustHave, mustNotHave string
	}{
		{0, "What's the weather?", "PDFs"},
		{2, "Where do exported docs go?", "weather"},
	} {
		req := httptest.NewRequest("GET",
			"/api/session/export?agentId=default&sessionKey=k1&format=md&messageIndex="+
				strconv.Itoa(tc.idx), nil)
		rec := httptest.NewRecorder()
		h.Export(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "idx=%d body=%s", tc.idx, rec.Body.String())
		assert.Contains(t, rec.Body.String(), tc.mustHave, "idx=%d", tc.idx)
		assert.NotContains(t, rec.Body.String(), tc.mustNotHave, "idx=%d", tc.idx)
	}
}

// TestExport_MessageIndexIncludeToolsKeepsToolCall confirms the
// tool call between the user prompt and the target assistant
// message renders when includeTools is on.
func TestExport_MessageIndexIncludeToolsKeepsToolCall(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedMultiTurnSession(t, store, "default", "k1")
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=md&messageIndex=1&includeTools=true", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Tool call")
	assert.Contains(t, body, "read_file")
	assert.Contains(t, body, "Tool result")
}

// TestExport_MessageIndexOutOfRange returns 400 when the requested
// index doesn't resolve to an assistant message.
func TestExport_MessageIndexOutOfRange(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedMultiTurnSession(t, store, "default", "k1")
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=md&messageIndex=99", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "out of range")
}

// TestExport_HTMLPrintModeWrapsCodeAndTables catches the v0.1.10
// regression where the PDF export clipped long code lines at the
// right edge — `import { supabase } from './supabaseClient'; //`
// got cut mid-word because chromedp's PrintToPDF can't render the
// pre block's overflow-x:auto scrollbar. The fix is a print-mode
// CSS override that switches code blocks to wrap and tables to
// table-layout:fixed; both must appear inside the @media print
// block of every served HTML export.
func TestExport_HTMLPrintModeWrapsCodeAndTables(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedTestSession(t, store, "default", "k1", false)
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=html", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	// Must have the @media print block.
	assert.Contains(t, body, "@media print")
	// Must override pre's overflow + add a wrap directive in print.
	for _, want := range []string{
		"pre-wrap",
		"break-all",
		"overflow-wrap: anywhere",
		"table-layout: fixed",
	} {
		assert.Contains(t, body, want,
			"print-mode CSS must contain %q to keep PDFs from clipping wide content", want)
	}
}

// TestExport_HTMLRendersMarkdownNotLiteral catches the regression
// where the HTML / PDF export htmlEscape'd the raw markdown body
// and the user's PDF showed **bold**, # headings, and | tables |
// as literal text. The renderer must produce real <strong>, <h1>,
// <table> markup instead.
func TestExport_HTMLRendersMarkdownNotLiteral(t *testing.T) {
	store := session.NewStore(t.TempDir())
	sess, err := store.Load("default", "k1")
	require.NoError(t, err)
	sess.Append(session.UserMessageEntry("ask"))
	sess.Append(session.AssistantMessageEntry(
		"# Big header\n\n" +
			"Some **bold** and *italic* text with `code`.\n\n" +
			"- bullet one\n" +
			"- bullet two\n\n" +
			"| Col A | Col B |\n" +
			"| --- | --- |\n" +
			"| cell-a | cell-b |\n",
	))
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=html", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	body := rec.Body.String()

	// Real markup, not literal markdown.
	assert.Contains(t, body, "<h1", "# heading must become <h1>")
	assert.Contains(t, body, "<strong>bold</strong>",
		"**bold** must become <strong>")
	assert.Contains(t, body, "<em>italic</em>",
		"*italic* must become <em>")
	assert.Contains(t, body, "<code>code</code>",
		"`code` must become <code>")
	assert.Contains(t, body, "<ul",
		"- bullets must become <ul>/<li>")
	assert.Contains(t, body, "<table",
		"GFM | tables | must become <table>")
	assert.Contains(t, body, "cell-a", "table cell content must survive")

	// Old bug guard — these patterns appeared as literal text in the
	// pre-goldmark export.
	assert.NotContains(t, body, "**bold**",
		"raw **bold** marker must not survive into HTML output")
	assert.NotContains(t, body, "| Col A | Col B |",
		"raw markdown table syntax must not survive as text")
}

// TestExport_MessageIndexNonInteger rejects garbage in the param.
func TestExport_MessageIndexNonInteger(t *testing.T) {
	store := session.NewStore(t.TempDir())
	seedMultiTurnSession(t, store, "default", "k1")
	h := NewExportHandlers(store)

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=md&messageIndex=abc", nil)
	rec := httptest.NewRecorder()
	h.Export(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "messageIndex")
}
