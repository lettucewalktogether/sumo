package gateway

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSafeRawMessage verifies the WS layer's RawMessage guard.
// Empty or invalid input must become nil (marshals to null) instead
// of triggering "unexpected end of JSON input" at marshal time —
// which would abort the entire WebSocket write and leave the chat
// client's tool_call entry stuck in a pending state.
func TestSafeRawMessage(t *testing.T) {
	tests := []struct {
		name string
		in   json.RawMessage
		want any
	}{
		{"nil", nil, nil},
		{"empty", json.RawMessage{}, nil},
		{"whitespace_only_invalid", json.RawMessage(`   `), nil},
		{"truncated_object", json.RawMessage(`{"a":`), nil},
		{"plain_text_invalid", json.RawMessage(`hello world`), nil},
		{"valid_object", json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":1}`)},
		{"valid_null", json.RawMessage(`null`), json.RawMessage(`null`)},
		{"valid_array", json.RawMessage(`[1,2,3]`), json.RawMessage(`[1,2,3]`)},
		{"valid_string", json.RawMessage(`"hi"`), json.RawMessage(`"hi"`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safeRawMessage(tc.in)
			assert.Equal(t, tc.want, got)

			// And — load-bearing — the result must round-trip through
			// json.Marshal without error. That's the regression we're
			// guarding: a valid `null` is fine, valid JSON is fine,
			// invalid bytes become null, never an error.
			_, err := json.Marshal(map[string]any{"input": got})
			assert.NoError(t, err)
		})
	}
}

// TestWriteJSONIsGoroutineSafe is the regression guard for the
// "panic: concurrent write to websocket connection" crash. The
// gateway has multiple paths that write to the same WS conn from
// different goroutines (main agent-event drain loop, trace SetOnMark
// callbacks, mid-stream chat.compact responses). gorilla/websocket
// panics if two of those races into Conn.NextWriter at once. This
// test fans 200 writes across 50 goroutines through writeJSON and
// asserts no panic and that the receiving end can decode every frame.
func TestWriteJSONIsGoroutineSafe(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		defer releaseConnMutex(conn)

		const goroutines = 50
		const writesPerG = 4
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for g := 0; g < goroutines; g++ {
			g := g
			go func() {
				defer wg.Done()
				for i := 0; i < writesPerG; i++ {
					writeJSON(conn, map[string]any{
						"goroutine": g,
						"index":     i,
					})
				}
			}()
		}
		wg.Wait()
		// One sentinel so the client knows when to stop reading.
		writeJSON(conn, map[string]any{"done": true})
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	got := 0
	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read after %d messages: %v", got, err)
		}
		if done, _ := msg["done"].(bool); done {
			break
		}
		got++
	}
	const expected = 50 * 4
	assert.Equal(t, expected, got, "every write should arrive intact, none dropped")
}

// TestDecodeChatAttachments covers the chat.send attachment guard. The
// decoder is the only place malformed UI input is rejected before the
// bytes flow into the runtime, so each branch needs explicit coverage:
// the MIME allowlist, base64 validity, the per-image size cap, and the
// per-message count cap.
func TestDecodeChatAttachments(t *testing.T) {
	pngBytes := []byte("\x89PNG\r\n\x1a\nfake-png-data")
	pngB64 := base64.StdEncoding.EncodeToString(pngBytes)

	t.Run("nil_input", func(t *testing.T) {
		out, err := decodeChatAttachments(nil)
		require.NoError(t, err)
		assert.Nil(t, out)
	})

	t.Run("empty_slice", func(t *testing.T) {
		out, err := decodeChatAttachments([]chatAttachmentParam{})
		require.NoError(t, err)
		assert.Nil(t, out)
	})

	t.Run("single_valid_png", func(t *testing.T) {
		out, err := decodeChatAttachments([]chatAttachmentParam{
			{MimeType: "image/png", Data: pngB64, Name: "x.png"},
		})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, "image/png", out[0].MimeType)
		assert.Equal(t, pngBytes, out[0].Data)
	})

	t.Run("mime_normalised", func(t *testing.T) {
		out, err := decodeChatAttachments([]chatAttachmentParam{
			{MimeType: "  IMAGE/JPEG  ", Data: pngB64},
		})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, "image/jpeg", out[0].MimeType)
	})

	t.Run("multiple_mixed_mimes", func(t *testing.T) {
		out, err := decodeChatAttachments([]chatAttachmentParam{
			{MimeType: "image/png", Data: pngB64},
			{MimeType: "image/jpeg", Data: pngB64},
			{MimeType: "image/gif", Data: pngB64},
			{MimeType: "image/webp", Data: pngB64},
			{MimeType: "image/bmp", Data: pngB64},
		})
		require.NoError(t, err)
		assert.Len(t, out, 5)
	})

	t.Run("rejects_unsupported_mime", func(t *testing.T) {
		_, err := decodeChatAttachments([]chatAttachmentParam{
			{MimeType: "application/pdf", Data: pngB64},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported mime")
	})

	t.Run("rejects_unsupported_mime_at_index", func(t *testing.T) {
		// Second attachment is bad — index should appear in the error.
		_, err := decodeChatAttachments([]chatAttachmentParam{
			{MimeType: "image/png", Data: pngB64},
			{MimeType: "audio/mp3", Data: pngB64},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "attachment 1")
	})

	t.Run("rejects_invalid_base64", func(t *testing.T) {
		_, err := decodeChatAttachments([]chatAttachmentParam{
			{MimeType: "image/png", Data: "not!!!base64@@@"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "base64")
	})

	t.Run("rejects_empty_data", func(t *testing.T) {
		_, err := decodeChatAttachments([]chatAttachmentParam{
			{MimeType: "image/png", Data: ""},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("rejects_oversized_image", func(t *testing.T) {
		big := make([]byte, maxAttachmentBytes+1)
		_, err := decodeChatAttachments([]chatAttachmentParam{
			{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(big)},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too large")
	})

	t.Run("accepts_at_size_cap", func(t *testing.T) {
		// Boundary case: exactly the cap is allowed.
		atCap := make([]byte, maxAttachmentBytes)
		atCap[0] = 0xFF // non-zero so the empty-data guard doesn't fire
		out, err := decodeChatAttachments([]chatAttachmentParam{
			{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(atCap)},
		})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, maxAttachmentBytes, len(out[0].Data))
	})

	t.Run("rejects_too_many_attachments", func(t *testing.T) {
		atts := make([]chatAttachmentParam, maxAttachmentCount+1)
		for i := range atts {
			atts[i] = chatAttachmentParam{MimeType: "image/png", Data: pngB64}
		}
		_, err := decodeChatAttachments(atts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too many")
	})

	t.Run("accepts_at_count_cap", func(t *testing.T) {
		atts := make([]chatAttachmentParam, maxAttachmentCount)
		for i := range atts {
			atts[i] = chatAttachmentParam{MimeType: "image/png", Data: pngB64}
		}
		out, err := decodeChatAttachments(atts)
		require.NoError(t, err)
		assert.Len(t, out, maxAttachmentCount)
	})
}

// TestChatSendParamsJSON locks down the wire format the chat UI emits
// against the params struct. If a future field rename or tag tweak
// breaks deserialisation, the chat.send path silently drops attachments
// — this ensures the contract stays intact.
func TestChatSendParamsJSON(t *testing.T) {
	raw := []byte(`{
		"agentId": "default",
		"text": "what's this?",
		"sessionKey": "ws_default",
		"attachments": [
			{"mimeType": "image/png", "data": "AAAA", "name": "shot.png"},
			{"mimeType": "image/jpeg", "data": "////"}
		]
	}`)
	var p chatSendParams
	require.NoError(t, json.Unmarshal(raw, &p))
	assert.Equal(t, "default", p.AgentID)
	assert.Equal(t, "what's this?", p.Text)
	assert.Equal(t, "ws_default", p.SessionKey)
	require.Len(t, p.Attachments, 2)
	assert.Equal(t, "image/png", p.Attachments[0].MimeType)
	assert.Equal(t, "shot.png", p.Attachments[0].Name)
	assert.Equal(t, "AAAA", p.Attachments[0].Data)
	assert.Equal(t, "image/jpeg", p.Attachments[1].MimeType)
	assert.Equal(t, "", p.Attachments[1].Name) // omitted ⇒ empty
}
