package config

import "os"

const ExampleYAML = `# RelayCode proxy config
server:
  network:
    host: 127.0.0.1
    port: 8080
    auth_token: ""   # when non-empty, clients must send matching x-api-key / Authorization
  web_tools:
    # Local Anthropic web_search/web_fetch handler. Disabled by default because it
    # performs outbound HTTP from the proxy. Runs only when tool_choice forces it.
    enable: false
    allowed_schemes: http,https
    allow_private_networks: false
  claude_code:
    # Claude Code fast-path optimizations. Disable individually for debugging.
    fast_prefix_detection: true
    enable_network_probe_mock: true
    enable_title_generation_skip: true
    enable_suggestion_mode_skip: true
    enable_filepath_extraction_mock: true
  logging:
    log_request_snapshots: false   # safe shape-only request logs; no raw prompt text
    compact_tool_results: false    # compact long tool output before replaying upstream
  updates:
    enable_notification: false # check GitHub latest release tag on startup
    # check_url: https://api.github.com/repos/5nYqnHvk/RelayCode/releases/latest
    # check_timeout_seconds: 3
  responses:
    session_store_path: "" # optional durable Responses session/cache metadata JSON

# Incoming Claude model name -> backend route.
# "match" is a virtual model id surfaced to Claude Code's /model picker.
# Route order matters. Fallback entry with match: "*" is required.
routes:
  - match: "claude/opus-4-7"
    provider: openai_responses
    model: gpt-5.5
  - match: "claude/sonnet-4-6"
    provider: openai_responses
    model: gpt-5.4
  - match: "claude/haiku-4-5"
    provider: openai_responses
    model: gpt-5.4
  - match: "claude/test"
    provider: openai_responses
    model: gpt-5.4
  - match: "*"
    provider: openai_responses
    model: gpt-5.4

tool_validation:
  unknown_tools: drop
  invalid_known_tools: warn
  malformed_arguments: repair

providers:
  openai_responses:
    kind: openai_responses                 # POST /v1/responses
    endpoint:
      base_url: https://api.openai.com/v1
      api_key: "${OPENAI_API_KEY}"
      # Codex auth.json; overrides api_key when set.
      # codex_auth_path: /home/you/.codex/auth.json
    http:
      # Upstream request timeout in seconds.
      # timeout_seconds: 300
      # HTTP proxy URL.
      # proxy: "${HTTPS_PROXY}"
      # Max retry count for upstream requests.
      # max_retries: 2
      # Max parallel upstream requests.
      # max_concurrency: 4
    experimental:
      # Use non-Codex response chaining instead of replaying the full prefix.
      # previous_response_id: false
      # Pass server tools upstream without translation.
      # passthrough_server_tools: true
    responses:
      # Use native Responses custom tools instead of function-style tools.
      # custom_tool_mode: native
      # Group mcp__server__tool entries as Responses namespace tools.
      # namespace_tools: false
      # Upstream service tier.
      # service_tier: priority
      # Upstream reasoning summary mode.
      # reasoning_summary: none
      # Enable parallel tool calls upstream.
      # parallel_tool_calls: false

  openai_chat:
    kind: openai_chat                      # POST /v1/chat/completions
    endpoint:
      base_url: https://api.openai.com/v1
      api_key: "${OPENAI_API_KEY}"

  anthropic_native:
    kind: anthropic_messages               # POST /v1/messages, raw Anthropic SSE passthrough
    endpoint:
      base_url: https://api.anthropic.com/v1
      api_key: "${ANTHROPIC_API_KEY}"

  deepseek_chat:
    kind: openai_chat
    endpoint:
      base_url: https://api.deepseek.com/v1
      api_key: "${DEEPSEEK_API_KEY}"
`

func WriteExample(path string) error {
	return os.WriteFile(path, []byte(ExampleYAML), 0o600)
}
