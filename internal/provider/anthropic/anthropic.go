package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	anthropictypes "github.com/5nYqnHvk/RelayCode/internal/anthropic"
	"github.com/5nYqnHvk/RelayCode/internal/config"
	"github.com/5nYqnHvk/RelayCode/internal/provider"
	"github.com/5nYqnHvk/RelayCode/internal/sse"
)

type Adapter struct {
	pc     config.ProviderConfig
	client *http.Client
	sem    chan struct{}
}

func New(pc config.ProviderConfig) (provider.Adapter, error) {
	if pc.APIKey == "" {
		return nil, fmt.Errorf("anthropic_messages: api_key is empty")
	}
	client, err := provider.HTTPClient(pc)
	if err != nil {
		return nil, err
	}
	var sem chan struct{}
	if pc.MaxConcurrency > 0 {
		sem = make(chan struct{}, pc.MaxConcurrency)
	}
	return &Adapter{pc: pc, client: client, sem: sem}, nil
}

func (a *Adapter) Stream(ctx context.Context, req *anthropictypes.Request, upstreamModel string, b *sse.Builder) error {
	if a.sem != nil {
		select {
		case a.sem <- struct{}{}:
			defer func() { <-a.sem }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	normalizedReq := *req
	normalizedReq.Messages = anthropictypes.NormalizeMessagesForUpstream(req.Messages, true, req.HasToolSearchBeta())
	body := cloneRequest(&normalizedReq)
	body["model"] = upstreamModel
	body["stream"] = true
	if _, ok := body["max_tokens"]; !ok || body["max_tokens"] == 0 {
		body["max_tokens"] = 4096
	}
	raw, _ := json.Marshal(body)

	reader, closer, err := a.postStream(ctx, raw, req.Betas)
	if err != nil {
		b.Start()
		b.FinishWithError(err.Error())
		return nil
	}
	defer closer.Close()
	return pipeRawSSE(reader, b, len(req.Thinking) > 0)
}

func cloneRequest(req *anthropictypes.Request) map[string]any {
	raw, _ := json.Marshal(req)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return body
}

func (a *Adapter) postStream(ctx context.Context, body []byte, betas []string) (*bufio.Reader, io.Closer, error) {
	extraHeaders := map[string]string{"anthropic-version": "2023-06-01"}
	if len(betas) > 0 {
		extraHeaders["anthropic-beta"] = strings.Join(betas, ",")
	}
	return provider.PostStreamWithClient(ctx, a.client, a.pc.MaxRetries, a.pc.BaseURL, "/messages", a.pc.APIKey, "x-api-key", extraHeaders, body)
}

func pipeRawSSE(reader *bufio.Reader, b *sse.Builder, thinkingEnabled bool) error {
	defer b.MarkFinished()
	tracker := newStreamTracker()
	policy := newNativePolicy()
	var frame strings.Builder
	flush := func() error {
		if strings.TrimSpace(frame.String()) == "" {
			frame.Reset()
			return nil
		}
		out := policy.Transform(frame.String(), thinkingEnabled)
		frame.Reset()
		if out == "" {
			return nil
		}
		tracker.Feed(out)
		return b.RawWriter().WriteRaw(out)
	}
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				if writeErr := flush(); writeErr != nil {
					return writeErr
				}
			} else {
				frame.WriteString(line)
			}
		}
		if err != nil {
			if err == io.EOF {
				if writeErr := flush(); writeErr != nil {
					return writeErr
				}
				return nil
			}
			emitStreamErrorTail(b, tracker, err.Error())
			return nil
		}
	}
}

func emitStreamErrorTail(b *sse.Builder, tracker *streamTracker, msg string) {
	if !tracker.SawEvents() {
		b.Start()
		b.FinishWithError(msg)
		return
	}
	for {
		idx, ok := tracker.PopOpen()
		if !ok {
			break
		}
		b.RawWriter().Event("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
	}
	idx := tracker.NextIndex()
	b.RawWriter().Event("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": idx,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	})
	b.RawWriter().Event("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": idx,
		"delta": map[string]any{
			"type": "text_delta",
			"text": msg,
		},
	})
	b.RawWriter().Event("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
	b.RawWriter().Event("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"input_tokens": 0, "output_tokens": 1},
	})
	b.RawWriter().Event("message_stop", map[string]any{"type": "message_stop"})
}
