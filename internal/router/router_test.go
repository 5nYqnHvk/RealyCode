package router

import (
	"testing"

	"github.com/5nYqnHvk/RelayCode/internal/config"
)

func TestResolveFirstCaseInsensitiveSubstringMatch(t *testing.T) {
	cfg := &config.Config{
		Routes: []config.Route{
			{Match: "sonnet", Provider: "fast", Model: "gpt-fast"},
			{Match: "*", Provider: "fallback", Model: "gpt-fallback"},
		},
		Providers: map[string]config.ProviderConfig{
			"fast":     {Kind: config.KindOpenAIResponses, BaseURL: "https://fast.example"},
			"fallback": {Kind: config.KindOpenAIChat, BaseURL: "https://fallback.example"},
		},
	}

	got, err := New(cfg).Resolve("claude-SONNET-4-6")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.ProviderName != "fast" || got.Model != "gpt-fast" || got.Provider.Kind != config.KindOpenAIResponses {
		t.Fatalf("Resolve() = %+v, want fast gpt-fast openai_responses", got)
	}
}

func TestResolveUsesFallbackWhenNoRouteMatches(t *testing.T) {
	cfg := &config.Config{
		Routes: []config.Route{
			{Match: "opus", Provider: "strong", Model: "gpt-strong"},
			{Match: "*", Provider: "fallback", Model: "gpt-fallback"},
		},
		Providers: map[string]config.ProviderConfig{
			"strong":   {Kind: config.KindOpenAIResponses, BaseURL: "https://strong.example"},
			"fallback": {Kind: config.KindOpenAIChat, BaseURL: "https://fallback.example"},
		},
	}

	got, err := New(cfg).Resolve("claude-haiku-4-5")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got.ProviderName != "fallback" || got.Model != "gpt-fallback" {
		t.Fatalf("Resolve() = %+v, want fallback gpt-fallback", got)
	}
}

func TestResolveErrorsOnUnknownProvider(t *testing.T) {
	cfg := &config.Config{
		Routes:    []config.Route{{Match: "*", Provider: "missing", Model: "gpt"}},
		Providers: map[string]config.ProviderConfig{},
	}

	if _, err := New(cfg).Resolve("anything"); err == nil {
		t.Fatal("Resolve returned nil error for unknown provider")
	}
}

func TestModelsReturnsConfiguredRouteModelsOnce(t *testing.T) {
	cfg := &config.Config{
		Routes: []config.Route{
			{Match: "opus", Provider: "strong", Model: "gpt-strong"},
			{Match: "sonnet", Provider: "strong", Model: "gpt-strong"},
			{Match: "*", Provider: "fallback", Model: "gpt-fallback"},
		},
		Providers: map[string]config.ProviderConfig{
			"strong":   {Kind: config.KindOpenAIResponses, BaseURL: "https://strong.example"},
			"fallback": {Kind: config.KindOpenAIChat, BaseURL: "https://fallback.example"},
		},
	}

	models := New(cfg).Models()
	if len(models) != 2 {
		t.Fatalf("Models len = %d, models=%+v", len(models), models)
	}
	if models[0].ID != "gpt-strong" || models[0].ProviderName != "strong" || models[0].Kind != config.KindOpenAIResponses {
		t.Fatalf("first model = %+v", models[0])
	}
	if models[1].ID != "gpt-fallback" || models[1].ProviderName != "fallback" || models[1].Kind != config.KindOpenAIChat {
		t.Fatalf("second model = %+v", models[1])
	}
}
