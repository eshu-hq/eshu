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

// cloudInventoryAWSAccountAliasPayloadRow returns one canonical
// reducer_cloud_resource_identity fact payload for a synthetic AWS scope, in
// the readback envelope shape buildCloudInventoryIdentitiesSQL projects.
func cloudInventoryAWSAccountAliasPayloadRow(t *testing.T, scopeID, uid string) []byte {
	t.Helper()
	row := map[string]any{
		"fact_id":       "reducer_cloud_resource_identity:" + uid,
		"fact_kind":     "reducer_cloud_resource_identity",
		"scope_id":      scopeID,
		"generation_id": "gen-1",
		"source_system": "aws_cloud_inventory",
		"observed_at":   "2026-06-09T00:00:00Z",
		"payload": map[string]any{
			"cloud_resource_uid":    uid,
			"provider":              "aws",
			"resource_type":         "aws_s3_bucket",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"scope_id":              scopeID,
		},
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal cloud inventory readback row: %v", err)
	}
	return encoded
}

// TestBuildCloudInventoryIdentitiesSQLAccountAliasJoinsScopeMetadata is the
// #5238 regression: an account_id/project_id/subscription_id alias must
// resolve against the owning scope's raw provider metadata
// (ingestion_scopes.payload), never against fact_records.scope_id itself.
// Every provider's scope_id is a derived, opaque per-shard identifier (for
// AWS: one shard per account+region+service partition -- see
// go/internal/collector/awscloud/awsruntime/source.go) that is never
// literally equal to the raw account/project/subscription number, so
// comparing the alias value straight against scope_id silently matches zero
// rows for a real multi-shard account.
func TestBuildCloudInventoryIdentitiesSQLAccountAliasJoinsScopeMetadata(t *testing.T) {
	t.Parallel()

	query, args := buildCloudInventoryIdentitiesSQL(cloudInventoryFilter{
		Provider:          "aws",
		AccountAliasKey:   "account_id",
		AccountAliasValue: "111111111111",
		Limit:             50,
	})

	if !strings.Contains(query, "scope.payload->>'account_id' = $2") {
		t.Fatalf("query must filter on scope.payload->>'account_id', got:\n%s", query)
	}
	if strings.Contains(query, "fact_records.scope_id = $2") {
		t.Fatalf("query must NOT compare the account_id value directly to fact_records.scope_id, got:\n%s", query)
	}
	found := false
	for _, arg := range args {
		if s, ok := arg.(string); ok && s == "111111111111" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bound args %#v did not include the account alias value", args)
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
	if strings.Contains(query, "scope.payload->>") {
		t.Fatalf("exact scope_id filter must not add an account-alias metadata join, got:\n%s", query)
	}
	if len(args) == 0 || args[0] != "aws:cloud:111111111111:us-east-1:s3" {
		t.Fatalf("bound args = %#v, want first arg to be the exact scope id", args)
	}
}

// TestCloudInventoryHandlerAccountIDDispatchesMetadataJoinNotLiteralScopeMatch
// proves the dispatched SQL and bound args the HTTP handler actually sends to
// Postgres for ?account_id=... use the scope-metadata join, and that the
// response echoes the filter back under "account_id" rather than mislabeling
// a raw account number as "scope_id".
func TestCloudInventoryHandlerAccountIDDispatchesMetadataJoinNotLiteralScopeMatch(t *testing.T) {
	t.Parallel()

	scopeID := "aws:cloud:111111111111:us-east-1:s3"
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: [][]driver.Value{
			{cloudInventoryAWSAccountAliasPayloadRow(t, scopeID, "aws:s3:bucket-1")},
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
	if !strings.Contains(dispatched, "scope.payload->>'account_id' = $") {
		t.Fatalf("dispatched query missing account-alias metadata join:\n%s", dispatched)
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
