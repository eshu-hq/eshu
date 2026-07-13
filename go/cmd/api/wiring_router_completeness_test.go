// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
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
// cmd/mcp-server/wiring_router_completeness_test.go carries the
// mirror-image test (TestNewMCPQueryRouterWiresEveryFieldOrDocumentsWhyNot)
// with its own exclusion list, since the two entrypoints are separate
// `main` packages and cannot share a helper or call each other's
// constructors.
func TestNewRouterWiresEveryFieldOrDocumentsWhyNot(t *testing.T) {
	t.Parallel()

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

	assertRouterFieldsWired(t, router, routerFieldsNotWiredByNewRouter)
}

// routerFieldsNotWiredByNewRouter documents every query.APIRouter top-level
// field, and every "TopLevelField.NestedInterfaceField" pair, that
// newRouter/newRouterWithSemanticEmbedding (wiring_router.go) intentionally
// leaves nil, with why. Top-level fields fall into two groups: fields wired
// later by the outer wireAPI (wiring.go) once a live provider-secret
// keyring, bootstrap mode, or OIDC/SAML provider resolver exists -- outside
// this constructor's scope -- and APIRouter.Ask, which neither entrypoint
// ever assigns (POST /api/v0/ask is mounted directly on the mux instead).
var routerFieldsNotWiredByNewRouter = map[string]string{
	"AdminDeadLetters":             "MCP-only: the API's Admin handler already covers dead-letter queries; cmd/mcp-server has no Admin handler so it gets a dedicated read-only AdminDeadLetters handler instead",
	"Setup":                        "wired later by wireAPI once providerSecretKeyring/bootstrapMode exist",
	"OIDCLogin":                    "wired later by wireAPI once a live OIDC provider resolver exists",
	"SAML":                         "wired later by wireAPI once a live SAML provider resolver exists",
	"AuthProviders":                "wired later by wireAPI, composed from OIDCLogin/SAML built after this constructor returns",
	"AdminProviderConfigReads":     "wired later by wireAPI",
	"AdminProviderConfigMutations": "wired later by wireAPI",
	"SignInPolicyReads":            "wired later by wireAPI",
	"SignInPolicyMutations":        "wired later by wireAPI",
	"Ask":                          "POST /api/v0/ask is mounted directly on the mux by wireAPI/buildAskHandler; APIRouter.Ask is never assigned on either entrypoint",

	// Nested interface fields: gated on runtime config that this
	// constructor-level test does not (and should not) flip on, or wired
	// by wireAPI after this constructor returns. Verified against
	// go/cmd/api/semantic_search_vector_wiring.go and go/cmd/api/wiring.go.
	"Code.HybridRanker":          "config-gated: newCodeHybridRanker returns nil unless semantic search embedding is enabled (semantic_search_vector_wiring.go); nil is the documented default, falling back to lexical order",
	"Content.HybridRanker":       "config-gated: newContentHybridRanker returns nil unless semantic search embedding is enabled (semantic_search_vector_wiring.go); nil is the documented default, falling back to lexical order",
	"SemanticSearch.LocalHybrid": "config-gated: newSemanticSearchHybrid returns nil unless semantic search embedding is enabled (semantic_search_vector_wiring.go)",
	"LocalIdentity.SignInPolicy": "wired later by wireAPI (router.LocalIdentity.SignInPolicy = router.SignInPolicyReads.Store, built after this constructor returns); LocalIdentityHandler documents nil SignInPolicy as fail-open",
}

// assertRouterFieldsWired fails the test for:
//  1. every top-level pointer field of *query.APIRouter that is nil, and
//  2. every exported interface-typed field one level inside a non-nil
//     top-level handler that is nil (the reviewer's "nil interface field"
//     criterion on #5148's #5150 follow-up comment) --
//
// unless the field (or "TopField.NestedField" pair) is present in
// exceptions. Duplicated in
// cmd/mcp-server/wiring_router_completeness_test.go: the two entrypoints
// are separate `main` packages and cannot import a shared test helper.
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
