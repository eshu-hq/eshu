// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfconfigstate

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

func exactDriftCandidate(address, driftKind string) model.Candidate {
	return model.Candidate{
		ID:             "drift:hash-1:" + address + ":" + driftKind,
		Kind:           rules.TerraformConfigStateDriftPackName,
		CorrelationKey: address,
		Confidence:     1,
		State:          model.CandidateStateAdmitted,
		Evidence: []model.EvidenceAtom{
			{
				ID:           "drift:hash-1:" + address + ":" + driftKind + "/drift_kind",
				SourceSystem: "reducer/terraform_config_state_drift", EvidenceType: "terraform_drift_kind",
				ScopeID: "state_snapshot:s3:hash-1", Key: "drift_kind", Value: driftKind, Confidence: 1,
			},
		},
	}
}

// TestPostgresTerraformConfigStateDriftWriterPersistsOneFactPerFinding proves
// the writer persists one durable "exact" fact per admitted candidate through
// the shared reducerBatchInsertVersionedFacts bulk-insert path, with the
// governed schema version and a byte-identical typed payload.
func TestPostgresTerraformConfigStateDriftWriterPersistsOneFactPerFinding(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresTerraformConfigStateDriftWriter{DB: db}

	write := TerraformConfigStateDriftWrite{
		IntentID:     "intent-drift-1",
		ScopeID:      "state_snapshot:s3:hash-1",
		GenerationID: "generation-drift-1",
		SourceSystem: "collector/terraform-state",
		Cause:        "drift intent",
		BackendKind:  "s3",
		LocatorHash:  "hash-1",
		Candidates: []model.Candidate{
			exactDriftCandidate("aws_s3_bucket.added_state", "added_in_state"),
			exactDriftCandidate("aws_iam_role.added_config", "added_in_config"),
		},
	}

	result, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteTerraformConfigStateDriftFindings() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("ExecContext calls = %d, want %d (batched insert + generation-authoritative retire)", got, want)
	}
	assertTerraformConfigStateDriftRetireCall(t, db.execs[1], write.ScopeID, write.GenerationID)

	rows := decodeBatchedVersionedFactCalls(t, db.execs[:1])
	if got, want := len(rows), 2; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}
	for i, candidate := range write.Candidates {
		row := rows[i]
		if got, want := row.FactKind, terraformConfigStateDriftFactKind; got != want {
			t.Fatalf("row %d FactKind = %q, want %q", i, got, want)
		}
		if got, want := row.SchemaVersion, facts.ReducerDerivedSchemaVersionV1; got != want {
			t.Fatalf("row %d SchemaVersion = %q, want %q", i, got, want)
		}

		var decoded reducerderivedv1.TerraformConfigStateDriftFinding
		if err := json.Unmarshal([]byte(row.Payload), &decoded); err != nil {
			t.Fatalf("row %d unmarshal payload: %v", i, err)
		}
		if decoded.Outcome != "exact" {
			t.Fatalf("row %d Outcome = %q, want %q", i, decoded.Outcome, "exact")
		}
		if decoded.Address != candidate.CorrelationKey {
			t.Fatalf("row %d Address = %q, want %q", i, decoded.Address, candidate.CorrelationKey)
		}
		if decoded.DriftKind == "" {
			t.Fatalf("row %d DriftKind = empty, want non-empty", i)
		}
		if len(decoded.AmbiguousOwnerCandidates) != 0 {
			t.Fatalf("row %d AmbiguousOwnerCandidates = %v, want empty for an exact row", i, decoded.AmbiguousOwnerCandidates)
		}
		if decoded.BackendKind != "s3" || decoded.LocatorHash != "hash-1" {
			t.Fatalf("row %d BackendKind/LocatorHash = %q/%q, want s3/hash-1", i, decoded.BackendKind, decoded.LocatorHash)
		}
	}
}

// TestPostgresTerraformConfigStateDriftWriterIsIdempotentAcrossReplays proves
// writing the same admitted candidate twice within one scope+generation
// produces the same fact_id and stable_fact_key, matching the ON CONFLICT
// (fact_id) DO UPDATE upsert contract every other governed reducer-derived
// writer relies on for replay safety.
func TestPostgresTerraformConfigStateDriftWriterIsIdempotentAcrossReplays(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresTerraformConfigStateDriftWriter{DB: db}
	write := TerraformConfigStateDriftWrite{
		IntentID: "intent-1", ScopeID: "state_snapshot:s3:hash-1", GenerationID: "generation-1",
		SourceSystem: "collector/terraform-state", BackendKind: "s3", LocatorHash: "hash-1",
		Candidates: []model.Candidate{exactDriftCandidate("aws_s3_bucket.x", "added_in_state")},
	}

	if _, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), write); err != nil {
		t.Fatalf("first write error = %v", err)
	}
	if _, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), write); err != nil {
		t.Fatalf("second write error = %v", err)
	}

	// Each write call is [insert, retire]; take just the two insert calls.
	if len(db.execs) != 4 {
		t.Fatalf("ExecContext calls = %d, want 4 (2 write calls x [insert, retire])", len(db.execs))
	}
	rows := decodeBatchedVersionedFactCalls(t, []fakeWorkloadIdentityExecCall{db.execs[0], db.execs[2]})
	if len(rows) != 2 {
		t.Fatalf("decoded rows = %d, want 2 (one per replay call)", len(rows))
	}
	if rows[0].FactID != rows[1].FactID {
		t.Fatalf("FactID drifted across replays: %q vs %q", rows[0].FactID, rows[1].FactID)
	}
	if rows[0].StableFactKey != rows[1].StableFactKey {
		t.Fatalf("StableFactKey drifted across replays: %q vs %q", rows[0].StableFactKey, rows[1].StableFactKey)
	}
}

// TestPostgresTerraformConfigStateDriftWriterPersistsAmbiguousOwnerFinding
// proves the ambiguous-owner path writes exactly one durable "ambiguous" row
// carrying every competing repo's identity, with Address/DriftKind empty
// (no anchor was resolved to classify against).
func TestPostgresTerraformConfigStateDriftWriterPersistsAmbiguousOwnerFinding(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresTerraformConfigStateDriftWriter{DB: db}

	write := TerraformConfigStateDriftWrite{
		IntentID: "intent-ambiguous-1", ScopeID: "state_snapshot:s3:hash-1", GenerationID: "generation-1",
		SourceSystem: "collector/terraform-state", BackendKind: "s3", LocatorHash: "hash-1",
		AmbiguousOwners: []tfstatebackend.TerraformBackendRow{
			{RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa", BackendKind: "s3", LocatorHash: "hash-1"},
			{RepoID: "repo-b", ScopeID: "repo:repo-b@1", CommitID: "bbb", BackendKind: "s3", LocatorHash: "hash-1"},
		},
	}

	result, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteTerraformConfigStateDriftFindings() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d (one scope-level ambiguous row)", got, want)
	}
	assertTerraformConfigStateDriftRetireCall(t, db.execs[1], write.ScopeID, write.GenerationID)

	rows := decodeBatchedVersionedFactCalls(t, db.execs[:1])
	if len(rows) != 1 {
		t.Fatalf("decoded rows = %d, want 1", len(rows))
	}
	var decoded reducerderivedv1.TerraformConfigStateDriftFinding
	if err := json.Unmarshal([]byte(rows[0].Payload), &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.Outcome != "ambiguous" {
		t.Fatalf("Outcome = %q, want %q", decoded.Outcome, "ambiguous")
	}
	if decoded.Address != "" || decoded.DriftKind != "" {
		t.Fatalf("Address/DriftKind = %q/%q, want both empty for an ambiguous row", decoded.Address, decoded.DriftKind)
	}
	if len(decoded.AmbiguousOwnerCandidates) != 2 {
		t.Fatalf("len(AmbiguousOwnerCandidates) = %d, want 2 (no winner picked)", len(decoded.AmbiguousOwnerCandidates))
	}
}

// TestPostgresTerraformConfigStateDriftWriterPersistsUnresolvedOwnerFinding
// proves the no-owner rejection (tfstatebackend.ErrNoConfigRepoOwnsBackend) is
// recorded as one durable, provenance-only "unresolved" finding per
// state-snapshot scope, so a caller reading this scope's findings can tell
// "evaluated, no drift" (an empty page) apart from "could not resolve
// ownership at all" (one unresolved row) instead of both looking identical
// (issue #5594 follow-up). Address/DriftKind stay empty, matching the
// ambiguous row's shape: no anchor was resolved to classify against.
// AmbiguousOwnerCandidates stays empty too -- unlike the ambiguous case there
// is no competing evidence to record, only the absence of any owner.
func TestPostgresTerraformConfigStateDriftWriterPersistsUnresolvedOwnerFinding(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresTerraformConfigStateDriftWriter{DB: db}

	write := TerraformConfigStateDriftWrite{
		IntentID: "intent-unresolved-1", ScopeID: "state_snapshot:local:hash-2", GenerationID: "generation-1",
		SourceSystem: "collector/terraform-state", BackendKind: "local", LocatorHash: "hash-2",
		UnresolvedOwner: true,
	}

	result, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteTerraformConfigStateDriftFindings() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d (one scope-level unresolved row)", got, want)
	}
	assertTerraformConfigStateDriftRetireCall(t, db.execs[1], write.ScopeID, write.GenerationID)

	rows := decodeBatchedVersionedFactCalls(t, db.execs[:1])
	if len(rows) != 1 {
		t.Fatalf("decoded rows = %d, want 1", len(rows))
	}
	var decoded reducerderivedv1.TerraformConfigStateDriftFinding
	if err := json.Unmarshal([]byte(rows[0].Payload), &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.Outcome != "unresolved" {
		t.Fatalf("Outcome = %q, want %q", decoded.Outcome, "unresolved")
	}
	if decoded.Address != "" || decoded.DriftKind != "" {
		t.Fatalf("Address/DriftKind = %q/%q, want both empty for an unresolved row", decoded.Address, decoded.DriftKind)
	}
	if len(decoded.AmbiguousOwnerCandidates) != 0 {
		t.Fatalf("len(AmbiguousOwnerCandidates) = %d, want 0 (no competing evidence for an unresolved row)", len(decoded.AmbiguousOwnerCandidates))
	}
	if decoded.BackendKind != "local" || decoded.LocatorHash != "hash-2" {
		t.Fatalf("BackendKind/LocatorHash = %q/%q, want local/hash-2", decoded.BackendKind, decoded.LocatorHash)
	}
}

// TestWriteTerraformConfigStateDriftFindingsRejectsMultipleWriteModes proves
// the writer refuses a request that sets more than one of
// Candidates/AmbiguousOwners/UnresolvedOwner, extending the existing
// Candidates+AmbiguousOwners guard to the new three-way split.
func TestWriteTerraformConfigStateDriftFindingsRejectsMultipleWriteModes(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresTerraformConfigStateDriftWriter{DB: db}

	_, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), TerraformConfigStateDriftWrite{
		IntentID: "intent-bad", ScopeID: "state_snapshot:s3:hash-1", GenerationID: "generation-1",
		AmbiguousOwners: []tfstatebackend.TerraformBackendRow{{RepoID: "repo-a"}},
		UnresolvedOwner: true,
	})
	if err == nil {
		t.Fatal("WriteTerraformConfigStateDriftFindings() error = nil, want non-nil for AmbiguousOwners + UnresolvedOwner both set")
	}
}

// TestWriteTerraformConfigStateDriftFindingsBoundedExecCount proves N
// candidates are persisted in O(N/batchSize) bulk inserts rather than one
// ExecContext per candidate, mirroring the AWS/multi-cloud runtime drift
// writers' bounded-exec-count proof. This is the write-path performance
// contract for issue #5442: there is no prior baseline to compare against
// (the domain was never durable before this change), so the proof is the
// O(N/batchSize) round-trip bound itself, not a before/after delta.
func TestWriteTerraformConfigStateDriftFindingsBoundedExecCount(t *testing.T) {
	t.Parallel()

	const candidateCount = 1500
	candidates := make([]model.Candidate, candidateCount)
	for i := range candidates {
		address := fmt.Sprintf("aws_s3_bucket.bucket_%d", i)
		candidates[i] = exactDriftCandidate(address, "added_in_state")
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresTerraformConfigStateDriftWriter{DB: db}

	result, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), TerraformConfigStateDriftWrite{
		IntentID: "intent-drift-batch", ScopeID: "state_snapshot:s3:hash-1", GenerationID: "generation-batch",
		SourceSystem: "collector/terraform-state", BackendKind: "s3", LocatorHash: "hash-1",
		Candidates: candidates,
	})
	if err != nil {
		t.Fatalf("WriteTerraformConfigStateDriftFindings() error = %v", err)
	}
	if got, want := result.CanonicalWrites, candidateCount; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	// +1 for the trailing generation-authoritative retire call, on top of the
	// bounded batched-insert count.
	wantExecs := expectedBatchedExecCount(candidateCount) + 1
	if got := len(db.execs); got != wantExecs {
		t.Fatalf("ExecContext calls = %d for %d candidates, want %d (bounded batched inserts + 1 retire)", got, candidateCount, wantExecs)
	}
	insertExecs := db.execs[:len(db.execs)-1]
	if rows := decodeBatchedVersionedFactCalls(t, insertExecs); len(rows) != candidateCount {
		t.Fatalf("decoded rows = %d, want %d", len(rows), candidateCount)
	}
	assertTerraformConfigStateDriftRetireCall(t, db.execs[len(db.execs)-1], "state_snapshot:s3:hash-1", "generation-batch")
}

// assertTerraformConfigStateDriftRetireCall proves one ExecContext call is
// the generation-authoritative retire (terraformConfigStateDriftRetireQuery)
// scoped to the expected (scope_id, generation_id).
func assertTerraformConfigStateDriftRetireCall(t *testing.T, call fakeWorkloadIdentityExecCall, wantScopeID, wantGenerationID string) {
	t.Helper()
	if call.query != terraformConfigStateDriftRetireQuery {
		t.Fatalf("retire call query = %q, want the generation-authoritative retire query", call.query)
	}
	if len(call.args) != 4 {
		t.Fatalf("retire call args = %d, want 4 (fact_kind, scope_id, generation_id, keep_fact_ids)", len(call.args))
	}
	if got, want := call.args[0], terraformConfigStateDriftFactKind; got != want {
		t.Fatalf("retire call fact_kind = %v, want %v", got, want)
	}
	if got, want := call.args[1], wantScopeID; got != want {
		t.Fatalf("retire call scope_id = %v, want %v", got, want)
	}
	if got, want := call.args[2], wantGenerationID; got != want {
		t.Fatalf("retire call generation_id = %v, want %v", got, want)
	}
}

// BenchmarkWriteTerraformConfigStateDriftFindings measures the write-path
// cost this issue adds to the reducer. There is no prior baseline (the
// domain emitted no durable write before #5442); this benchmark is the
// starting measurement future changes to this writer should be compared
// against, run via:
//
//	go test ./internal/reducer -run '^$' -bench BenchmarkWriteTerraformConfigStateDriftFindings -benchtime=200x
func BenchmarkWriteTerraformConfigStateDriftFindings(b *testing.B) {
	candidates := make([]model.Candidate, 500)
	for i := range candidates {
		candidates[i] = exactDriftCandidate(fmt.Sprintf("aws_s3_bucket.bucket_%d", i), "added_in_state")
	}
	write := TerraformConfigStateDriftWrite{
		IntentID: "intent-bench", ScopeID: "state_snapshot:s3:hash-1", GenerationID: "generation-bench",
		SourceSystem: "collector/terraform-state", BackendKind: "s3", LocatorHash: "hash-1",
		Candidates: candidates,
	}
	writer := PostgresTerraformConfigStateDriftWriter{DB: &fakeWorkloadIdentityExecer{}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), write); err != nil {
			b.Fatalf("WriteTerraformConfigStateDriftFindings() error = %v", err)
		}
	}
}

// TestPostgresTerraformConfigStateDriftWriterRequiresDatabase mirrors every
// other reducer-derived writer's nil-database guard.
func TestPostgresTerraformConfigStateDriftWriterRequiresDatabase(t *testing.T) {
	t.Parallel()

	writer := PostgresTerraformConfigStateDriftWriter{}
	_, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), TerraformConfigStateDriftWrite{
		Candidates: []model.Candidate{exactDriftCandidate("aws_s3_bucket.x", "added_in_state")},
	})
	if err == nil {
		t.Fatal("WriteTerraformConfigStateDriftFindings() error = nil, want non-nil for a nil database")
	}
}

// TestEncodeDecodeReducerTerraformConfigStateDriftFindingRoundTrip proves the
// typed struct round-trips through Encode -> json.Marshal -> json.Unmarshal
// -> Decode (the same path the durable write and the Postgres read model take)
// without losing fields, for both outcome shapes.
func TestEncodeDecodeReducerTerraformConfigStateDriftFindingRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []reducerderivedv1.TerraformConfigStateDriftFinding{
		{
			ReducerDomain: "config_state_drift", IntentID: "intent-1", ScopeID: "state_snapshot:s3:hash-1",
			GenerationID: "gen-1", SourceSystem: "collector/terraform-state", Cause: "drift",
			CanonicalID: "canonical:x", CandidateID: "candidate:x", CandidateKind: "terraform_config_state_drift",
			Outcome: "exact", Address: "aws_s3_bucket.x", DriftKind: "added_in_state",
			BackendKind: "s3", LocatorHash: "hash-1", Confidence: 1,
			Evidence:     []map[string]any{{"id": "e1", "key": "resource_address", "value": "aws_s3_bucket.x"}},
			SourceLayers: []string{"source_declaration", "observed_resource"},
		},
		{
			ReducerDomain: "config_state_drift", IntentID: "intent-2", ScopeID: "state_snapshot:s3:hash-2",
			GenerationID: "gen-2", SourceSystem: "collector/terraform-state", Cause: "ambiguous owner",
			CanonicalID: "canonical:y", CandidateID: "ambiguous_owner:state_snapshot:s3:hash-2",
			CandidateKind: "terraform_config_state_drift_ambiguous_owner",
			Outcome:       "ambiguous", BackendKind: "s3", LocatorHash: "hash-2", Confidence: 1,
			AmbiguousOwnerCandidates: []map[string]any{
				{"repo_id": "repo-a", "scope_id": "repo:repo-a@1", "commit_id": "aaa"},
				{"repo_id": "repo-b", "scope_id": "repo:repo-b@1", "commit_id": "bbb"},
			},
			Evidence:     []map[string]any{},
			SourceLayers: []string{"source_declaration"},
		},
	}

	for _, want := range cases {
		encoded, err := factschema.EncodeReducerTerraformConfigStateDriftFinding(want)
		if err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
		raw, err := json.Marshal(encoded)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		got, err := factschema.DecodeReducerTerraformConfigStateDriftFinding(factschema.Envelope{
			FactKind:      factschema.FactKindReducerTerraformConfigStateDriftFinding,
			SchemaVersion: facts.ReducerDerivedSchemaVersionV1,
			Payload:       payload,
		})
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if got.Outcome != want.Outcome || got.Address != want.Address || got.DriftKind != want.DriftKind {
			t.Fatalf("round trip mismatch: got Outcome/Address/DriftKind = %q/%q/%q, want %q/%q/%q",
				got.Outcome, got.Address, got.DriftKind, want.Outcome, want.Address, want.DriftKind)
		}
		if len(got.AmbiguousOwnerCandidates) != len(want.AmbiguousOwnerCandidates) {
			t.Fatalf("round trip AmbiguousOwnerCandidates length = %d, want %d",
				len(got.AmbiguousOwnerCandidates), len(want.AmbiguousOwnerCandidates))
		}
	}
}
