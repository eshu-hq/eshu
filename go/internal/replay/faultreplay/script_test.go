// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// validScriptJSON is a fault script containing exactly one FaultOp of each of
// the 5 supported kinds, each with a valid trigger (and target, where
// required). It is the round-trip fixture shared by the parse and
// determinism tests below.
const validScriptJSON = `{
  "version": 1,
  "faults": [
    {"kind": "kill-worker-after-claim", "trigger": {"after_claims": 3}},
    {"kind": "expire-lease-mid-handler", "trigger": {"intent_ordinal": 2}},
    {"kind": "expire-lease-mid-handler", "trigger": {"intent_id": "dir:/repo/src"}},
    {"kind": "fail-graph-write-once-then-succeed", "trigger": {"statement_ordinal": 1}, "target": {"lane": "executor-retry"}},
    {"kind": "fail-graph-write-once-then-succeed", "trigger": {"operation_match": "MERGE (n:Directory)"}, "target": {"lane": "queue-retry"}},
    {"kind": "restart-backend-between-phase-groups", "trigger": {"after_phase_groups": 1}},
    {"kind": "fail-terminal", "trigger": {"intent_id": "dir:/repo/vendor"}}
  ]
}`

func TestParse_ValidScriptAllKinds(t *testing.T) {
	script, err := Parse([]byte(validScriptJSON))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if script.Version != CurrentVersion {
		t.Errorf("Version = %d, want %d", script.Version, CurrentVersion)
	}
	if len(script.Faults) != 7 {
		t.Fatalf("len(Faults) = %d, want 7", len(script.Faults))
	}
	if err := script.Validate(); err != nil {
		t.Errorf("round-tripped script.Validate() = %v, want nil", err)
	}
}

func TestParse_Determinism(t *testing.T) {
	// Parsing the same bytes twice MUST yield equal structs: the schema has no
	// time.Time, time.Duration, or random field in the real trigger vocabulary,
	// so byte-identical input always produces byte-identical (in the Go value
	// sense) output. This is the property a fault run's replayability rests on.
	a, err := Parse([]byte(validScriptJSON))
	if err != nil {
		t.Fatalf("Parse() #1 error = %v", err)
	}
	b, err := Parse([]byte(validScriptJSON))
	if err != nil {
		t.Fatalf("Parse() #2 error = %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("Parse() is not deterministic: got %+v and %+v", a, b)
	}
}

func TestParse_Rejects(t *testing.T) {
	tests := map[string]struct {
		json    string
		wantErr string
	}{
		"version 0 (omitted)": {
			json:    `{"faults":[{"kind":"kill-worker-after-claim","trigger":{"after_claims":1}}]}`,
			wantErr: "version",
		},
		"version 2": {
			json:    `{"version":2,"faults":[{"kind":"kill-worker-after-claim","trigger":{"after_claims":1}}]}`,
			wantErr: "version",
		},
		"unknown kind": {
			json:    `{"version":1,"faults":[{"kind":"detonate-the-cluster","trigger":{"after_claims":1}}]}`,
			wantErr: "unknown fault kind",
		},
		"unknown lane": {
			json:    `{"version":1,"faults":[{"kind":"fail-graph-write-once-then-succeed","trigger":{"statement_ordinal":1},"target":{"lane":"sidecar-retry"}}]}`,
			wantErr: "lane",
		},
		"fail-graph-write-once missing lane": {
			json:    `{"version":1,"faults":[{"kind":"fail-graph-write-once-then-succeed","trigger":{"statement_ordinal":1}}]}`,
			wantErr: "lane",
		},
		"expire-lease trigger with both intent_ordinal and intent_id": {
			json:    `{"version":1,"faults":[{"kind":"expire-lease-mid-handler","trigger":{"intent_ordinal":1,"intent_id":"dir:/x"}}]}`,
			wantErr: "exactly one",
		},
		"expire-lease trigger with neither intent_ordinal nor intent_id": {
			json:    `{"version":1,"faults":[{"kind":"expire-lease-mid-handler","trigger":{}}]}`,
			wantErr: "exactly one",
		},
		"fail-graph-write-once trigger with both statement_ordinal and operation_match": {
			json:    `{"version":1,"faults":[{"kind":"fail-graph-write-once-then-succeed","trigger":{"statement_ordinal":1,"operation_match":"MERGE"},"target":{"lane":"executor-retry"}}]}`,
			wantErr: "exactly one",
		},
		"non-positive after_claims (zero)": {
			json:    `{"version":1,"faults":[{"kind":"kill-worker-after-claim","trigger":{"after_claims":0}}]}`,
			wantErr: "after_claims",
		},
		"non-positive after_claims (negative)": {
			json:    `{"version":1,"faults":[{"kind":"kill-worker-after-claim","trigger":{"after_claims":-1}}]}`,
			wantErr: "after_claims",
		},
		"non-positive after_phase_groups": {
			json:    `{"version":1,"faults":[{"kind":"restart-backend-between-phase-groups","trigger":{"after_phase_groups":0}}]}`,
			wantErr: "after_phase_groups",
		},
		"non-positive intent_ordinal": {
			json:    `{"version":1,"faults":[{"kind":"expire-lease-mid-handler","trigger":{"intent_ordinal":0}}]}`,
			wantErr: "intent_ordinal",
		},
		"non-positive statement_ordinal": {
			json:    `{"version":1,"faults":[{"kind":"fail-graph-write-once-then-succeed","trigger":{"statement_ordinal":0},"target":{"lane":"executor-retry"}}]}`,
			wantErr: "statement_ordinal",
		},
		"fail-terminal missing intent_id": {
			json:    `{"version":1,"faults":[{"kind":"fail-terminal","trigger":{}}]}`,
			wantErr: "intent_id",
		},
		"non-ordinal duration field on an otherwise-valid trigger": {
			json:    `{"version":1,"faults":[{"kind":"kill-worker-after-claim","trigger":{"after_claims":1,"after_duration":"5s"}}]}`,
			wantErr: "non-ordinal",
		},
		"non-ordinal timestamp field on an otherwise-valid trigger": {
			json:    `{"version":1,"faults":[{"kind":"expire-lease-mid-handler","trigger":{"intent_ordinal":1,"at_timestamp":"2026-07-10T00:00:00Z"}}]}`,
			wantErr: "non-ordinal",
		},
		"non-ordinal random_seed field on an otherwise-valid trigger": {
			json:    `{"version":1,"faults":[{"kind":"restart-backend-between-phase-groups","trigger":{"after_phase_groups":1,"random_seed":42}}]}`,
			wantErr: "non-ordinal",
		},
		"unknown top-level JSON field": {
			json:    `{"version":1,"faults":[{"kind":"kill-worker-after-claim","trigger":{"after_claims":1}}],"schedule":"nightly"}`,
			wantErr: "unknown field",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
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

func TestScript_Validate_DirectConstruction(t *testing.T) {
	// Validate() must reject a forbidden non-ordinal trigger field even when the
	// FaultOp is built directly in Go (not round-tripped through JSON), proving
	// the rejection lives in Validate and not only in the JSON decoder.
	after := 1
	duration := "5s"
	script := Script{
		Version: CurrentVersion,
		Faults: []FaultOp{
			{
				Kind: KindKillWorkerAfterClaim,
				Trigger: Trigger{
					AfterClaims:   &after,
					AfterDuration: &duration,
				},
			},
		},
	}
	err := script.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error rejecting the non-ordinal after_duration field")
	}
	if !strings.Contains(err.Error(), "non-ordinal") {
		t.Errorf("Validate() error = %q, want it to mention the non-ordinal trigger class", err.Error())
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fault-script.json")
	if err := os.WriteFile(path, []byte(validScriptJSON), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	script, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if len(script.Faults) != 7 {
		t.Errorf("len(Faults) = %d, want 7", len(script.Faults))
	}

	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("Load() on a missing file error = nil, want error")
	}

	badPath := filepath.Join(dir, "bad-version.json")
	if err := os.WriteFile(badPath, []byte(`{"version":9,"faults":[]}`), 0o600); err != nil {
		t.Fatalf("write bad-version fixture: %v", err)
	}
	if _, err := Load(badPath); err == nil {
		t.Error("Load() on an invalid script error = nil, want error")
	}
}

func TestScript_Validate_EmptyFaultsIsValid(t *testing.T) {
	// A script with zero faults is the fault-free baseline run in the same
	// vocabulary as a scripted-fault run (design doc 4389 Layer 4: the fault run
	// is compared against "the fault-free run of the same Odù"). It must parse
	// and validate cleanly, not be rejected as if it were malformed.
	script := Script{Version: CurrentVersion}
	if err := script.Validate(); err != nil {
		t.Errorf("Validate() on an empty-faults script = %v, want nil", err)
	}
}
