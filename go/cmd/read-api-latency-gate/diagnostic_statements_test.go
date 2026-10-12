// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"math"
	"testing"
)

func TestDiagnosticStatementDeltas(t *testing.T) {
	before := []diagnosticStatement{{StatsSinceUnixMicro: 100, DBID: 1, UserID: 2, QueryID: 3, TopLevel: true, Calls: 2, Rows: 4, Blocks: 6, ExecMS: 8}}
	after := []diagnosticStatement{{StatsSinceUnixMicro: 100, DBID: 1, UserID: 2, QueryID: 3, TopLevel: true, Calls: 5, Rows: 10, Blocks: 20, ExecMS: 12}, {StatsSinceUnixMicro: 100, DBID: 1, UserID: 2, QueryID: 4, TopLevel: true, Calls: 1, Rows: 1, Blocks: 2, ExecMS: 1}}
	delta, err := diagnosticStatementDeltas(before, after)
	if err != nil || len(delta) != 2 || delta[0].Calls != 3 || delta[0].Rows != 6 || delta[0].Blocks != 14 || delta[0].ExecMS != 4 || delta[1].Calls != 1 {
		t.Fatalf("delta=%+v err=%v", delta, err)
	}
}

func TestDiagnosticStatementDeltasRejectInvalidEvidence(t *testing.T) {
	row := diagnosticStatement{StatsSinceUnixMicro: 100, DBID: 1, UserID: 2, QueryID: 3, TopLevel: true, Calls: 2, Rows: 4, Blocks: 6, ExecMS: 8}
	for _, mode := range []string{"duplicate_before", "duplicate_after", "missing", "calls_decreased", "negative", "nonfinite"} {
		t.Run(mode, func(t *testing.T) {
			before, after := []diagnosticStatement{row}, []diagnosticStatement{row}
			switch mode {
			case "duplicate_before":
				before = append(before, row)
			case "duplicate_after":
				after = append(after, row)
			case "missing":
				after = nil
			case "calls_decreased":
				after[0].Calls--
			case "negative":
				after[0].Rows = -1
			case "nonfinite":
				after[0].ExecMS = math.NaN()
			}
			if _, err := diagnosticStatementDeltas(before, after); err == nil {
				t.Fatal("invalid counter evidence accepted")
			}
		})
	}
}

func TestDiagnosticStatementDeltasRejectResetRepopulation(t *testing.T) {
	before := []diagnosticStatement{{DBID: 1, UserID: 2, QueryID: 3, TopLevel: true, Calls: 2, StatsSinceUnixMicro: 100}}
	after := []diagnosticStatement{{DBID: 1, UserID: 2, QueryID: 3, TopLevel: true, Calls: 5, StatsSinceUnixMicro: 101}}
	if _, err := diagnosticStatementDeltas(before, after); err == nil {
		t.Fatal("same-ID reset/repopulation accepted")
	}
	after[0].StatsSinceUnixMicro = 100
	if _, err := diagnosticStatementDeltas(before, after); err != nil {
		t.Fatalf("stable epoch rejected: %v", err)
	}
}
