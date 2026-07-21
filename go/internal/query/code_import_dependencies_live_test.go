// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_import_cycle_proof

package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const importDependencyInteractiveSLO = 1500 * time.Millisecond

func TestLiveFileImportCyclesBoundedEdgeScan(t *testing.T) {
	repoID := os.Getenv("ESHU_PROOF_REPO_ID")
	if repoID == "" {
		t.Fatal("ESHU_PROOF_REPO_ID is required")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://localhost:7687"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}

	req := importDependencyRequest{QueryType: "file_import_cycles", RepoID: repoID, Limit: 6}
	params := importDependencyParams(req)
	params["cycle_language"] = "python"
	params["scan_limit"] = importDependencyInternalScanLimit + 1
	cypher := fileImportCycleEdgeRowsCypher(req)
	graph := NewNeo4jReader(driver, "nornic")

	started := time.Now()
	edgeRows, err := graph.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("bounded edge candidate: %v", err)
	}
	duration := time.Since(started)
	cycleRows, err := buildFileImportCycleRows(req, edgeRows)
	if err != nil {
		t.Fatalf("reconstruct exact reciprocal cycles: %v", err)
	}
	if len(cycleRows) == 0 {
		t.Fatal("reconstruct exact reciprocal cycles: no cycles returned")
	}
	firstCycle := cycleRows[0]
	scopedReq := importDependencyRequest{
		QueryType:    "file_import_cycles",
		RepoID:       repoID,
		SourceFile:   StringVal(firstCycle, "source_file"),
		TargetFile:   StringVal(firstCycle, "target_file"),
		SourceModule: StringVal(firstCycle, "source_module"),
		TargetModule: StringVal(firstCycle, "target_module"),
		Limit:        1,
	}
	scopedRows, err := (&CodeHandler{Neo4j: graph}).fileImportCycleRows(ctx, scopedReq)
	if err != nil {
		t.Fatalf("directionally scoped cycle read: %v", err)
	}
	if len(scopedRows) != 1 {
		t.Fatalf("directionally scoped cycle rows = %d, want 1", len(scopedRows))
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE")),
	})
	defer func() { _ = session.Close(context.Background()) }()
	plan := profileImportCycleProof(ctx, t, session, cypher, params)
	t.Logf(
		"candidate_seconds=%.6f edge_rows=%d exact_cycles=%d scoped_cycles=%d profile=%s",
		duration.Seconds(),
		len(edgeRows),
		len(cycleRows),
		len(scopedRows),
		plan,
	)
}

func TestLiveImportDependencyRepresentativeShapes(t *testing.T) {
	repoID := os.Getenv("ESHU_PROOF_REPO_ID")
	if repoID == "" {
		t.Fatal("ESHU_PROOF_REPO_ID is required")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://localhost:7687"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	handler := &CodeHandler{Neo4j: NewNeo4jReader(driver, "nornic")}

	for _, test := range []struct {
		name    string
		request importDependencyRequest
		minRows int
	}{
		{
			name: "repository-file",
			request: importDependencyRequest{
				QueryType: "imports_by_file", RepoID: repoID, SourceFile: "src/file-0.go", Limit: 200,
			},
			minRows: 1,
		},
		{
			name: "target-module",
			request: importDependencyRequest{
				QueryType: "importers", TargetModule: "common-dependency", Limit: 200,
			},
			minRows: 50,
		},
		{
			name: "source-module",
			request: importDependencyRequest{
				QueryType: "module_dependencies", SourceModule: "common-source", Limit: 200,
			},
			minRows: 50,
		},
		{
			name: "packages",
			request: importDependencyRequest{
				QueryType: "package_imports", RepoID: repoID, Limit: 200,
			},
			minRows: 1,
		},
		{
			name: "cycles",
			request: importDependencyRequest{
				QueryType: "file_import_cycles", RepoID: repoID, Limit: 200,
			},
			minRows: 200,
		},
		{
			name: "cross-module",
			request: importDependencyRequest{
				QueryType: "cross_module_calls", RepoID: repoID,
				SourceModule: "common-source", TargetModule: "common-target", Limit: 200,
			},
			minRows: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var durations [2]time.Duration
			var rowCount int
			for index := range durations {
				started := time.Now()
				rows, err := handler.importDependencyRows(ctx, test.request)
				durations[index] = time.Since(started)
				if err != nil {
					t.Fatalf("representative query run %d: %v", index+1, err)
				}
				rowCount = len(rows)
				if rowCount < test.minRows {
					t.Fatalf("rows = %d, want at least %d", rowCount, test.minRows)
				}
				if durations[index] > importDependencyInteractiveSLO {
					t.Fatalf("duration = %s, want <= %s", durations[index], importDependencyInteractiveSLO)
				}
			}
			t.Logf(
				"cold_seconds=%.6f warm_seconds=%.6f rows=%d slo_seconds=%.1f",
				durations[0].Seconds(),
				durations[1].Seconds(),
				rowCount,
				importDependencyInteractiveSLO.Seconds(),
			)
		})
	}
}

func profileImportCycleProof(
	ctx context.Context,
	t *testing.T,
	session neo4jdriver.SessionWithContext,
	cypher string,
	params map[string]any,
) string {
	t.Helper()
	result, err := session.Run(ctx, "PROFILE "+cypher, params)
	if err != nil {
		return "unavailable:" + err.Error()
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return "unavailable:" + err.Error()
	}
	profile := summary.Profile()
	if profile == nil {
		return "unavailable:no-profile-returned"
	}
	return summarizeImportCycleProfile(profile)
}

func summarizeImportCycleProfile(plan neo4jdriver.ProfiledPlan) string {
	parts := []string{fmt.Sprintf("%s(hits=%d,rows=%d)", plan.Operator(), plan.DbHits(), plan.Records())}
	for _, child := range plan.Children() {
		parts = append(parts, summarizeImportCycleProfile(child))
	}
	return strings.Join(parts, ">")
}
