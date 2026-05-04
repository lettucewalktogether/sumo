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
	srv := httptest.NewServer(NewChatHandler(18789))
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
		`id="main-pane"`,
		`id="messages-empty"`,
	} {
		assert.Contains(t, html, want, "expected element %q in served HTML", want)
	}

	// Stale-element regression — the original session dropdown and
	// "+ New" header button were replaced by the sidebar; if either
	// reappears the JS will have two competing inputs for the same
	// state.
	for _, gone := range []string{
		`id="session-select"`,
		`id="new-session-btn"`,
	} {
		assert.NotContains(t, html, gone, "expected pre-sidebar element %q to be gone", gone)
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
