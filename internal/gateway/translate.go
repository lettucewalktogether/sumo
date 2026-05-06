package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sausheong/felix/internal/config"
	"github.com/sausheong/felix/internal/llm"
)

// TranslateLanguage is one supported language for the per-response
// translate dropdown. Code is the BCP-47-ish identifier the client
// passes back; Name is the English label rendered in the menu.
type TranslateLanguage struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// translateLanguagesOrdered is the language list shown in the chat's
// per-response Translate dropdown. Built around two intersecting
// goals: (a) mirror the kc-planning-assistant set so users carrying
// conventions across the two surfaces see the same set, and (b)
// cover every language SEA-LION ships native support for so a user
// who runs a SEA-LION model can actually translate into it. The
// SEA block (Thai, Indonesian, Malay, Tamil, Khmer, Lao, Javanese,
// Sundanese) is grouped together near the other Asian languages.
var translateLanguagesOrdered = []TranslateLanguage{
	{"en", "English"},
	{"es", "Spanish"},
	{"fr", "French"},
	{"pt", "Portuguese"},
	{"de", "German"},
	{"ru", "Russian"},
	{"uk", "Ukrainian"},
	// Southeast Asian — natively supported by SEA-LION
	{"vi", "Vietnamese"},
	{"th", "Thai"},
	{"id", "Indonesian (Bahasa)"},
	{"ms", "Malay"},
	{"tl", "Tagalog (Filipino)"},
	{"my", "Burmese"},
	{"km", "Khmer"},
	{"lo", "Lao"},
	{"ta", "Tamil"},
	{"jv", "Javanese"},
	{"su", "Sundanese"},
	// East Asian
	{"zh", "Chinese (Simplified)"},
	{"zh-TW", "Chinese (Traditional)"},
	{"ja", "Japanese"},
	{"ko", "Korean"},
	// South Asian
	{"hi", "Hindi"},
	{"ur", "Urdu"},
	{"ne", "Nepali"},
	// MENA / Africa
	{"ar", "Arabic"},
	{"sw", "Swahili"},
	{"so", "Somali"},
	{"am", "Amharic"},
}

// translateLanguagesByCode is the validation map. Built once at init
// time from the ordered list so the server-side check matches what
// the client offered.
var translateLanguagesByCode = func() map[string]string {
	m := make(map[string]string, len(translateLanguagesOrdered))
	for _, l := range translateLanguagesOrdered {
		m[l.Code] = l.Name
	}
	return m
}()

// TranslateHandlers wires up /api/translate.
type TranslateHandlers struct {
	Translate http.HandlerFunc
}

// translateMaxBodyChars caps the input we forward to the LLM so a
// malicious or accidental large paste can't run up unbounded provider
// cost. 12 KiB is generous for a single chat response while keeping
// total wall-clock bounded on a local model. Anything larger is
// truncated (the user sees the truncation marker server-side).
const translateMaxBodyChars = 12000

// translateTimeout caps how long a single translation may run end to
// end. The previous 60 s was tuned for cloud models — when the
// active agent is a local model (Ollama / SEA-LION 27B), translating
// a 10 KB response can easily run 90–180 s on consumer hardware.
// 5 minutes gives local models room while still bailing on a
// genuinely stuck provider rather than hanging the chat indefinitely.
const translateTimeout = 5 * time.Minute

// translateSystemFmt is the system prompt for translation calls. The
// %s placeholder is filled with the target language name twice — once
// in the directive, once in the closing rule. Mirrors the
// kc-planning-assistant translator system prompt with the
// citation-token rule generalised away (Felix isn't municode-specific).
const translateSystemFmt = `You are a professional translator. Translate the user's text into %s.

CRITICAL RULES:
1. Preserve ALL markdown formatting: **bold**, *italic*, bullet points, headings (#, ##, ###), numbered lists, code blocks, and inline code.
2. Preserve ALL URLs EXACTLY as-is. Do not translate any http:// or https:// links.
3. Preserve any citation-style tokens like [§88-420], [IB121], [Section 5.2] EXACTLY as-is.
4. Preserve line breaks and paragraph structure.
5. Do NOT add any preamble like "Here is the translation:" — output ONLY the translated text.
6. Phone numbers and email addresses stay unchanged.
7. Use natural, professional %s — not a word-for-word literal translation.`

// NewTranslateHandlers builds the /api/translate endpoint backed by
// the same provider map and config that the WebSocket chat path uses.
// Translation runs the active agent's configured LLM as a one-shot
// streaming call and pipes text deltas straight to the client as
// `text/plain`, matching the kc-planning-assistant contract that the
// chat UI already knows how to consume.
func NewTranslateHandlers(h *WebSocketHandler) *TranslateHandlers {
	return &TranslateHandlers{
		Translate: func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST required", http.StatusMethodNotAllowed)
				return
			}
			var body struct {
				AgentID string `json:"agentId"`
				Text    string `json:"text"`
				Lang    string `json:"lang"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			body.Text = strings.TrimSpace(body.Text)
			body.AgentID = strings.TrimSpace(body.AgentID)
			body.Lang = strings.TrimSpace(body.Lang)
			if body.Text == "" {
				http.Error(w, "text is required", http.StatusBadRequest)
				return
			}
			if len(body.Text) > translateMaxBodyChars {
				body.Text = body.Text[:translateMaxBodyChars]
			}
			langName, ok := translateLanguagesByCode[body.Lang]
			if !ok {
				http.Error(w, "unsupported language code: "+body.Lang, http.StatusBadRequest)
				return
			}
			if body.AgentID == "" {
				body.AgentID = "default"
			}

			provider, model, err := h.ResolveAgentProvider(body.AgentID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), translateTimeout)
			defer cancel()

			req := llm.ChatRequest{
				Model:     model,
				MaxTokens: 4096,
				SystemPromptParts: []llm.SystemPromptPart{
					{Text: fmt.Sprintf(translateSystemFmt, langName, langName)},
				},
				Messages: []llm.Message{
					{Role: "user", Content: body.Text},
				},
			}

			stream, err := provider.ChatStream(ctx, req)
			if err != nil {
				http.Error(w, "translate: "+err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			for evt := range stream {
				switch evt.Type {
				case llm.EventTextDelta:
					if evt.Text != "" {
						if _, werr := w.Write([]byte(evt.Text)); werr != nil {
							return
						}
						if flusher != nil {
							flusher.Flush()
						}
					}
				case llm.EventError:
					if evt.Error != nil {
						_, _ = w.Write([]byte("\n\n[translation error: " + evt.Error.Error() + "]"))
						if flusher != nil {
							flusher.Flush()
						}
					}
					return
				case llm.EventDone:
					return
				}
			}
		},
	}
}

// ResolveAgentProvider picks the configured LLM provider+bare-model
// for a given agent ID. Reads under RLock so a concurrent
// UpdateConfig / UpdateProviders cannot tear the maps. Exposed
// (capitalised) so the translate handler — and any other one-shot
// LLM endpoint added later — can share the same lookup the chat
// path uses without duplicating the resolution logic.
func (h *WebSocketHandler) ResolveAgentProvider(agentID string) (llm.LLMProvider, string, error) {
	h.mu.RLock()
	cfg := h.config
	providers := h.providers
	h.mu.RUnlock()
	if cfg == nil {
		return nil, "", fmt.Errorf("gateway: no config loaded")
	}
	var ac *config.AgentConfig
	for i := range cfg.Agents.List {
		if cfg.Agents.List[i].ID == agentID {
			ac = &cfg.Agents.List[i]
			break
		}
	}
	if ac == nil {
		return nil, "", fmt.Errorf("unknown agent: %s", agentID)
	}
	pName, model := llm.ParseProviderModel(ac.Model)
	p, ok := providers[pName]
	if !ok {
		return nil, "", fmt.Errorf("provider %q not configured for agent %s", pName, agentID)
	}
	return p, model, nil
}
