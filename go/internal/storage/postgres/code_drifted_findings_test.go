// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// driftedFindingPayloadJSON encodes one admitted pair through the production
// encoder so the fixture carries the exact stored shape without hand-copying
// it; the store test then proves the SQL-to-row mapping, not the encoder.
func driftedFindingPayloadJSON(t *testing.T, findingID string, tokensA, tokensB int) string {
	t.Helper()

	payload, err := factschema.EncodeReducerCodeDriftedFinding(reducerderivedv1.CodeDriftedFinding{
		ReducerDomain: "code_drifted",
		IntentID:      "intent-1",
		ScopeID:       "repo:repo-1",
		GenerationID:  "gen-1",
		SourceSystem:  "eshu",
		Cause:         "code_drifted_projection",
		FindingID:     findingID,
		RepoID:        "repo-1",
		Similarity:    0.87,
		Threshold:     0.7,
		SharedBands:   9,
		MemberA: reducerderivedv1.CodeDriftedMember{
			EntityID: "e1", EntityName: "renderTable", EntityType: "Function",
			RelativePath: "a/table.go", Language: "go", StartLine: 10, EndLine: 60, TokenCount: tokensA,
		},
		MemberB: reducerderivedv1.CodeDriftedMember{
			EntityID: "e2", EntityName: "renderTable", EntityType: "Function",
			RelativePath: "b/table.go", Language: "go", StartLine: 12, EndLine: 62, TokenCount: tokensB,
		},
		TruthLevel:   "derived",
		Evidence:     []map[string]any{{"id": "ev-1", "evidence_type": "lsh_bands", "key": "shared_bands", "value": "9"}},
		SourceLayers: []string{"source_declaration"},
	})
	if err != nil {
		t.Fatalf("EncodeReducerCodeDriftedFinding() error = %v", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal drifted payload error = %v", err)
	}
	return string(raw)
}

// TestPostgresCodeDriftedFindingStoreReadsActiveRows proves the read
// contract: active-generation fact payloads decode into drifted stats
// (finding id fingerprint, pair token max) and hydrate into rows keyed by
// finding id for exactly the requested window.
func TestPostgresCodeDriftedFindingStoreReadsActiveRows(t *testing.T) {
	t.Parallel()

	payloadLow := driftedFindingPayloadJSON(t, "find-low", 210, 190)
	payloadHigh := driftedFindingPayloadJSON(t, "find-high", 300, 300)
	fake := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"1.0.0", payloadLow}, {"1.0.0", payloadHigh}}},
			{rows: [][]any{{"1.0.0", payloadHigh}}},
		},
	}
	store := PostgresCodeDriftedFindingStore{DB: fake}

	stats, err := store.DriftedFindingStats(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("DriftedFindingStats() error = %v, want nil", err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats = %d, want 2 active rows", len(stats))
	}
	byFP := map[string]codedivergence.GroupStat{}
	for _, stat := range stats {
		if stat.Kind != codedivergence.KindDrifted {
			t.Errorf("stat kind = %q, want drifted", stat.Kind)
		}
		byFP[stat.Fingerprint] = stat
	}
	if byFP["find-low"].Tokens != 210 || byFP["find-low"].Members != 2 {
		t.Errorf("find-low stat = %+v, want members 2 tokens 210", byFP["find-low"])
	}
	if byFP["find-high"].Tokens != 300 {
		t.Errorf("find-high stat = %+v, want tokens 300", byFP["find-high"])
	}

	rows, err := store.DriftedFindingRows(context.Background(), "repo-1", []string{"find-high"})
	if err != nil {
		t.Fatalf("DriftedFindingRows() error = %v, want nil", err)
	}
	row, ok := rows["find-high"]
	if !ok {
		t.Fatalf("rows missing find-high, got %v", rows)
	}
	if row.Similarity != 0.87 || row.Threshold != 0.7 || row.SharedBands != 9 {
		t.Errorf("row = %+v, want similarity 0.87 threshold 0.7 bands 9", row)
	}
	if len(row.Members) != 2 || row.Members[0].EntityID != "e1" || row.Members[1].TokenCount != 300 {
		t.Errorf("members = %+v, want e1/e2 with 300-token max", row.Members)
	}
}

// TestDriftedFindingsQueryShape pins the shipped statement: active
// generation only (the retire is per-generation, so older rows stay), no
// tombstones, repo predicate on the payload, window hydration by finding
// id, and never a source_cache read (#6835 contract gate).
func TestDriftedFindingsQueryShape(t *testing.T) {
	t.Parallel()

	for _, clause := range []string{
		"FROM fact_records",
		"JOIN ingestion_scopes",
		"scope.active_generation_id = fact.generation_id",
		"fact.is_tombstone = false",
		"payload->>'repo_id' = $2",
	} {
		if !strings.Contains(driftedFindingsQuery, clause) {
			t.Errorf("drifted findings query must contain %q, got:\n%s", clause, driftedFindingsQuery)
		}
	}
	if strings.Contains(driftedFindingsQuery, "source_cache") {
		t.Errorf("drifted findings query must never read source_cache, got:\n%s", driftedFindingsQuery)
	}
	if !strings.Contains(driftedFindingsQuery, "payload->>'finding_id' = ANY($3)") {
		t.Errorf("drifted findings query must hydrate by finding id, got:\n%s", driftedFindingsQuery)
	}
	if !strings.Contains(driftedFindingsQuery, "cardinality($3::text[]) = 0") {
		t.Errorf("drifted findings query must read all active rows when the window is empty, got:\n%s", driftedFindingsQuery)
	}
}
