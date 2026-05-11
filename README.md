# RelayCode (Go)

Single-binary proxy. Claude Code client -> RelayCode -> OpenAI Chat Completions or Responses API.

## Build

```bash
cd go
go build -o relaycode ./cmd/relaycode
```

Zero external deps. Stdlib only. One static binary.

## Run

```bash
cp relaycode.example.yaml relaycode.yaml
# edit api keys or export env vars
export OPENAI_API_KEY=sk-...
export MAXPLUS_API_KEY=...
export DEEPSEEK_API_KEY=...
./relaycode -config relaycode.yaml
```

## Point Claude Code at it

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8080
export ANTHROPIC_AUTH_TOKEN=""   # or whatever server.auth_token you set
claude
```

## Routing

Incoming model name (e.g. `claude-opus-4-7`) matched against `routes[].match` (substring). First match wins. `"*"` is the fallback.

## Providers

- `openai_chat` — POST `/chat/completions` (OpenAI, DeepSeek, any OpenAI-compat)
- `openai_responses` — POST `/responses` (OpenAI, MaxPlus)

Both translate Claude Code's Anthropic Messages protocol (streaming SSE, tool_use, tool_result) to the chosen backend shape and back.

## Status

MVP — stateless translation. Session/delta optimization to come (see root `PLAN.md`).
