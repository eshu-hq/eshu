// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

const (
	appName           = "eshu-mock-prometheus-mimir"
	envListenAddr     = "MOCK_PROMETHEUS_MIMIR_LISTEN_ADDR"
	defaultListenAddr = "127.0.0.1:19090"
	shutdownTimeout   = 5 * time.Second
)

type appConfig struct {
	listenAddr string
}

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, appName); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(context.Background(), os.Getenv, logger); err != nil {
		logger.Error("mock prometheus/mimir failed", "error", err)
		os.Exit(1)
	}
}

// run serves the synthetic range endpoint until the process receives an
// interrupt. This test-only binary has no database or product telemetry.
func run(parent context.Context, getenv func(string) string, logger *slog.Logger) error {
	cfg := configFromEnv(getenv)
	server := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("mock prometheus/mimir listening", "addr", cfg.listenAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func configFromEnv(getenv func(string) string) appConfig {
	listenAddr := ""
	if getenv != nil {
		listenAddr = strings.TrimSpace(getenv(envListenAddr))
	}
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	return appConfig{listenAddr: listenAddr}
}
