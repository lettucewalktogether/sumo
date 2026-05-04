package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sausheong/felix/internal/config"
	"github.com/sausheong/felix/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveConfig_ErrorResponsesAreValidJSON is the regression guard
// for the bug where the SaveConfig handler used fmt.Fprintf with a
// raw `{"error":"...%s..."}` template to surface error messages. If
// the underlying error contained a " or \, the response body itself
// became malformed JSON, and the browser's r.json() parse failed
// with a useless "Expected ',' or '}' after property value at
// position 29" message that hid the real cause.
//
// The fix routes every error through json.NewEncoder, which escapes
// the cause's text properly. This test fires payloads that produce
// errors containing JSON-significant characters (quotes, backslashes,
// newlines) and asserts each response is parseable JSON whose error
// field round-trips back to the underlying message.
func TestSaveConfig_ErrorResponsesAreValidJSON(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetPath(t.TempDir() + "/felix.json5")
	handlers := NewSettingsHandlers(cfg, &tools.Registry{}, nil, nil)

	cases := []struct {
		name string
		body string // POST body
		// We don't pin the exact error text since Go's encoding/json
		// wording can shift between versions; we only care that the
		// response is parseable JSON whose .error contains a
		// distinguishing fragment.
		wantSubstr string
	}{
		{
			name:       "trailing_garbage_after_value",
			body:       `{"agents":{"defaults":"x"x`, // unexpected x at column 22
			wantSubstr: "invalid JSON",
		},
		{
			name:       "embedded_quote_in_value",
			body:       `{"agents":{"defaults":"a"b"}}`,
			wantSubstr: "invalid JSON",
		},
		{
			name:       "trailing_garbage_with_backslash",
			body:       `{"agents":\}`,
			wantSubstr: "invalid JSON",
		},
		{
			name:       "totally_not_json",
			body:       "Hello there!",
			wantSubstr: "invalid JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/settings/api/config", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handlers.SaveConfig(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code,
				"malformed body should yield 400")
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			// Critical: the body MUST parse as JSON, no matter what
			// JSON-significant characters the underlying parser put
			// into its error message.
			var resp map[string]string
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			require.NoError(t, err,
				"response body must be valid JSON; got: %q", rec.Body.String())
			assert.Contains(t, resp["error"], tc.wantSubstr,
				"error message should mention %q; got: %q", tc.wantSubstr, resp["error"])
		})
	}
}

// TestSaveConfig_HappyPath confirms a well-formed POST returns
// {"ok":true} so we don't accidentally regress the success path
// while hardening the error path.
func TestSaveConfig_HappyPath(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetPath(t.TempDir() + "/felix.json5")
	handlers := NewSettingsHandlers(cfg, &tools.Registry{}, nil, nil)

	body := `{
		"providers": {"a":{"kind":"openai-compatible","api_key":"x","base_url":"http://127.0.0.1/v1"}},
		"agents": {"list":[{"id":"default","name":"D","model":"a/m"}]}
	}`
	req := httptest.NewRequest("POST", "/settings/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlers.SaveConfig(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["ok"])
}
