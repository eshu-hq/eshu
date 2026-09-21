// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

func driftedAdmittedPair() AdmittedPair {
	a := MemberRow{
		EntityID: "e1", EntityName: "big", EntityType: "Function",
		RelativePath: "a.go", Language: "go",
		StartLine: 10, EndLine: 40, TokenCount: 64,
		Shingles: shingleSet(1, 9),
		FPExact:  "exact-e1", FPRenamed: "renamed-e1",
	}
	b := MemberRow{
		EntityID: "e2", EntityName: "bigCopy", EntityType: "Function",
		RelativePath: "b.go", Language: "go",
		StartLine: 50, EndLine: 80, TokenCount: 66,
		Shingles: append(shingleSet(1, 8), 101),
		FPExact:  "exact-e2", FPRenamed: "renamed-e2",
	}
	admitted, suppressed, reason := ApplyRules(CandidatePair{A: a, B: b, SharedBands: 9})
	if suppressed {
		panic("driftedAdmittedPair fixture suppresses under " + reason)
	}
	return admitted
}

func driftedWrite() DriftedWrite {
	return DriftedWrite{
		IntentID:     "intent-1",
		ScopeID:      "repo:repo-1",
		GenerationID: "gen-9",
		RepoID:       "repo-1",
		SourceSystem: "collector/git",
		Cause:        "fingerprints published",
		Pairs:        []AdmittedPair{driftedAdmittedPair()},
		Suppressions: map[string]int{
			"similarity_below_threshold": 1,
			"generated_file":             0,
		},
		BudgetExhausted: []string{"e1"},
	}
}

// TestPostgresCodeDriftedWriterPersistsOneFactPerPair proves the writer
// persists one durable drifted fact per admitted pair with the governed
// schema version, derived confidence, and a byte-identical typed payload
// carrying similarity, member ranges, and the generation suppression
// totals — followed by the generation-authoritative retire.
func TestPostgresCodeDriftedWriterPersistsOneFactPerPair(t *testing.T) {
	t.Parallel()

	db := &testutil.FakeExecer{}
	writer := PostgresCodeDriftedWriter{DB: db}
	write := driftedWrite()

	result, err := writer.WriteDriftedFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteDriftedFindings() error = %v, want nil", err)
	}
	if result.Written != 1 {
		t.Fatalf("Written = %d, want 1", result.Written)
	}
	if len(db.Execs) != 2 {
		t.Fatalf("ExecContext calls = %d, want 2 (batched insert + retire)", len(db.Execs))
	}
	rows := testutil.DecodeBatchedVersionedFactCalls(t, db.Execs[:1])
	if len(rows) != 1 {
		t.Fatalf("decoded rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.FactKind != facts.ReducerCodeDriftedFindingFactKind {
		t.Fatalf("FactKind = %q, want drifted kind", row.FactKind)
	}
	if row.ScopeID != "repo:repo-1" || row.GenerationID != "gen-9" {
		t.Fatalf("scope/generation = %q/%q, want repo:repo-1/gen-9", row.ScopeID, row.GenerationID)
	}
	if row.SchemaVersion != facts.ReducerDerivedSchemaVersionV1 {
		t.Fatalf("SchemaVersion = %q, want governed v1", row.SchemaVersion)
	}
	if row.SourceConfidence != facts.SourceConfidenceDerived {
		t.Fatalf("SourceConfidence = %q, want derived", row.SourceConfidence)
	}
	decoded, err := factschema.DecodeReducerCodeDriftedFinding(factschema.Envelope{
		FactKind:      row.FactKind,
		SchemaVersion: row.SchemaVersion,
		Payload:       mustUnmarshalPayload(t, row.Payload),
	})
	if err != nil {
		t.Fatalf("decode persisted payload: %v", err)
	}
	if decoded.Similarity != 0.8 || decoded.Threshold != DriftedSimilarityThreshold {
		t.Fatalf("similarity/threshold = %v/%v, want 0.8/%v", decoded.Similarity, decoded.Threshold, DriftedSimilarityThreshold)
	}
	if decoded.TruthLevel != "derived" {
		t.Fatalf("TruthLevel = %q, want derived", decoded.TruthLevel)
	}
	if decoded.MemberA.StartLine != 10 || decoded.MemberB.EndLine != 80 {
		t.Fatalf("member ranges = %d/%d, want 10/80", decoded.MemberA.StartLine, decoded.MemberB.EndLine)
	}
	if decoded.FindingID != DriftedFindingID("repo-1", write.Pairs[0].Pair.A, write.Pairs[0].Pair.B) {
		t.Fatal("payload FindingID must equal the stable pair identity")
	}
	// Shipped evidence atoms carry joinable identity: a finding-scoped id,
	// the domain source system, the write scope, and measured confidence.
	// Consumers filtering on evidence identity must never see empty strings.
	if len(decoded.Evidence) != 2 {
		t.Fatalf("evidence atoms = %d, want 2 (similarity, differs_in_ranges)", len(decoded.Evidence))
	}
	for _, atom := range decoded.Evidence {
		wantID := decoded.FindingID + "/" + atom["key"].(string)
		if atom["id"] != wantID {
			t.Errorf("evidence id = %v, want %q", atom["id"], wantID)
		}
		if atom["source_system"] != "reducer/code_drifted" {
			t.Errorf("evidence source_system = %v, want reducer/code_drifted", atom["source_system"])
		}
		if atom["scope_id"] != "repo:repo-1" {
			t.Errorf("evidence scope_id = %v, want repo:repo-1", atom["scope_id"])
		}
		if atom["confidence"] != 1.0 {
			t.Errorf("evidence confidence = %v, want 1", atom["confidence"])
		}
	}
	assertDriftedRetireCall(t, db.Execs[1], write.ScopeID, write.GenerationID, []string{row.FactID})
}

// TestPostgresCodeDriftedWriterIsIdempotent proves retries never duplicate a
// row: the same write twice yields identical fact IDs (the insert
// upsert-collapses on them).
func TestPostgresCodeDriftedWriterIsIdempotent(t *testing.T) {
	t.Parallel()

	first := &testutil.FakeExecer{}
	writer := PostgresCodeDriftedWriter{DB: first}
	if _, err := writer.WriteDriftedFindings(context.Background(), driftedWrite()); err != nil {
		t.Fatalf("first WriteDriftedFindings() error = %v", err)
	}
	second := &testutil.FakeExecer{}
	writer = PostgresCodeDriftedWriter{DB: second}
	if _, err := writer.WriteDriftedFindings(context.Background(), driftedWrite()); err != nil {
		t.Fatalf("second WriteDriftedFindings() error = %v", err)
	}
	firstRows := testutil.DecodeBatchedVersionedFactCalls(t, first.Execs[:1])
	secondRows := testutil.DecodeBatchedVersionedFactCalls(t, second.Execs[:1])
	if firstRows[0].FactID != secondRows[0].FactID {
		t.Fatal("retry must reproduce the identical fact ID")
	}
}

// TestPostgresCodeDriftedWriterRetiresOnEmptyWrite proves the complete
// assessment: a generation with zero admitted pairs still retires every
// stale finding under its (scope, generation) instead of leaving resolved
// drift behind.
func TestPostgresCodeDriftedWriterRetiresOnEmptyWrite(t *testing.T) {
	t.Parallel()

	db := &testutil.FakeExecer{}
	writer := PostgresCodeDriftedWriter{DB: db}
	write := driftedWrite()
	write.Pairs = nil
	result, err := writer.WriteDriftedFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteDriftedFindings() error = %v, want nil", err)
	}
	if result.Written != 0 {
		t.Fatalf("Written = %d, want 0", result.Written)
	}
	if len(db.Execs) != 1 {
		t.Fatalf("ExecContext calls = %d, want 1 (retire only)", len(db.Execs))
	}
	assertDriftedRetireCall(t, db.Execs[0], write.ScopeID, write.GenerationID, nil)
}

func mustUnmarshalPayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return out
}

// assertDriftedRetireCall pins the generation-authoritative retire call:
// the shipped retire query with (fact_kind, scope_id, generation_id, keep
// set) args, so a regression to a scope-wide or kind-blind delete fails
// loudly here.
func assertDriftedRetireCall(t *testing.T, call testutil.ExecCall, scopeID, generationID string, keepFactIDs []string) {
	t.Helper()
	if call.Query != driftedRetireQuery {
		t.Fatalf("retire call query = %.120q, want the generation-authoritative drifted retire", call.Query)
	}
	if len(call.Args) != 4 {
		t.Fatalf("retire call args = %d, want 4 (fact_kind, scope_id, generation_id, keep_fact_ids)", len(call.Args))
	}
	if got, want := call.Args[0], facts.ReducerCodeDriftedFindingFactKind; got != want {
		t.Fatalf("retire call fact_kind = %v, want %v", got, want)
	}
	if got, want := call.Args[1], scopeID; got != want {
		t.Fatalf("retire call scope_id = %v, want %v", got, want)
	}
	if got, want := call.Args[2], generationID; got != want {
		t.Fatalf("retire call generation_id = %v, want %v", got, want)
	}
	kept, ok := call.Args[3].(pgarray.StringArray)
	if !ok {
		t.Fatalf("retire keep arg type = %T, want pgarray.StringArray", call.Args[3])
	}
	if len(kept) != len(keepFactIDs) {
		t.Fatalf("retire keep ids = %v, want %v", kept, keepFactIDs)
	}
	for i := range kept {
		if kept[i] != keepFactIDs[i] {
			t.Fatalf("retire keep ids = %v, want %v", kept, keepFactIDs)
		}
	}
}
