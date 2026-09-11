// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// This file duplicates a handful of small #5167 code-family grant-test
// fixtures that both package query (auth_scoped_language_query_grant_test.go
// and its siblings, which test LanguageQueryHandler and stay here) and
// package codequery (the CodeHandler-family grant tests, moved for #6060)
// need. A _test.go symbol is not importable across a package boundary, and
// these are small enough -- two repository id constants, one route-request
// builder, one grant predicate, one column list, one envelope decoder -- that
// duplicating them is cheaper and less risky than promoting them to a shared
// importable package. The canonical copies live in
// codequery/auth_scoped_code_topic_grant_test.go (codeGrantGrantedRepo,
// codeGrantOtherRepo, newCodeGrantRouteRequest),
// codequery/auth_scoped_code_content_grant_test.go (codeContentGrantAdmits),
// codequery/auth_scoped_code_graph_rows_grant_test.go
// (repositoryProjectedColumns), and codequery/dead_code_investigation_test.go
// (decodeEnvelopeData). Keep both copies in lockstep by hand.

// codeGrantGrantedRepo and codeGrantOtherRepo are canonical repository ids
// (the repo:// form queryselector.LooksCanonicalRepositoryID recognises), so
// a route that takes a repository selector resolves them through the grant
// rather than through a catalog or graph lookup the fakes do not implement.
//
// The values live in querytestutil (#6642): package language's own grant
// tests need the identical ids and a _test.go symbol is not importable
// across a package boundary. This stays a const so every existing call site
// in this package (93+ files) compiles unchanged.
const (
	codeGrantGrantedRepo = querytestutil.CodeGrantGrantedRepo
	codeGrantOtherRepo   = querytestutil.CodeGrantOtherRepo
)

// codeGrantConsumerRepo is a second repository inside the caller's grant, so
// the cross-repo consumer tests can tell "dropped because ungranted" apart
// from "dropped because it is the producer".
const codeGrantConsumerRepo = "repo://tenant-a/consumer-service"

// newCodeGrantRouteRequest builds a POST request against a scoped-token code
// route, carrying auth as the request's AuthContext when non-nil (nil means
// an unscoped shared-key caller).
func newCodeGrantRouteRequest(t *testing.T, path string, body map[string]any, auth *AuthContext) *http.Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal(body) error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Accept", EnvelopeMIMEType)
	if auth != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
	}
	return req
}

// codeContentGrantAdmits's canonical declaration is
// auth_scoped_code_content_grant_test.go in this same package (it moved back
// to package query at the #6060 move: it tests
// content_reader_security_secrets.go, content_reader_symbol_search.go, and
// content_reader_structural_inventory.go, which stayed here).
// codequery/auth_scoped_code_dead_code_grant_test.go keeps its own copy.

// repositoryProjectedColumns are the columns an OPTIONAL MATCH nulls when its
// pattern (including its WHERE) does not match.
func repositoryProjectedColumns() []string {
	return []string{"file_path", "repo_id", "repo_name"}
}

// limitEntityContent truncates rows to limit. Canonical copy:
// codequery/search_authz_test.go.
func limitEntityContent(rows []EntityContent, limit int) []EntityContent {
	if limit > 0 && limit < len(rows) {
		return append([]EntityContent(nil), rows[:limit]...)
	}
	return append([]EntityContent(nil), rows...)
}

// decodeEnvelopeData unmarshals a {data: ...} envelope response body.
func decodeEnvelopeData(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", resp["data"])
	}
	return data
}

// codeGrantScopeOnlyAuthContext grants exactly one git repository ingestion
// scope and no canonical repository id at all -- the shape a token gets when
// the operator granted the scope the collector created. Canonical copy:
// codequery/auth_scoped_code_scope_grant_test.go.
func codeGrantScopeOnlyAuthContext(repoIDs []string) AuthContext {
	scopeIDs := make([]string, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		scopeIDs = append(scopeIDs, "git-repository-scope:"+repoID)
	}
	return AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: scopeIDs,
	}
}

// assertCrossRepoDeadCodeBucketMissing fails if buckets[name] contains
// entityID. Canonical copy: codequery/dead_code_cross_repo_review_test.go.
func assertCrossRepoDeadCodeBucketMissing(t *testing.T, buckets map[string]any, name string, entityID string) {
	t.Helper()

	rawRows, ok := buckets[name].([]any)
	if !ok {
		t.Fatalf("candidate_buckets[%s] type = %T, want []any", name, buckets[name])
	}
	for _, raw := range rawRows {
		row := raw.(map[string]any)
		if row["entity_id"] == entityID {
			t.Fatalf("candidate_buckets[%s] unexpectedly contains entity %q: %#v", name, entityID, row)
		}
	}
}

// crossRepoDeadCodeEvidenceColumns are the columns the consumer-evidence SQL
// projects, in order. Canonical copy:
// codequery/auth_scoped_code_dead_code_cross_repo_grant_test.go.
func crossRepoDeadCodeEvidenceColumns() []string {
	return []string{
		"entity_id", "repository_id", "consumer_repo_name", "root_entity_id", "depth",
		"state", "confidence", "min_resolution_method", "evidence", "root_kinds",
		"generation_id", "generation_status", "observed_at", "updated_at",
	}
}

// assertCrossRepoDeadCodeBucketEntity returns buckets[name]'s row for
// entityID, failing if absent. Canonical copy:
// codequery/dead_code_cross_repo_test.go.
func assertCrossRepoDeadCodeBucketEntity(t *testing.T, buckets map[string]any, name string, entityID string) map[string]any {
	t.Helper()

	rawRows, ok := buckets[name].([]any)
	if !ok {
		t.Fatalf("candidate_buckets[%s] type = %T, want []any", name, buckets[name])
	}
	for _, raw := range rawRows {
		row := raw.(map[string]any)
		if row["entity_id"] == entityID {
			return row
		}
	}
	t.Fatalf("candidate_buckets[%s] missing entity %q: %#v", name, entityID, rawRows)
	return nil
}
