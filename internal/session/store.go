package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SessionInfo describes a session without loading its full contents.
type SessionInfo struct {
	Key string `json:"key"`
	// Name is a human-readable label for the session set via the chat
	// UI's rename action. Empty when no friendly name has been chosen —
	// callers fall back to Key for display in that case.
	Name         string    `json:"name,omitempty"`
	Pinned       bool      `json:"pinned,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	LastActivity time.Time `json:"lastActivity"`
	EntryCount   int       `json:"entryCount"`
}

// sessionMeta is the on-disk shape of the per-session metadata sidecar
// (<key>.meta.json next to <key>.jsonl). Stored as a separate file
// rather than embedded in the JSONL header so the existing append-only
// session writer doesn't need to be aware of the metadata model.
type sessionMeta struct {
	Name   string `json:"name,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
}

// Store handles JSONL file I/O for sessions.
type Store struct {
	baseDir string
	mu      sync.Mutex
}

// NewStore creates a new session store.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// sessionDir returns the directory for a given agent's sessions.
func (s *Store) sessionDir(agentID string) string {
	return filepath.Join(s.baseDir, agentID)
}

// sessionPath returns the file path for a session.
func (s *Store) sessionPath(agentID, key string) string {
	return filepath.Join(s.sessionDir(agentID), key+".jsonl")
}

// metaPath returns the per-session metadata sidecar path. The sidecar
// holds the friendly name and pinned flag, both managed via SetName /
// SetPinned. The file is optional — sessions that have never been
// renamed or pinned simply have no sidecar.
func (s *Store) metaPath(agentID, key string) string {
	return filepath.Join(s.sessionDir(agentID), key+".meta.json")
}

// loadMeta reads the sidecar for a session, returning the zero value
// for missing/invalid files. Errors other than not-found are logged
// but not surfaced — UI display falls back to the bare key, which is
// the right behaviour for a corrupted sidecar (don't block the chat
// because a label is unreadable).
func (s *Store) loadMeta(agentID, key string) sessionMeta {
	var m sessionMeta
	data, err := os.ReadFile(s.metaPath(agentID, key))
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("session meta unreadable", "agent", agentID, "key", key, "error", err)
		}
		return m
	}
	if err := json.Unmarshal(data, &m); err != nil {
		slog.Warn("session meta malformed", "agent", agentID, "key", key, "error", err)
		return sessionMeta{}
	}
	return m
}

// saveMeta writes the sidecar atomically (write to .tmp, rename) so a
// crash mid-write can't leave a half-written JSON. If both Name and
// Pinned are zero, deletes the sidecar — the on-disk default state is
// "no sidecar means no overrides".
func (s *Store) saveMeta(agentID, key string, m sessionMeta) error {
	dir := s.sessionDir(agentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	path := s.metaPath(agentID, key)
	if m.Name == "" && !m.Pinned {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove empty session meta: %w", err)
		}
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal session meta: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write session meta: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("commit session meta: %w", err)
	}
	return nil
}

// SetName sets the human-readable display name for a session. Pass an
// empty string to clear the name (the UI will fall back to the key).
// The session must exist; otherwise this returns an error so a stale
// UI can't seed metadata for a key that's already been deleted.
func (s *Store) SetName(agentID, key, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.existsLocked(agentID, key) {
		return fmt.Errorf("session %q does not exist", key)
	}
	m := s.loadMeta(agentID, key)
	m.Name = strings.TrimSpace(name)
	return s.saveMeta(agentID, key, m)
}

// SetPinned flips the pinned flag for a session. Pinned sessions
// surface above the time-bucketed list in the chat sidebar.
func (s *Store) SetPinned(agentID, key string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.existsLocked(agentID, key) {
		return fmt.Errorf("session %q does not exist", key)
	}
	m := s.loadMeta(agentID, key)
	m.Pinned = pinned
	return s.saveMeta(agentID, key, m)
}

// existsLocked is the lock-already-held variant of Exists, for use
// from the metadata setters which already hold s.mu.
func (s *Store) existsLocked(agentID, key string) bool {
	_, err := os.Stat(s.sessionPath(agentID, key))
	return err == nil
}

// Load reads a session from its JSONL file.
func (s *Store) Load(agentID, key string) (*Session, error) {
	path := s.sessionPath(agentID, key)

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			sess := NewSession(agentID, key)
			sess.SetStore(s)
			return sess, nil
		}
		return nil, fmt.Errorf("open session file: %w", err)
	}
	defer f.Close()

	sess := NewSession(agentID, key)
	sess.SetStore(s)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry SessionEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			slog.Warn("skipping malformed session entry", "error", err)
			continue
		}

		// Add to session without re-persisting
		sess.entries = append(sess.entries, entry)
		sess.entryMap[entry.ID] = &sess.entries[len(sess.entries)-1]
		sess.leafID = entry.ID
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read session file: %w", err)
	}

	return sess, nil
}

// AppendEntry writes a single entry to the session's JSONL file.
func (s *Store) AppendEntry(sess *Session, entry SessionEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.sessionDir(sess.AgentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("failed to create session dir", "error", err)
		return
	}

	path := s.sessionPath(sess.AgentID, sess.Key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		slog.Error("failed to open session file", "error", err)
		return
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		slog.Error("failed to marshal session entry", "error", err)
		return
	}

	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		slog.Error("failed to write session entry", "error", err)
	}
}

// Create creates an empty session file on disk so it shows up in List.
func (s *Store) Create(agentID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.sessionDir(agentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	path := s.sessionPath(agentID, key)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	return f.Close()
}

// List returns metadata for all sessions belonging to the given agent.
func (s *Store) List(agentID string) ([]SessionInfo, error) {
	dir := s.sessionDir(agentID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session dir: %w", err)
	}

	var sessions []SessionInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		key := strings.TrimSuffix(entry.Name(), ".jsonl")
		path := filepath.Join(dir, entry.Name())

		info := SessionInfo{Key: key}
		// Layer the friendly-name + pinned sidecar over the bare key.
		// loadMeta returns zero for sessions that have never been
		// renamed or pinned, leaving info.Name / info.Pinned at their
		// natural defaults.
		if m := s.loadMeta(agentID, key); m.Name != "" || m.Pinned {
			info.Name = m.Name
			info.Pinned = m.Pinned
		}

		// Count lines and extract timestamps from first/last entries
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
		var firstTS, lastTS int64
		lineCount := 0
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			lineCount++
			var partial struct {
				Timestamp int64 `json:"timestamp"`
			}
			if json.Unmarshal(line, &partial) == nil && partial.Timestamp > 0 {
				if firstTS == 0 {
					firstTS = partial.Timestamp
				}
				lastTS = partial.Timestamp
			}
		}
		f.Close()

		info.EntryCount = lineCount
		if firstTS > 0 {
			info.CreatedAt = time.Unix(firstTS, 0)
		} else {
			// Fall back to file modification time
			if fi, err := entry.Info(); err == nil {
				info.CreatedAt = fi.ModTime()
			}
		}
		if lastTS > 0 {
			info.LastActivity = time.Unix(lastTS, 0)
		} else {
			info.LastActivity = info.CreatedAt
		}

		sessions = append(sessions, info)
	}

	return sessions, nil
}

// Exists checks whether a session file exists for the given agent and key.
func (s *Store) Exists(agentID, key string) bool {
	path := s.sessionPath(agentID, key)
	_, err := os.Stat(path)
	return err == nil
}

// Rename renames a session file from oldKey to newKey, moving the
// metadata sidecar alongside if one exists. This is the rename-the-
// underlying-key operation used by the CLI; in the chat UI, friendly
// name changes are handled by SetName, which leaves the key alone.
func (s *Store) Rename(agentID, oldKey, newKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldPath := s.sessionPath(agentID, oldKey)
	newPath := s.sessionPath(agentID, newKey)

	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return fmt.Errorf("session %q does not exist", oldKey)
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("session %q already exists", newKey)
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	// Best-effort sidecar follow. A failure here doesn't undo the
	// jsonl rename — the metadata is recoverable (it's just a label
	// and a flag), the conversation content isn't.
	oldMeta := s.metaPath(agentID, oldKey)
	if _, err := os.Stat(oldMeta); err == nil {
		newMeta := s.metaPath(agentID, newKey)
		if err := os.Rename(oldMeta, newMeta); err != nil {
			slog.Warn("session meta sidecar rename failed", "agent", agentID, "old", oldKey, "new", newKey, "error", err)
		}
	}
	return nil
}

// Delete removes a session's JSONL file and any metadata sidecar.
func (s *Store) Delete(agentID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.sessionPath(agentID, key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session file: %w", err)
	}
	// Sidecar removal is best-effort: a leftover .meta.json with no
	// matching .jsonl is harmless (List skips it because it's not a
	// .jsonl), but cleaning up keeps the directory tidy.
	if err := os.Remove(s.metaPath(agentID, key)); err != nil && !os.IsNotExist(err) {
		slog.Warn("session meta sidecar remove failed", "agent", agentID, "key", key, "error", err)
	}
	return nil
}

// Rewrite replaces the entire session JSONL file with the current entries.
// Used after compaction to replace the old file.
func (s *Store) Rewrite(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.sessionDir(sess.AgentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("failed to create session dir", "error", err)
		return
	}

	path := s.sessionPath(sess.AgentID, sess.Key)

	f, err := os.Create(path)
	if err != nil {
		slog.Error("failed to create session file for rewrite", "error", err)
		return
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, entry := range sess.Entries() {
		data, err := json.Marshal(entry)
		if err != nil {
			slog.Error("failed to marshal session entry", "error", err)
			continue
		}
		w.Write(data)
		w.WriteByte('\n')
	}

	if err := w.Flush(); err != nil {
		slog.Error("failed to flush session file", "error", err)
	}
}
