// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/webhook/intake"
)

// intakeServeRoutePrefix is the subtree this binary serves core's intake
// API on in direct store-owning mode. The child mux keeps its full route
// patterns, so the parent delegates by prefix and the child matches each
// versioned route exactly.
const intakeServeRoutePrefix = "/api/v1/webhook/intake/"

// serveIntakeConfigured reports whether this binary serves core's
// authenticated versioned intake API (#9404): direct store-owning mode (no
// outward intake URL) with a configured intake token. The satellite keeps
// sole submitter authority in intake mode, so it never serves: serving
// without the Postgres-backed stores would fail every submission closed.
func serveIntakeConfigured(cfg webhookListenerConfig) bool {
	return strings.TrimSpace(cfg.IntakeURL) == "" && strings.TrimSpace(cfg.IntakeToken) != ""
}

// buildServedIntake returns the intake API server backed by the direct-mode
// Postgres stores, or nil when this binary does not serve it. An empty
// token fails closed inside intake.NewServerChecked: an intake API without
// authentication must never serve.
func buildServedIntake(cfg webhookListenerConfig, stores intake.Stores, logger *slog.Logger) (*intake.Server, error) {
	if !serveIntakeConfigured(cfg) {
		return nil, nil
	}
	return intake.NewServerChecked(intake.ServerConfig{
		Token:        strings.TrimSpace(cfg.IntakeToken),
		MaxBodyBytes: cfg.MaxRequestBodyBytes,
		Logger:       logger,
	}, stores)
}

// mountServedIntake registers the intake API subtree on the combined mux.
// A nil server is a no-op so direct mode without a token and intake
// (submitter) mode keep exactly their current route sets. The intake
// routes ride the same admin mux as the provider routes, so /healthz and
// /readyz cover the intake path on the same port.
func mountServedIntake(mux *http.ServeMux, server *intake.Server) {
	if mux == nil || server == nil {
		return
	}
	mux.Handle(intakeServeRoutePrefix, server.Mux())
}
