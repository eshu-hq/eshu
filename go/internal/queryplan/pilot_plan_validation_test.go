// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPilotEvidenceRejectsMeaninglessPlanAndInventedWork(t *testing.T) {
	manifest, artifact := pilotEvidenceFixture()
	caseRun := &artifact.Entries[0].Cases[0].Candidate
	for _, tc := range []struct {
		name string
		plan string
		work string
	}{
		{"unrelated object", `{"note":1}`, `{"rows":1,"shared_blocks":2,"query_count":1}`},
		{"missing operator", `{"Plan":{"Actual Rows":1,"Shared Hit Blocks":2}}`, `{"rows":1,"shared_blocks":2,"query_count":1}`},
		{"invented zero work", `[{"Plan":{"Node Type":"Index Scan","Actual Rows":1,"Shared Hit Blocks":12,"Shared Read Blocks":0,"Local Hit Blocks":0,"Local Read Blocks":0,"Temp Read Blocks":0,"Temp Written Blocks":0}}]`, `{"root_buffers_total":0,"root_temp_blocks_total":0,"query_count":1,"shared_blocks":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caseRun.Plan = json.RawMessage(tc.plan)
			caseRun.Work = json.RawMessage(tc.work)
			if err := ValidatePilotEvidence(manifest, &artifact); err == nil {
				t.Fatal("accepted invalid typed plan or inconsistent work")
			}
		})
	}
}

func TestPilotAlternateProofRejectsMeaninglessPlan(t *testing.T) {
	manifest, artifact := pilotEvidenceFixture()
	run := &artifact.Entries[0].Cases[0].Candidate
	run.Plan = nil
	run.PlanUnavailable = "backend does not expose plan"
	plan := json.RawMessage(`{"note":1}`)
	run.AlternateProof = json.RawMessage(fmt.Sprintf(`{"plan":%s,"work":{"query_count":1,"shared_blocks":2},"producer":"independent-probe","artifact_sha256":%q}`, plan, PilotJSONSHA256(plan)))
	if err := ValidatePilotEvidence(manifest, &artifact); err == nil || !strings.Contains(err.Error(), "plan") {
		t.Fatalf("accepted meaningless alternate plan: %v", err)
	}
}

func TestPilotGraphPlanMetricsRequireEveryOperatorCounter(t *testing.T) {
	for _, tc := range []struct {
		name     string
		plan     string
		wantHits float64
		valid    bool
	}{
		{"legitimate zero leaf", `{"operator":"ProduceResults@neo4j","db_hits":0,"rows":0,"page_cache_hits":0,"page_cache_misses":0,"arguments":{"DbHits":0,"Rows":0,"PageCacheHits":0,"PageCacheMisses":0},"children":null}`, 0, true},
		{"child work", `{"operator":"ProduceResults@neo4j","db_hits":0,"rows":1,"page_cache_hits":0,"page_cache_misses":0,"arguments":{"DbHits":0,"Rows":1,"PageCacheHits":0,"PageCacheMisses":0},"children":[{"operator":"NodeIndexSeek@neo4j","db_hits":8,"rows":1,"page_cache_hits":0,"page_cache_misses":0,"arguments":{"DbHits":8,"Rows":1,"PageCacheHits":0,"PageCacheMisses":0},"children":null}]}`, 8, true},
		{"missing raw counter", `{"operator":"ProduceResults@neo4j","db_hits":0,"rows":1,"arguments":{"Rows":1},"children":null}`, 0, false},
		{"missing cache metric", `{"operator":"ProduceResults@neo4j","db_hits":0,"rows":1,"page_cache_hits":0,"arguments":{"DbHits":0,"Rows":1,"PageCacheHits":0},"children":null}`, 0, false},
		{"invented optional time", `{"operator":"ProduceResults@neo4j","db_hits":0,"rows":1,"page_cache_hits":0,"page_cache_misses":0,"time_raw":0,"arguments":{"DbHits":0,"Rows":1,"PageCacheHits":0,"PageCacheMisses":0},"children":null}`, 0, false},
		{"child mismatch", `{"operator":"ProduceResults@neo4j","db_hits":0,"rows":1,"arguments":{"DbHits":0,"Rows":1},"children":[{"operator":"NodeIndexSeek@neo4j","db_hits":0,"rows":1,"arguments":{"DbHits":8,"Rows":1}}]}`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metrics, valid := pilotPlanMetrics(json.RawMessage(tc.plan), queryKindCypher)
			if valid != tc.valid || valid && metrics["total_operator_db_hits"] != tc.wantHits {
				t.Fatalf("valid=%v metrics=%v, want valid=%v hits=%v", valid, metrics, tc.valid, tc.wantHits)
			}
		})
	}
}

func TestPilotGraphWorkMustMatchMeasuredPlan(t *testing.T) {
	_, artifact := pilotEvidenceFixture()
	run := artifact.Entries[0].Cases[0].Candidate
	run.Plan = json.RawMessage(`{"operator":"ProduceResults@neo4j","db_hits":0,"rows":1,"page_cache_hits":0,"page_cache_misses":0,"arguments":{"DbHits":0,"Rows":1,"PageCacheHits":0,"PageCacheMisses":0},"children":[{"operator":"NodeIndexSeek@neo4j","db_hits":8,"rows":1,"page_cache_hits":0,"page_cache_misses":0,"arguments":{"DbHits":8,"Rows":1,"PageCacheHits":0,"PageCacheMisses":0},"children":null}]}`)
	run.Work = json.RawMessage(`{"query_count":1,"total_operator_db_hits":0,"root_output_rows":1}`)
	budget := PilotBudget{MaxNormalMilliseconds: 2000, MaxQueryCount: 1, MaxWork: map[string]float64{"total_operator_db_hits": 10, "root_output_rows": 1}}
	if got := validatePilotCaseRun("candidate", run, run.Result, budget, "graph-runner", queryKindCypher); len(got) == 0 {
		t.Fatal("accepted invented zero graph hits")
	}
	run.Work = json.RawMessage(`{"query_count":1,"total_operator_db_hits":8,"root_output_rows":1}`)
	if got := validatePilotCaseRun("candidate", run, run.Result, budget, "graph-runner", queryKindCypher); len(got) != 0 {
		t.Fatalf("rejected work matching the graph plan: %v", got)
	}
}
