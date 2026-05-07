package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnloadOllamaModel_PostsKeepAliveZero verifies the unload helper
// hits Ollama's native /api/generate endpoint with the documented
// keep_alive=0 + empty prompt payload that triggers immediate
// eviction (no inference). Catches a regression where the helper
// might silently use the wrong path or forget to set keep_alive.
func TestUnloadOllamaModel_PostsKeepAliveZero(t *testing.T) {
	var seenPath, seenMethod string
	var seenBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &seenBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"done":true}`))
	}))
	defer srv.Close()

	err := unloadOllamaModel(context.Background(), srv.URL+"/v1",
		"aisingapore/Llama-SEA-LION-v3.5-8B-R")
	require.NoError(t, err)
	assert.Equal(t, "/api/generate", seenPath,
		"unload must hit native /api/generate, not the OpenAI shim")
	assert.Equal(t, http.MethodPost, seenMethod)
	assert.Equal(t, "aisingapore/Llama-SEA-LION-v3.5-8B-R", seenBody["model"])
	assert.Equal(t, "", seenBody["prompt"])
	// JSON unmarshal turns numeric 0 into float64.
	assert.Equal(t, float64(0), seenBody["keep_alive"],
		"keep_alive must be 0 — anything else means we'll keep the model loaded")
	assert.Equal(t, false, seenBody["stream"])
}

// TestUnloadOllamaModel_StripsTrailingV1 confirms the helper turns the
// OpenAI-shim base URL ("<host>/v1") into the native API base
// ("<host>") before appending /api/generate. The OpenAI shim has no
// /api routes, so without the trim the unload would 404.
func TestUnloadOllamaModel_StripsTrailingV1(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		assert.Equal(t, "/api/generate", r.URL.Path,
			"helper should not double-prefix /v1/api/generate")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	for _, base := range []string{
		srv.URL,         // bare host
		srv.URL + "/",   // trailing slash
		srv.URL + "/v1", // OpenAI-shim base
		srv.URL + "/v1/",
	} {
		err := unloadOllamaModel(context.Background(), base, "x:latest")
		require.NoError(t, err, "base=%s", base)
	}
	assert.Equal(t, int32(4), atomic.LoadInt32(&hits))
}

// TestUnloadOllamaModel_RejectsEmptyInputs guards the obvious bad
// inputs so we don't fire a malformed request that would surface
// as a confusing log line during a Settings save.
func TestUnloadOllamaModel_RejectsEmptyInputs(t *testing.T) {
	assert.Error(t, unloadOllamaModel(context.Background(), "", "model"))
	assert.Error(t, unloadOllamaModel(context.Background(), "http://x", ""))
	assert.Error(t, unloadOllamaModel(context.Background(), "  ", "  "))
}

// TestUnloadOllamaModel_PropagatesHTTPError surfaces non-2xx Ollama
// responses as errors so the Settings save path can log them. The
// caller still keeps going (the block-from-use safety net is
// independent), but the operator gets a useful diagnostic.
func TestUnloadOllamaModel_PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()

	err := unloadOllamaModel(context.Background(), srv.URL, "x:latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
	assert.Contains(t, err.Error(), "model not found")
}
