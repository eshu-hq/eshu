// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestDiagnosticPlanUsesCompiledProductionSQL(t *testing.T) {
	asOf := time.Unix(100, 0).UTC()
	query, args, err := diagnosticActiveWorkSQL(asOf)
	if err != nil || len(args) != 1 || args[0] != asOf {
		t.Fatalf("query selection failed: err=%v args=%v", err, args)
	}
	sum := sha256.Sum256([]byte(query))
	if hex.EncodeToString(sum[:]) != postgres.ActiveWorkSummarySourceSHA256() {
		t.Fatal("plan query does not match actual compiled status statement")
	}
}

func TestDiagnosticPlanSanitizesPrivateFields(t *testing.T) {
	raw := []byte(`[{"Plan":{"Node Type":"Seq Scan","Relation Name":"private_repo","Filter":"secret-token","Actual Rows":4,"Actual Loops":1,"Actual Startup Time":0.1,"Actual Total Time":2,"Shared Hit Blocks":7},"Planning Time":0.2,"Execution Time":2.1}]`)
	plan, err := sanitizeDiagnosticPlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private_repo", "secret-token", "Relation Name", "Filter"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("private field survived: %s", private)
		}
	}
	if !strings.Contains(string(encoded), `"Shared Hit Blocks":7`) {
		t.Fatal("numeric buffer evidence lost")
	}
}

func TestDiagnosticPlanRejectsMalformedEvidence(t *testing.T) {
	for _, raw := range []string{
		`[]`,
		`[{"Plan":{"Node Type":"invented"},"Planning Time":1,"Execution Time":1}]`,
		`[{"Plan":{"Node Type":"Result","Actual Rows":-1,"Actual Loops":1,"Actual Startup Time":0,"Actual Total Time":1},"Planning Time":1,"Execution Time":1}]`,
		`[{"Plan":{"Node Type":"Result","Actual Rows":1,"Actual Loops":1,"Actual Startup Time":0,"Actual Total Time":1},"Planning Time":1e999,"Execution Time":1}]`,
		`[{"Plan":{"Node Type":"Result","Actual Rows":1,"Actual Loops":1,"Actual Startup Time":0,"Actual Total Time":1},"Execution Time":1}]`,
	} {
		if _, err := sanitizeDiagnosticPlan([]byte(raw)); err == nil {
			t.Fatalf("accepted malformed plan: %s", raw)
		}
	}
}

func TestDiagnosticPlanBoundsTree(t *testing.T) {
	leaf := map[string]any{"Node Type": "Result", "Actual Rows": 0, "Actual Loops": 1, "Actual Startup Time": 0, "Actual Total Time": 0}
	nested := leaf
	for i := 0; i < 66; i++ {
		nested = map[string]any{"Node Type": "Append", "Actual Rows": 0, "Actual Loops": 1, "Actual Startup Time": 0, "Actual Total Time": 0, "Plans": []any{nested}}
	}
	children := make([]any, 2049)
	for i := range children {
		children[i] = leaf
	}
	wide := map[string]any{"Node Type": "Append", "Actual Rows": 0, "Actual Loops": 1, "Actual Startup Time": 0, "Actual Total Time": 0, "Plans": children}
	for _, node := range []map[string]any{nested, wide} {
		raw, err := json.Marshal([]any{map[string]any{"Plan": node, "Planning Time": 0, "Execution Time": 0}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sanitizeDiagnosticPlan(raw); err == nil {
			t.Fatal("unbounded tree accepted")
		}
	}
	if _, err := sanitizeDiagnosticPlan(make([]byte, (4<<20)+1)); err == nil {
		t.Fatal("oversized input accepted")
	}
}

type diagnosticPlanFixture struct {
	t       *testing.T
	asOf    time.Time
	failure bool
}

func (f diagnosticPlanFixture) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	production, _, err := diagnosticActiveWorkSQL(f.asOf)
	if err != nil || query != "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+production || len(args) != 1 || args[0] != f.asOf {
		f.t.Fatal("plan did not execute byte-exact production SELECT with bound timestamp")
	}
	return diagnosticPlanRow{failure: f.failure}
}

type diagnosticPlanRow struct{ failure bool }

func (r diagnosticPlanRow) Scan(dest ...any) error {
	if r.failure {
		return errors.New("private connection detail")
	}
	*dest[0].(*[]byte) = []byte(`[{"Plan":{"Node Type":"Result","Actual Rows":1,"Actual Loops":1,"Actual Startup Time":0,"Actual Total Time":1},"Planning Time":0,"Execution Time":1}]`)
	return nil
}

func TestDiagnosticPlanExecutesBoundProductionRead(t *testing.T) {
	asOf := time.Unix(100, 0).UTC()
	plan, err := readDiagnosticPlan(context.Background(), diagnosticPlanFixture{t: t, asOf: asOf}, asOf)
	if err != nil || plan["Plan"] == nil {
		t.Fatalf("read failed: %v", err)
	}
	_, err = readDiagnosticPlan(context.Background(), diagnosticPlanFixture{t: t, asOf: asOf, failure: true}, asOf)
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("unclosed read failure: %v", err)
	}
}
