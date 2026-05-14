package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/5nYqnHvk/RelayCode/internal/anthropic"
)

type diskSnapshot struct {
	Version int         `json:"version"`
	SavedAt string      `json:"saved_at"`
	Entries []diskEntry `json:"entries"`
	Stats   diskStats   `json:"stats"`
}

type diskEntry struct {
	Key              string            `json:"key"`
	ParentKey        string            `json:"parent_key"`
	Provider         string            `json:"provider"`
	UpstreamModel    string            `json:"upstream_model"`
	SessionID        string            `json:"session_id"`
	ToolsHash        string            `json:"tools_hash"`
	InstructionsHash string            `json:"instructions_hash"`
	MessageCount     int               `json:"message_count"`
	LastMessage      diskMessage       `json:"last_message"`
	ResponseID       string            `json:"response_id"`
	CallKinds        map[string]string `json:"call_kinds,omitempty"`
	LastUsed         string            `json:"last_used"`
	OutputTokens     int               `json:"output_tokens"`
	InputTokens      int               `json:"input_tokens"`
}

type diskMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type diskStats struct {
	Hits           int64 `json:"hits"`
	Misses         int64 `json:"misses"`
	ForcedReplays  int64 `json:"forced_replays"`
	ExpiredInvalid int64 `json:"expired_invalid"`
	InputTokens    int64 `json:"input_tokens"`
	OutputTokens   int64 `json:"output_tokens"`
}

func LoadFile(path string, ttl time.Duration, max int) (*Store, error) {
	store := NewStore(ttl, max)
	if path == "" {
		return store, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	var snap diskSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	store.Stats.Hits.Store(snap.Stats.Hits)
	store.Stats.Misses.Store(snap.Stats.Misses)
	store.Stats.ForcedReplays.Store(snap.Stats.ForcedReplays)
	store.Stats.ExpiredInvalid.Store(snap.Stats.ExpiredInvalid)
	store.Stats.InputTokens.Store(snap.Stats.InputTokens)
	store.Stats.OutputTokens.Store(snap.Stats.OutputTokens)
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, item := range snap.Entries {
		lastUsed, err := time.Parse(time.RFC3339Nano, item.LastUsed)
		if err != nil || item.Key == "" || item.ResponseID == "" {
			continue
		}
		var content anthropic.Content
		if len(item.LastMessage.Content) > 0 {
			if err := json.Unmarshal(item.LastMessage.Content, &content); err != nil {
				continue
			}
		}
		store.entries[item.Key] = &Entry{
			Key:              item.Key,
			ParentKey:        item.ParentKey,
			Provider:         item.Provider,
			UpstreamModel:    item.UpstreamModel,
			SessionID:        item.SessionID,
			ToolsHash:        item.ToolsHash,
			InstructionsHash: item.InstructionsHash,
			MessageCount:     item.MessageCount,
			LastMessage:      anthropic.Message{Role: item.LastMessage.Role, Content: content},
			ResponseID:       item.ResponseID,
			CallKinds:        copyStringMap(item.CallKinds),
			LastUsed:         lastUsed,
			OutputTokens:     item.OutputTokens,
			InputTokens:      item.InputTokens,
		}
	}
	store.pruneLocked()
	for len(store.entries) > store.max {
		store.evictLRULocked()
	}
	return store, nil
}

func (s *Store) SaveFile(path string) error {
	if s == nil || path == "" {
		return nil
	}
	s.mu.Lock()
	s.pruneLocked()
	entries := make([]diskEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		content, err := json.Marshal(entry.LastMessage.Content)
		if err != nil {
			continue
		}
		entries = append(entries, diskEntry{
			Key:              entry.Key,
			ParentKey:        entry.ParentKey,
			Provider:         entry.Provider,
			UpstreamModel:    entry.UpstreamModel,
			SessionID:        entry.SessionID,
			ToolsHash:        entry.ToolsHash,
			InstructionsHash: entry.InstructionsHash,
			MessageCount:     entry.MessageCount,
			LastMessage:      diskMessage{Role: entry.LastMessage.Role, Content: content},
			ResponseID:       entry.ResponseID,
			CallKinds:        copyStringMap(entry.CallKinds),
			LastUsed:         entry.LastUsed.UTC().Format(time.RFC3339Nano),
			OutputTokens:     entry.OutputTokens,
			InputTokens:      entry.InputTokens,
		})
	}
	s.mu.Unlock()

	snap := diskSnapshot{
		Version: 1,
		SavedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Entries: entries,
		Stats: diskStats{
			Hits:           s.Stats.Hits.Load(),
			Misses:         s.Stats.Misses.Load(),
			ForcedReplays:  s.Stats.ForcedReplays.Load(),
			ExpiredInvalid: s.Stats.ExpiredInvalid.Load(),
			InputTokens:    s.Stats.InputTokens.Load(),
			OutputTokens:   s.Stats.OutputTokens.Load(),
		},
	}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
