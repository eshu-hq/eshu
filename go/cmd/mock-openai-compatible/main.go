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
	appName           = "eshu-mock-openai-compatible"
	envListenAddr     = "MOCK_OPENAI_COMPATIBLE_LISTEN_ADDR"
	defaultListenAddr = "127.0.0.1:19191"
	shutdownTimeout   = 5 * time.Second
)

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
		logger.Error("mock OpenAI-compatible provider failed", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context, getenv func(string) string, logger *slog.Logger) error {
	addr := strings.TrimSpace(getenv(envListenAddr))
	if addr == "" {
		addr = defaultListenAddr
	}
	server := &http.Server{Addr: addr, Handler: newHandler(), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		logger.Info("mock OpenAI-compatible provider listening", "addr", addr)
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
