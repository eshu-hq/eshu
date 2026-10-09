// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// methodologyPairedRun records normal execution timings independently from
// PROFILE, with explicit plan cache reset evidence for each cold sample.
type methodologyPairedRun struct {
	ColdPreparation  string                   `json:"cold_preparation"`
	ColdProofSHA256  string                   `json:"cold_proof_sha256"`
	ColdProof        []string                 `json:"cold_proof"`
	ColdMilliseconds []float64                `json:"cold_ms"`
	WarmMilliseconds []float64                `json:"warm_ms"`
	Profile          methodologyProfileReport `json:"profile"`
	ResultIDs        []string                 `json:"result_ids"`
}

func methodologyPairedMeasurements(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string, capture methodologyCapturedStatement) (methodologyPairedRun, methodologyPairedRun) {
	t.Helper()
	runs := [2]methodologyPairedRun{
		{ColdPreparation: "cold_plan_warm_buffers"},
		{ColdPreparation: "cold_plan_warm_buffers"},
	}
	// AB, BA ensures neither label always sees the colder part of the host run.
	for sample := range 2 {
		for turn := range 2 {
			side := (sample + turn) % 2
			clear := methodologyExecute(t, ctx, driver, database, "CALL db.clearQueryCaches() YIELD value RETURN value", nil)
			if len(clear) != 1 {
				t.Fatalf("clear plan cache returned %d records", len(clear))
			}
			proof := fmt.Sprint(clear[0]["value"])
			if proof == "<nil>" || proof == "" {
				t.Fatal("plan cache reset returned no proof")
			}
			runs[side].ColdProof = append(runs[side].ColdProof, proof)
			cold, coldIDs := methodologyTimedRead(t, ctx, driver, database, capture)
			runs[side].ColdMilliseconds = append(runs[side].ColdMilliseconds, cold)
			warm, warmIDs := methodologyTimedRead(t, ctx, driver, database, capture)
			runs[side].WarmMilliseconds = append(runs[side].WarmMilliseconds, warm)
			if !slices.Equal(coldIDs, warmIDs) {
				t.Fatalf("paired %s cold/warm identities diverged", methodologyHash(capture.cypher))
			}
			runs[side].ResultIDs = warmIDs
		}
	}
	for side := range runs {
		proofJSON, err := json.Marshal(runs[side].ColdProof)
		if err != nil {
			t.Fatal(err)
		}
		runs[side].ColdProofSHA256 = methodologyHash(string(proofJSON))
		// RePROFILE separately for each side after all normal timing samples.
		runs[side].Profile = methodologyProfile(t, ctx, driver, database, capture)
	}
	return runs[0], runs[1]
}

func methodologyTimedRead(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string, capture methodologyCapturedStatement) (float64, []string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: database, AccessMode: neo4j.AccessModeRead})
	defer func() { _ = session.Close(context.Background()) }()
	started := time.Now()
	result, err := session.Run(ctx, capture.cypher, capture.params)
	if err != nil {
		t.Fatal(err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	ids := methodologyStatementIDs(capture.entryID, rows)
	if !slices.Equal(ids, capture.expectedIDs) {
		t.Fatalf("timed query %s IDs=%v, independent fixture wants %v", methodologyHash(capture.cypher), ids, capture.expectedIDs)
	}
	elapsed := float64(time.Since(started).Microseconds()) / 1000
	if elapsed <= 0 {
		t.Fatalf("timed query %s nonpositive duration", methodologyHash(capture.cypher))
	}
	return elapsed, ids
}
