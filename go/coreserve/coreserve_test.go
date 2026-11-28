// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package coreserve_test proves the shared auth serving library computes
// the auth family in-process from fixture stores: GET /readyz answers 200
// with live fixture backends (and 503 when the graph verifier is unwired),
// one auth read (GET /api/v0/auth/providers) answers 200 with the
// fixture-derived body, and no request dials another API backend.
package coreserve_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/coreserve"
)

// pingOKDriver is a stdlib-only fake database/sql driver whose connections
// always answer PingContext successfully. It keeps the proof free of third
// party fixture dependencies while satisfying the Postgres half of the
// dual-gated /readyz probe (db.PingContext must return nil).
type pingOKDriver struct{}

func (pingOKDriver) Open(string) (driver.Conn, error) { return pingOKConn{}, nil }

// pingOKConn implements driver.Conn plus driver.Pinger so PingContext
// succeeds without preparing statements.
type pingOKConn struct{}

func (pingOKConn) Prepare(string) (driver.Stmt, error) { return pingOKStmt{}, nil }
func (pingOKConn) Close() error                        { return nil }
func (pingOKConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (pingOKConn) Ping(context.Context) error          { return nil }

// pingOKStmt is an unreachable stub: the ping path never prepares.
type pingOKStmt struct{}

func (pingOKStmt) Close() error                               { return nil }
func (pingOKStmt) NumInput() int                              { return -1 }
func (pingOKStmt) Exec([]driver.Value) (driver.Result, error) { return nil, driver.ErrSkip }
func (pingOKStmt) Query([]driver.Value) (driver.Rows, error)  { return nil, driver.ErrSkip }

func init() {
	sql.Register("coreservefixture", pingOKDriver{})
}

// fixtureProviderStore is the single auth fixture: one OIDC provider for
// any non-empty tenant, mirroring the auth-safe AuthProviderItem shape
// (opaque config ID, generic label, protocol kind — no secrets, domains,
// or metadata URLs).
type fixtureProviderStore struct{}

func (fixtureProviderStore) ListLoginProviders(_ context.Context, tenantID string) ([]coreserve.AuthProviderItem, error) {
	if tenantID == "" {
		return nil, nil
	}
	return []coreserve.AuthProviderItem{{
		ProviderConfigID: "fixture-oidc-1",
		DisplayLabel:     "Single sign-on (OIDC)",
		ProviderKind:     "oidc",
		IconHint:         "oidc",
	}}, nil
}

// guardTransport fails the test on ANY outbound HTTP dial, proving the
// served requests computed in-process with zero core-origin calls.
type guardTransport struct {
	t    *testing.T
	hits *atomic.Int64
}

func (g *guardTransport) RoundTrip(*http.Request) (*http.Response, error) {
	g.hits.Add(1)
	g.t.Error("served request made an outbound HTTP call: want zero core-origin calls")
	return nil, context.DeadlineExceeded
}

// installHTTPGuard swaps the process-wide transport for the test.
func installHTTPGuard(t *testing.T, hits *atomic.Int64) {
	t.Helper()
	prev := http.DefaultTransport
	http.DefaultTransport = &guardTransport{t: t, hits: hits}
	t.Cleanup(func() { http.DefaultTransport = prev })
}

// fixtureDB opens the ping-succeeding stub handle for the /readyz probe.
func fixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("coreservefixture", "")
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// serveFixtureMux builds the shared-library mux with fixture stores: a
// ping-OK database, the given GraphVerify, and the fixture auth provider
// store. Policy stays nil, which the handler documents as safe
// (local_login_offered defaults to true).
func serveFixtureMux(t *testing.T, verify func(context.Context) error) http.Handler {
	t.Helper()
	deps := coreserve.CoreServeDeps{
		DB:          fixtureDB(t),
		GraphVerify: verify,
		Auth: &coreserve.CoreAuthWiring{
			AuthProviders: &coreserve.AuthProviderListHandler{Store: fixtureProviderStore{}},
		},
	}
	mux, err := coreserve.NewCoreServeMux(deps)
	if err != nil {
		t.Fatalf("NewCoreServeMux: %v", err)
	}
	return mux
}

// TestReadyzAndAuthProvidersViaSharedLibrary is the #11618 acceptance: the
// shared library serves /readyz plus one auth read from fixture stores
// with zero core-origin HTTP calls.
func TestReadyzAndAuthProvidersViaSharedLibrary(t *testing.T) {
	var hits atomic.Int64
	installHTTPGuard(t, &hits)

	mux := serveFixtureMux(t, func(context.Context) error { return nil })

	ready := httptest.NewRecorder()
	mux.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("GET /readyz = %d, want 200 (body %s)", ready.Code, ready.Body.String())
	}
	var probe struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(ready.Body.Bytes(), &probe); err != nil {
		t.Fatalf("decode /readyz body: %v", err)
	}
	if probe.Status != "ok" {
		t.Fatalf("/readyz status = %q, want %q", probe.Status, "ok")
	}

	providers := httptest.NewRecorder()
	mux.ServeHTTP(providers, httptest.NewRequest(http.MethodGet, "/api/v0/auth/providers?tenant_id=t1", nil))
	if providers.Code != http.StatusOK {
		t.Fatalf("GET /api/v0/auth/providers = %d, want 200 (body %s)", providers.Code, providers.Body.String())
	}
	var body struct {
		Providers []struct {
			ProviderConfigID string `json:"provider_config_id"`
		} `json:"providers"`
		LocalLoginOffered bool `json:"local_login_offered"`
	}
	if err := json.Unmarshal(providers.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode providers body: %v", err)
	}
	if len(body.Providers) != 1 || body.Providers[0].ProviderConfigID != "fixture-oidc-1" {
		t.Fatalf("providers body = %s, want the single fixture provider", providers.Body.String())
	}
	if !body.LocalLoginOffered {
		t.Fatalf("local_login_offered = false, want true under the nil-policy default")
	}

	if got := hits.Load(); got != 0 {
		t.Fatalf("outbound HTTP calls = %d, want 0", got)
	}
}

// TestReadyzNegativeControl proves the /readyz gate was not stubbed away:
// with GraphVerify unwired the same fixture mux must report 503.
func TestReadyzNegativeControl(t *testing.T) {
	var hits atomic.Int64
	installHTTPGuard(t, &hits)

	mux := serveFixtureMux(t, nil)

	ready := httptest.NewRecorder()
	mux.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz with nil GraphVerify = %d, want 503 (body %s)", ready.Code, ready.Body.String())
	}

	if got := hits.Load(); got != 0 {
		t.Fatalf("outbound HTTP calls = %d, want 0", got)
	}
}
