// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
)

// TestRepoDependencyRunnerDefersGraphWriteUntilGenerationActive proves the
// end-to-end fence at the runner: with acceptance committed for the intent's
// generation but that generation NOT yet active, the repo-dependency runner
// writes NO graph edges and processes no intents; once the generation is
// activated, the next cycle projects the edges.
//
// This test stays in the reducer root rather than moving with
// maintenance.GateAcceptedGenerationOnActive (issue #6061): it exercises the
// gate against RepoDependencyProjectionRunner and its unexported test
// fixtures (fakeRepoDependencyIntentStore, repoDependencyIntentRow,
// recordingCodeCallProjectionEdgeWriter), all of which are root-owned and
// cannot cross a package boundary.
func TestRepoDependencyRunnerDefersGraphWriteUntilGenerationActive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	intent := repoDependencyIntentRow(
		"active-1", "scope-b", repoID, repoID, "repo_dependency:scope-b", "gen-2", now,
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:r_target_1",
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   CrossRepoEvidenceSource,
		},
	)
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{intent},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{
			repoID: {intent},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}

	active := false
	gated := maintenance.GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("gen-2", true),
		func(string) (bool, error) { return active, nil },
		nil,
	)
	runner := RepoDependencyProjectionRunner{
		IntentReader:       reader,
		LeaseManager:       reader,
		AcceptanceUnitGate: reader,
		EdgeWriter:         writer,
		AcceptedGen:        gated,
		Config:             RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() (inactive) error = %v", err)
	}
	if result.ProcessedIntents != 0 {
		t.Fatalf("ProcessedIntents = %d before activation, want 0", result.ProcessedIntents)
	}
	if len(writer.writeCalls) != 0 {
		t.Fatalf("graph write calls = %d before activation, want 0", len(writer.writeCalls))
	}

	active = true
	result, err = runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() (active) error = %v", err)
	}
	if result.ProcessedIntents != 1 {
		t.Fatalf("ProcessedIntents = %d after activation, want 1", result.ProcessedIntents)
	}
	if len(writer.writeCalls) != 1 {
		t.Fatalf("graph write calls = %d after activation, want 1", len(writer.writeCalls))
	}
}
