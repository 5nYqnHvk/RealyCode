package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/5nYqnHvk/RelayCode/internal/anthropic"
)

func TestFileStoreRoundTripSupportsLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store := NewStore(time.Hour, 10)
	req := &anthropic.Request{
		Messages: []anthropic.Message{{Role: "user", Content: anthropic.Content{Raw: "first"}}},
		Tools:    []anthropic.Tool{{Name: "Read", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}
	lookup, err := store.Prepare("openai", "gpt", req)
	if err != nil {
		t.Fatal(err)
	}
	store.Commit(lookup, "openai", "gpt", 1, "resp_1", 10, 2, map[string]string{"call_1": "custom"})
	store.Stats.Hits.Add(3)
	if err := store.SaveFile(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := NewFileStore(path, time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Stats.Hits.Load() != 3 {
		t.Fatalf("hits = %d", loaded.Stats.Hits.Load())
	}
	if got := loaded.Snapshot()[lookup.NewKey].CallKinds["call_1"]; got != "custom" {
		t.Fatalf("call kind = %q", got)
	}
	next := &anthropic.Request{Messages: []anthropic.Message{
		{Role: "user", Content: anthropic.Content{Raw: "first"}},
		{Role: "assistant", Content: anthropic.Content{Raw: "reply"}},
		{Role: "user", Content: anthropic.Content{Raw: "second"}},
	}, Tools: req.Tools}
	got, err := loaded.Prepare("openai", "gpt", next)
	if err != nil {
		t.Fatal(err)
	}
	if got.Chain == nil || got.Chain.ResponseID != "resp_1" || len(got.Tail) != 1 {
		t.Fatalf("lookup after load = %+v", got)
	}
}

func TestFileStorePrunesExpiredEntriesOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	snap := diskSnapshot{
		Version: 1,
		Entries: []diskEntry{{
			Key: "k", Provider: "p", UpstreamModel: "m", ResponseID: "resp", LastUsed: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano), LastMessage: diskMessage{Role: "user", Content: json.RawMessage(`"hi"`)},
		}},
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewFileStore(path, time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Snapshot()) != 0 {
		t.Fatalf("expired entries loaded: %+v", loaded.Snapshot())
	}
}
