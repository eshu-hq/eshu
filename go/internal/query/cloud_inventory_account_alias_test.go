// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cloudInventoryAccountAliasPayloadRow returns one canonical
// reducer_cloud_resource_identity fact payload carrying the normalized
// "account_id" field the reducer writes for every provider (see
// go/internal/reducer/cloud_inventory_admission_writer.go), in the readback
// envelope shape buildCloudInventoryIdentitiesSQL projects.
func cloudInventoryAccountAliasPayloadRow(t *testing.T, provider, scopeID, uid, accountID string) []byte {
	t.Helper()
	row := map[string]any{
		"fact_id":       "reducer_cloud_resource_identity:" + uid,
		"fact_kind":     "reducer_cloud_resource_identity",
		"scope_id":      scopeID,
		"generation_id": "gen-1",
		"source_system": provider + "_cloud_inventory",
		"observed_at":   "2026-06-09T00:00:00Z",
		"payload": map[string]any{
			"cloud_resource_uid":    uid,
			"provider":              provider,
			"resource_type":         "resource_type",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"scope_id":              scopeID,
			"account_id":            accountID,
		},
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal cloud inventory readback row: %v", err)
	}
	return encoded
}

// TestBuildCloudInventoryIdentitiesSQLAccountAliasMatchesCanonicalPayload is
// the #5238 regression: an account_id/project_id/subscription_id alias must
// resolve against the canonical payload's normalized "account_id" field
// (which the reducer populates from the resolving provider source fact's own
// identity -- aws_resource.account_id, gcp_cloud_resource.project_id,
// azure_cloud_resource.subscription_id), never against fact_records.scope_id.
// Every provider's scope_id is a derived, opaque per-shard identifier (for
// AWS: one shard per account+region+service partition -- see
// go/internal/collector/awscloud/awsruntime/source.go) that is never
// literally equal to the raw account/project/subscription number, so
// comparing the alias value straight against scope_id silently matched zero
// rows for a real multi-shard account, on every provider.
func TestBuildCloudInventoryIdentitiesSQLAccountAliasMatchesCanonicalPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		aliasKey  string
		provider  string
		accountID string
	}{
		{"aws account_id", "account_id", "aws", "111111111111"},
		{"gcp project_id", "project_id", "gcp", "eshu-prod"},
		{"azure subscription_id", "subscription_id", "azure", "11111111-2222-3333-4444-555555555555"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			query, args := buildCloudInventoryIdentitiesSQL(cloudInventoryFilter{
				Provider:          tc.provider,
				AccountAliasKey:   tc.aliasKey,
				AccountAliasValue: tc.accountID,
				Limit:             50,
			})

			if !strings.Contains(query, "fact_records.payload->>'account_id' = $2") {
				t.Fatalf("query must filter on fact_records.payload->>'account_id', got:\n%s", query)
			}
			if strings.Contains(query, "fact_records.scope_id = $2") {
				t.Fatalf("query must NOT compare the alias value directly to fact_records.scope_id, got:\n%s", query)
			}
			found := false
			for _, arg := range args {
				if s, ok := arg.(string); ok && s == tc.accountID {
					found = true
				}
			}
			if !found {
				t.Fatalf("bound args %#v did not include the account alias value %q", args, tc.accountID)
			}
		})
	}
}

// TestBuildCloudInventoryIdentitiesSQLExactScopeIDStillMatchesLiterally is the
// no-regression counterpart: an explicit scope_id selector keeps its existing
// exact-match behavior against fact_records.scope_id, unaffected by the
// account-alias resolution path.
func TestBuildCloudInventoryIdentitiesSQLExactScopeIDStillMatchesLiterally(t *testing.T) {
	t.Parallel()

	query, args := buildCloudInventoryIdentitiesSQL(cloudInventoryFilter{
		ScopeID: "aws:cloud:111111111111:us-east-1:s3",
		Limit:   50,
	})

	if !strings.Contains(query, "fact_records.scope_id = $1 OR fact_records.payload->>'scope_id' = $1") {
		t.Fatalf("exact scope_id filter must keep literal scope_id matching, got:\n%s", query)
	}
	if strings.Contains(query, "payload->>'account_id'") {
		t.Fatalf("exact scope_id filter must not add an account-alias predicate, got:\n%s", query)
	}
	if len(args) == 0 || args[0] != "aws:cloud:111111111111:us-east-1:s3" {
		t.Fatalf("bound args = %#v, want first arg to be the exact scope id", args)
	}
}

// TestBuildCloudInventoryIdentitiesSQLScopeIDTakesPrecedenceOverAccountAlias
// is the P2 precedence proof: when both scope_id and an account alias are
// somehow present on the filter (the HTTP layer only ever sets one, per
// cloudInventoryScopeSelector, but the SQL builder is the actual contract
// the readback depends on), scope_id wins outright and no account-alias
// predicate is added.
func TestBuildCloudInventoryIdentitiesSQLScopeIDTakesPrecedenceOverAccountAlias(t *testing.T) {
	t.Parallel()

	query, args := buildCloudInventoryIdentitiesSQL(cloudInventoryFilter{
		AllScopes:         true,
		ScopeID:           "aws:cloud:111111111111:us-east-1:s3",
		AccountAliasKey:   "account_id",
		AccountAliasValue: "111111111111",
		Limit:             50,
	})
	if !strings.Contains(query, "fact_records.scope_id = $1 OR fact_records.payload->>'scope_id' = $1") {
		t.Fatalf("scope_id must still win when both selectors are set, got:\n%s", query)
	}
	if strings.Contains(query, "payload->>'account_id'") {
		t.Fatalf("account-alias predicate must be dropped once scope_id wins, got:\n%s", query)
	}
	// AllScopes:true skips the #5167 access-scoping clause, so with scope_id
	// as the only filter the bound args are exactly [scope_id, limit+1, offset]
	// -- 3 total. A 4th arg would mean the account alias also bound a
	// parameter it should not have.
	if len(args) != 3 {
		t.Fatalf("bound args = %#v, want exactly 3 (scope_id, limit, offset) -- account alias must not also bind", args)
	}
}

// TestCloudInventoryHandlerAccountIDDispatchesCanonicalPayloadMatch proves the
// dispatched SQL and bound args the HTTP handler actually sends to Postgres
// for ?account_id=... filter on the canonical payload's account_id field, and
// that the response echoes the filter back under "account_id" rather than
// mislabeling a raw account number as "scope_id".
func TestCloudInventoryHandlerAccountIDDispatchesCanonicalPayloadMatch(t *testing.T) {
	t.Parallel()

	scopeID := "aws:cloud:111111111111:us-east-1:s3"
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: [][]driver.Value{
			{cloudInventoryAccountAliasPayloadRow(t, "aws", scopeID, "aws:s3:bucket-1", "111111111111")},
		}},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=aws&account_id=111111111111", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("Postgres received %d queries, want exactly %d", got, want)
	}
	dispatched := recorder.queries[0]
	if !strings.Contains(dispatched, "fact_records.payload->>'account_id' = $") {
		t.Fatalf("dispatched query missing account-alias predicate:\n%s", dispatched)
	}
	found := false
	for _, arg := range recorder.args[0] {
		if s := fmt.Sprintf("%v", arg); s == "111111111111" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bound args %#v did not include the account_id value", recorder.args[0])
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	scope := data["scope"].(map[string]any)
	if got, want := scope["account_id"], "111111111111"; got != want {
		t.Fatalf(`scope["account_id"] = %#v, want %#v`, got, want)
	}
	if _, present := scope["scope_id"]; present {
		t.Fatalf(`scope["scope_id"] must not be set when the request used account_id, got %#v`, scope)
	}
}

// TestCloudInventoryHandlerGCPProjectIDAndAzureSubscriptionIDDispatchCanonicalPayloadMatch
// is the per-provider regression demanded by review: GCP and Azure were left
// broken by the first pass of this fix (their scope Metadata never carries
// project_id/subscription_id, so a scope-metadata join would have stayed
// permanently vacuous for them). This proves both now dispatch the same
// canonical-payload predicate AWS uses, since the reducer normalizes all
// three providers' identity fields onto one "account_id" payload key.
func TestCloudInventoryHandlerGCPProjectIDAndAzureSubscriptionIDDispatchCanonicalPayloadMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		provider  string
		aliasKey  string
		accountID string
	}{
		{"gcp project_id", "gcp", "project_id", "eshu-prod"},
		{"azure subscription_id", "azure", "subscription_id", "11111111-2222-3333-4444-555555555555"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scopeID := "cloud-scope:" + tc.provider + ":synthetic"
			db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
				{columns: []string{"payload"}, rows: [][]driver.Value{
					{cloudInventoryAccountAliasPayloadRow(t, tc.provider, scopeID, tc.provider+":resource-1", tc.accountID)},
				}},
			})
			handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v0/cloud/inventory?provider="+tc.provider+"&"+tc.aliasKey+"="+tc.accountID,
				nil,
			)
			req.Header.Set("Accept", EnvelopeMIMEType)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			dispatched := recorder.queries[0]
			if !strings.Contains(dispatched, "fact_records.payload->>'account_id' = $") {
				t.Fatalf("%s: dispatched query missing account-alias predicate:\n%s", tc.name, dispatched)
			}

			var resp ResponseEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal() error = %v, want nil", err)
			}
			data := resp.Data.(map[string]any)
			resources := data["resources"].([]any)
			if len(resources) != 1 {
				t.Fatalf("%s: len(resources) = %d, want 1; body = %s", tc.name, len(resources), w.Body.String())
			}
			scope := data["scope"].(map[string]any)
			if got, want := scope[tc.aliasKey], tc.accountID; got != want {
				t.Fatalf(`%s: scope[%q] = %#v, want %#v`, tc.name, tc.aliasKey, got, want)
			}
		})
	}
}

// TestCloudInventoryHandlerScopedTokenAccountAliasExcludesUngrantedSiblingScope
// is the P2 tenant-boundary proof: a scoped caller (AllowedScopeIDs limited to
// one of two same-account scopes) combined with an account_id filter must
// still only see the granted scope's row -- the #5167 access-scoping
// predicate stays unconditionally ANDed regardless of which selector branch
// (scope_id vs account alias) built the account/scope predicate.
func TestCloudInventoryHandlerScopedTokenAccountAliasExcludesUngrantedSiblingScope(t *testing.T) {
	t.Parallel()

	grantedScope := "aws:cloud:111111111111:us-east-1:s3"
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		// The fake driver returns whatever row is queued regardless of the
		// predicate text; this test's job is to prove the DISPATCHED SQL and
		// bound args carry BOTH the account-alias predicate and the scoped
		// access-control predicate (ANDed), which is what actually enforces
		// the boundary against live Postgres.
		{columns: []string{"payload"}, rows: [][]driver.Value{
			{cloudInventoryAccountAliasPayloadRow(t, "aws", grantedScope, "aws:s3:bucket-granted", "111111111111")},
		}},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=aws&account_id=111111111111", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		AllowedScopeIDs: []string{grantedScope},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("Postgres received %d queries, want exactly %d", got, want)
	}
	dispatched := recorder.queries[0]
	if !strings.Contains(dispatched, "fact_records.payload->>'account_id' = $") {
		t.Fatalf("dispatched query missing account-alias predicate:\n%s", dispatched)
	}
	if !strings.Contains(dispatched, "fact_records.scope_id = ANY(") {
		t.Fatalf("dispatched query missing #5167 access-scoping predicate alongside the account alias:\n%s", dispatched)
	}
	foundAccount, foundGrant := false, false
	for _, arg := range recorder.args[0] {
		s := fmt.Sprintf("%v", arg)
		if s == "111111111111" {
			foundAccount = true
		}
		if strings.Contains(s, grantedScope) {
			foundGrant = true
		}
	}
	if !foundAccount {
		t.Fatalf("bound args %#v did not include the account_id value", recorder.args[0])
	}
	if !foundGrant {
		t.Fatalf("bound args %#v did not include the granted scope id -- the account alias and the access grant must both bind", recorder.args[0])
	}
}
