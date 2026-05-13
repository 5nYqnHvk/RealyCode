package capture

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type readCloser struct {
	*strings.Reader
}

func (r readCloser) Close() error { return nil }

func TestCaptureSplitsUpstreamFrames(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RELAYCODE_CAPTURE_DIR", root)
	ctx, err := Start(context.Background(), []byte(`{"model":"claude","messages":[]}`), "claude", "openai", "gpt")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	ctx, err = StartUpstream(ctx, req, []byte(`{"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	body := WrapResponse(ctx, readCloser{strings.NewReader("event: a\ndata: {}\n\nevent: b\ndata: {}\n\n")})
	if _, err := io.ReadAll(body); err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("capture dirs = %d", len(entries))
	}
	dir := filepath.Join(root, entries[0].Name())
	files, err := os.ReadDir(filepath.Join(dir, "upstream", "001-responses", "events"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name() != "0001-a.sse" || files[1].Name() != "0002-b.sse" {
		t.Fatalf("event files = %+v", files)
	}
	meta, err := os.ReadFile(filepath.Join(dir, "upstream", "001-responses", "request_meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(meta), "secret") {
		t.Fatalf("meta leaked secret: %s", meta)
	}
}
