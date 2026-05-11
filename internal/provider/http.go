package provider

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PostStream issues a POST with the given JSON body, returns an SSE line reader.
// Caller is responsible for closing the returned io.Closer.
func PostStream(ctx context.Context, baseURL, path, apiKey, authHeader string, body []byte) (*bufio.Reader, io.Closer, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(baseURL, "/")+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		if authHeader == "" {
			authHeader = "Authorization"
		}
		if strings.EqualFold(authHeader, "Authorization") {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		} else {
			req.Header.Set(authHeader, apiKey)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return bufio.NewReaderSize(resp.Body, 1<<15), resp.Body, nil
}

// SSEEvent is one parsed Server-Sent Event.
type SSEEvent struct {
	Event string
	Data  string
}

// IterSSE reads SSE events from r and invokes fn for each. fn may return
// ErrStopSSE to stop early.
func IterSSE(r *bufio.Reader, fn func(SSEEvent) error) error {
	var event string
	var data strings.Builder
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if data.Len() > 0 || event != "" {
					out := SSEEvent{Event: event, Data: data.String()}
					data.Reset()
					event = ""
					if err := fn(out); err != nil {
						if err == ErrStopSSE {
							return nil
						}
						return err
					}
				}
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(line[len("event:"):])
			case strings.HasPrefix(line, "data:"):
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(strings.TrimSpace(line[len("data:"):]))
			case strings.HasPrefix(line, ":"):
				// comment, ignore
			}
		}
		if err != nil {
			if err == io.EOF {
				if data.Len() > 0 {
					fn(SSEEvent{Event: event, Data: data.String()})
				}
				return nil
			}
			return err
		}
	}
}

// ErrStopSSE is returned by the IterSSE callback to stop iteration cleanly.
var ErrStopSSE = stopSSE{}

type stopSSE struct{}

func (stopSSE) Error() string { return "sse: stop" }
