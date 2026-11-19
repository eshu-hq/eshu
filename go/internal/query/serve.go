// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/status"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// errPostgresNotConfigured and errGraphNotConfigured are the /readyz causes
// when the process was built without that backend handle.
var (
	errPostgresNotConfigured = errors.New("postgres: handle not configured")
	errGraphNotConfigured    = errors.New("graph: driver not configured")
)

// Core readiness timeouts. The probe answers whether the process can serve
// traffic, not whether a full query round-trip would finish, so each
// dependency check stays far below the satellite relay bound (30 s).
const (
	coreReadinessProbeTimeout = 2 * time.Second
	coreReadinessServiceName  = "eshu-core-api"
)

// CoreServeDeps carries everything NewCoreServeMux needs to build the
// serving mux. Store handles (DB, Graph) come from the thin main's store
// wiring; every handler, route, and middleware decision lives here in
// package query. Nil stores fail closed at request time (503 through the
// handler's own not-configured path), never at construction: the frozen
// served-method test builds the full mux with nil stores and probes route
// registration without executing handlers.
type CoreServeDeps struct {
	// DB is the Postgres writer handle. Required in production; nil in tests.
	DB *sql.DB
	// Graph is the concurrent-safe read-only graph traversal surface,
	// built by the binary over the pinned Neo4j driver. Required in
	// production; nil in tests (handlers fail closed, readiness is
	// not-ready).
	Graph GraphQuery
	// GraphVerify checks pinned-Neo4j connectivity for /readyz. Nil in
	// tests (readiness then reports not-ready).
	GraphVerify func(context.Context) error
	// GraphDriven reports whether the graph reader has a live driver
	// behind it (always true in production; false in tests).
	GraphDriven bool
	// Profile is the query runtime profile (production in deployment).
	Profile QueryProfile
	// APIKey is the shared bearer credential. Empty means headerless reads
	// stay open only when no other auth source is configured (see
	// AuthEnforcementConfigured below).
	APIKey string
	// ScopedTokenResolver chains the Postgres identity, OIDC bearer, and
	// scoped-token file resolvers. Nil disables scoped-token resolution.
	ScopedTokenResolver ScopedTokenResolver
	// BrowserSessionResolver resolves server-managed dashboard sessions.
	// Nil disables the cookie path.
	BrowserSessionResolver BrowserSessionResolver
	// AuthEnforcementConfigured mirrors cmd wiring: true when at least one
	// explicit operator credential source (shared key, scoped-token file,
	// OIDC bearer audience) is set, so headerless requests are denied.
	AuthEnforcementConfigured bool
	// Governance carries the hosted-governance posture for authz decisions.
	Governance GovernanceStatusConfig
	// GovernanceAudit appends denied/allowed read-authorization events.
	GovernanceAudit GovernanceAuditAppender
	// ComponentHome and ComponentPolicy back the component-extensions reads.
	ComponentHome   string
	ComponentPolicy component.Policy
	// GraphBackend names the configured graph backend (neo4j in deployment).
	GraphBackend GraphBackend
	// CookieSecure selects the Secure attribute mode for session cookies.
	CookieSecure CookieSecureMode
	// BootstrapMode selects the first-run setup wizard posture.
	BootstrapMode string
	// Getenv reads operator configuration for sub-wiring (metrics targets,
	// SSM bounds, semantic embedder). Nil reads as empty.
	Getenv func(string) string
	// Instruments carries the shared OTEL meter provider. Nil disables emission.
	Instruments *telemetry.Instruments
	// Logger is the process structured logger. Nil falls back to slog.Default.
	Logger *slog.Logger
	// ExtraMounts registers routes that cannot live in package query without
	// an import cycle (the CLI-support handlers and the service-intelligence
	// report import query). They mount on the bare mux before the auth chain
	// wraps it, so they share the exact middleware as every query route.
	// Each extra sees the built router for handler reuse.
	ExtraMounts []func(*http.ServeMux, *APIRouter)
	// NarrationPosture resolves the governed answer-narration posture for
	// the status endpoint. Nil leaves the endpoint's static posture.
	NarrationPosture func() status.AnswerNarrationStatus
	// Auth carries the binary-built auth-family handlers. The binary builds
	// them in cmd/core-api because secret-sealed material must never cross
	// package query (see secretcrypto_open_boundary_test.go). Nil mounts
	// empty fail-closed handlers (tests).
	Auth *CoreAuthWiring
}

// CoreAuthWiring bundles the binary-built auth-family handlers. The binary
// builds them in cmd/core-api (never package query) because several need
// secret-sealed material. Every field is optional: a nil entry mounts an
// empty fail-closed handler for its routes.
type CoreAuthWiring struct {
	LocalIdentity                *LocalIdentityHandler
	BrowserSessions              *BrowserSessionHandler
	SessionList                  *BrowserSessionListHandler
	AdminIdentityReads           *AdminIdentityReadHandler
	AdminIdentityMutations       *AdminIdentityMutationHandler
	Profile                      *ProfileHandler
	SignInPolicyReads            *SignInPolicyReadHandler
	SignInPolicyMutations        *SignInPolicyMutationHandler
	Setup                        *SetupHandler
	OIDCLogin                    *OIDCLoginHandler
	SAML                         *SAMLHandler
	GitHubLogin                  *GitHubLoginHandler
	AuthProviders                *AuthProviderListHandler
	AdminProviderConfigReads     *AdminProviderConfigReadHandler
	AdminProviderConfigMutations *AdminProviderConfigMutationHandler
}

// AuthEnforcementConfigured reports whether headerless requests to
// non-public routes must be denied: true when at least one explicit
// operator credential source is configured.
func AuthEnforcementConfigured(apiKey string, fileResolver, oidcResolver ScopedTokenResolver) bool {
	return apiKey != "" || fileResolver != nil || oidcResolver != nil
}

// coreLogger returns the deployment logger or the process default.
func coreLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.Default()
}

// readinessCheckResult is the JSON body of the backend-gated /readyz probe.
type readinessCheckResult struct {
	Service string `json:"service"`
	Probe   string `json:"probe"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// writeReadiness writes the /readyz verdict: 200 only when every dependency
// check passes, otherwise the permanent 503 with no Retry-After. The 503 is
// permanent (never retryable): a caller must fix the backend, not retry.
func writeReadiness(w http.ResponseWriter, service string, err error) {
	body := readinessCheckResult{Service: service, Probe: "readyz", Status: "ok"}
	status := http.StatusOK
	if err != nil {
		status = http.StatusServiceUnavailable
		body.Status = "error"
		body.Error = err.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// checkCoreReadiness pings Postgres and verifies pinned-Neo4j connectivity,
// each under its own bounded timeout. Both must answer: a process-liveness
// 200 without live backends is non-compliant (step-2 ruling decision 5).
func checkCoreReadiness(ctx context.Context, db *sql.DB, verifyGraph func(context.Context) error) error {
	type outcome struct {
		name string
		err  error
	}
	results := make(chan outcome, 2)
	go func() {
		var err error
		if db == nil {
			err = errPostgresNotConfigured
		} else {
			pingCtx, cancel := context.WithTimeout(ctx, coreReadinessProbeTimeout)
			defer cancel()
			err = db.PingContext(pingCtx)
		}
		results <- outcome{name: "postgres", err: err}
	}()
	go func() {
		var err error
		if verifyGraph == nil {
			err = errGraphNotConfigured
		} else {
			verifyCtx, cancel := context.WithTimeout(ctx, coreReadinessProbeTimeout)
			defer cancel()
			err = verifyGraph(verifyCtx)
		}
		results <- outcome{name: "graph", err: err}
	}()
	var first error
	for range 2 {
		if got := <-results; got.err != nil && first == nil {
			first = got.err
		}
	}
	return first
}

// mountCoreReadiness registers the backend-gated /readyz probe. It stays
// public (publicHTTPPaths already bypasses auth for /readyz) and never sets
// Retry-After on either verdict.
func mountCoreReadiness(mux *http.ServeMux, db *sql.DB, verifyGraph func(context.Context) error) {
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		writeReadiness(w, coreReadinessServiceName, checkCoreReadiness(r.Context(), db, verifyGraph))
	})
}

// MountCoreMux registers the full serving surface on a bare mux: every
// APIRouter route plus the backend-gated /readyz probe. /health comes from
// APIRouter.Mount (shallow process check). No auth here: NewCoreServeMux
// wraps the result in the middleware chain, and the frozen served-method
// test probes this bare mux directly.
func MountCoreMux(mux *http.ServeMux, router *APIRouter, db *sql.DB, verifyGraph func(context.Context) error) {
	router.Mount(mux)
	mountCoreReadiness(mux, db, verifyGraph)
}

// WrapCoreAuth wraps the mounted mux in the production middleware chain:
// shared-key, scoped-token, and browser-session resolution with the
// deployment enforcement predicate, governance audit, and the governance
// route policy.
func WrapCoreAuth(next http.Handler, deps CoreServeDeps) http.Handler {
	return AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement(
		deps.APIKey,
		deps.ScopedTokenResolver,
		deps.BrowserSessionResolver,
		next,
		deps.GovernanceAudit,
		ScopedRoutePolicyForGovernanceMode(deps.Governance),
		deps.AuthEnforcementConfigured,
	)
}

// NewCoreServeMux builds the production serving mux: full handler wiring,
// extra cycle-forced mounts, the backend-gated probe, and the auth chain.
// The returned handler answers the whole frozen served-method set from
// stores directly; it never dials another API backend (no-loop invariant).
func NewCoreServeMux(deps CoreServeDeps) (http.Handler, error) {
	router, err := BuildCoreRouter(deps)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	MountCoreMux(mux, router, deps.DB, deps.GraphVerify)
	for _, extra := range deps.ExtraMounts {
		if extra != nil {
			extra(mux, router)
		}
	}
	return WrapCoreAuth(mux, deps), nil
}

// NewCoreTestMux builds the bare serving mux (no auth, no live backends)
// for the frozen served-method test. Every handler is non-nil so every
// frozen path registers; stores stay nil because the test probes mux
// registration (405 on bogus method) without executing handlers. The ask
// engine lives in the binary (import cycle), so the test mounts the
// default-off handler itself, mirroring main's extras.
func NewCoreTestMux() (*http.ServeMux, error) {
	router, err := BuildCoreRouter(CoreServeDeps{Profile: ProfileProduction})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	MountCoreMux(mux, router, nil, nil)
	(&AskHandler{}).Mount(mux)
	return mux, nil
}
