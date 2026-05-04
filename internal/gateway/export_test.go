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

	req := httptest.NewRequest("GET",
		"/api/session/export?agentId=default&sessionKey=k1&format=pdf", nil)
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
	// XSS guard: payload-side text is HTML-escaped.
	store2 := session.NewStore(t.TempDir())
	xss, _ := store2.Load("a", "x")
	xss.Append(session.UserMessageEntry(`<script>alert(1)</script> attempt`))
	h2 := NewExportHandlers(store2)
	req2 := httptest.NewRequest("GET", "/api/session/export?agentId=a&sessionKey=x&format=html", nil)
	rec2 := httptest.NewRecorder()
	h2.Export(rec2, req2)
	assert.Contains(t, rec2.Body.String(), "&lt;script&gt;")
	assert.NotContains(t, rec2.Body.String(), "<script>alert(1)</script>")
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
