// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_import_cycle_proof

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestLiveFileImportCyclesRepositoryAnchor(t *testing.T) {
	repoID := os.Getenv("ESHU_PROOF_REPO_ID")
	if repoID == "" {
		t.Fatal("ESHU_PROOF_REPO_ID is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext("bolt://localhost:7687", neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}

	req := importDependencyRequest{QueryType: "file_import_cycles", RepoID: repoID, Limit: 6}
	params := importDependencyParams(req)
	newCypher := fileImportCyclesCypher(req)
	oldCypher := legacyUnanchoredFileImportCyclesProofCypher(newCypher)
	assertImportCycleProofShapes(t, oldCypher, newCypher)
	graph := NewNeo4jReader(driver, "nornic")

	newStarted := time.Now()
	newRows, err := graph.Run(ctx, newCypher, params)
	if err != nil {
		t.Fatalf("new candidate: %v", err)
	}
	newDuration := time.Since(newStarted)
	oldStarted := time.Now()
	oldRows, err := graph.Run(ctx, oldCypher, params)
	if err != nil {
		t.Fatalf("old query: %v", err)
	}
	oldDuration := time.Since(oldStarted)
	if !reflect.DeepEqual(oldRows, newRows) {
		t.Fatalf("old/new ordered rows differ: old=%#v new=%#v", oldRows, newRows)
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: "nornic",
	})
	defer func() { _ = session.Close(context.Background()) }()
	oldPlan := profileImportCycleProof(ctx, t, session, oldCypher, params)
	newPlan := profileImportCycleProof(ctx, t, session, newCypher, params)
	t.Logf(
		"old_seconds=%.6f new_seconds=%.6f rows=%d exact_diff=0/0 old_profile=%s new_profile=%s",
		oldDuration.Seconds(),
		newDuration.Seconds(),
		len(oldRows),
		oldPlan,
		newPlan,
	)
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
