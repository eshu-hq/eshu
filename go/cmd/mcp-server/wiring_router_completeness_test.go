// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestNewMCPQueryRouterWiresEveryFieldOrDocumentsWhyNot is the #5148
// dual-main structural fix (issue review on #5150), mirroring
// cmd/api/wiring_router_completeness_test.go's
// TestNewRouterWiresEveryFieldOrDocumentsWhyNot: a reflective sweep over
// every field of query.APIRouter AND every interface-typed field one level
// inside each wired handler, rather than one more hand-written
// `if router.X == nil` assertion. The #5143 regression this targets --
// Repositories.Freshness shipped wired on cmd/api but not cmd/mcp-server,
// 503ing get_repository_freshness on the standalone MCP binary -- was a
// nested field (RepositoryHandler.Freshness), not a missing top-level
// APIRouter field, so the sweep MUST go one level deep to actually catch
// that regression class. It was guarded afterward only by per-field
// assertions (see TestNewMCPQueryRouterMountsMCPBackedHandlers in
// wiring_test.go), which only catch a regression on a field someone
// remembered to assert. This test instead fails on any top-level nil
// pointer field or nested nil interface field that is not in
// routerFieldsNotWiredByNewMCPQueryRouter, so a new field -- top-level or
// nested -- lands wired here or requires a reviewed, documented exclusion.
//
// neo4jReader/contentReader are real reader instances, not nil, so store
// fields built from them are genuinely exercised instead of trivially nil
// because the test passed nil dependencies.
//
// This cannot share a helper or call cmd/api's constructor: the two
// entrypoints are separate `main` packages.
func TestNewMCPQueryRouterWiresEveryFieldOrDocumentsWhyNot(t *testing.T) {
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

	router := newMCPQueryRouter(
		db,
		neo4jReader,
		contentReader,
		staticStatusReader{},
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
	)

	assertRouterFieldsWired(t, router, routerFieldsNotWiredByNewMCPQueryRouter)
}

// routerFieldsNotWiredByNewMCPQueryRouter documents every query.APIRouter
// top-level field, and every "TopLevelField.NestedInterfaceField" pair,
// that newMCPQueryRouter/newMCPQueryRouterWithSemanticEmbedding
// (wiring.go) intentionally leaves nil, with why: the standalone MCP server
// has no browser-session identity, admin mutation, SSO, or sign-in-policy
// surface, and no HTTP-only routes (Images, Dependencies, Metrics,
// GraphEntityInventory) that lack an MCP tool equivalent. APIRouter.Ask is
// never assigned on either entrypoint (POST /api/v0/ask is mounted directly
// on the mux instead).
var routerFieldsNotWiredByNewMCPQueryRouter = map[string]string{
	"GraphEntityInventory":         "API-only: graph entity inventory browsing has no MCP tool",
	"Images":                       "API-only: the image handler backs an HTTP-only route with no MCP tool",
	"Dependencies":                 "API-only: the dependencies handler backs an HTTP-only route with no MCP tool",
	"Metrics":                      "API-only: the time-series metrics endpoint has no MCP tool",
	"Admin":                        "the MCP server has no admin mutation surface (see the router.Admin != nil check above)",
	"LocalIdentity":                "the MCP server has no browser-session identity surface",
	"BrowserSessions":              "the MCP server has no browser-session identity surface",
	"SessionList":                  "the MCP server has no browser-session identity surface",
	"AdminIdentityReads":           "the MCP server has no admin identity surface",
	"AdminIdentityMutations":       "the MCP server has no admin identity surface",
	"Profile":                      "the MCP server has no first-party account profile surface",
	"Ask":                          "POST /api/v0/ask is mounted directly on the mux by wiring_ask.go; APIRouter.Ask is never assigned on either entrypoint",
	"Setup":                        "the MCP server has no first-run setup wizard surface",
	"OIDCLogin":                    "the MCP server has no SSO login surface",
	"SAML":                         "the MCP server has no SSO login surface",
	"AuthProviders":                "the MCP server has no SSO login surface",
	"AdminProviderConfigReads":     "the MCP server has no SSO admin config surface",
	"AdminProviderConfigMutations": "the MCP server has no SSO admin config surface",
	"SignInPolicyReads":            "the MCP server has no sign-in policy surface",
	"SignInPolicyMutations":        "the MCP server has no sign-in policy surface",

	// Nested interface fields: gated on runtime config that this
	// constructor-level test does not (and should not) flip on. Verified
	// against go/cmd/mcp-server/wiring.go's use of
	// newCodeHybridRanker/newContentHybridRanker/newSemanticSearchHybrid,
	// which mirror cmd/api's semantic_search_vector_wiring.go behavior.
	"Code.HybridRanker":          "config-gated: newCodeHybridRanker returns nil unless semantic search embedding is enabled; nil is the documented default, falling back to lexical order",
	"Content.HybridRanker":       "config-gated: newContentHybridRanker returns nil unless semantic search embedding is enabled; nil is the documented default, falling back to lexical order",
	"SemanticSearch.LocalHybrid": "config-gated: newSemanticSearchHybrid returns nil unless semantic search embedding is enabled",

	// Status.LiveActivity backs GET /api/v0/status/operations, the live
	// operations board (#5137/#5140). Commit f33333b5ab's own message scopes
	// it to "status,console" and states the telemetry is "wired through
	// cmd/api"; the console (an cmd/api-only operator UI) is its only
	// consumer and no MCP tool wraps this route, unlike Repositories.
	// Freshness (get_repository_freshness) or Incident.Authorizer
	// (get_incident_context) above. Nil here 503s the route on the
	// standalone MCP server exactly as StatusHandler's doc comment
	// describes, with no MCP tool affected.
	"Status.LiveActivity": "API/console-only: the live operations board has no MCP tool; #5137/#5140 wired it through cmd/api only",
}

// assertRouterFieldsWired fails the test for:
//  1. every top-level pointer field of *query.APIRouter that is nil, and
//  2. every exported interface-typed field one level inside a non-nil
//     top-level handler that is nil (the reviewer's "nil interface field"
//     criterion on #5148's #5150 follow-up comment) --
//
// unless the field (or "TopField.NestedField" pair) is present in
// exceptions. Duplicated in cmd/api/wiring_router_completeness_test.go: the
// two entrypoints are separate `main` packages and cannot import a shared
// test helper.
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
			t.Errorf("newMCPQueryRouter().%s = nil, want wired (add it to the struct literal, or if this entrypoint intentionally omits it, add a documented entry to routerFieldsNotWiredByNewMCPQueryRouter)", name)
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
				t.Errorf("newMCPQueryRouter().%s = nil (interface field), want wired (add it to the struct literal, or if this entrypoint intentionally omits it, add a documented entry to routerFieldsNotWiredByNewMCPQueryRouter)", key)
			}
		}
	}
}
