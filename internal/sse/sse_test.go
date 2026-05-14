package sse

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriterEmitsHeadersAndEvents(t *testing.T) {
	rw := httptest.NewRecorder()
	w := NewWriter(rw)
	w.Event("ping", map[string]any{"ok": true})

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d", rw.Code)
	}
	if got := rw.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	out := rw.Body.String()
	for _, want := range []string{"event: ping\n", `data: {"ok":true}`, "\n\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestWriterStoresWriteError(t *testing.T) {
	w := &Writer{w: errResponseWriter{err: errors.New("boom")}}
	if err := w.WriteRaw("data: x\n\n"); err == nil || err.Error() != "boom" {
		t.Fatalf("WriteRaw err = %v", err)
	}
	if err := w.Err(); err == nil || err.Error() != "boom" {
		t.Fatalf("stored err = %v", err)
	}
}

func TestBuilderLifecycleClosesBlocksInOrder(t *testing.T) {
	rw := httptest.NewRecorder()
	b := NewBuilder(NewWriter(rw), "msg_1", "claude", 12)

	b.Start()
	b.EmitText("hello")
	b.EmitThinking("thinking")
	b.StartTool("tool_1", "bash")
	b.EmitToolInput("tool_1", `{"cmd":"go test"}`)
	b.StopTool("tool_1")
	b.EmitServerToolResult("web_search_tool_result", "srv_1", []map[string]string{{"title": "result"}})
	b.AddServerToolUse("web_search_requests", 2)
	b.SetStopReason("tool_use")
	b.SetOutputTokens(9)
	b.AddInputTokens(3)
	b.Finish()
	b.Finish()

	out := rw.Body.String()
	for _, want := range []string{`"id":"tool_1"`, `"name":"bash"`, `"tool_use_id":"srv_1"`, `"server_tool_use":{"web_search_requests":2}`, `"output_tokens":9`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	assertOrdered(t, out,
		"event: message_start",
		`"id":"msg_1"`,
		`"input_tokens":12`,
		`"type":"text"`,
		`"text":"hello"`,
		`"index":0`,
		`"type":"thinking"`,
		`"thinking":"thinking"`,
		`"index":1`,
		`"type":"tool_use"`,
		`"partial_json":"{\"cmd\":\"go test\"}"`,
		`"type":"web_search_tool_result"`,
		`"stop_reason":"tool_use"`,
		"event: message_stop",
	)
	if strings.Count(out, "event: message_stop") != 1 {
		t.Fatalf("message_stop count = %d\n%s", strings.Count(out, "event: message_stop"), out)
	}
}

func TestBuilderFinishWithErrorBeforeStartEmitsErrorEvent(t *testing.T) {
	rw := httptest.NewRecorder()
	b := NewBuilder(NewWriter(rw), "msg", "claude", 0)
	b.FinishWithError("upstream down")

	out := rw.Body.String()
	for _, want := range []string{"event: error", `"type":"api_error"`, `"message":"upstream down"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	if !b.Finished() {
		t.Fatal("builder not marked finished")
	}
}

func TestBuilderFinishWithErrorAfterStartProducesValidMessage(t *testing.T) {
	rw := httptest.NewRecorder()
	b := NewBuilder(NewWriter(rw), "msg", "claude", 0)
	b.Start()
	b.EmitText("partial")
	b.FinishWithError("upstream down")

	out := rw.Body.String()
	assertOrdered(t, out,
		"event: message_start",
		`"text":"partial"`,
		`"text":"upstream down"`,
		`"stop_reason":"end_turn"`,
		"event: message_stop",
	)
	if !b.Finished() {
		t.Fatal("builder not marked finished")
	}
}

func TestEstimateOutputTokens(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcdefgh", 2},
	} {
		if got := EstimateOutputTokens(tc.text); got != tc.want {
			t.Fatalf("EstimateOutputTokens(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func assertOrdered(t *testing.T, s string, parts ...string) {
	t.Helper()
	pos := 0
	for _, part := range parts {
		idx := strings.Index(s[pos:], part)
		if idx < 0 {
			t.Fatalf("missing %q after byte %d in:\n%s", part, pos, s)
		}
		pos += idx + len(part)
	}
}

type errResponseWriter struct{ err error }

func (w errResponseWriter) Header() http.Header       { return http.Header{} }
func (w errResponseWriter) Write([]byte) (int, error) { return 0, w.err }
func (w errResponseWriter) WriteHeader(int)           {}
