package capture

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

type contextKey struct{}
type upstreamKey struct{}

type Session struct {
	Dir         string
	upstreamSeq atomic.Uint64
}

type Upstream struct {
	Dir string
}

type Stream struct {
	Dir        string
	Subdir     string
	raw        io.ReadCloser
	buf        bytes.Buffer
	eventIndex int
}

type ResponseWriter struct {
	http.ResponseWriter
	stream Stream
}

var counter atomic.Uint64

func Enabled() bool {
	return strings.TrimSpace(os.Getenv("RELAYCODE_CAPTURE_DIR")) != ""
}

func Start(ctx context.Context, incoming []byte, incomingModel, providerName, upstreamModel string) (context.Context, error) {
	root := strings.TrimSpace(os.Getenv("RELAYCODE_CAPTURE_DIR"))
	if root == "" {
		return ctx, nil
	}
	name := fmt.Sprintf("%s-%06d-%s-%s", time.Now().UTC().Format("20060102T150405.000000000Z"), counter.Add(1), safeName(providerName), safeName(upstreamModel))
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "downstream_events"), 0o700); err != nil {
		return ctx, err
	}
	meta := map[string]any{
		"captured_at":    time.Now().UTC().Format(time.RFC3339Nano),
		"incoming_model": incomingModel,
		"provider":       providerName,
		"upstream_model": upstreamModel,
	}
	if err := writeJSON(filepath.Join(dir, "meta.json"), meta); err != nil {
		return ctx, err
	}
	if len(incoming) > 0 {
		if err := writePrettyJSON(filepath.Join(dir, "incoming_anthropic.json"), incoming); err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, contextKey{}, &Session{Dir: dir}), nil
}

func FromContext(ctx context.Context) *Session {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(contextKey{}).(*Session)
	return s
}

func UpstreamDir(ctx context.Context) string {
	s, _ := ctx.Value(upstreamKey{}).(*Upstream)
	if s == nil {
		return ""
	}
	return s.Dir
}

func StartUpstream(ctx context.Context, req *http.Request, body []byte) (context.Context, error) {
	s := FromContext(ctx)
	if s == nil || s.Dir == "" {
		return ctx, nil
	}
	seq := s.upstreamSeq.Add(1)
	dir := filepath.Join(s.Dir, "upstream", fmt.Sprintf("%03d-%s", seq, safeName(req.URL.Path)))
	if err := os.MkdirAll(filepath.Join(dir, "events"), 0o700); err != nil {
		return ctx, err
	}
	if err := writePrettyJSON(filepath.Join(dir, "request.json"), body); err != nil {
		return ctx, err
	}
	if err := writeJSON(filepath.Join(dir, "request_meta.json"), map[string]any{
		"method":   req.Method,
		"url_path": req.URL.Path,
		"headers":  safeHeaders(req.Header),
	}); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, upstreamKey{}, &Upstream{Dir: dir}), nil
}

func WrapResponse(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	dir := UpstreamDir(ctx)
	if dir == "" || body == nil {
		return body
	}
	return &Stream{Dir: dir, Subdir: "events", raw: body}
}

func WrapDownstream(ctx context.Context, w http.ResponseWriter) http.ResponseWriter {
	s := FromContext(ctx)
	if s == nil || s.Dir == "" || w == nil {
		return w
	}
	return &ResponseWriter{ResponseWriter: w, stream: Stream{Dir: s.Dir, Subdir: "downstream_events"}}
}

func (s *Stream) Read(p []byte) (int, error) {
	n, err := s.raw.Read(p)
	if n > 0 {
		s.capture(p[:n])
	}
	if err == io.EOF {
		s.flush()
	}
	return n, err
}

func (s *Stream) Close() error {
	s.flush()
	return s.raw.Close()
}

func (w *ResponseWriter) Write(p []byte) (int, error) {
	w.stream.capture(p)
	return w.ResponseWriter.Write(p)
}

func (w *ResponseWriter) Flush() {
	w.stream.flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Stream) capture(chunk []byte) {
	_, _ = s.buf.Write(chunk)
	for {
		data := s.buf.Bytes()
		idx := bytes.Index(data, []byte("\n\n"))
		sepLen := 2
		if idx < 0 {
			idx = bytes.Index(data, []byte("\r\n\r\n"))
			sepLen = 4
		}
		if idx < 0 {
			return
		}
		frame := append([]byte(nil), data[:idx+sepLen]...)
		s.buf.Next(idx + sepLen)
		s.writeFrame(frame)
	}
}

func (s *Stream) flush() {
	if s.buf.Len() == 0 {
		return
	}
	frame := append([]byte(nil), s.buf.Bytes()...)
	s.buf.Reset()
	s.writeFrame(frame)
}

func (s *Stream) writeFrame(frame []byte) {
	if len(bytes.TrimSpace(frame)) == 0 {
		return
	}
	s.eventIndex++
	subdir := s.Subdir
	if subdir == "" {
		subdir = "upstream_events"
	}
	name := fmt.Sprintf("%04d-%s.sse", s.eventIndex, eventName(frame))
	_ = os.WriteFile(filepath.Join(s.Dir, subdir, name), frame, 0o600)
}

func safeHeaders(headers http.Header) map[string][]string {
	out := map[string][]string{}
	for name, values := range headers {
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "x-api-key" || lower == "api-key" || strings.Contains(lower, "token") || strings.Contains(lower, "account") {
			out[name] = []string{"[REDACTED]"}
			continue
		}
		out[name] = append([]string(nil), values...)
	}
	return out
}

func writePrettyJSON(path string, raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return os.WriteFile(path, raw, 0o600)
	}
	return writeJSON(path, v)
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func eventName(frame []byte) string {
	name := "data"
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("event:")) {
			name = strings.TrimSpace(string(bytes.TrimPrefix(line, []byte("event:"))))
			break
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			data := strings.TrimSpace(string(bytes.TrimPrefix(line, []byte("data:"))))
			if data == "[DONE]" {
				name = "done"
			}
		}
	}
	return safeName(name)
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		var buf [4]byte
		_, _ = rand.Read(buf[:])
		return hex.EncodeToString(buf[:])
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}
