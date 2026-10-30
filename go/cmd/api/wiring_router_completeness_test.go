// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestNewRouterWiresEveryFieldOrDocumentsWhyNot is the #5148 dual-main
// structural fix (issue review on #5150): a reflective sweep over every
// field of query.APIRouter AND every interface-typed field one level inside
// each wired handler, rather than one more hand-written
// `if router.X == nil` assertion. The #5143 regression this targets --
// Repositories.Freshness shipped wired on cmd/api but not cmd/mcp-server,
// 503ing get_repository_freshness on the standalone MCP binary -- was a
// nested field (RepositoryHandler.Freshness), not a missing top-level
// APIRouter field, so the sweep MUST go one level deep to actually catch
// that regression class; a top-level-only sweep was tried first and
// verified NOT to catch it. It was guarded afterward only by per-field
// assertions (see TestNewRouterMountsPostgresBackedHandlers in
// wiring_test.go and its cmd/mcp-server twin), which only catch a
// regression on a field someone remembered to assert. This test instead
// fails on any top-level nil pointer field or nested nil interface field
// that is not in routerFieldsNotWiredByNewRouter, so a new field --
// top-level or nested -- lands wired here or requires a reviewed,
// documented exclusion; it cannot silently ship missing on only one
// entrypoint.
//
// db is a real (never-dialed) *sql.DB and neo4jReader/contentReader are real
// reader instances, not nil, so store fields built from them (for example
// RepositoryHandler.Freshness, built from db) are genuinely exercised
// instead of trivially nil because the test passed nil dependencies.
//
// The removed MCP entrypoint used to carry the mirror-image test
// (TestNewMCPQueryRouterWiresEveryFieldOrDocumentsWhyNot) with its own
// exclusion list, since the two entrypoints were separate `main` packages
// and could not share a helper or call each other's constructors (removed
// under eshu-hq/eshu#9670; the satellite eshu-mcp serves that surface now).
func TestNewRouterWiresEveryFieldOrDocumentsWhyNot(t *testing.T) {
	t.Parallel()

	router := newFullyWiredTestRouter(t)

	assertRouterFieldsWired(t, router, routerFieldsNotWiredByNewRouter)
}

// TestNewRouterWiresCodeLogger is the #5761 P1-1 review-fix regression: the
// reflective sweep in assertRouterFieldsWired only checks nil *interface*
// fields (see its Kind() != reflect.Interface guard below), so a
// *slog.Logger field -- a concrete pointer type, not an interface -- is
// invisible to it. newFullyWiredTestRouter also always passes nil for the
// logger parameter, so router.Code.Logger is trivially nil there regardless
// of whether newRouter's `Logger: logger` line (wiring_router.go) exists.
// In production, LanguageQueryHandler's only construction path is
// CodeHandler.Mount forwarding CodeHandler.Logger -- the sole operator
// signal for a generic (non-bounded) language-query failure, since the 500
// response body stays static -- so a silently-dropped Logger field here
// means an operator gets nothing at 3 AM. This test passes a real,
// distinguishable logger through newRouter and asserts pointer identity
// against the constructed router.Code.Logger, catching a deleted or
// mis-wired `Logger: logger` line that the reflective sweep and
// newFullyWiredTestRouter both miss.
func TestNewRouterWiresCodeLogger(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("pgx", "postgres://example.invalid/eshu")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	router, err := newRouter(
		db,
		query.NewNeo4jReader(nil, ""),
		query.NewContentReader(db),
		staticStatusReader{},
		staticMetricsSource{},
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		logger,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
		false,
		query.CookieSecureAuto,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}

	if router.Code == nil {
		t.Fatal("newRouter().Code = nil, want wired")
	}
	if router.Code.Logger != logger {
		t.Errorf("newRouter().Code.Logger = %p, want the passed logger %p (a generic language-query failure would log to nowhere)", router.Code.Logger, logger)
	}
}

// newFullyWiredTestRouter builds a *query.APIRouter the same way
// TestNewRouterWiresEveryFieldOrDocumentsWhyNot does: a real (never-dialed)
// *sql.DB and real neo4jReader/contentReader instances, so every store field
// newRouter builds from them is genuinely constructed rather than trivially
// nil because the test passed nil dependencies. Shared with
// fact_kind_mounted_route_gate_test.go's mounted-route-parity gate (#5359),
// which needs the identical production wiring -- not a hand-picked subset of
// handlers -- to prove a fact-kind read_surface resolves on the router
// production traffic actually gets.
func newFullyWiredTestRouter(t *testing.T) *query.APIRouter {
	t.Helper()

	db, err := sql.Open("pgx", "postgres://example.invalid/eshu")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	neo4jReader := query.NewNeo4jReader(nil, "")
	contentReader := query.NewContentReader(db)

	router, err := newRouter(
		db,
		neo4jReader,
		contentReader,
		staticStatusReader{},
		staticMetricsSource{},
		query.ProfileLocalFullStack,
		query.GraphBackendNornicDB,
		nil,
		nil,
		"",
		"",
		component.Policy{},
		query.GovernanceStatusConfig{},
		nil,
		false,
		query.CookieSecureAuto,
	)
	if err != nil {
		t.Fatalf("newRouter() error = %v, want nil", err)
	}
	return router
}

// routerFieldsNotWiredByNewRouter documents every query.APIRouter top-level
// field, and every "TopLevelField.NestedInterfaceField" pair, that
// newRouter/newRouterWithSemanticEmbedding (wiring_router.go) intentionally
// leaves nil, with why. Top-level fields fall into two groups: fields wired
// later by the outer wireAPI (wiring.go) once a live provider-secret
// keyring, bootstrap mode, or OIDC/SAML provider resolver exists -- outside
// this constructor's scope -- and APIRouter.Ask, which this entrypoint
// never assigns (POST /api/v0/ask is mounted directly on the mux instead).
var routerFieldsNotWiredByNewRouter = map[string]string{
	"AdminDeadLetters":               "the API's Admin handler already covers dead-letter queries; no separate handler is wired on this entrypoint (a dedicated read-only one existed only on the MCP entrypoint removed under eshu-hq/eshu#9670)",
	"AdminInputInvalidFacts":         "the API's Admin handler already covers input-invalid-facts queries (AdminHandler.Mount, issue #4630); no separate handler is wired on this entrypoint (a dedicated read-only one existed only on the MCP entrypoint removed under eshu-hq/eshu#9670)",
	"AdminChangedSincePoisonedLinks": "the API's Admin handler already covers changed-since poisoned-links queries (AdminHandler.Mount, issue #7290); no separate handler is wired on this entrypoint (a dedicated read-only one existed only on the MCP entrypoint removed under eshu-hq/eshu#9670)",
	"Setup":                          "wired later by wireAPI once providerSecretKeyring/bootstrapMode exist",
	"OIDCLogin":                      "wired later by wireAPI once a live OIDC provider resolver exists",
	"SAML":                           "wired later by wireAPI once a live SAML provider resolver exists",
	"GitHubLogin":                    "wired later by wireAPI once a live GitHub provider resolver exists",
	"AuthProviders":                  "wired later by wireAPI, composed from OIDCLogin/SAML/GitHubLogin built after this constructor returns",
	"AdminProviderConfigReads":       "wired later by wireAPI",
	"AdminProviderConfigMutations":   "wired later by wireAPI",
	"SignInPolicyReads":              "wired later by wireAPI",
	"SignInPolicyMutations":          "wired later by wireAPI",
	"Ask":                            "POST /api/v0/ask is mounted directly on the mux by wireAPI/buildAskHandler; APIRouter.Ask is never assigned on either entrypoint",

	// Nested interface fields: gated on runtime config that this
	// constructor-level test does not (and should not) flip on, or wired
	// by wireAPI after this constructor returns. Verified against
	// go/cmd/api/semantic_search_vector_wiring.go and go/cmd/api/wiring.go.
	"Code.HybridRanker":          "config-gated: newCodeHybridRanker returns nil unless semantic search embedding is enabled (semantic_search_vector_wiring.go); nil is the documented default, falling back to lexical order",
	"Content.HybridRanker":       "config-gated: newContentHybridRanker returns nil unless semantic search embedding is enabled (semantic_search_vector_wiring.go); nil is the documented default, falling back to lexical order",
	"Impact.CodeSurface":         "injection seam: nil is the documented default, falling back to impact.DefaultCodeSurface (see codeSurface in go/internal/query/impact/handler.go); production always uses the package default",
	"Impact.TraceContext":        "injection seam: nil is the documented default, falling back to impact.DefaultTraceContext (see traceContext in go/internal/query/impact/handler.go); production always uses the package default",
	"Impact.PathProbe":           "injection seam: nil is the documented default, falling back to impact.DefaultPathProbe (see pathProbe in go/internal/query/impact/handler.go); production always uses the package default",
	"SemanticSearch.LocalHybrid": "config-gated: newSemanticSearchHybrid returns nil unless semantic search embedding is enabled (semantic_search_vector_wiring.go)",
	"LocalIdentity.SignInPolicy": "wired later by wireAPI (router.LocalIdentity.SignInPolicy = router.SignInPolicyReads.Store, built after this constructor returns); LocalIdentityHandler documents nil SignInPolicy as fail-open",
	"TagHistory.Cursors":         "wired later by wireAPI once providerSecretKeyring exists (router.TagHistory.Cursors = providerSecretKeyring, #6564); nil is the documented no-DEK deployment, where grant-filtered paging fails closed and unscoped paging is unaffected",
}

// assertRouterFieldsWired fails the test for:
//  1. every top-level pointer field of *query.APIRouter that is nil, and
//  2. every exported interface-typed field one level inside a non-nil
//     top-level handler that is nil (the reviewer's "nil interface field"
//     criterion on #5148's #5150 follow-up comment) --
//
// unless the field (or "TopField.NestedField" pair) is present in
// exceptions. This helper was duplicated in the removed MCP entrypoint's
// twin test while the two entrypoints were separate `main` packages that
// could not import a shared test helper (removed under eshu-hq/eshu#9670).
func assertRouterFieldsWired(t *testing.T, router *query.APIRouter, exceptions map[string]string) {
	t.Helper()
	top := reflect.ValueOf(router).Elem()
	topType := top.Type()
	for i := 0; i < topType.NumField(); i++ {
		name := topType.Field(i).Name
		fv := top.Field(i)
		if _, excluded := exceptions[name]; excluded {
			continue
		}
		if fv.IsNil() {
			t.Errorf("newRouter().%s = nil, want wired (add it to the struct literal, or if this entrypoint intentionally omits it, add a documented entry to routerFieldsNotWiredByNewRouter)", name)
			continue
		}
		elem := fv.Elem()
		if elem.Kind() != reflect.Struct {
			continue
		}
		elemType := elem.Type()
		for j := 0; j < elemType.NumField(); j++ {
			sub := elemType.Field(j)
			if !sub.IsExported() {
				continue
			}
			sv := elem.Field(j)
			if sv.Kind() != reflect.Interface {
				continue
			}
			key := name + "." + sub.Name
			if _, excluded := exceptions[key]; excluded {
				continue
			}
			if sv.IsNil() {
				t.Errorf("newRouter().%s = nil (interface field), want wired (add it to the struct literal, or if this entrypoint intentionally omits it, add a documented entry to routerFieldsNotWiredByNewRouter)", key)
			}
		}
	}
}

// staticMetricsSource is a trivial query.MetricsTimeSeriesSource stand-in so
// TestNewRouterWiresEveryFieldOrDocumentsWhyNot can assert
// MetricsHandler.Source is genuinely wired through from the metricsSource
// parameter, instead of documenting it as an exception because the test
// passed nil.
type staticMetricsSource struct{}

func (staticMetricsSource) RangeQuery(context.Context, query.MetricsRangeQuery) ([]query.MetricPoint, error) {
	return nil, nil
}
