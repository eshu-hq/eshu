// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

var methodologyGraphProductionFiles = []string{
	"go/internal/query/codemodel/code_import_dependencies_queries.go",
	"go/internal/query/codemodel/code_import_dependencies_rows.go",
	"go/internal/query/codemodel/code_import_dependencies_response.go",
	"go/internal/query/codemodel/code_import_dependencies_cycles.go",
	"go/internal/query/codemodel/code_import_dependencies_cycle_rows.go",
	"go/internal/query/codemodel/code_import_dependencies_enumeration.go",
	"go/internal/query/codequery/imports/params.go",
	"go/internal/query/codequery/imports/rows.go",
}

func methodologyWriteGraphArtifact(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string, report methodologyReport) {
	t.Helper()
	manifest, err := queryplan.LoadManifestFile(filepath.Join("..", "queryplan", "testdata", "handler-hot-cypher.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	baseCommit := methodologyGitCommit(t, "origin/main")
	candidateCommit := methodologyGitCommit(t, "HEAD")
	methodologyVerifySameProduction(t, baseCommit, methodologyGraphProductionFiles)
	schema, indexes := methodologyGraphProductionDDL(t)
	methodologyCheckGraphSchema(t, ctx, driver, database)
	config := methodologyGraphConfig(t, ctx, driver, database)
	dataset := methodologyGraphDataset(t, ctx, driver, database)
	workload := methodologyJSON(t, map[string]any{
		"valid_requests":             report.ValidRequests,
		"request_query_counts":       report.RequestQueryCounts,
		"max_request_query_count":    report.MaxRequestQueryCount,
		"request_query_count_budget": report.RequestQueryCountBudget,
		"distinct_statements":        280,
		"measured_cases":             len(report.Statements),
		"cold_samples_per_side":      2,
		"warm_samples_per_side":      2,
		"sequence":                   "AB BA; query plan cache cleared before each cold sample; PROFILE outside normal timings",
	})
	fixtureDefinition := strings.Join(append(append([]string{"MATCH (n) DETACH DELETE n"}, schema...), append(indexes, methodologyGraphSeedDDL...)...), ";\n")
	version := methodologyGraphVersion(t, report.BackendComponents)
	image := strings.TrimSpace(os.Getenv("ESHU_QUERY_METHODOLOGY_GRAPH_IMAGE"))
	if image == "" {
		t.Fatal("ESHU_QUERY_METHODOLOGY_GRAPH_IMAGE must identify the actual engine image")
	}
	artifact := queryplan.PilotEvidenceArtifact{
		Version: 1,
		Environment: queryplan.PilotRunEnvironment{
			Engine: "neo4j", EngineVersion: version, EngineImage: image,
			Config: config, ConfigSHA256: queryplan.PilotJSONSHA256(config),
			Dataset: dataset, DatasetSHA256: queryplan.PilotJSONSHA256(dataset),
			FixtureDefinition: fixtureDefinition, FixtureSHA256: methodologyHash(fixtureDefinition),
			Workload: workload, WorkloadSHA256: queryplan.PilotJSONSHA256(workload),
			HarnessSHA256: methodologySourceHash(t, []string{
				"methodology_graph_live_test.go", "methodology_graph_fixture_live_test.go",
				"methodology_graph_oracle_live_test.go", "methodology_graph_measure_live_test.go",
				"methodology_graph_artifact_rows_test.go", "methodology_graph_artifact_live_test.go",
				"methodology_graph_artifact_test.go", "methodology_identity_test.go",
			}),
			BinarySHA256: methodologyBinaryHash(t), ColdReset: "cold_plan_warm_buffers",
		},
		Base:      methodologyBuild(t, baseCommit, schema, []string{}, indexes),
		Candidate: methodologyBuild(t, candidateCommit, schema, []string{}, indexes),
	}
	entries := make(map[string]*queryplan.PilotEvidenceEntry)
	for _, statement := range report.Statements {
		entry := methodologyGraphManifestEntry(t, manifest, statement.EntryID)
		if entry.Contract == nil {
			t.Fatalf("pilot contract absent for %s", statement.EntryID)
		}
		recorded := entries[statement.EntryID]
		if recorded == nil {
			recorded = &queryplan.PilotEvidenceEntry{
				EntryID:        statement.EntryID,
				ContractSHA256: queryplan.PilotContractSHA256(*entry.Contract),
				SourceSHA256:   entry.Source.SourceSHA256,
			}
			entries[statement.EntryID] = recorded
		}
		params := make(map[string]json.RawMessage, len(statement.Params))
		for name, value := range statement.Params {
			params[name] = methodologyJSON(t, value)
		}
		expected := methodologyJSON(t, append([]string{}, statement.ExpectedStatementIDs...))
		actual := methodologyJSON(t, append([]string{}, statement.ActualStatementIDs...))
		oracleRecord := methodologyJSON(t, map[string]any{
			"fixture_sha256":                     artifact.Environment.FixtureSHA256,
			"variant_id":                         statement.VariantID,
			"parameters":                         params,
			"independent_expected_statement_ids": append([]string{}, statement.ExpectedStatementIDs...),
		})
		if statement.Base == nil || statement.Candidate == nil {
			t.Fatalf("paired evidence absent for %s", statement.VariantID)
		}
		recorded.Cases = append(recorded.Cases, queryplan.PilotCaseEvidence{
			VariantID: statement.VariantID, CaseID: statement.CaseID, ScopeMode: statement.ScopeMode,
			EmittedText: statement.EmittedText, EmittedSHA256: statement.SHA256,
			BaseEmittedText: statement.EmittedText, BaseEmittedSHA256: statement.SHA256,
			Parameters: params, OracleProducer: "import-edge-ledger-v1",
			OracleArtifactSHA256: methodologyHash(string(oracleRecord)),
			OracleRecordedAt:     statement.OracleRecordedAt, MeasuredAt: statement.MeasuredAt,
			Expected: expected, Actual: actual,
			Base:      methodologyGraphPilotRun(t, *statement.Base),
			Candidate: methodologyGraphPilotRun(t, *statement.Candidate),
		})
	}
	for _, id := range manifest.PilotRequiredIDs {
		entry := methodologyGraphManifestEntry(t, manifest, id)
		if entry.Contract == nil || entry.Contract.Environment.Engine != "neo4j" {
			continue
		}
		if recorded := entries[id]; recorded != nil {
			artifact.Entries = append(artifact.Entries, *recorded)
		}
	}
	if err := queryplan.ValidatePilotEvidenceForBackend(manifest, &artifact); err != nil {
		t.Fatalf("Neo4j pilot artifact invalid: %v", err)
	}
	path := strings.TrimSpace(os.Getenv("ESHU_QUERY_METHODOLOGY_GRAPH_ARTIFACT"))
	if path == "" {
		path = filepath.Join(t.TempDir(), "neo4j-pilot.json")
	}
	methodologyWriteArtifact(t, path, artifact)
	t.Logf("Neo4j typed pilot evidence %s, entries=%d cases=%d", path, len(artifact.Entries), len(report.Statements))
}

func methodologyGraphManifestEntry(t *testing.T, manifest queryplan.Manifest, id string) queryplan.Entry {
	t.Helper()
	for _, entry := range manifest.Entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("missing manifest entry %s", id)
	return queryplan.Entry{}
}

func methodologyGraphPilotRun(t *testing.T, paired methodologyPairedRun) queryplan.PilotCaseRun {
	t.Helper()
	proof := methodologyJSON(t, paired.ColdProof)
	return queryplan.PilotCaseRun{
		Result: methodologyJSON(t, append([]string{}, paired.ResultIDs...)),
		Plan:   methodologyJSON(t, paired.Profile.Plan),
		Work: methodologyJSON(t, map[string]any{
			"query_count":            1,
			"total_operator_db_hits": paired.Profile.DbHits,
			"root_output_rows":       paired.Profile.Rows,
			"operators":              paired.Profile.Operators,
			"alerts":                 paired.Profile.Alerts,
			"operator_counters":      paired.Profile.Plan,
			"accounting":             "per-operator inclusive counters; no summed cache total claimed",
		}),
		ColdMilliseconds:    paired.ColdMilliseconds,
		WarmMilliseconds:    paired.WarmMilliseconds,
		ColdPreparation:     paired.ColdPreparation,
		ColdProof:           string(proof),
		ColdProofSHA256:     methodologyHash(string(proof)),
		PlanCaptureSeparate: true,
	}
}

func methodologyJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func methodologyGraphVersion(t *testing.T, components []map[string]any) string {
	t.Helper()
	for _, component := range components {
		versions, ok := component["versions"].([]any)
		if !ok {
			if values, yes := component["versions"].([]string); yes {
				for _, value := range values {
					if value != "" {
						return value
					}
				}
			}
		}
		for _, raw := range versions {
			if value, yes := raw.(string); yes && value != "" {
				return value
			}
		}
	}
	t.Fatal("Neo4j components returned no engine version")
	return ""
}

func methodologyGraphConfig(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string) json.RawMessage {
	t.Helper()
	rows := methodologyExecute(t, ctx, driver, database,
		"SHOW SETTINGS YIELD name, value WHERE name IN ['server.memory.pagecache.size', 'server.memory.heap.max_size', 'dbms.default_database'] RETURN name, value ORDER BY name", nil)
	if len(rows) == 0 {
		t.Fatal("Neo4j returned no relevant effective settings")
	}
	return methodologyJSON(t, map[string]any{"effective_settings": rows, "database": database, "transport": "Bolt"})
}

func methodologyGraphDataset(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string) json.RawMessage {
	t.Helper()
	nodes := methodologyExecute(t, ctx, driver, database, "MATCH (n) RETURN count(n) AS nodes", nil)
	rels := methodologyExecute(t, ctx, driver, database, "MATCH ()-[r]->() RETURN count(r) AS relationships", nil)
	if len(nodes) != 1 || len(rels) != 1 {
		t.Fatal("dataset counts unavailable")
	}
	return methodologyJSON(t, map[string]any{
		"version": "import-edge-ledger-v1", "seed": "deterministic fixture DDL embedded in artifact",
		"distribution": map[string]any{"nodes": nodes[0]["nodes"], "relationships": rels[0]["relationships"], "skew_import_files": 128, "duplicate_import_edges": 1, "null_import_attributes": 1},
		"topology":     "two repository tenants, shared module names, one cross-repository CALLS edge, one orphan history file",
		"storage":      "owned disposable Neo4j database; nodes cleared before seed, production anchor schema retained, query plan cache cleared per cold sample",
	})
}
