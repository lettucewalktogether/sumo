package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewChatHandlerServesAttachmentUI is a smoke test that the chat HTML
// renders cleanly through fmt.Fprintf (no stray %-directives, no PRINTF
// errors) and that the attachment-upload widgets we depend on are
// actually present in the served document. It's the cheapest guard
// against accidentally deleting the file picker, drop zone, or chip
// strip when refactoring the giant chatHTML constant.
func TestNewChatHandlerServesAttachmentUI(t *testing.T) {
	srv := httptest.NewServer(NewChatHandler(18789, "v0.6.3-22-g9486572"))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	html := string(body)

	// Printf bailed on a bad directive? Look for the obvious tells.
	assert.NotContains(t, html, "%!(", "Printf reported a bad format directive")
	assert.NotContains(t, html, "MISSING", "Printf reported a missing argument")

	// The port substitution itself must have happened.
	assert.Contains(t, html, "var PORT = 18789;")
	// Version substitution must produce a visible chip with the
	// version we passed in. Catches regressions where a refactor
	// drops the second %s slot or the brand element rename.
	assert.Contains(t, html, `id="sidebar-version"`)
	assert.Contains(t, html, "v0.6.3-22-g9486572")

	// Attachment UI elements that the JS reaches for by id — if any
	// rename the JS will silently break.
	for _, want := range []string{
		`id="input-area-wrap"`,
		`id="attach-btn"`,
		`id="file-picker"`,
		`id="attachment-strip"`,
		`id="attach-error"`,
		// Sidebar layout — sessions live here, not in a header dropdown.
		`id="layout"`,
		`id="sidebar"`,
		`id="sidebar-toggle"`,
		`id="new-chat-btn"`,
		`id="session-list"`,
		`id="session-list-empty"`,
		`id="sidebar-footer"`,
		// Bug-report button — opens a prefilled GitHub issue.
		`id="bug-report-btn"`,
		`id="main-pane"`,
		`id="messages-empty"`,
		// Tabs + cog + Jobs surface added in PR 3.
		`id="settings-btn"`,
		`id="sidebar-tabs"`,
		`id="tab-threads-btn"`,
		`id="tab-jobs-btn"`,
		`id="tab-threads"`,
		`id="tab-jobs"`,
		`id="jobs-list"`,
		`id="jobs-list-empty"`,
		// SVG icon symbols that the new row UI depends on.
		`id="i-more"`,
		`id="i-cog"`,
		`id="i-pencil"`,
		`id="i-pin"`,
		`id="i-trash"`,
		`id="i-export"`,
	} {
		assert.Contains(t, html, want, "expected element %q in served HTML", want)
	}

	// Stale-element regression — the original session dropdown and
	// "+ New" header button were replaced by the sidebar; if either
	// reappears the JS will have two competing inputs for the same
	// state. Also locks the v0.1.4 move from a global header Export
	// button to the per-bubble hover icon — id="export-btn" must
	// not reappear, and the new .msg-export-btn class must.
	for _, gone := range []string{
		`id="session-select"`,
		`id="new-session-btn"`,
		`id="export-btn"`,
	} {
		assert.NotContains(t, html, gone, "expected pre-sidebar element %q to be gone", gone)
	}
	// Per-response action toolbar — the assistant bubble factory
	// always attaches Copy / Download ▾ / Translate ▾ pills. The
	// label strings ride along inside the JS bundle so we can
	// assert on the exact text the user will see.
	for _, want := range []string{
		"msg-toolbar",
		"msg-pill",
		"buildAssistantToolbar",
		"buildDownloadDropdown",
		"buildTranslateDropdown",
		"Save as PDF",
		"Save as Word (.docx)",
		"Save as Markdown",
		"translateLanguages",
		"Sources Referenced",
		// v0.1.7 — SEA languages added to the Translate dropdown so
		// users running a SEA-LION model can actually translate
		// into the languages SEA-LION specialises in.
		"Indonesian (Bahasa)",
		"Malay",
		"Thai",
		"Tamil",
		"Khmer",
		"Lao",
		"Javanese",
		"Sundanese",
	} {
		assert.Contains(t, html, want,
			"expected per-response toolbar / sources hook %q in served HTML", want)
	}

	// Allowed MIME list (UI side) must mirror the server allowlist.
	for mime := range allowedAttachmentMimes {
		assert.Contains(t, html, mime,
			"file picker accept= or JS allowlist should mention %q", mime)
	}

	// blob: needs to be in the CSP for the live thumbnail object URLs.
	csp := resp.Header.Get("Content-Security-Policy")
	assert.Contains(t, csp, "blob:", "CSP must allow blob: for attachment thumbnails")

	// Sanity: no double-rendered template (would suggest the constant was
	// substituted into itself somehow).
	assert.Equal(t, 1, strings.Count(html, "<title>Felix Chat</title>"))
}

// TestSanitizeVersionString covers the input filter that protects the
// chat HTML from a hostile version string. The output ends up
// substituted into the page without further escaping, so anything
// outside the documented charset (alnum + . - _ +) must be stripped.
func TestSanitizeVersionString(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// git describe forms — the realistic happy path
		{"v0.6.3", "v0.6.3"},
		{"v0.6.3-22-g9486572", "v0.6.3-22-g9486572"},
		{"v0.6.3-22-g9486572-dirty", "v0.6.3-22-g9486572-dirty"},
		{"v1.0.0+exp.sha.5114f85", "v1.0.0+exp.sha.5114f85"},
		// edge inputs
		{"", "dev"},
		{"   ", "dev"},
		{"!@#$%^&*", "dev"},
		// unsafe characters get stripped, safe ones preserved
		{"v1.0<script>", "v1.0script"},
		{"v1.0\"alert\"", "v1.0alert"},
		{"v1.0' DROP TABLE", "v1.0DROPTABLE"},
		// length cap
		{strings.Repeat("a", 100), strings.Repeat("a", 40)},
	}
	for _, tc := range cases {
		got := sanitizeVersionString(tc.in)
		assert.Equal(t, tc.want, got, "input=%q", tc.in)
	}
}
