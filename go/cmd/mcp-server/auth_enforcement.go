// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// authEnforcementConfigured reports whether a headerless request to a
// non-public read route must be denied (some explicit credential source is
// configured) rather than served open in demo dev-mode.
//
// It is true when at least one of the three EXPLICIT operator credential
// knobs is set: the shared key (ESHU_API_KEY, a persisted key, or an
// auto-generated one — all folded into apiKey by
// internalruntime.ResolveAPIKey), the scoped-token file
// (ESHU_SCOPED_TOKENS_FILE, non-nil fileResolver), or the OIDC bearer
// audience (ESHU_AUTH_RESOURCE_URI, non-nil oidcResolver).
//
// It deliberately EXCLUDES the always-wired Postgres identity resolver, which
// is constructed unconditionally whenever Postgres is available (i.e. always,
// since Postgres is a hard boot requirement). Counting it would make this
// constant-true and 401 the documented demo-open read surface — the exact
// break the first fix attempt caused. Seeded bootstrap identities (#4962/#4963)
// are likewise not a signal: the demo seeds them by default, and the console
// consumes them through the self-enforcing browser-session cookie path. The
// residual risk (a deployment whose only credentials are DB-resident) is
// accepted, visible via logAuthEnforcementPosture, and closable with any one
// of the three env vars.
//
// This is definitionally the same predicate as #5168's
// credentialSourceConfigured (F-7, cmd/mcp-server/wiring.go).
// TODO(#5168-follow-up): consolidate this and cmd/api's identical expression
// into one shared helper once F-7 merges; kept per-package now to avoid a
// cross-branch build dependency.
func authEnforcementConfigured(
	apiKey string,
	fileResolver query.ScopedTokenResolver,
	oidcResolver query.ScopedTokenResolver,
) bool {
	return apiKey != "" || fileResolver != nil || oidcResolver != nil
}

// logAuthEnforcementPosture records, once at wiring time, whether headerless
// read requests are enforced or served open. This is the 3 AM operator signal
// for the residual DB-only-credential open-reads risk; denied requests
// additionally emit governance-audit read-authorization-denied events at
// request time.
func logAuthEnforcementPosture(logger *slog.Logger, enforced bool) {
	if logger == nil {
		return
	}
	if enforced {
		logger.Info(
			"headerless reads require authentication (a credential source is configured)",
			telemetry.EventAttr("auth.enforcement.configured"),
		)
		return
	}
	logger.Warn(
		"headerless reads are served open; set ESHU_API_KEY, ESHU_SCOPED_TOKENS_FILE, "+
			"or ESHU_AUTH_RESOURCE_URI to require authentication",
		telemetry.EventAttr("auth.enforcement.open"),
	)
}
