package anthropic

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestContentUnmarshalAndAsBlocks(t *testing.T) {
	var raw Content
	if err := json.Unmarshal([]byte(`"hello"`), &raw); err != nil {
		t.Fatal(err)
	}
	if got := raw.AsBlocks(); len(got) != 1 || got[0].Type != "text" || got[0].Text != "hello" {
		t.Fatalf("raw.AsBlocks() = %+v", got)
	}

	var blocks Content
	if err := json.Unmarshal([]byte(`[{"type":"text","text":"hi"}]`), &blocks); err != nil {
		t.Fatal(err)
	}
	if got := blocks.AsBlocks(); len(got) != 1 || got[0].Text != "hi" {
		t.Fatalf("blocks.AsBlocks() = %+v", got)
	}
}

func TestToolsForUpstreamFiltersAnthropicServerToolsByDefault(t *testing.T) {
	tools := []Tool{
		{Name: "bash", Type: "custom"},
		{Name: "web_search", Type: "custom"},
		{Name: "server", Type: "web_search_20250305"},
		{Name: "plain"},
	}

	got := ToolsForUpstream(tools, false)
	want := []Tool{{Name: "bash", Type: "custom"}, {Name: "plain"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ToolsForUpstream(false) = %+v, want %+v", got, want)
	}
	if got := ToolsForUpstream(tools, true); !reflect.DeepEqual(got, tools) {
		t.Fatalf("ToolsForUpstream(true) = %+v, want %+v", got, tools)
	}
}

func TestStripServerToolBlocks(t *testing.T) {
	blocks := []Block{
		{Type: "text", Text: "keep"},
		{Type: "server_tool_use", Name: "web_search"},
		{Type: "web_search_tool_result"},
		{Type: "tool_use", Name: "bash"},
	}

	got := BlocksForUpstream(blocks, false)
	want := []Block{{Type: "text", Text: "keep"}, {Type: "tool_use", Name: "bash"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BlocksForUpstream(false) = %+v, want %+v", got, want)
	}
	if got := BlocksForUpstream(blocks, true); !reflect.DeepEqual(got, blocks) {
		t.Fatalf("BlocksForUpstream(true) = %+v, want %+v", got, blocks)
	}
}

func TestSystemTextDropsVolatileBillingHeader(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"x-anthropic-billing-header: abc cch=random"},
		{"type":"text","text":"stable instructions"},
		{"type":"thinking","thinking":"skip"},
		{"type":"text","text":"more instructions"}
	]`)

	got, err := SystemText(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != "stable instructions\n\nmore instructions" {
		t.Fatalf("SystemText() = %q", got)
	}
}

func TestSessionIDPrefersSessionIDThenDeviceIDThenPlainUserID(t *testing.T) {
	tests := []struct {
		name string
		md   json.RawMessage
		want string
	}{
		{
			name: "session id",
			md:   json.RawMessage(`{"user_id":"{\"session_id\":\"sess-1\",\"device_id\":\"dev-1\"}"}`),
			want: "sess-1",
		},
		{
			name: "device id fallback",
			md:   json.RawMessage(`{"user_id":"{\"device_id\":\"dev-1\"}"}`),
			want: "dev-1",
		},
		{
			name: "plain user id",
			md:   json.RawMessage(`{"user_id":"plain-user"}`),
			want: "plain-user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Request{Metadata: tt.md}
			if got := req.SessionID(); got != tt.want {
				t.Fatalf("SessionID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolResultTextFlattensTextBlocks(t *testing.T) {
	got, err := ToolResultText(json.RawMessage(`[{"type":"text","text":"one"},{"type":"image"},{"type":"text","text":"two"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got != "one\ntwo" {
		t.Fatalf("ToolResultText() = %q", got)
	}
}
