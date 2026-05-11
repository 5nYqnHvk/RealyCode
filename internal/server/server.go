// Package server implements the Anthropic-compatible HTTP ingress.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/relaycode/relaycode/internal/anthropic"
	"github.com/relaycode/relaycode/internal/config"
	"github.com/relaycode/relaycode/internal/provider"
	"github.com/relaycode/relaycode/internal/provider/chat"
	"github.com/relaycode/relaycode/internal/provider/responses"
	"github.com/relaycode/relaycode/internal/router"
	"github.com/relaycode/relaycode/internal/session"
	"github.com/relaycode/relaycode/internal/sse"
)

type Server struct {
	cfg      *config.Config
	router   *router.Router
	adapters map[string]provider.Adapter // lazy, keyed by provider name in cfg
	sessions *session.Store
	addr     string
}

func New(cfg *config.Config) (*Server, error) {
	for name, pc := range cfg.Providers {
		if pc.Kind != config.KindOpenAIChat && pc.Kind != config.KindOpenAIResponses {
			return nil, fmt.Errorf("provider %q: unsupported kind %q", name, pc.Kind)
		}
	}
	return &Server{
		cfg:      cfg,
		router:   router.New(cfg),
		adapters: map[string]provider.Adapter{},
		sessions: session.NewStore(60*time.Minute, 1000),
		addr:     net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port)),
	}, nil
}

func (s *Server) adapterFor(name string, pc config.ProviderConfig) (provider.Adapter, error) {
	if a, ok := s.adapters[name]; ok {
		return a, nil
	}
	var (
		a   provider.Adapter
		err error
	)
	switch pc.Kind {
	case config.KindOpenAIChat:
		a, err = chat.New(pc)
	case config.KindOpenAIResponses:
		a, err = responses.New(pc)
	default:
		return nil, fmt.Errorf("provider %q: unsupported kind %q", name, pc.Kind)
	}
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", name, err)
	}
	if aware, ok := a.(provider.SessionAware); ok {
		aware.SetSession(s.sessions, name)
	}
	s.adapters[name] = a
	return a, nil
}

func (s *Server) Addr() string { return s.addr }

func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", s.handleMessages)
	mux.HandleFunc("/debug/stats", s.handleStats)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.Server.AuthToken != "" && !authOK(r, s.cfg.Server.AuthToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	snapshot := s.sessions.Snapshot()
	type entryView struct {
		Provider      string `json:"provider"`
		UpstreamModel string `json:"upstream_model"`
		MessageCount  int    `json:"message_count"`
		ResponseID    string `json:"response_id"`
		LastUsed      string `json:"last_used"`
		InputTokens   int    `json:"input_tokens"`
		OutputTokens  int    `json:"output_tokens"`
	}
	entries := make([]entryView, 0, len(snapshot))
	for _, e := range snapshot {
		entries = append(entries, entryView{
			Provider:      e.Provider,
			UpstreamModel: e.UpstreamModel,
			MessageCount:  e.MessageCount,
			ResponseID:    e.ResponseID,
			LastUsed:      e.LastUsed.UTC().Format(time.RFC3339),
			InputTokens:   e.InputTokens,
			OutputTokens:  e.OutputTokens,
		})
	}
	out := map[string]any{
		"sessions": entries,
		"counters": map[string]int64{
			"hits":            s.sessions.Stats.Hits.Load(),
			"misses":          s.sessions.Stats.Misses.Load(),
			"forced_replays":  s.sessions.Stats.ForcedReplays.Load(),
			"expired_invalid": s.sessions.Stats.ExpiredInvalid.Load(),
			"input_tokens":    s.sessions.Stats.InputTokens.Load(),
			"output_tokens":   s.sessions.Stats.OutputTokens.Load(),
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.cfg.Server.AuthToken != "" {
		if !authOK(r, s.cfg.Server.AuthToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var req anthropic.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"type":"error","error":{"type":"invalid_request_error","message":%q}}`, err.Error()), http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"model is required"}}`, http.StatusBadRequest)
		return
	}

	resolved, err := s.router.Resolve(req.Model)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"type":"error","error":{"type":"invalid_request_error","message":%q}}`, err.Error()), http.StatusBadRequest)
		return
	}
	adapter, err := s.adapterFor(resolved.ProviderName, resolved.Provider)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"type":"error","error":{"type":"invalid_request_error","message":%q}}`, err.Error()), http.StatusInternalServerError)
		return
	}

	log.Printf("messages: incoming=%s -> provider=%s upstream_model=%s", req.Model, resolved.ProviderName, resolved.Model)

	sw := sse.NewWriter(w)
	builder := sse.NewBuilder(sw, newMessageID(), req.Model, estimateInputTokens(&req))
	if err := adapter.Stream(r.Context(), &req, resolved.Model, builder); err != nil {
		log.Printf("messages: adapter error: %v", err)
	}
	if !builder.Finished() {
		builder.Finish()
	}
}

func authOK(r *http.Request, expected string) bool {
	if h := r.Header.Get("x-api-key"); h != "" && h == expected {
		return true
	}
	if h := r.Header.Get("Authorization"); h != "" {
		if len(h) > 7 && h[:7] == "Bearer " && h[7:] == expected {
			return true
		}
		if h == expected {
			return true
		}
	}
	return false
}

func newMessageID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return "msg_" + hex.EncodeToString(buf[:])
}

func estimateInputTokens(req *anthropic.Request) int {
	total := 0
	if sys, err := anthropic.SystemText(req.System); err == nil {
		total += len(sys) / 4
	}
	for _, m := range req.Messages {
		for _, b := range m.Content.AsBlocks() {
			total += len(b.Text) / 4
			total += len(b.Thinking) / 4
			if len(b.Input) > 0 {
				total += len(b.Input) / 4
			}
			if len(b.Content) > 0 {
				total += len(b.Content) / 4
			}
		}
	}
	return total
}
