package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionAppendAndHistory(t *testing.T) {
	sess := NewSession("default", "test")

	sess.Append(UserMessageEntry("hello"))
	sess.Append(AssistantMessageEntry("hi there"))
	sess.Append(UserMessageEntry("how are you?"))

	history := sess.History()
	assert.Len(t, history, 3)

	assert.Equal(t, EntryTypeMessage, history[0].Type)
	assert.Equal(t, "user", history[0].Role)
	assert.Equal(t, "assistant", history[1].Role)
	assert.Equal(t, "user", history[2].Role)
}

func TestSessionDAGTraversal(t *testing.T) {
	sess := NewSession("default", "test")

	sess.Append(UserMessageEntry("first"))
	sess.Append(AssistantMessageEntry("second"))

	history := sess.History()
	assert.Len(t, history, 2)

	// Parent chain should be connected
	assert.Empty(t, history[0].ParentID)
	assert.Equal(t, history[0].ID, history[1].ParentID)
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create and populate a session
	sess, err := store.Load("agent1", "test_peer")
	require.NoError(t, err)

	sess.Append(UserMessageEntry("hello"))
	sess.Append(AssistantMessageEntry("world"))

	// Reload from disk
	sess2, err := store.Load("agent1", "test_peer")
	require.NoError(t, err)

	history := sess2.History()
	assert.Len(t, history, 2)

	// Check file exists
	path := filepath.Join(dir, "agent1", "test_peer.jsonl")
	assert.FileExists(t, path)
}

// TestValidateSessionKey covers the path-traversal guard added so a
// malicious WebSocket caller can't pass keys like "../../../tmp/x"
// and have the Store compose them into filesystem paths that escape
// the session directory. Every key-taking Store method calls
// ValidateSessionKey before any I/O.
func TestValidateSessionKey(t *testing.T) {
	good := []string{"a", "default", "ws_default", "ws_KCMO", "k1", "with-hyphen", "1234"}
	for _, k := range good {
		assert.NoError(t, ValidateSessionKey(k), "key %q should be accepted", k)
	}
	bad := []struct {
		key  string
		want string // substring of error message
	}{
		{"", "required"},
		{".", "cannot start with"},
		{"..", "cannot start with"},
		{"../escape", "cannot start with"},
		{".bashrc", "cannot start with"},
		{"foo/bar", "path separators"},
		{"foo\\bar", "path separators"},
		{"sub/dir/key", "path separators"},
		{"/abs", "path separators"},
		{"\\abs", "path separators"},
	}
	for _, tc := range bad {
		err := ValidateSessionKey(tc.key)
		require.Error(t, err, "key %q must be rejected", tc.key)
		assert.Contains(t, err.Error(), tc.want, "expected %q in error for %q, got %q", tc.want, tc.key, err.Error())
	}
}

// TestStorePathTraversalRejected is the integration-level guard:
// every public Store method that takes a key returns an error
// (rather than touching the filesystem) when given a traversal-y
// key. Belt-and-suspenders with TestValidateSessionKey since each
// method's path-joining could otherwise diverge silently.
func TestStorePathTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Seed a real session so the store has something to compare
	// against (some methods short-circuit on missing keys before
	// hitting the validate guard if the order's off).
	sess, err := store.Load("agent1", "real")
	require.NoError(t, err)
	sess.Append(UserMessageEntry("hi"))

	for _, key := range []string{"../escape", "..", ".", "sub/key", "foo/bar"} {
		t.Run("Load_"+key, func(t *testing.T) {
			_, err := store.Load("agent1", key)
			require.Error(t, err)
		})
		t.Run("Create_"+key, func(t *testing.T) {
			err := store.Create("agent1", key)
			require.Error(t, err)
		})
		t.Run("Delete_"+key, func(t *testing.T) {
			err := store.Delete("agent1", key)
			require.Error(t, err)
		})
		t.Run("SetName_"+key, func(t *testing.T) {
			err := store.SetName("agent1", key, "x")
			require.Error(t, err)
		})
		t.Run("SetPinned_"+key, func(t *testing.T) {
			err := store.SetPinned("agent1", key, true)
			require.Error(t, err)
		})
		t.Run("Rename_oldKey_"+key, func(t *testing.T) {
			err := store.Rename("agent1", key, "newkey")
			require.Error(t, err)
		})
		t.Run("Rename_newKey_"+key, func(t *testing.T) {
			err := store.Rename("agent1", "real", key)
			require.Error(t, err)
		})
	}

	// Final sanity: nothing leaked outside the agent dir. List the
	// parent (TempDir) — should hold only the agent1 subdir.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "agent1", entries[0].Name())
}

// TestStoreSessionMeta covers the friendly-name + pinned sidecar
// added for the chat sidebar — set, persist, list, clear, missing-
// session error paths, plus the rename and delete sidecar-follow
// behaviour.
func TestStoreSessionMeta(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Need a session file to attach metadata to.
	sess, err := store.Load("agent1", "k1")
	require.NoError(t, err)
	sess.Append(UserMessageEntry("hi"))

	t.Run("default_state_is_zero", func(t *testing.T) {
		infos, err := store.List("agent1")
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, "", infos[0].Name)
		assert.False(t, infos[0].Pinned)
	})

	t.Run("set_name_and_list_returns_it", func(t *testing.T) {
		require.NoError(t, store.SetName("agent1", "k1", "  Auth migration  "))
		infos, err := store.List("agent1")
		require.NoError(t, err)
		require.Len(t, infos, 1)
		assert.Equal(t, "Auth migration", infos[0].Name) // trimmed
	})

	t.Run("set_pinned_persists", func(t *testing.T) {
		require.NoError(t, store.SetPinned("agent1", "k1", true))
		infos, err := store.List("agent1")
		require.NoError(t, err)
		require.True(t, infos[0].Pinned)
		// Sidecar must exist on disk.
		assert.FileExists(t, filepath.Join(dir, "agent1", "k1.meta.json"))
	})

	t.Run("clear_both_removes_sidecar", func(t *testing.T) {
		require.NoError(t, store.SetName("agent1", "k1", ""))
		require.NoError(t, store.SetPinned("agent1", "k1", false))
		// File should be gone.
		_, err := os.Stat(filepath.Join(dir, "agent1", "k1.meta.json"))
		assert.True(t, os.IsNotExist(err), "expected sidecar removed when both fields zero, got %v", err)
		// And List should report defaults again.
		infos, _ := store.List("agent1")
		assert.Equal(t, "", infos[0].Name)
		assert.False(t, infos[0].Pinned)
	})

	t.Run("missing_session_yields_error", func(t *testing.T) {
		err := store.SetName("agent1", "ghost", "x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
		err = store.SetPinned("agent1", "ghost", true)
		require.Error(t, err)
	})

	t.Run("rename_moves_sidecar", func(t *testing.T) {
		require.NoError(t, store.SetName("agent1", "k1", "Friendly"))
		require.NoError(t, store.SetPinned("agent1", "k1", true))
		require.NoError(t, store.Rename("agent1", "k1", "k2"))
		// Old sidecar gone, new sidecar exists.
		_, err := os.Stat(filepath.Join(dir, "agent1", "k1.meta.json"))
		assert.True(t, os.IsNotExist(err))
		assert.FileExists(t, filepath.Join(dir, "agent1", "k2.meta.json"))
		// And the metadata travelled with the rename.
		infos, _ := store.List("agent1")
		require.Len(t, infos, 1)
		assert.Equal(t, "k2", infos[0].Key)
		assert.Equal(t, "Friendly", infos[0].Name)
		assert.True(t, infos[0].Pinned)
	})

	t.Run("delete_removes_sidecar", func(t *testing.T) {
		require.NoError(t, store.Delete("agent1", "k2"))
		_, err := os.Stat(filepath.Join(dir, "agent1", "k2.meta.json"))
		assert.True(t, os.IsNotExist(err), "sidecar should be cleaned up on Delete")
		_, err = os.Stat(filepath.Join(dir, "agent1", "k2.jsonl"))
		assert.True(t, os.IsNotExist(err))
	})
}

func TestToolCallEntries(t *testing.T) {
	sess := NewSession("default", "test")

	sess.Append(UserMessageEntry("run ls"))
	sess.Append(ToolCallEntry("tc_1", "bash", []byte(`{"command":"ls"}`)))
	sess.Append(ToolResultEntry("tc_1", "file1\nfile2", "", nil))
	sess.Append(AssistantMessageEntry("Here are the files."))

	history := sess.History()
	assert.Len(t, history, 4)
	assert.Equal(t, EntryTypeToolCall, history[1].Type)
	assert.Equal(t, EntryTypeToolResult, history[2].Type)
}

// TestToolCallEntrySanitisesEmptyInput is the regression guard for
// the "data:null" bug. When the LLM emits a tool_use whose arguments
// stream produces zero bytes (or an empty/non-nil json.RawMessage,
// or invalid JSON), the unsanitised version of ToolCallEntry hit
// json.Marshal's "unexpected end of JSON input" path, swallowed the
// error via `data, _ :=`, and persisted Data: nil. On disk: `"data":null`.
// On reload, assembleMessages would build a tool_use with an empty
// ID, and the next LLM call would fail with the Anthropic 400:
// "messages.N.content.0: unexpected `tool_use_id` ... Each
// `tool_result` block must have a corresponding `tool_use` block in
// the previous message." The fix substitutes "{}" for invalid input;
// this test asserts the entry's Data is non-nil and round-trips.
func TestToolCallEntrySanitisesEmptyInput(t *testing.T) {
	cases := []struct {
		name  string
		input json.RawMessage
	}{
		{"nil_input", nil},
		{"empty_non_nil_input", json.RawMessage{}},
		{"truncated_object", json.RawMessage(`{"a":`)},
		{"plain_text", json.RawMessage(`hello`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := ToolCallEntry("toolu_x", "search", tc.input)
			require.NotNil(t, e.Data, "Data must not be nil — that's the bug")
			require.True(t, json.Valid(e.Data), "Data must be valid JSON")

			var td ToolCallData
			require.NoError(t, json.Unmarshal(e.Data, &td))
			assert.Equal(t, "toolu_x", td.ID, "ID must round-trip")
			assert.Equal(t, "search", td.Tool, "Tool must round-trip")
			assert.True(t, json.Valid(td.Input), "Input must round-trip as valid JSON")
		})
	}
}

func TestSessionBranch(t *testing.T) {
	sess := NewSession("default", "test")

	sess.Append(UserMessageEntry("first"))
	firstID := sess.LeafID()
	sess.Append(AssistantMessageEntry("response 1"))
	sess.Append(UserMessageEntry("second"))

	// Branch back to first entry
	err := sess.Branch(firstID)
	require.NoError(t, err)

	assert.Equal(t, firstID, sess.LeafID())

	// Append on the branch
	sess.Append(AssistantMessageEntry("alternate response"))

	// History should follow the branch
	history := sess.History()
	assert.Len(t, history, 2) // first + alternate response
	assert.Equal(t, "user", history[0].Role)
	assert.Equal(t, "assistant", history[1].Role)
}

func TestSessionBranchInvalidID(t *testing.T) {
	sess := NewSession("default", "test")
	sess.Append(UserMessageEntry("hello"))

	err := sess.Branch("nonexistent")
	assert.Error(t, err)
}

func TestSessionCompact(t *testing.T) {
	sess := NewSession("default", "test")

	// Add 10 exchanges
	for i := 0; i < 10; i++ {
		sess.Append(UserMessageEntry("question " + string(rune('0'+i))))
		sess.Append(AssistantMessageEntry("answer " + string(rune('0'+i))))
	}

	history := sess.History()
	assert.Len(t, history, 20)

	// Compact, keeping last 4 entries
	sess.Compact("Summary of conversation: discussed topics 0-7", 4)

	history = sess.History()
	// Should have: 1 summary + 4 kept entries = 5
	assert.Len(t, history, 5)

	// First entry should be the summary meta entry
	assert.Equal(t, EntryTypeMeta, history[0].Type)
	assert.Equal(t, "system", history[0].Role)
}

func TestSessionCompactNoOp(t *testing.T) {
	sess := NewSession("default", "test")
	sess.Append(UserMessageEntry("hello"))
	sess.Append(AssistantMessageEntry("world"))

	// Compacting with keepEntries >= history length should be a no-op
	sess.Compact("summary", 10)

	history := sess.History()
	assert.Len(t, history, 2)
}

func TestSessionCompactWithStore(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess, err := store.Load("agent1", "compact_test")
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		sess.Append(UserMessageEntry("msg " + string(rune('0'+i))))
		sess.Append(AssistantMessageEntry("reply " + string(rune('0'+i))))
	}

	sess.Compact("Summary of conversation", 4)

	// Reload and verify
	sess2, err := store.Load("agent1", "compact_test")
	require.NoError(t, err)

	history := sess2.History()
	assert.Len(t, history, 5) // 1 summary + 4 kept
	assert.Equal(t, EntryTypeMeta, history[0].Type)
}

func TestEstimateTokens(t *testing.T) {
	sess := NewSession("default", "test")
	sess.Append(UserMessageEntry("Hello, how are you doing today?"))
	sess.Append(AssistantMessageEntry("I'm doing well, thank you for asking!"))

	tokens := sess.EstimateTokens()
	assert.Greater(t, tokens, 0)
}

func TestSessionViewWithoutCompactionMatchesHistory(t *testing.T) {
	sess := NewSession("default", "test")
	sess.Append(UserMessageEntry("hi"))
	sess.Append(AssistantMessageEntry("hello"))
	sess.Append(UserMessageEntry("hello again"))

	view := sess.View()
	hist := sess.History()
	assert.Equal(t, len(hist), len(view))
	for i := range hist {
		assert.Equal(t, hist[i].ID, view[i].ID)
	}
}

func TestSessionViewWithSingleCompaction(t *testing.T) {
	sess := NewSession("default", "test")
	sess.Append(UserMessageEntry("u1"))
	sess.Append(AssistantMessageEntry("a1"))
	sess.Append(UserMessageEntry("u2"))
	// Simulate compaction over [u1, a1, u2]: append a CompactionEntry,
	// then continue appending normal entries after it.
	sess.Append(CompactionEntry("summary of u1/a1/u2", "", "", "ollama/qwen2.5:3b-instruct", 100, 25, 3))
	sess.Append(AssistantMessageEntry("a2 after compaction"))
	sess.Append(UserMessageEntry("u3"))

	view := sess.View()
	require.Len(t, view, 3)
	assert.Equal(t, EntryTypeCompaction, view[0].Type)
	assert.Equal(t, EntryTypeMessage, view[1].Type)
	assert.Equal(t, "assistant", view[1].Role)
	assert.Equal(t, "user", view[2].Role)
}

func TestSessionViewWithMultipleCompactions(t *testing.T) {
	sess := NewSession("default", "test")
	sess.Append(UserMessageEntry("old"))
	sess.Append(CompactionEntry("first summary", "", "", "m", 0, 0, 1))
	sess.Append(UserMessageEntry("middle"))
	sess.Append(CompactionEntry("second summary", "", "", "m", 0, 0, 1))
	sess.Append(UserMessageEntry("recent"))

	view := sess.View()
	require.Len(t, view, 2)
	// Most recent compaction supersedes the first — view starts at it.
	var cd CompactionData
	require.NoError(t, json.Unmarshal(view[0].Data, &cd))
	assert.Equal(t, "second summary", cd.Summary)
	assert.Equal(t, "user", view[1].Role)
}

func TestCompactionEntryHasCorrectFields(t *testing.T) {
	e := CompactionEntry("hello summary", "start_id", "end_id", "ollama/qwen2.5:3b", 1000, 250, 12)
	assert.Equal(t, EntryTypeCompaction, e.Type)
	assert.Equal(t, "system", e.Role)
	var cd CompactionData
	require.NoError(t, json.Unmarshal(e.Data, &cd))
	assert.Equal(t, "hello summary", cd.Summary)
	assert.Equal(t, "start_id", cd.RangeStartID)
	assert.Equal(t, "end_id", cd.RangeEndID)
	assert.Equal(t, "ollama/qwen2.5:3b", cd.Model)
	assert.Equal(t, 1000, cd.TokensBefore)
	assert.Equal(t, 250, cd.TokensEstimatedAfter)
	assert.Equal(t, 12, cd.TurnsCompacted)
}

func TestToolResultData_AbortedFieldRoundTrip(t *testing.T) {
	entry := AbortedToolResultEntry("tc_abc")
	require.Equal(t, EntryTypeToolResult, entry.Type)

	var data ToolResultData
	require.NoError(t, json.Unmarshal(entry.Data, &data))

	require.Equal(t, "tc_abc", data.ToolCallID)
	require.Equal(t, "aborted by user", data.Error)
	require.True(t, data.IsError)
	require.True(t, data.Aborted)
	require.Empty(t, data.Output)
}

func TestToolResultData_OldJSONLWithoutAbortedField(t *testing.T) {
	// Simulate an old session entry written before the Aborted field existed.
	oldJSON := []byte(`{"tool_call_id":"tc_old","output":"hello","is_error":false}`)
	var data ToolResultData
	require.NoError(t, json.Unmarshal(oldJSON, &data))

	require.Equal(t, "tc_old", data.ToolCallID)
	require.Equal(t, "hello", data.Output)
	require.False(t, data.IsError)
	require.False(t, data.Aborted, "missing field must default to false")
}

func TestSession_AppendConcurrent(t *testing.T) {
	// Race-detector test: 100 goroutines each Append a uniquely-IDed entry.
	// Run with `go test -race` to catch any unguarded mutation.
	sess := NewSession("a", "k")

	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			sess.Append(SessionEntry{
				ID:   fmt.Sprintf("e_%d", i),
				Type: EntryTypeMessage,
			})
		}()
	}
	wg.Wait()

	view := sess.View()
	require.Len(t, view, N, "every Append must land")

	seen := map[string]bool{}
	for _, e := range view {
		require.False(t, seen[e.ID], "duplicate ID %s", e.ID)
		seen[e.ID] = true
	}
	require.Len(t, seen, N)
}

func TestSession_ViewReturnsCopy(t *testing.T) {
	// Mutating the returned slice must not affect subsequent View() calls.
	sess := NewSession("a", "k")
	sess.Append(SessionEntry{ID: "e_1", Type: EntryTypeMessage})

	v1 := sess.View()
	require.Len(t, v1, 1)
	v1[0] = SessionEntry{ID: "MUTATED"}

	v2 := sess.View()
	require.Len(t, v2, 1)
	require.Equal(t, "e_1", v2[0].ID, "internal state must not be mutated by caller's slice modification")
}
