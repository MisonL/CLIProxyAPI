package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/platform"
	log "github.com/sirupsen/logrus"
)

func main() {
	cfg := platform.LoadConfigFromEnv()
	cfg.Role = "worker"
	if err := cfg.Validate(); err != nil {
		log.Fatalf("platform worker config invalid: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	runtime, err := platform.NewRuntime(ctx, cfg)
	if err != nil {
		log.Fatalf("start platform worker runtime: %v", err)
	}
	defer runtime.Close()

	fmt.Printf("CLIProxyAPI platform worker started for workspace %s/%s\n", cfg.TenantSlug, cfg.WorkspaceSlug)
	if err = runtime.RunWorker(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("platform worker stopped: %v", err)
	}
	_, _ = os.Stdout.WriteString("platform worker stopped\n")
}
