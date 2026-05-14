package provider

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/5nYqnHvk/RelayCode/internal/config"
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
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Test"); got != "extra" {
			t.Fatalf("X-Test = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: done\ndata: {}\n\n"))
	}))
	defer server.Close()

	reader, closer, err := PostStreamWithClient(context.Background(), server.Client(), 0, server.URL, "/responses", "key", "Authorization", map[string]string{"X-Test": "extra", "X-Blank": " "}, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	line, err := reader.ReadString('\n')
	if err != nil || line != "event: done\n" {
		t.Fatalf("line=%q err=%v", line, err)
	}
}

func TestPostStreamUsesCustomAuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "key" {
			t.Fatalf("X-API-Key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer server.Close()

	_, closer, err := PostStreamWithClient(context.Background(), server.Client(), 0, server.URL, "/responses", "key", "X-API-Key", nil, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	closer.Close()
}

func TestPostStreamRetriesRetriableStatuses(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer server.Close()

	_, closer, err := PostStreamWithClient(context.Background(), server.Client(), 1, server.URL, "/responses", "", "", nil, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	closer.Close()
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestPostStreamDoesNotRetryClientError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	_, _, err := PostStreamWithClient(context.Background(), server.Client(), 2, server.URL, "/responses", "", "", nil, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "upstream 400: bad request") {
		t.Fatalf("err = %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestPostStreamRetryStopsOnContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server down", http.StatusInternalServerError)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := PostStreamWithClient(ctx, server.Client(), 1, server.URL, "/responses", "", "", nil, []byte(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestHTTPClientConfiguresTimeoutAndRejectsBadProxy(t *testing.T) {
	client, err := HTTPClient(config.ProviderConfig{HTTPTimeoutSeconds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 3*time.Second {
		t.Fatalf("timeout = %v", client.Timeout)
	}
	if _, err := HTTPClient(config.ProviderConfig{HTTPProxy: "http://[::1"}); err == nil {
		t.Fatal("expected proxy parse error")
	}
}

func TestIterSSEParsesMultilineCommentsEOFAndStop(t *testing.T) {
	input := ": ignored\nevent: update\ndata: first\ndata: second\n\ndata: tail"
	var events []SSEEvent
	err := IterSSE(bufio.NewReader(strings.NewReader(input)), func(ev SSEEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Event != "update" || events[0].Data != "first\nsecond" {
		t.Fatalf("first event = %#v", events[0])
	}
	if events[1].Data != "tail" {
		t.Fatalf("tail event = %#v", events[1])
	}

	called := 0
	err = IterSSE(bufio.NewReader(strings.NewReader("data: one\n\ndata: two\n\n")), func(ev SSEEvent) error {
		called++
		return ErrStopSSE
	})
	if err != nil || called != 1 {
		t.Fatalf("stop err=%v called=%d", err, called)
	}
}

func TestIterSSEReturnsCallbackError(t *testing.T) {
	want := errors.New("callback")
	err := IterSSE(bufio.NewReader(strings.NewReader("data: one\n\n")), func(SSEEvent) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}
