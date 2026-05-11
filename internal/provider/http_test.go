package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostStreamRejectsNonSSESuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":"not an event stream"}`))
	}))
	defer server.Close()

	_, _, err := PostStreamWithClient(context.Background(), server.Client(), 0, server.URL, "/responses", "key", "Authorization", nil, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "non-SSE response") || !strings.Contains(err.Error(), "not an event stream") {
		t.Fatalf("err = %v", err)
	}
}

func TestPostStreamAcceptsSSESuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: done\ndata: {}\n\n"))
	}))
	defer server.Close()

	reader, closer, err := PostStreamWithClient(context.Background(), server.Client(), 0, server.URL, "/responses", "key", "Authorization", nil, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	line, err := reader.ReadString('\n')
	if err != nil || line != "event: done\n" {
		t.Fatalf("line=%q err=%v", line, err)
	}
}
