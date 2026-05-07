package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ollamaUnloadTimeout caps the unload call so a slow / stuck Ollama
// can't block a Settings save. The unload is best-effort: if it
// fails we still record the deactivation in config so the
// block-from-use safety net remains intact. The user just has to
// wait Ollama's natural keep_alive timer (default 5 min) for the
// RAM to come back.
const ollamaUnloadTimeout = 5 * time.Second

// unloadOllamaModel asks the bundled Ollama to evict a model from
// RAM/VRAM immediately. Implemented via Ollama's documented
// `keep_alive: 0` semantics on the /api/generate endpoint with an
// empty prompt — modern Ollama (v0.4+) treats this as "set
// keep_alive to 0 on any existing context for this model and
// evict on the next idle scan", which fires within milliseconds.
// If the model is not currently loaded the call is a cheap no-op
// (~50 ms round trip).
//
// Returns nil on success, an error on transport / HTTP failure.
// Callers should log but not surface failures — the model is
// already marked deactivated in config, so the block-from-use
// guarantee holds even when the unload itself fails.
func unloadOllamaModel(parentCtx context.Context, ollamaBaseURL, model string) error {
	if strings.TrimSpace(ollamaBaseURL) == "" || strings.TrimSpace(model) == "" {
		return fmt.Errorf("unloadOllamaModel: empty base URL or model")
	}

	// Strip any trailing "/v1" / "/" — the OpenAI-shim base URL we
	// use for inference is "<host>/v1"; the native Ollama API for
	// generate lives at "<host>/api/generate".
	base := strings.TrimSuffix(ollamaBaseURL, "/")
	base = strings.TrimSuffix(base, "/v1")

	body, err := json.Marshal(map[string]any{
		"model":      model,
		"prompt":     "",
		"keep_alive": 0,
		"stream":     false,
	})
	if err != nil {
		return fmt.Errorf("unloadOllamaModel: marshal: %w", err)
	}

	ctx, cancel := context.WithTimeout(parentCtx, ollamaUnloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("unloadOllamaModel: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unloadOllamaModel: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// Drain body for the log line; Ollama returns JSON errors.
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		return fmt.Errorf("unloadOllamaModel: HTTP %d: %s",
			resp.StatusCode, strings.TrimSpace(string(buf[:n])))
	}
	return nil
}

// LocalOllamaBaseURL returns the OpenAI-compatible base URL for
// the bundled Ollama provider, or empty if there is no `local`
// provider configured. Used by the Settings save flow to know
// where to send the unload call.
func (h *WebSocketHandler) LocalOllamaBaseURL() string {
	h.mu.RLock()
	cfg := h.config
	h.mu.RUnlock()
	if cfg == nil {
		return ""
	}
	if p, ok := cfg.Providers["local"]; ok {
		return p.BaseURL
	}
	return ""
}

// EvictDeactivatedFromOllama runs through every deactivated model
// in cfg.Models.Deactivated and best-effort unloads each from
// Ollama. Called by the Settings save handler after persisting the
// new deactivated list so the user-felt RAM reclaim happens
// immediately rather than waiting for Ollama's idle eviction.
//
// Failures are logged but not returned — see unloadOllamaModel
// for the rationale.
func (h *WebSocketHandler) EvictDeactivatedFromOllama(ctx context.Context) {
	base := h.LocalOllamaBaseURL()
	if base == "" {
		return // no bundled Ollama → nothing to unload
	}
	h.mu.RLock()
	cfg := h.config
	h.mu.RUnlock()
	if cfg == nil {
		return
	}
	for _, model := range cfg.Models.Deactivated {
		if err := unloadOllamaModel(ctx, base, model); err != nil {
			slog.Info("ollama unload failed (model may not be loaded; safe to ignore)",
				"model", model, "err", err)
		}
	}
}
