// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type methodologyPostgresCapture struct {
	handle *sql.DB
	text   string
	args   []any
	calls  int
}

func (c *methodologyPostgresCapture) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	c.text, c.args = query, append([]any(nil), args...)
	c.calls++
	return c.handle.QueryContext(ctx, query, args...)
}

type methodologyPostgresCoverage struct {
	Variants int
	Cases    int
	Evidence []queryplan.PilotCaseEvidence
}

func runMethodologyPostgresProof(t *testing.T, ctx context.Context, handle *sql.DB) methodologyPostgresCoverage {
	t.Helper()
	seedMethodologyPostgresEdges(t, ctx, handle)
	reader := &methodologyPostgresCapture{handle: handle}
	store := NewPostgresCloudResourceListStoreWithReadStore(reader)
	variants := cloudResourceListQueryplanVariants()
	names := make(map[string]string, len(variants))
	for name, text := range variants {
		names[queryplan.ProductionCypherSHA256(text)] = name
	}
	seen := make(map[string]bool)
	proof := methodologyPostgresCoverage{}
	for _, access := range cloudResourceListLiveAccessVariants() {
		for mask := 0; mask < 32; mask++ {
			filter := cloudResourceListLiveFilter(access.filter, mask)
			want := methodologyPostgresExpected(access, filter)
			oracleTime := time.Now().UTC().Format(time.RFC3339Nano)
			caseProof := queryplan.PilotCaseEvidence{
				CaseID:           access.name,
				OracleProducer:   "cloud-resource-fixture-arithmetic-v1",
				OracleRecordedAt: oracleTime, Expected: methodologyJSON(t, want),
			}
			caseProof.ScopeMode = "scoped"
			if filter.AllScopes {
				caseProof.ScopeMode = "all_scopes"
			}
			caseProof.OracleArtifactSHA256 = queryplan.ProductionCypherSHA256(string(caseProof.Expected))
			caseProof.MeasuredAt = time.Now().UTC().Format(time.RFC3339Nano)
			for round := 0; round < 4; round++ {
				// Alternate the order to avoid systematically favouring either build.
				for side := 0; side < 2; side++ {
					candidate := (round+side)%2 == 1
					if round < 2 {
						if _, err := handle.ExecContext(ctx, "DISCARD PLANS"); err != nil {
							t.Fatal(err)
						}
					}
					before := reader.calls
					start := time.Now()
					got, err := store.ListCloudResourceIdentities(ctx, filter)
					elapsed := time.Since(start)
					if err != nil {
						t.Fatalf("%s/%02d: %v", access.name, mask, err)
					}
					if reader.calls-before != 1 || !reflect.DeepEqual(got, want) {
						t.Fatalf("%s/%02d production result/call-count mismatch: calls=%d got=%v want=%v",
							access.name, mask, reader.calls-before, got, want)
					}
					if elapsed > cloudResourceListInteractiveSLO {
						t.Fatalf("%s/%02d exceeded %s: %s", access.name, mask, cloudResourceListInteractiveSLO, elapsed)
					}
					run := &caseProof.Base
					if candidate {
						run = &caseProof.Candidate
					}
					if round < 2 {
						run.ColdMilliseconds = append(run.ColdMilliseconds, float64(elapsed)/float64(time.Millisecond))
					} else {
						run.WarmMilliseconds = append(run.WarmMilliseconds, float64(elapsed)/float64(time.Millisecond))
					}
					caseProof.Actual = methodologyJSON(t, got)
					run.Result = caseProof.Actual
				}
			}
			caseProof.EmittedSHA256 = queryplan.ProductionCypherSHA256(reader.text)
			caseProof.EmittedText = reader.text
			caseProof.BaseEmittedText = reader.text
			caseProof.BaseEmittedSHA256 = caseProof.EmittedSHA256
			caseProof.VariantID = names[caseProof.EmittedSHA256]
			if caseProof.VariantID == "" {
				t.Fatalf("unregistered production emission %s/%02d: %s", access.name, mask, reader.text)
			}
			seen[caseProof.VariantID] = true
			caseProof.Parameters = make(map[string]json.RawMessage, len(reader.args))
			for i, value := range reader.args {
				caseProof.Parameters[fmt.Sprintf("%d", i+1)] = methodologyJSON(t, value)
			}
			for _, run := range []*queryplan.PilotCaseRun{&caseProof.Base, &caseProof.Candidate} {
				plan := explainCloudResourceListLiveQuery(t, ctx, handle, reader.text, reader.args)
				run.Plan = json.RawMessage(plan)
				run.Work = methodologyPostgresWork(t, plan)
				run.ColdPreparation = "cold_plan_warm_buffers"
				run.ColdProof = "DISCARD PLANS succeeded before each plan-cold normal sample; buffers retained after seed"
				run.ColdProofSHA256 = queryplan.ProductionCypherSHA256(run.ColdProof)
				run.PlanCaptureSeparate = true
			}
			proof.Evidence = append(proof.Evidence, caseProof)
		}
	}
	proof.Variants, proof.Cases = len(seen), len(proof.Evidence)
	proveMethodologyPostgresScopeMutation(t, ctx, reader, store)
	proveMethodologyPostgresMissingIndex(t, ctx, handle)
	return proof
}

func seedMethodologyPostgresEdges(t *testing.T, ctx context.Context, handle *sql.DB) {
	t.Helper()
	statements := []string{
		`UPDATE graph_node_owner SET winning_row = jsonb_set(winning_row, '{resource_type}', 'null'::jsonb) WHERE uid = 'uid-000001'`,
		`UPDATE fact_records SET is_tombstone = true WHERE fact_id = 'fact-000003'`,
		`INSERT INTO scope_generations VALUES ('generation:old', 'scope:allowed', 'retired')`,
		`UPDATE fact_records SET generation_id = 'generation:old' WHERE fact_id = 'fact-000005'`,
		`UPDATE graph_node_owner SET winning_row = jsonb_set(winning_row, '{collector_kind}', 'null'::jsonb) WHERE uid = 'uid-000009'`,
		`INSERT INTO graph_node_owner SELECT 'uid-duplicate', winning_row FROM graph_node_owner WHERE uid = 'uid-000020'`,
		`ANALYZE graph_node_owner`,
		`ANALYZE fact_records`,
	}
	for _, statement := range statements {
		if _, err := handle.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
}

// methodologyPostgresExpected is arithmetic over the declared fixture, never
// a database query, previous implementation, or copy of the SQL under proof.
func methodologyPostgresExpected(access cloudResourceListLiveAccess, filter CloudResourceListPageFilter) []CloudResourceListIdentity {
	unbounded := filter
	unbounded.Limit = 20001
	rows := cloudResourceListLiveExpectedRows(access, unbounded)
	want := make([]CloudResourceListIdentity, 0, len(rows)+1)
	for _, row := range rows {
		if row.UID == "uid-000001" || row.UID == "uid-000003" || row.UID == "uid-000005" || (row.UID == "uid-000009" && filter.Provider != "") {
			continue
		}
		want = append(want, row)
	}
	duplicate := CloudResourceListIdentity{UID: "uid-duplicate", ResourceType: "type-00"}
	if access.allowsID(20) && (filter.Provider == "" || filter.Provider == "provider-00") &&
		(filter.ResourceType == "" || filter.ResourceType == "type-00") &&
		(filter.Region == "" || filter.Region == "region-04") &&
		(filter.AccountID == "" || filter.AccountID == "account-04") &&
		(filter.AfterID == "" || filter.AfterResourceType < "type-00" || (filter.AfterResourceType == "type-00" && filter.AfterID < duplicate.UID)) {
		want = append(want, duplicate)
	}
	sort.Slice(want, func(i, j int) bool {
		if want[i].ResourceType != want[j].ResourceType {
			return want[i].ResourceType < want[j].ResourceType
		}
		return want[i].UID < want[j].UID
	})
	if len(want) > filter.Limit {
		want = want[:filter.Limit]
	}
	return want
}

func proveMethodologyPostgresScopeMutation(t *testing.T, ctx context.Context, reader *methodologyPostgresCapture, store *PostgresCloudResourceListStore) {
	t.Helper()
	access := cloudResourceListLiveAccessVariants()[1]
	filter := cloudResourceListLiveFilter(access.filter, 0)
	want := methodologyPostgresExpected(access, filter)
	if _, err := store.ListCloudResourceIdentities(ctx, filter); err != nil {
		t.Fatal(err)
	}
	start := strings.Index(reader.text, "AND ((scope.scope_kind")
	if start < 0 {
		t.Fatal("scope mutation could not locate actual emitted grant predicate")
	}
	end := strings.Index(reader.text[start:], "\n        LIMIT 1") + start
	if end < start {
		t.Fatal("scope mutation could not locate actual emitted grant predicate")
	}
	mutated := reader.text[:start] + "AND ($1::text[] IS NOT NULL AND $2::text[] IS NOT NULL)" + reader.text[end:]
	rows, err := reader.handle.QueryContext(ctx, mutated, reader.args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var leaked []CloudResourceListIdentity
	for rows.Next() {
		var row CloudResourceListIdentity
		if err := rows.Scan(&row.UID, &row.ResourceType); err != nil {
			t.Fatal(err)
		}
		leaked = append(leaked, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(leaked, want) {
		t.Fatal("RED: removed scope filter escaped independent result oracle")
	}
	t.Log("RED removed-scope-filter rejected by independent fixture results; GREEN production scoped results passed")
}

func methodologyPostgresIndexPresent(ctx context.Context, handle *sql.DB, name string) error {
	var present bool
	err := handle.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = ANY(current_schemas(true)) AND indexname = $1)`, name).Scan(&present)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("required physical index %s is absent", name)
	}
	return nil
}

func proveMethodologyPostgresMissingIndex(t *testing.T, ctx context.Context, handle *sql.DB) {
	t.Helper()
	const name = "graph_node_owner_cloud_resource_page_idx"
	if err := methodologyPostgresIndexPresent(ctx, handle, name); err != nil {
		t.Fatal(err)
	}
	var definition string
	if err := handle.QueryRowContext(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname = ANY(current_schemas(true)) AND indexname = $1`, name).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.ExecContext(ctx, "DROP INDEX "+name); err != nil {
		t.Fatal(err)
	}
	if err := methodologyPostgresIndexPresent(ctx, handle, name); err == nil {
		t.Fatal("RED: physically absent required index escaped guard")
	}
	if _, err := handle.ExecContext(ctx, definition); err != nil {
		t.Fatal(err)
	}
	if err := methodologyPostgresIndexPresent(ctx, handle, name); err != nil {
		t.Fatal(err)
	}
	t.Log("RED physically missing required index rejected; GREEN restored real index passed")
}

func methodologyPostgresWork(t *testing.T, plan string) json.RawMessage {
	t.Helper()
	var value []map[string]any
	if err := json.Unmarshal([]byte(plan), &value); err != nil || len(value) != 1 {
		t.Fatalf("decode measured plan: %v", err)
	}
	// Preserve each operator's counters. PostgreSQL buffer counters include
	// descendants, so adding them would invent work by counting it twice.
	root, ok := value[0]["Plan"].(map[string]any)
	if !ok {
		t.Fatal("measured plan has no root operator")
	}
	rootTotal := func(keys ...string) float64 {
		var total float64
		for _, key := range keys {
			counter, present := root[key]
			if !present {
				t.Fatalf("missing root counter %s", key)
			}
			number, valid := counter.(float64)
			if !valid {
				t.Fatalf("invalid root counter %s: %v", key, counter)
			}
			total += number
		}
		return total
	}
	return methodologyJSON(t, map[string]any{
		"query_count": 1, "operator_counters": value[0]["Plan"],
		"root_buffers_total":     rootTotal("Shared Hit Blocks", "Shared Read Blocks", "Local Hit Blocks", "Local Read Blocks"),
		"root_temp_blocks_total": rootTotal("Temp Read Blocks", "Temp Written Blocks"),
		"buffer_accounting":      "inclusive per-node, never summed", "timing": "normal execution measured separately",
	})
}

func methodologyJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestMethodologyPostgresWorkUsesInclusiveRootCounters(t *testing.T) {
	plan := `[{"Plan":{"Shared Hit Blocks":1,"Shared Read Blocks":2,"Local Hit Blocks":3,"Local Read Blocks":4,"Temp Read Blocks":0,"Temp Written Blocks":0,"Plans":[{"Shared Hit Blocks":1000}]}}]`
	var work map[string]any
	if err := json.Unmarshal(methodologyPostgresWork(t, plan), &work); err != nil {
		t.Fatal(err)
	}
	if work["root_buffers_total"] != float64(10) || work["root_temp_blocks_total"] != float64(0) {
		t.Fatalf("root work=%v", work)
	}
}

func TestMethodologyPostgresWorkRequiresSixRootCounters(t *testing.T) {
	probes := map[string]string{
		"all_missing":        `[ {"Plan":{"Node Type":"Index Scan"}} ]`,
		"one_shared_missing": `[ {"Plan":{"Shared Hit Blocks":0,"Shared Read Blocks":0,"Local Hit Blocks":0,"Temp Read Blocks":0,"Temp Written Blocks":0}} ]`,
		"one_temp_missing":   `[ {"Plan":{"Shared Hit Blocks":0,"Shared Read Blocks":0,"Local Hit Blocks":0,"Local Read Blocks":0,"Temp Read Blocks":0}} ]`,
		"invalid_type":       `[ {"Plan":{"Shared Hit Blocks":"zero","Shared Read Blocks":0,"Local Hit Blocks":0,"Local Read Blocks":0,"Temp Read Blocks":0,"Temp Written Blocks":0}} ]`,
	}
	if name := os.Getenv("ESHU_METHOD_PG_COUNTER_PROBE"); name != "" {
		methodologyPostgresWork(t, probes[name])
		return
	}
	for name := range probes {
		t.Run(name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run", "^TestMethodologyPostgresWorkRequiresSixRootCounters$")
			command.Env = append(os.Environ(), "ESHU_METHOD_PG_COUNTER_PROBE="+name)
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "root counter") {
				t.Fatalf("invalid EXPLAIN root accepted or failed for wrong reason: %v: %s", err, output)
			}
		})
	}
}
