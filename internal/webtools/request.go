package webtools

import (
	"encoding/json"
	"strings"

	"github.com/5nYqnHvk/RelayCode/internal/anthropic"
)

func ForcedServerToolName(req *anthropic.Request) string {
	if len(req.ToolChoice) == 0 {
		return ""
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.ToolChoice, &choice); err != nil {
		return ""
	}
	if choice.Type != "tool" {
		return ""
	}
	if choice.Name == "web_search" || choice.Name == "web_fetch" {
		return choice.Name
	}
	return ""
}

func HasToolNamed(req *anthropic.Request, name string) bool {
	_, ok := findToolNamed(req, name)
	return ok
}

func findToolNamed(req *anthropic.Request, name string) (anthropic.Tool, bool) {
	for _, tool := range req.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return anthropic.Tool{}, false
}

func IsWebServerToolRequest(req *anthropic.Request) bool {
	name := ForcedServerToolName(req)
	if name == "" {
		return false
	}
	tool, ok := findToolNamed(req, name)
	return ok && IsAnthropicWebServerTool(tool)
}

func IsAnthropicWebServerTool(tool anthropic.Tool) bool {
	if strings.HasPrefix(tool.Type, "web_search") || strings.HasPrefix(tool.Type, "web_fetch") {
		return true
	}
	name := strings.TrimSpace(tool.Name)
	return tool.Type == "" && len(tool.InputSchema) == 0 && (name == "web_search" || name == "web_fetch")
}

func HasListedAnthropicWebServerTools(req *anthropic.Request) bool {
	for _, tool := range req.Tools {
		if IsAnthropicWebServerTool(tool) {
			return true
		}
	}
	return false
}

func ForcedToolTurnText(req *anthropic.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return ContentText(req.Messages[i].Content)
		}
	}
	return ""
}
