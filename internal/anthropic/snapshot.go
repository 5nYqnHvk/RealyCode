package anthropic

import "encoding/json"

type RequestSnapshot struct {
	Model                 string            `json:"model"`
	MessageCount          int               `json:"message_count"`
	Messages              []MessageSnapshot `json:"messages"`
	ToolNames             []string          `json:"tool_names,omitempty"`
	ToolChoice            string            `json:"tool_choice,omitempty"`
	ToolChoiceName        string            `json:"tool_choice_name,omitempty"`
	HasSystem             bool              `json:"has_system"`
	HasThinking           bool              `json:"has_thinking"`
	HasContextManagement  bool              `json:"has_context_management"`
	HasStructuredOutputs  bool              `json:"has_structured_outputs"`
	HasEffort             bool              `json:"has_effort"`
	HasTaskBudgets        bool              `json:"has_task_budgets"`
	HasPromptCachingScope bool              `json:"has_prompt_caching_scope"`
	HasExtendedCacheTTL   bool              `json:"has_extended_cache_ttl"`
	HasSpeed              bool              `json:"has_speed"`
	HasRedactThinking     bool              `json:"has_redact_thinking"`
}

type MessageSnapshot struct {
	Role          string   `json:"role"`
	ContentKind   string   `json:"content_kind"`
	ContentLength int      `json:"content_length,omitempty"`
	BlockTypes    []string `json:"block_types,omitempty"`
}

func Snapshot(req *Request) RequestSnapshot {
	out := RequestSnapshot{
		Model:                 req.Model,
		MessageCount:          len(req.Messages),
		HasSystem:             len(req.System) > 0,
		HasThinking:           len(req.Thinking) > 0,
		HasContextManagement:  len(req.ContextManagement) > 0,
		HasStructuredOutputs:  len(req.StructuredOutputs) > 0,
		HasEffort:             len(req.Effort) > 0,
		HasTaskBudgets:        len(req.TaskBudgets) > 0,
		HasPromptCachingScope: len(req.PromptCachingScope) > 0,
		HasExtendedCacheTTL:   len(req.ExtendedCacheTTL) > 0,
		HasSpeed:              len(req.Speed) > 0,
		HasRedactThinking:     len(req.RedactThinking) > 0,
	}
	for _, tool := range req.Tools {
		out.ToolNames = append(out.ToolNames, tool.Name)
	}
	out.ToolChoice, out.ToolChoiceName = SnapshotToolChoice(req.ToolChoice)
	for _, msg := range req.Messages {
		item := MessageSnapshot{Role: msg.Role}
		if msg.Content.Blocks != nil {
			item.ContentKind = "blocks"
			limit := len(msg.Content.Blocks)
			if limit > 12 {
				limit = 12
			}
			for _, block := range msg.Content.Blocks[:limit] {
				item.BlockTypes = append(item.BlockTypes, block.Type)
			}
		} else {
			item.ContentKind = "string"
			item.ContentLength = len(msg.Content.Raw)
		}
		out.Messages = append(out.Messages, item)
	}
	return out
}

func SnapshotToolChoice(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &choice); err != nil {
		return "invalid", ""
	}
	return choice.Type, choice.Name
}

func SnapshotJSON(req *Request) string {
	raw, err := json.Marshal(Snapshot(req))
	if err != nil {
		return "{}"
	}
	return string(raw)
}
