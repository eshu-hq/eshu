// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBlastRadiusSqlTableCypherDropsDeadBranchesKeepsLiveOnes guards the
// #5330 rewrite (extended in #5345, #5346, #5410): the SqlTable UNION must drop every
// branch that has no writer (MAPS_TO_TABLE and the
// combined READS_FROM|TRIGGERS_ON|INDEXES EXISTS branch), keep the branches
// that do have writers (CONTAINS, QUERIES_TABLE), and add
// endpoint-label-constrained TRIGGERS, INDEXES, READS_FROM, WRITES_TO,
// REFERENCES_TABLE, and MIGRATES
// branches now that all have real writers (TRIGGERS reconciled from the
// never-written TRIGGERS_ON name; INDEXES wired in #5330 Task 3; READS_FROM's
// SqlView/SqlFunction source_tables bridge wired in #5345; MIGRATES'
// SqlMigration migration_targets bridge wired in #5346; FK REFERENCES_TABLE
// and routine WRITES_TO wired in #5410). READS_FROM gets two branches (SqlView and
// SqlFunction sources) since NornicDB matches zero rows on a node-label
// disjunction (#5116), so a single branch cannot cover both source labels.
func TestBlastRadiusSqlTableCypherDropsDeadBranchesKeepsLiveOnes(t *testing.T) {
	t.Parallel()

	q := blastRadiusSqlTableQuery(repositoryAccessFilter{allScopes: true})

	for _, dead := range []string{"MAPS_TO_TABLE", "TRIGGERS_ON"} {
		if strings.Contains(q, dead) {
			t.Errorf("sql_table query must not reference dead edge type %q (no writer produces it): %s", dead, q)
		}
	}

	for _, live := range []string{
		"REPO_CONTAINS]->(:File)-[:CONTAINS]->(table)",
		"[:CONTAINS]->(:Function)-[:QUERIES_TABLE]->(table)",
		"[:CONTAINS]->(:SqlTrigger)-[:TRIGGERS]->(table)",
		"[:CONTAINS]->(:SqlIndex)-[:INDEXES]->(table)",
		"(table:SqlTable)<-[:READS_FROM*1..2]-(:SqlView)<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(repo:Repository)",
		"[:CONTAINS]->(:SqlFunction)-[:READS_FROM]->(table)",
		"[:CONTAINS]->(:SqlFunction)-[:WRITES_TO]->(table)",
		"[:CONTAINS]->(:SqlTable)-[:REFERENCES_TABLE]->(table)",
		"[:CONTAINS]->(:SqlMigration)-[:MIGRATES]->(table)",
	} {
		if !strings.Contains(q, live) {
			t.Errorf("sql_table query missing live branch shape %q: %s", live, q)
		}
	}

	// The branch multiplier constant must track the live branch count exactly
	// (9), or the over-fetch-before-dedup math in blastRadiusAffected drifts.
	if blastRadiusSqlTableBranches != 9 {
		t.Errorf("blastRadiusSqlTableBranches = %d, want 9 (CONTAINS, QUERIES_TABLE, TRIGGERS, INDEXES, READS_FROM x2, WRITES_TO, REFERENCES_TABLE, MIGRATES)", blastRadiusSqlTableBranches)
	}
	if got := strings.Count(q, " UNION\n") + 1; got != blastRadiusSqlTableBranches {
		t.Errorf("sql_table query has %d UNION branches, want %d (blastRadiusSqlTableBranches)", got, blastRadiusSqlTableBranches)
	}
}

// decodedBlastRadiusResponse mirrors the JSON shape findBlastRadius writes,
// including the #5330 complete/coverage honesty fields.
type decodedBlastRadiusResponse struct {
	AffectedCount int  `json:"affected_count"`
	Complete      bool `json:"complete"`
	Coverage      []struct {
		EdgeType     string `json:"edge_type"`
		Materialized bool   `json:"materialized"`
		Reason       string `json:"reason"`
	} `json:"coverage"`
}

// TestFindBlastRadiusSqlTableReportsUnmaterializedCoverage proves the
// sql_table response is honest: MAPS_TO_TABLE is reported as
// materialized:false with
// reason "no_writer" and drive complete:false, while the live branches
// (CONTAINS, QUERIES_TABLE, READS_FROM, TRIGGERS, INDEXES, MIGRATES) are
// reported materialized:true (#5330 Task 2, #5345, #5346, #5410).
func TestFindBlastRadiusSqlTableReportsUnmaterializedCoverage(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "CALL {"):
					return []map[string]any{{"repo": "orders-db", "repo_id": "repo-orders-db", "hops": int64(0)}}, nil
				case strings.Contains(cypher, "CONTAINS]-(tier:Tier)"):
					return nil, nil
				default:
					t.Fatalf("unexpected cypher: %s", cypher)
					return nil, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius",
		bytes.NewBufferString(`{"target":"orders","target_type":"sql_table"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}

	var resp decodedBlastRadiusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Complete {
		t.Fatal("complete = true, want false (MAPS_TO_TABLE has no writer)")
	}

	byType := map[string]struct {
		Materialized bool
		Reason       string
	}{}
	for _, c := range resp.Coverage {
		byType[c.EdgeType] = struct {
			Materialized bool
			Reason       string
		}{c.Materialized, c.Reason}
	}

	for _, dead := range []string{"MAPS_TO_TABLE"} {
		got, ok := byType[dead]
		if !ok {
			t.Errorf("coverage missing entry for %q", dead)
			continue
		}
		if got.Materialized {
			t.Errorf("coverage[%q].materialized = true, want false", dead)
		}
		if got.Reason != "no_writer" {
			t.Errorf("coverage[%q].reason = %q, want %q", dead, got.Reason, "no_writer")
		}
	}
	for _, live := range []string{"CONTAINS", "QUERIES_TABLE", "READS_FROM", "WRITES_TO", "REFERENCES_TABLE", "TRIGGERS", "INDEXES", "MIGRATES"} {
		got, ok := byType[live]
		if !ok {
			t.Errorf("coverage missing entry for %q", live)
			continue
		}
		if !got.Materialized {
			t.Errorf("coverage[%q].materialized = false, want true", live)
		}
	}
}

// TestFindBlastRadiusCrossplaneXrdReportsMaterializedCoverage proves the
// crossplane_xrd blast-radius response is complete now that a SATISFIED_BY
// writer exists (issue #5347, cypher.CrossplaneSatisfiedByEdgeWriter): the
// response must report complete:true and list both CONTAINS and
// SATISFIED_BY in coverage as materialized:true. The mock Cypher matcher
// binds the claim side to :K8sResource, not :CrossplaneClaim — the SATISFIED_BY
// node model is edge-only, so no node ever carries the CrossplaneClaim label
// (relabeling would collide with the per-label generation-retract). Mirrors
// TestFindBlastRadiusSqlTableReportsUnmaterializedCoverage (#5330).
func TestFindBlastRadiusCrossplaneXrdReportsMaterializedCoverage(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "K8sResource)-[:SATISFIED_BY]->(xrd)"):
					return []map[string]any{{"repo": "platform-infra", "repo_id": "repo-platform-infra", "claim": "database-claim"}}, nil
				case strings.Contains(cypher, "CONTAINS]-(tier:Tier)"):
					return nil, nil
				default:
					t.Fatalf("unexpected cypher: %s", cypher)
					return nil, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius",
		bytes.NewBufferString(`{"target":"database-xrd","target_type":"crossplane_xrd"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}

	var resp decodedBlastRadiusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AffectedCount != 1 {
		t.Fatalf("affected_count = %d, want 1", resp.AffectedCount)
	}
	if !resp.Complete {
		t.Fatal("complete = false, want true (both CONTAINS and SATISFIED_BY now have writers)")
	}

	byType := map[string]struct {
		Materialized bool
		Reason       string
	}{}
	for _, c := range resp.Coverage {
		byType[c.EdgeType] = struct {
			Materialized bool
			Reason       string
		}{c.Materialized, c.Reason}
	}

	satisfiedBy, ok := byType["SATISFIED_BY"]
	if !ok {
		t.Fatal("coverage missing entry for \"SATISFIED_BY\"")
	}
	if !satisfiedBy.Materialized {
		t.Error("coverage[\"SATISFIED_BY\"].materialized = false, want true (cypher.CrossplaneSatisfiedByEdgeWriter MERGEs this edge)")
	}
	if satisfiedBy.Reason == "" || satisfiedBy.Reason == "no_writer" {
		t.Errorf("coverage[\"SATISFIED_BY\"].reason = %q, want a real reason", satisfiedBy.Reason)
	}

	contains, ok := byType["CONTAINS"]
	if !ok {
		t.Fatal("coverage missing entry for \"CONTAINS\"")
	}
	if !contains.Materialized {
		t.Error("coverage[\"CONTAINS\"].materialized = false, want true (generic File->entity containment writer)")
	}
}

// TestFindBlastRadiusRepositoryCompleteWithEmptyCoverage proves a target_type
// with no known coverage gaps registered reports complete:true and an empty
// (never null) coverage array (#5330 Task 2).
func TestFindBlastRadiusRepositoryCompleteWithEmptyCoverage(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, ":DEPENDS_ON*1..5"):
					return []map[string]any{{"repo": "web", "repo_id": "repo-web", "hops": int64(1)}}, nil
				case strings.Contains(cypher, "CONTAINS]-(tier:Tier)"):
					return nil, nil
				default:
					t.Fatalf("unexpected cypher: %s", cypher)
					return nil, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius",
		bytes.NewBufferString(`{"target":"payments-core","target_type":"repository"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}

	if !bytes.Contains(w.Body.Bytes(), []byte(`"coverage":[]`)) {
		t.Errorf("response coverage must be an empty array, not null/omitted: %s", w.Body.String())
	}

	var resp decodedBlastRadiusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Complete {
		t.Fatal("complete = false, want true (repository target_type has no registered coverage gaps)")
	}
	if len(resp.Coverage) != 0 {
		t.Fatalf("coverage = %#v, want empty", resp.Coverage)
	}
}
