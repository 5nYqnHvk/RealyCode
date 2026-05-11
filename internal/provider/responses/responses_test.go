package responses

import (
	"encoding/json"
	"testing"

	"github.com/relaycode/relaycode/internal/anthropic"
)

func TestBuildRequestFiltersServerToolsByDefault(t *testing.T) {
	req := &anthropic.Request{
		Tools: []anthropic.Tool{
			{Name: "bash", InputSchema: json.RawMessage(`{"type":"object"}`)},
			{Name: "web_search", Type: "web_search_20250305", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}

	body, err := buildRequest(req, "gpt", false)
	if err != nil {
		t.Fatal(err)
	}
	tools := body["tools"].([]toolDecl)
	if len(tools) != 1 || tools[0].Name != "bash" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestBuildRequestPassesServerToolsWhenExperimental(t *testing.T) {
	req := &anthropic.Request{
		Tools: []anthropic.Tool{
			{Name: "bash", InputSchema: json.RawMessage(`{"type":"object"}`)},
			{Name: "web_search", Type: "web_search_20250305", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
		Messages: []anthropic.Message{
			{
				Role: "assistant",
				Content: anthropic.Content{Blocks: []anthropic.Block{
					{Type: "server_tool_use", ID: "call_1", Name: "web_search", Input: json.RawMessage(`{"query":"relaycode"}`)},
				}},
			},
			{
				Role: "user",
				Content: anthropic.Content{Blocks: []anthropic.Block{
					{Type: "web_search_tool_result", ToolUseID: "call_1", Content: json.RawMessage(`"result text"`)},
				}},
			},
		},
	}

	body, err := buildRequest(req, "gpt", true)
	if err != nil {
		t.Fatal(err)
	}
	tools := body["tools"].([]toolDecl)
	if len(tools) != 2 || tools[1].Name != "web_search" {
		t.Fatalf("tools = %+v", tools)
	}
	items := body["input"].([]inputItem)
	if len(items) != 2 {
		t.Fatalf("input = %+v", items)
	}
	if items[0].Type != "function_call" || items[0].Name != "web_search" || items[0].CallID != "call_1" {
		t.Fatalf("function call item = %+v", items[0])
	}
	if items[1].Type != "function_call_output" || items[1].CallID != "call_1" || items[1].Output != "result text" {
		t.Fatalf("function output item = %+v", items[1])
	}
}
