package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/relaycode/relaycode/internal/config"
	"github.com/relaycode/relaycode/internal/server"
)

func main() {
	configPath := flag.String("config", "relaycode.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("relaycode listening on %s (routes=%d)", srv.Addr(), len(cfg.Routes))
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
