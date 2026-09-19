// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"
	"testing"
)

// TestBuildBoundedRetractProbeCypherReadsOneMatchForBareLabelStatements
// verifies that a bare-label full-refresh retract gets a read that reports
// whether any node still matches (#6822). The read keeps the statement's MATCH
// and WHERE verbatim and bounds the plan with WITH ... ORDER BY elementId()
// LIMIT 1 before RETURN; a bare RETURN ... LIMIT costs a store-proportional
// scan on NornicDB v1.3.3.
func TestBuildBoundedRetractProbeCypherReadsOneMatchForBareLabelStatements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cypher    string
		drainVar  string
		wantProbe string
	}{
		{
			name:     "directories",
			cypher:   canonicalNodeRetractDirectoriesCypher,
			drainVar: "d",
			wantProbe: `MATCH (d:Directory)
WHERE d.repo_id = $repo_id AND d.generation_id <> $generation_id
  AND (d.path IS NULL OR NOT (d.path IN $directory_paths))
WITH d ORDER BY elementId(d) LIMIT 1
RETURN elementId(d) AS __id`,
		},
		{
			name:     "entity label",
			cypher:   fmt.Sprintf(canonicalNodeRetractEntityTemplate, "Function"),
			drainVar: "n",
			wantProbe: `MATCH (n:Function)
WHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical' AND n.generation_id <> $generation_id
WITH n ORDER BY elementId(n) LIMIT 1
RETURN elementId(n) AS __id`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok, err := BuildBoundedRetractProbeCypher(tt.cypher, tt.drainVar)
			if err != nil {
				t.Fatalf("BuildBoundedRetractProbeCypher() error = %v, want nil", err)
			}
			if !ok {
				t.Fatal("BuildBoundedRetractProbeCypher() ok = false, want true for a bare-label statement")
			}
			if got != tt.wantProbe {
				t.Fatalf("probe =\n%s\nwant\n%s", got, tt.wantProbe)
			}
		})
	}
}

// TestBuildBoundedRetractProbeCypherLeavesAnchoredStatementsAlone verifies
// that relationship-anchored retracts report ok=false: their single-statement
// drain is bounded by the anchor and needs no probe.
func TestBuildBoundedRetractProbeCypherLeavesAnchoredStatementsAlone(t *testing.T) {
	t.Parallel()

	for name, cypher := range map[string]string{
		"files":         canonicalNodeRetractFilesCypher,
		"removed files": canonicalNodeRetractRemovedFilesCypher,
	} {
		_, ok, err := BuildBoundedRetractProbeCypher(cypher, "f")
		if err != nil {
			t.Fatalf("%s: BuildBoundedRetractProbeCypher() error = %v, want nil", name, err)
		}
		if ok {
			t.Fatalf("%s: BuildBoundedRetractProbeCypher() ok = true, want false for a relationship-anchored statement", name)
		}
	}
}

// TestBuildBoundedRetractProbeCypherRejectsMalformedInput verifies the
// builder refuses statements it cannot probe safely.
func TestBuildBoundedRetractProbeCypherRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cypher   string
		drainVar string
		wantErr  string
	}{
		{"wrong trailing verb", "MATCH (d:Directory)\nDELETE d", "d", "DETACH DELETE d"},
		{"wrong drain var", canonicalNodeRetractDirectoriesCypher, "n", "DETACH DELETE n"},
		{"empty drain var", canonicalNodeRetractDirectoriesCypher, "", "drainVar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := BuildBoundedRetractProbeCypher(tt.cypher, tt.drainVar)
			if err == nil {
				t.Fatal("BuildBoundedRetractProbeCypher() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want message containing %q", err, tt.wantErr)
			}
		})
	}
}
