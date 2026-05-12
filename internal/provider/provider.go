// Package provider defines the egress adapter interface used by the server.
package provider

import (
	"context"

	"github.com/5nYqnHvk/RelayCode/internal/anthropic"
	"github.com/5nYqnHvk/RelayCode/internal/config"
	"github.com/5nYqnHvk/RelayCode/internal/session"
	"github.com/5nYqnHvk/RelayCode/internal/sse"
)

// Adapter streams a translated Anthropic response to the given SSE Builder.
//
// Implementations must:
//   - call b.Start() before emitting content,
//   - call b.Finish() exactly once on normal completion, or
//   - call b.FinishWithError(msg) on a mid-stream transport/upstream error.
type Adapter interface {
	Stream(ctx context.Context, req *anthropic.Request, upstreamModel string, b *sse.Builder) error
}

// SessionAware is implemented by adapters that can take advantage of a
// server-side session store (currently: openai_responses). The server
// calls SetSession once at wire-up; adapters that don't implement this
// interface simply run stateless.
type SessionAware interface {
	SetSession(store *session.Store, providerName string)
}

// Factory builds an Adapter for a provider config entry.
type Factory func(pc config.ProviderConfig) (Adapter, error)

// ToolUseBridgeInstruction nudges OpenAI-style models to use the tools Claude
// Code exposed instead of narrating tool work as plain text.
const ToolUseBridgeInstruction = "Tool use contract for this OpenAI adapter: when the user asks you to inspect files, run commands, edit, write, test, or otherwise do work in the local environment, call the appropriate available tool in the same response. Do not only say that you will start. If no tool is needed, answer normally."

func InstructionsWithToolUseBridge(instructions string, hasTools bool) string {
	if !hasTools {
		return instructions
	}
	if instructions == "" {
		return ToolUseBridgeInstruction
	}
	return instructions + "\n\n" + ToolUseBridgeInstruction
}
