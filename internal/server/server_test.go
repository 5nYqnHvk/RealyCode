package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/5nYqnHvk/RelayCode/internal/config"
)

func TestHandleModelsListsConfiguredRoutes(t *testing.T) {
	srv, err := New(&config.Config{
		Server: config.ServerConfig{AuthToken: "token"},
		Routes: []config.Route{
			{Match: "opus", Provider: "responses", Model: "gpt-strong"},
			{Match: "sonnet", Provider: "responses", Model: "gpt-strong"},
			{Match: "*", Provider: "chat", Model: "gpt-fallback"},
		},
		Providers: map[string]config.ProviderConfig{
			"responses": {Kind: config.KindOpenAIResponses, BaseURL: "https://example.test/v1"},
			"chat":      {Kind: config.KindOpenAIChat, BaseURL: "https://example.test/v1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("x-api-key", "token")
	rw := httptest.NewRecorder()
	srv.handleModels(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rw.Code, rw.Body.String())
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Object != "list" || len(body.Data) != 2 {
		t.Fatalf("body = %+v", body)
	}
	if body.Data[0].ID != "gpt-strong" || body.Data[0].OwnedBy != "responses" || body.Data[1].ID != "gpt-fallback" {
		t.Fatalf("models = %+v", body.Data)
	}
}

func TestHandleModelsRequiresAuth(t *testing.T) {
	srv, err := New(&config.Config{
		Server: config.ServerConfig{AuthToken: "token"},
		Routes: []config.Route{{Match: "*", Provider: "chat", Model: "gpt"}},
		Providers: map[string]config.ProviderConfig{
			"chat": {Kind: config.KindOpenAIChat, BaseURL: "https://example.test/v1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rw := httptest.NewRecorder()
	srv.handleModels(rw, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rw.Code, rw.Body.String())
	}
}
