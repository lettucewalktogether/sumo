package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sausheong/felix/internal/config"
	"github.com/sausheong/felix/internal/llm"
	"github.com/sausheong/felix/internal/llm/llmtest"
	"github.com/sausheong/felix/internal/session"
	"github.com/sausheong/felix/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// translateTestSetup returns a TranslateHandlers wired against a stub
// LLM provider so the tests don't need real API keys.
func translateTestSetup(t *testing.T, canned string) (*TranslateHandlers, *llmtest.Stub) {
	t.Helper()
	stub := &llmtest.Stub{Text: canned}
	cfg := &config.Config{}
	cfg.Agents.List = []config.AgentConfig{
		{ID: "default", Name: "Default", Model: "stub/test-model"},
	}
	wsh := NewWebSocketHandler(
		map[string]llm.LLMProvider{"stub": stub},
		tools.NewRegistry(),
		session.NewStore(t.TempDir()),
		cfg,
	)
	return NewTranslateHandlers(wsh), stub
}

func translatePost(t *testing.T, h *TranslateHandlers, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/translate",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Translate(rec, req)
	return rec
}

func TestTranslate_StreamsResponse(t *testing.T) {
	h, stub := translateTestSetup(t, "Hola, mundo.")
	rec := translatePost(t, h,
		`{"agentId":"default","text":"Hello, world.","lang":"es"}`)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "Hola, mundo.", rec.Body.String())
	// stub.Text was returned and the request reached the stub.
	_ = stub
}

func TestTranslate_PassesLanguageNameToSystemPrompt(t *testing.T) {
	h, stub := translateTestSetup(t, "...")
	var seenSystem string
	stub.ChatHook = func(req llm.ChatRequest) {
		if len(req.SystemPromptParts) > 0 {
			seenSystem = req.SystemPromptParts[0].Text
		}
	}
	rec := translatePost(t, h,
		`{"agentId":"default","text":"hi","lang":"vi"}`)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, seenSystem, "Vietnamese",
		"system prompt should name the target language by its English label")
}

func TestTranslate_RejectsUnsupportedLang(t *testing.T) {
	h, _ := translateTestSetup(t, "")
	rec := translatePost(t, h,
		`{"agentId":"default","text":"hi","lang":"xx"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unsupported language")
}

func TestTranslate_RequiresText(t *testing.T) {
	h, _ := translateTestSetup(t, "")
	rec := translatePost(t, h,
		`{"agentId":"default","text":"   ","lang":"es"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "text is required")
}

func TestTranslate_RejectsBadJSON(t *testing.T) {
	h, _ := translateTestSetup(t, "")
	rec := translatePost(t, h, `{not json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid JSON")
}

func TestTranslate_RejectsGet(t *testing.T) {
	h, _ := translateTestSetup(t, "")
	req := httptest.NewRequest(http.MethodGet, "/api/translate", nil)
	rec := httptest.NewRecorder()
	h.Translate(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTranslate_UnknownAgent(t *testing.T) {
	h, _ := translateTestSetup(t, "")
	rec := translatePost(t, h,
		`{"agentId":"ghost","text":"hi","lang":"es"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unknown agent")
}

func TestTranslate_TruncatesOversizedInput(t *testing.T) {
	h, stub := translateTestSetup(t, "ok")
	var seenLen int
	stub.ChatHook = func(req llm.ChatRequest) {
		if len(req.Messages) > 0 {
			seenLen = len(req.Messages[0].Content)
		}
	}
	huge := strings.Repeat("x", translateMaxBodyChars+5000)
	body := `{"agentId":"default","text":"` + huge + `","lang":"es"}`
	rec := translatePost(t, h, body)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, translateMaxBodyChars, seenLen,
		"input above the cap must be trimmed before forwarding to the LLM")
}

// TestTranslateLanguagesByCode_MatchesOrdered guards the invariant
// that translateLanguagesByCode and translateLanguagesOrdered stay
// in sync — adding a row to one slice and forgetting the other
// would silently make a language unselectable or unvalidatable.
func TestTranslateLanguagesByCode_MatchesOrdered(t *testing.T) {
	assert.Equal(t, len(translateLanguagesOrdered), len(translateLanguagesByCode))
	for _, l := range translateLanguagesOrdered {
		got, ok := translateLanguagesByCode[l.Code]
		require.True(t, ok, "ordered code %q missing from byCode map", l.Code)
		assert.Equal(t, l.Name, got)
	}
}

// TestTranslateAcceptsSEALanguages locks in the v0.1.7 expansion so
// the SEA-LION-aligned languages (Thai, Indonesian, Malay, Tamil,
// Khmer, Lao, Javanese, Sundanese) can never be silently removed
// from the allowlist. Each one also still has to round-trip through
// the actual handler — a stub provider returns a canned response so
// we don't need an LLM. Burmese, Vietnamese, Tagalog were already
// supported and are sanity-checked here too.
func TestTranslateAcceptsSEALanguages(t *testing.T) {
	h, _ := translateTestSetup(t, "ok")
	for _, code := range []string{
		"th", "id", "ms", "ta", "km", "lo", "jv", "su",
		"vi", "my", "tl",
	} {
		body := `{"agentId":"default","text":"hi","lang":"` + code + `"}`
		rec := translatePost(t, h, body)
		assert.Equal(t, http.StatusOK, rec.Code,
			"lang %q should be accepted, got %s", code, rec.Body.String())
	}
}
