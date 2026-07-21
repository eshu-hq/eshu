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
	appName = "eshu-mock-oidc-idp"

	envListenAddr          = "MOCK_OIDC_LISTEN_ADDR"
	envIssuerURL           = "MOCK_OIDC_ISSUER_URL"
	envSubject             = "MOCK_OIDC_SUBJECT"
	envEmail               = "MOCK_OIDC_EMAIL"
	envGroups              = "MOCK_OIDC_GROUPS"
	envGroupClaim          = "MOCK_OIDC_GROUP_CLAIM"
	envAccessTokenJWT      = "MOCK_OIDC_ACCESS_TOKEN_JWT"
	envAccessTokenAudience = "MOCK_OIDC_ACCESS_TOKEN_AUDIENCE"
	envAccessTokenTTL      = "MOCK_OIDC_ACCESS_TOKEN_TTL_SECONDS"

	defaultListenAddr            = "0.0.0.0:8080"
	defaultSubject               = "member-user-1"
	defaultEmail                 = "member.user@example.test"
	defaultGroups                = "member"
	defaultAccessTokenTTLSeconds = 600

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
		logger.Error("mock-oidc-idp failed", "error", err)
		os.Exit(1)
	}
}

// run builds the Server from environment configuration and serves it until
// the process receives SIGINT/SIGTERM, then shuts down gracefully. This
// binary intentionally omits the runtime package's OTEL/Postgres wiring: it
// is a synthetic test-only IdP with no database and no telemetry contract of
// its own, matching the "no OTEL registration" precedent set by the
// admin-status CLI (go/cmd/admin-status).
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
		logger.Info("mock oidc idp listening", "addr", cfg.listenAddr, "issuer", cfg.server.Issuer)
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

// configFromEnv reads MOCK_OIDC_* environment variables into an appConfig.
// MOCK_OIDC_ISSUER_URL has no default: it must be the exact URL callers
// (Eshu's OIDC connector, and eventually a browser test driver) reach this
// container at, which depends on the compose network the caller runs on.
func configFromEnv(getenv func(string) string) (appConfig, error) {
	issuer := strings.TrimSpace(getenv(envIssuerURL))
	if issuer == "" {
		return appConfig{}, fmt.Errorf("%s is required", envIssuerURL)
	}
	listenAddr := defaultString(getenv(envListenAddr), defaultListenAddr)
	subject := defaultString(getenv(envSubject), defaultSubject)
	email := defaultString(getenv(envEmail), defaultEmail)
	groups := splitAndTrim(defaultString(getenv(envGroups), defaultGroups))
	groupClaim := strings.TrimSpace(getenv(envGroupClaim))
	accessTokenJWT := parseBool(getenv(envAccessTokenJWT))
	accessTokenAudience := strings.TrimSpace(getenv(envAccessTokenAudience))
	accessTokenTTLSeconds, err := parseIntDefault(getenv(envAccessTokenTTL), defaultAccessTokenTTLSeconds)
	if err != nil {
		return appConfig{}, fmt.Errorf("%s: %w", envAccessTokenTTL, err)
	}

	return appConfig{
		listenAddr: listenAddr,
		server: ServerConfig{
			Issuer: issuer,
			Identity: IdentityConfig{
				Subject: subject,
				Email:   email,
				Groups:  groups,
			},
			GroupClaim:          groupClaim,
			AccessTokenJWT:      accessTokenJWT,
			AccessTokenAudience: accessTokenAudience,
			AccessTokenTTL:      time.Duration(accessTokenTTLSeconds) * time.Second,
		},
	}, nil
}

// parseBool reports whether value is a recognized truthy string
// ("true"/"1", case-insensitive). Any other value, including empty, is
// false — matching the rest of Eshu's ESHU_*-style boolean env parsing.
func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true
	default:
		return false
	}
}

// parseIntDefault parses value as a base-10 integer, returning fallback when
// value is blank.
func parseIntDefault(value string, fallback int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q: %w", value, err)
	}
	return parsed, nil
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// splitAndTrim splits a comma-separated env value into trimmed, non-empty
// parts, used for MOCK_OIDC_GROUPS.
func splitAndTrim(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
