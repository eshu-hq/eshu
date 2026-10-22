// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay

import (
	"strings"
	"testing"
)

// TestParse_HoldStatementBeforeDelegation_Valid proves the seam (2) vocabulary
// from the #8539 arbiter ruling parses: a per-statement hold at the
// graph-executor seam, named for hold-BEFORE-delegation, firing on exactly one
// Cypher operation substring. The operation_match is required: without it the
// hold would have no statement to engage and could never fire.
func TestParse_HoldStatementBeforeDelegation_Valid(t *testing.T) {
	t.Parallel()

	const scriptJSON = `{
  "version": 1,
  "faults": [
    {"kind": "hold-statement-before-delegation", "trigger": {"operation_match": "MERGE (v)-[rel:SECRETS_IAM_USES_VAULT_POLICY]->(p)"}}
  ]
}`
	script, err := Parse([]byte(scriptJSON))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if len(script.Faults) != 1 {
		t.Fatalf("len(Faults) = %d, want 1", len(script.Faults))
	}
	if got := script.Faults[0].Kind; got != KindHoldStatementBeforeDelegation {
		t.Errorf("Kind = %q, want %q", got, KindHoldStatementBeforeDelegation)
	}
	if err := script.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestParse_HoldStatementBeforeDelegation_Rejects(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		json    string
		wantErr string
	}{
		"missing operation_match": {
			json:    `{"version":1,"faults":[{"kind":"hold-statement-before-delegation","trigger":{}}]}`,
			wantErr: "operation_match",
		},
		"empty operation_match": {
			json:    `{"version":1,"faults":[{"kind":"hold-statement-before-delegation","trigger":{"operation_match":"  "}}]}`,
			wantErr: "operation_match",
		},
		"statement_ordinal is not this kind's trigger": {
			json:    `{"version":1,"faults":[{"kind":"hold-statement-before-delegation","trigger":{"statement_ordinal":2}}]}`,
			wantErr: "does not accept trigger field",
		},
		"cross-kind after_claims": {
			json:    `{"version":1,"faults":[{"kind":"hold-statement-before-delegation","trigger":{"operation_match":"MERGE","after_claims":1}}]}`,
			wantErr: "does not accept trigger field",
		},
		"lane target is meaningless for a hold": {
			json:    `{"version":1,"faults":[{"kind":"hold-statement-before-delegation","trigger":{"operation_match":"MERGE"},"target":{"lane":"queue-retry"}}]}`,
			wantErr: "lane",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tc.json))
			if err == nil {
				t.Fatalf("Parse() error = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Parse() error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestNewFaultingWorkSourceDefersHoldToExecutor proves the hermetic work
// source treats the hold as an executor-seam fault owned elsewhere (like
// fail-graph-write-once-then-succeed), NOT as an unknown kind: RunFault
// builds the work source before the executor, so a hold-only script must
// survive the work source and fail at NewFaultingExecutor with the accurate
// "not supported by this hermetic runner" message -- never die earlier
// mislabeled "unknown fault kind".
func TestNewFaultingWorkSourceDefersHoldToExecutor(t *testing.T) {
	t.Parallel()

	match := "MERGE (v)"
	script := Script{
		Version: CurrentVersion,
		Faults: []FaultOp{
			{
				Kind:    KindHoldStatementBeforeDelegation,
				Trigger: Trigger{OperationMatch: &match},
			},
		},
	}
	if _, err := NewFaultingWorkSource(nil, script); err != nil {
		t.Fatalf("NewFaultingWorkSource(hold-statement-before-delegation) = %v, want nil (the executor owns this seam)", err)
	}
}

// TestNewFaultingExecutor_RejectsHoldStatementBeforeDelegation proves the
// hermetic tier fails closed on the hold kind (like
// restart-backend-between-phase-groups): the hermetic runner has no
// per-statement Cypher boundary -- StatementOrdinal counts Execute calls and
// OperationMatch matches intent IDs -- so a statement hold would run inert
// there, firing on the wrong thing while reporting a pass. A script naming
// the hold kind alongside a hermetic fault must be rejected at construction,
// never silently accepted.
func TestNewFaultingExecutor_RejectsHoldStatementBeforeDelegation(t *testing.T) {
	t.Parallel()

	match := "MERGE (v)"
	script := Script{
		Version: CurrentVersion,
		Faults: []FaultOp{
			{
				Kind:    KindHoldStatementBeforeDelegation,
				Trigger: Trigger{OperationMatch: &match},
			},
		},
	}
	if err := script.Validate(); err != nil {
		t.Fatalf("hold script Validate() = %v, want nil", err)
	}
	if _, err := NewFaultingExecutor(nil, script, nil, nil); err == nil {
		t.Fatal("NewFaultingExecutor(hold-statement-before-delegation) = nil error, want fail-closed rejection")
	} else if !strings.Contains(err.Error(), KindHoldStatementBeforeDelegation) {
		t.Errorf("NewFaultingExecutor() error = %q, want it to name %q", err.Error(), KindHoldStatementBeforeDelegation)
	}
}
