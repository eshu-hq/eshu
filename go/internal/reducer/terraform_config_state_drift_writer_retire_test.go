// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// fakeTerraformDriftFactStore is a faithful (not merely call-recording)
// in-memory stand-in for the fact_records table, scoped to exactly the two
// statement shapes PostgresTerraformConfigStateDriftWriter issues: the
// shared batched versioned-fact insert (upsert-by-fact_id) and this domain's
// generation-authoritative retire (delete-by-anti-join). It genuinely
// applies both, so a test can assert what actually "remains" after a
// sequence of writes -- the same question a live Postgres instance would
// answer -- without needing a live Postgres instance.
type fakeTerraformDriftFactStore struct {
	t    *testing.T
	rows map[string]decodedBatchedVersionedFactRow // keyed by FactID
}

func newFakeTerraformDriftFactStore(t *testing.T) *fakeTerraformDriftFactStore {
	t.Helper()
	return &fakeTerraformDriftFactStore{t: t, rows: map[string]decodedBatchedVersionedFactRow{}}
}

func (f *fakeTerraformDriftFactStore) ExecContext(
	_ context.Context, query string, args ...any,
) (sql.Result, error) {
	switch query {
	case reducerFactBatchInsertVersionedQuery:
		call := fakeWorkloadIdentityExecCall{query: query, args: args}
		for _, row := range decodeBatchedVersionedFactCall(f.t, call) {
			f.rows[row.FactID] = row
		}
		return fakeWorkloadIdentityResult{}, nil
	case terraformConfigStateDriftRetireQuery:
		if len(args) != 4 {
			return nil, fmt.Errorf("retire call args = %d, want 4", len(args))
		}
		factKind, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("retire fact_kind arg type = %T, want string", args[0])
		}
		scopeID, ok := args[1].(string)
		if !ok {
			return nil, fmt.Errorf("retire scope_id arg type = %T, want string", args[1])
		}
		generationID, ok := args[2].(string)
		if !ok {
			return nil, fmt.Errorf("retire generation_id arg type = %T, want string", args[2])
		}
		keepArr, ok := args[3].(pgarray.StringArray)
		if !ok {
			return nil, fmt.Errorf("retire keep_fact_ids arg type = %T, want pgarray.StringArray", args[3])
		}
		keep := map[string]bool{}
		for _, id := range keepArr {
			keep[id] = true
		}
		for id, row := range f.rows {
			if row.FactKind == factKind && row.ScopeID == scopeID && row.GenerationID == generationID && !keep[id] {
				delete(f.rows, id)
			}
		}
		return fakeWorkloadIdentityResult{}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

// TestPostgresTerraformConfigStateDriftWriterRetiresStaleUnresolvedOnResolve
// is the direct replay proof for the review finding on #5594 (P1, following
// the #5848 generation-authoritative-retire precedent): write an
// "unresolved" finding for one (scope_id, generation_id), then rewrite the
// SAME (scope_id, generation_id) as resolved with real "exact" drift
// findings -- the scenario where an earlier pass could not resolve
// ownership but a later pass (the config repo synced in, or bootstrap-index
// re-ran) does. Exactly one row must remain, and it must carry the new
// resolved outcome; the stale "unresolved" row must not survive alongside
// it.
func TestPostgresTerraformConfigStateDriftWriterRetiresStaleUnresolvedOnResolve(t *testing.T) {
	t.Parallel()

	store := newFakeTerraformDriftFactStore(t)
	writer := PostgresTerraformConfigStateDriftWriter{DB: store}
	const scopeID = "state_snapshot:local:hash-1"
	const generationID = "generation-1"

	// Pass 1: ownership never resolves -- writes one durable "unresolved" row.
	if _, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), TerraformConfigStateDriftWrite{
		IntentID: "intent-1", ScopeID: scopeID, GenerationID: generationID,
		SourceSystem: "collector/terraform-state", BackendKind: "local", LocatorHash: "hash-1",
		UnresolvedOwner: true,
	}); err != nil {
		t.Fatalf("pass 1 (unresolved) error = %v", err)
	}
	if got, want := len(store.rows), 1; got != want {
		t.Fatalf("after pass 1: len(store.rows) = %d, want %d", got, want)
	}

	// Pass 2: the SAME (scope_id, generation_id) now resolves, and drifts.
	if _, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), TerraformConfigStateDriftWrite{
		IntentID: "intent-2", ScopeID: scopeID, GenerationID: generationID,
		SourceSystem: "collector/terraform-state", BackendKind: "local", LocatorHash: "hash-1",
		Candidates: []model.Candidate{exactDriftCandidate("aws_s3_bucket.x", "added_in_state")},
	}); err != nil {
		t.Fatalf("pass 2 (resolved) error = %v", err)
	}

	if got, want := len(store.rows), 1; got != want {
		t.Fatalf("after pass 2: len(store.rows) = %d, want %d (the stale unresolved row must not survive)", got, want)
	}
	var remaining decodedBatchedVersionedFactRow
	for _, row := range store.rows {
		remaining = row
	}
	var decoded reducerderivedv1.TerraformConfigStateDriftFinding
	if err := json.Unmarshal(remaining.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal remaining row payload: %v", err)
	}
	if decoded.Outcome != "exact" {
		t.Fatalf("remaining row Outcome = %q, want %q", decoded.Outcome, "exact")
	}
	if decoded.Address != "aws_s3_bucket.x" {
		t.Fatalf("remaining row Address = %q, want %q", decoded.Address, "aws_s3_bucket.x")
	}
}

// TestPostgresTerraformConfigStateDriftWriterRetiresStaleExactOnUnresolve is
// the symmetric direction: a scope that previously resolved with real drift
// findings later becomes unresolvable again (e.g. the owning repo's active
// generation rotated out from under this state-snapshot generation). The
// stale "exact" rows must not survive alongside the new "unresolved" row.
func TestPostgresTerraformConfigStateDriftWriterRetiresStaleExactOnUnresolve(t *testing.T) {
	t.Parallel()

	store := newFakeTerraformDriftFactStore(t)
	writer := PostgresTerraformConfigStateDriftWriter{DB: store}
	const scopeID = "state_snapshot:s3:hash-2"
	const generationID = "generation-2"

	if _, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), TerraformConfigStateDriftWrite{
		IntentID: "intent-1", ScopeID: scopeID, GenerationID: generationID,
		SourceSystem: "collector/terraform-state", BackendKind: "s3", LocatorHash: "hash-2",
		Candidates: []model.Candidate{
			exactDriftCandidate("aws_s3_bucket.a", "added_in_state"),
			exactDriftCandidate("aws_iam_role.b", "added_in_config"),
		},
	}); err != nil {
		t.Fatalf("pass 1 (resolved) error = %v", err)
	}
	if got, want := len(store.rows), 2; got != want {
		t.Fatalf("after pass 1: len(store.rows) = %d, want %d", got, want)
	}

	if _, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), TerraformConfigStateDriftWrite{
		IntentID: "intent-2", ScopeID: scopeID, GenerationID: generationID,
		SourceSystem: "collector/terraform-state", BackendKind: "s3", LocatorHash: "hash-2",
		UnresolvedOwner: true,
	}); err != nil {
		t.Fatalf("pass 2 (unresolved) error = %v", err)
	}

	if got, want := len(store.rows), 1; got != want {
		t.Fatalf("after pass 2: len(store.rows) = %d, want %d (both stale exact rows must retire)", got, want)
	}
	var remaining decodedBatchedVersionedFactRow
	for _, row := range store.rows {
		remaining = row
	}
	var decoded reducerderivedv1.TerraformConfigStateDriftFinding
	if err := json.Unmarshal(remaining.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal remaining row payload: %v", err)
	}
	if decoded.Outcome != "unresolved" {
		t.Fatalf("remaining row Outcome = %q, want %q", decoded.Outcome, "unresolved")
	}
}

// TestPostgresTerraformConfigStateDriftWriterRetireLeavesOtherGenerationsAlone
// proves the retire is scoped to (scope_id, generation_id), not scope_id
// alone: a prior generation's durable finding for a DIFFERENT generation_id
// under the same scope must survive a later generation's write untouched.
func TestPostgresTerraformConfigStateDriftWriterRetireLeavesOtherGenerationsAlone(t *testing.T) {
	t.Parallel()

	store := newFakeTerraformDriftFactStore(t)
	writer := PostgresTerraformConfigStateDriftWriter{DB: store}
	const scopeID = "state_snapshot:s3:hash-3"

	if _, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), TerraformConfigStateDriftWrite{
		IntentID: "intent-gen1", ScopeID: scopeID, GenerationID: "generation-1",
		SourceSystem: "collector/terraform-state", BackendKind: "s3", LocatorHash: "hash-3",
		UnresolvedOwner: true,
	}); err != nil {
		t.Fatalf("generation-1 write error = %v", err)
	}
	if _, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), TerraformConfigStateDriftWrite{
		IntentID: "intent-gen2", ScopeID: scopeID, GenerationID: "generation-2",
		SourceSystem: "collector/terraform-state", BackendKind: "s3", LocatorHash: "hash-3",
		Candidates: []model.Candidate{exactDriftCandidate("aws_s3_bucket.x", "added_in_state")},
	}); err != nil {
		t.Fatalf("generation-2 write error = %v", err)
	}

	if got, want := len(store.rows), 2; got != want {
		t.Fatalf("len(store.rows) = %d, want %d (one per generation, neither retired)", got, want)
	}
}
