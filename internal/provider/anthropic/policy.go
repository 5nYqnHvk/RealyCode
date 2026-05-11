package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
)

type nativePolicy struct {
	nextIndex       int
	byUpstream      map[int]*upstreamBlock
	dropped         map[int]bool
	suppressedStops map[int]bool
}

type upstreamBlock struct {
	kind      string
	downIndex int
	open      bool
	start     map[string]any
}

func newNativePolicy() *nativePolicy {
	return &nativePolicy{
		byUpstream:      map[int]*upstreamBlock{},
		dropped:         map[int]bool{},
		suppressedStops: map[int]bool{},
	}
}

func (p *nativePolicy) Transform(frame string, thinkingEnabled bool) string {
	event, dataText := parseSSEFrame(frame)
	if event == "" || dataText == "" {
		return frame + "\n\n"
	}
	if (event == "data" || event == "done") && strings.EqualFold(strings.TrimSpace(dataText), "[DONE]") {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(dataText), &payload); err != nil {
		return frame + "\n\n"
	}
	switch event {
	case "content_block_start":
		return p.transformStart(event, payload, thinkingEnabled)
	case "content_block_delta":
		return p.transformDelta(event, payload, thinkingEnabled)
	case "content_block_stop":
		return p.transformStop(event, payload, thinkingEnabled)
	}
	return formatSSEFrame(event, payload)
}

func (p *nativePolicy) transformStart(event string, payload map[string]any, thinkingEnabled bool) string {
	idx, ok := payloadIndex(payload)
	if !ok {
		return formatSSEFrame(event, payload)
	}
	block, _ := payload["content_block"].(map[string]any)
	kind, _ := block["type"].(string)
	if shouldDropKind(kind, thinkingEnabled) {
		p.dropped[idx] = true
		return ""
	}
	prefix := p.closeOtherOpen(idx)
	down := p.allocate(idx, kind, block)
	payload["index"] = down
	return prefix + formatSSEFrame(event, payload)
}

func (p *nativePolicy) transformDelta(event string, payload map[string]any, thinkingEnabled bool) string {
	idx, ok := payloadIndex(payload)
	if !ok {
		return formatSSEFrame(event, payload)
	}
	if p.dropped[idx] {
		return ""
	}
	delta, _ := payload["delta"].(map[string]any)
	deltaType, _ := delta["type"].(string)
	if shouldDropKind(deltaType, thinkingEnabled) {
		return ""
	}
	kind := deltaKind(deltaType)
	if kind == "" {
		return formatSSEFrame(event, payload)
	}
	seg := p.byUpstream[idx]
	if seg != nil && seg.open {
		payload["index"] = seg.downIndex
		return formatSSEFrame(event, payload)
	}
	if seg != nil && !seg.open {
		p.suppressedStops[idx] = false
		down := p.allocate(idx, kind, seg.start)
		payload["index"] = down
		return formatSSEFrame("content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         down,
			"content_block": syntheticStart(kind, idx, seg.start),
		}) + formatSSEFrame(event, payload)
	}
	if kind == "text" || kind == "tool_use" || kind == "thinking" {
		start := syntheticStart(kind, idx, nil)
		down := p.allocate(idx, kind, start)
		payload["index"] = down
		return formatSSEFrame("content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         down,
			"content_block": start,
		}) + formatSSEFrame(event, payload)
	}
	return formatSSEFrame(event, payload)
}

func (p *nativePolicy) transformStop(event string, payload map[string]any, thinkingEnabled bool) string {
	idx, ok := payloadIndex(payload)
	if !ok {
		return formatSSEFrame(event, payload)
	}
	if p.dropped[idx] {
		return ""
	}
	if p.suppressedStops[idx] {
		delete(p.suppressedStops, idx)
		return ""
	}
	seg := p.byUpstream[idx]
	if seg != nil && seg.open {
		payload["index"] = seg.downIndex
		seg.open = false
		return formatSSEFrame(event, payload)
	}
	if seg != nil || !thinkingEnabled {
		return ""
	}
	return formatSSEFrame(event, payload)
}

func (p *nativePolicy) closeOtherOpen(current int) string {
	var out strings.Builder
	for upstream, seg := range p.byUpstream {
		if upstream == current || !seg.open {
			continue
		}
		out.WriteString(formatSSEFrame("content_block_stop", map[string]any{"type": "content_block_stop", "index": seg.downIndex}))
		seg.open = false
		p.suppressedStops[upstream] = true
	}
	return out.String()
}

func (p *nativePolicy) allocate(upstream int, kind string, start map[string]any) int {
	down := p.nextIndex
	p.nextIndex++
	p.byUpstream[upstream] = &upstreamBlock{kind: kind, downIndex: down, open: true, start: cloneMap(start)}
	return down
}

func parseSSEFrame(frame string) (string, string) {
	event := ""
	var data []string
	for _, line := range strings.Split(strings.TrimRight(frame, "\r\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return event, strings.Join(data, "\n")
}

func formatSSEFrame(event string, payload map[string]any) string {
	raw, _ := json.Marshal(payload)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, raw)
}

func payloadIndex(payload map[string]any) (int, bool) {
	value, ok := payload["index"]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

func shouldDropKind(kind string, thinkingEnabled bool) bool {
	if thinkingEnabled {
		return false
	}
	return strings.Contains(kind, "thinking")
}

func deltaKind(deltaType string) string {
	switch deltaType {
	case "thinking_delta", "signature_delta":
		return "thinking"
	case "text_delta":
		return "text"
	case "input_json_delta":
		return "tool_use"
	}
	return ""
}

func syntheticStart(kind string, upstream int, stored map[string]any) map[string]any {
	if kind == "tool_use" {
		if stored != nil && stored["type"] == "tool_use" {
			id, _ := stored["id"].(string)
			name, _ := stored["name"].(string)
			input, _ := stored["input"].(map[string]any)
			if id == "" {
				id = fmt.Sprintf("toolu_or_%d", upstream)
			}
			if input == nil {
				input = map[string]any{}
			}
			return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
		}
		return map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_or_%d", upstream), "name": "", "input": map[string]any{}}
	}
	if kind == "thinking" {
		return map[string]any{"type": "thinking", "thinking": ""}
	}
	return map[string]any{"type": "text", "text": ""}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	raw, _ := json.Marshal(in)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
