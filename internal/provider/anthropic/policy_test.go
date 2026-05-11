package anthropic

import (
	"strings"
	"testing"
)

func TestNativePolicyDropsThinkingWhenDisabled(t *testing.T) {
	p := newNativePolicy()
	start := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`
	delta := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"secret"}}`
	stop := `event: content_block_stop
data: {"type":"content_block_stop","index":0}`
	if got := p.Transform(start, false); got != "" {
		t.Fatalf("thinking start not dropped: %q", got)
	}
	if got := p.Transform(delta, false); got != "" {
		t.Fatalf("thinking delta not dropped: %q", got)
	}
	if got := p.Transform(stop, false); got != "" {
		t.Fatalf("thinking stop not dropped: %q", got)
	}
}

func TestNativePolicyRemapsOverlappingBlocks(t *testing.T) {
	p := newNativePolicy()
	first := p.Transform(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, true)
	second := p.Transform(`event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`, true)
	if !strings.Contains(first, `"index":0`) {
		t.Fatalf("first = %s", first)
	}
	if !strings.Contains(second, "content_block_stop") || !strings.Contains(second, `"index":1`) {
		t.Fatalf("second = %s", second)
	}
	stop := p.Transform(`event: content_block_stop
data: {"type":"content_block_stop","index":0}`, true)
	if stop != "" {
		t.Fatalf("suppressed stop = %s", stop)
	}
}

func TestNativePolicySynthesizesStartForDeltaWithoutStart(t *testing.T) {
	p := newNativePolicy()
	out := p.Transform(`event: content_block_delta
data: {"type":"content_block_delta","index":5,"delta":{"type":"text_delta","text":"hi"}}`, true)
	if !strings.Contains(out, "content_block_start") || !strings.Contains(out, "content_block_delta") || !strings.Contains(out, `"index":0`) {
		t.Fatalf("out = %s", out)
	}
}
