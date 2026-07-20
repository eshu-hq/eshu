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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

const (
	appName = "eshu-mock-github"

	envListenAddr = "MOCK_GITHUB_LISTEN_ADDR"
	envLogin      = "MOCK_GITHUB_LOGIN"
	envUserID     = "MOCK_GITHUB_USER_ID"
	envEmail      = "MOCK_GITHUB_EMAIL"
	envOrg        = "MOCK_GITHUB_ORG"
	envTeams      = "MOCK_GITHUB_TEAMS"

	defaultListenAddr = "0.0.0.0:8080"
	defaultLogin      = "e2e-github-user"
	defaultUserID     = "1001"
	defaultEmail      = "e2e-github-user@example.test"
	defaultOrg        = "eshu-e2e-org"
	defaultTeams      = "eshu-e2e-org/platform-team"

	shutdownTimeout = 5 * time.Second
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
		logger.Error("mock-github failed", "error", err)
		os.Exit(1)
	}
}

// run builds the Server from environment configuration and serves it until
// the process receives SIGINT/SIGTERM, then shuts down gracefully. Like
// mock-oidc-idp, this binary omits the runtime package's OTEL/Postgres
// wiring: it is a synthetic test-only fixture with no database and no
// telemetry contract of its own.
func run(parent context.Context, getenv func(string) string, logger *slog.Logger) error {
	cfg, err := configFromEnv(getenv)
	if err != nil {
		return err
	}
	server, err := NewServer(cfg.server)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           server.Mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("mock github listening", "addr", cfg.listenAddr, "login", cfg.server.Identity.Login)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// appConfig is the fully resolved binary configuration.
type appConfig struct {
	listenAddr string
	server     ServerConfig
}

// configFromEnv reads MOCK_GITHUB_* environment variables into an
// appConfig. Unlike mock-oidc-idp's MOCK_OIDC_ISSUER_URL, no variable here
// is required: every field has an example.test-safe default, since this
// mock's endpoints do not echo a caller-supplied issuer/audience back to
// itself the way OIDC discovery does.
func configFromEnv(getenv func(string) string) (appConfig, error) {
	listenAddr := defaultString(getenv(envListenAddr), defaultListenAddr)
	login := defaultString(getenv(envLogin), defaultLogin)
	userIDRaw := defaultString(getenv(envUserID), defaultUserID)
	userID, err := strconv.ParseInt(userIDRaw, 10, 64)
	if err != nil {
		return appConfig{}, fmt.Errorf("%s: invalid integer %q: %w", envUserID, userIDRaw, err)
	}
	email := defaultString(getenv(envEmail), defaultEmail)
	org := defaultString(getenv(envOrg), defaultOrg)
	teams, err := parseTeams(defaultString(getenv(envTeams), defaultTeams))
	if err != nil {
		return appConfig{}, fmt.Errorf("%s: %w", envTeams, err)
	}

	return appConfig{
		listenAddr: listenAddr,
		server: ServerConfig{
			Identity: IdentityConfig{
				Login:  login,
				UserID: userID,
				Email:  email,
				Org:    org,
				Teams:  teams,
			},
		},
	}, nil
}

// parseTeams splits a comma-separated "org/slug,org/slug,..." env value into
// TeamHandle values.
func parseTeams(value string) ([]TeamHandle, error) {
	parts := strings.Split(value, ",")
	teams := make([]TeamHandle, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		org, slug, ok := strings.Cut(p, "/")
		org = strings.TrimSpace(org)
		slug = strings.TrimSpace(slug)
		if !ok || org == "" || slug == "" {
			return nil, fmt.Errorf("invalid team handle %q: want \"org/slug\"", p)
		}
		teams = append(teams, TeamHandle{Org: org, Slug: slug})
	}
	return teams, nil
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
