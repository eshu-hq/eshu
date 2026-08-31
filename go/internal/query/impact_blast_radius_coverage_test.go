// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	q := blastRadiusSqlTableQuery(repositoryAccessFilter{AllScopes: true})

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

// TestSQLBlastRadiusCleanupVerifiesEveryDelete pins the pairing sqlBlastRadiusCleanup
// depends on: every fixture DELETE carries a query that reads the same pattern
// back out of the graph.
//
// A delete reporting success is not the claim "nothing is left". #6182 was that
// gap -- the trailing deletes failed, probe nodes leaked into the graph the
// replay tier asserts exact node and edge counts against, and the test passed.
// The live cleanup now fails on a leftover row, but the live tests only run
// against a real NornicDB; this case runs everywhere and holds the shape a
// future probe would have to keep. A probe added with a delete and no verify,
// or with a verify aimed at a different pattern than its delete, turns this red
// without a backend.
func TestSQLBlastRadiusCleanupVerifiesEveryDelete(t *testing.T) {
	const prefix = "probeCleanupShape"
	probes := sqlBlastRadiusProbes(prefix)
	if len(probes) == 0 {
		t.Fatal("sqlBlastRadiusProbes returned nothing: cleanup that deletes nothing cannot leave the graph clean")
	}
	for _, probe := range probes {
		if probe.what == "" {
			t.Errorf("probe %q has no description; a cleanup failure has to name what leaked", probe.delete)
		}
		if probe.verify == "" {
			t.Errorf("probe %q deletes without verifying: a delete that reports success is not "+
				"proof the graph is empty, which is how #6182 leaked probe nodes while passing", probe.what)
			continue
		}
		if !strings.Contains(probe.verify, "AS leftover") {
			t.Errorf("probe %q verify query must return its rows as `leftover`; sqlBlastRadiusLeftovers "+
				"reads that column and reports an empty name otherwise: %s", probe.what, probe.verify)
		}
		if !strings.Contains(probe.verify, "LIMIT") {
			t.Errorf("probe %q verify query is unbounded: %s", probe.what, probe.verify)
		}
		if strings.Contains(probe.verify, "DELETE") {
			t.Errorf("probe %q verify query mutates instead of reading back: %s", probe.what, probe.verify)
		}
		if !strings.Contains(probe.delete, prefix) || !strings.Contains(probe.verify, prefix) {
			t.Errorf("probe %q must scope both halves to the fixture prefix, or cleanup reaches "+
				"data this gate does not own: delete=%s verify=%s", probe.what, probe.delete, probe.verify)
		}
		deleteMatch := strings.TrimSpace(strings.Split(probe.delete, "DETACH DELETE")[0])
		verifyMatch := strings.TrimSpace(strings.Split(probe.verify, "RETURN")[0])
		if deleteMatch != verifyMatch {
			t.Errorf("probe %q verifies a different pattern than it deletes:\n delete matches %q\n verify matches %q",
				probe.what, deleteMatch, verifyMatch)
		}
	}
}

// TestSQLBlastRadiusLeftoversNamesTheRows covers what a cleanup failure prints.
// A row missing the column yields an empty name rather than a panic, because
// this runs inside t.Cleanup where a panic would bury the failure it is
// reporting.
func TestSQLBlastRadiusLeftoversNamesTheRows(t *testing.T) {
	got := sqlBlastRadiusLeftovers([]map[string]any{
		{"leftover": "probe5409_repo_a"},
		{"leftover": "probe5409_repo_b"},
		{"unexpected": 7},
	})
	want := []string{"probe5409_repo_a", "probe5409_repo_b", ""}
	if len(got) != len(want) {
		t.Fatalf("leftovers = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("leftovers[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// sqlBlastRadiusReportRecorder stands in for *testing.T so a test can see which
// call the cleanup helper made. It records Errorf and Logf separately on
// purpose: the bug being guarded is a report that went to Logf, which prints
// and leaves the run green, so a recorder that lumped them together would call
// the bug a pass.
type sqlBlastRadiusReportRecorder struct {
	errorf []string
	logf   []string
}

func (r *sqlBlastRadiusReportRecorder) Errorf(format string, args ...any) {
	r.errorf = append(r.errorf, fmt.Sprintf(format, args...))
}

func (r *sqlBlastRadiusReportRecorder) Logf(format string, args ...any) {
	r.logf = append(r.logf, fmt.Sprintf(format, args...))
}

// TestSQLBlastRadiusCleanupReportsEveryFailure drives sqlBlastRadiusCleanupWith
// down each of its three failure paths and asserts each one fails the test
// rather than logging. It is the function the live gate runs, reached through
// the same call the gate makes, not a copy of it.
//
// This is the case that was missing. TestSQLBlastRadiusCleanupVerifiesEveryDelete
// reads the probe table and never calls the cleanup loop, so downgrading a
// report from Errorf back to Logf left every test in this package green. A real
// backend cannot be asked to fail its own delete on cue, which is why the runner
// is a stub here.
func TestSQLBlastRadiusCleanupReportsEveryFailure(t *testing.T) {
	const prefix = "probeReportShape"
	probes := sqlBlastRadiusProbes(prefix)
	if len(probes) == 0 {
		t.Fatal("sqlBlastRadiusProbes returned nothing: there is no cleanup to report on")
	}
	target := probes[0]
	backendErr := errors.New("connection reset by peer")

	cases := []struct {
		name string
		run  sqlBlastRadiusRunner
		want string
	}{
		{
			name: "the delete itself fails",
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if cypher == target.delete {
					return nil, backendErr
				}
				return nil, nil
			},
			want: "cleanup of prefixed fixture nodes failed: connection reset by peer",
		},
		{
			name: "the read-back fails",
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if cypher == target.verify {
					return nil, backendErr
				}
				return nil, nil
			},
			want: "cleanup of prefixed fixture nodes could not be confirmed: connection reset by peer",
		},
		{
			name: "the delete reports success and leaves rows",
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if cypher == target.verify {
					return []map[string]any{{"leftover": prefix + "_repo_a"}}, nil
				}
				return nil, nil
			},
			want: "cleanup of prefixed fixture nodes left [probeReportShape_repo_a] behind",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &sqlBlastRadiusReportRecorder{}
			sqlBlastRadiusCleanupWith(context.Background(), recorder, tc.run, prefix)

			if len(recorder.logf) != 0 {
				t.Errorf("cleanup logged %v instead of failing the test -- Logf prints and the run "+
					"stays green, which is how #6182 leaked probe nodes into the graph the replay "+
					"tier asserts exact node and edge counts against", recorder.logf)
			}
			if len(recorder.errorf) != 1 {
				t.Fatalf("Errorf calls = %d %v, want exactly 1: a cleanup failure nothing reports "+
					"leaves fixtures in a shared graph and the run green",
					len(recorder.errorf), recorder.errorf)
			}
			if !strings.Contains(recorder.errorf[0], tc.want) {
				t.Errorf("report = %q, want it to contain %q -- whoever reads this failure has to "+
					"know which cleanup broke and what it left", recorder.errorf[0], tc.want)
			}
		})
	}
}

// TestSQLBlastRadiusCleanupSaysNothingWhenTheGraphIsEmpty is the negative
// control for the test above. Without it, a helper that reported on every probe
// unconditionally would satisfy every case there and fail every live run.
func TestSQLBlastRadiusCleanupSaysNothingWhenTheGraphIsEmpty(t *testing.T) {
	recorder := &sqlBlastRadiusReportRecorder{}
	sqlBlastRadiusCleanupWith(
		context.Background(),
		recorder,
		func(context.Context, string, map[string]any) ([]map[string]any, error) { return nil, nil },
		"probeReportShape",
	)
	if len(recorder.errorf) != 0 || len(recorder.logf) != 0 {
		t.Errorf("a cleanup that deleted everything reported errorf=%v logf=%v, want silence",
			recorder.errorf, recorder.logf)
	}
}
