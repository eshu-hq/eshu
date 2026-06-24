// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

func TestRepoDependencyProjectionRunnerProcessesRetractOnlyIntent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 20, 15, 0, 0, time.UTC)
	repoID := "repository:r_deploy"
	row := repoDependencyIntentRow(
		"retract-only",
		"git-repository-scope:repository:r_deploy",
		repoID,
		repoID,
		"repo_dependency:git-repository-scope:repository:r_deploy",
		"gen-removed-evidence",
		now,
		map[string]any{
			"repo_id":         repoID,
			"action":          "retract",
			"evidence_source": crossRepoEvidenceSource,
			"generation_id":   "gen-removed-evidence",
		},
	)
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{row},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{
			repoID: {row},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-removed-evidence", true),
		Config:       RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := result.ProcessedIntents, 1; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d", got, want)
	}
	if got, want := result.RetractedRows, 1; got != want {
		t.Fatalf("RetractedRows = %d, want %d", got, want)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retract calls = %d, want 1", len(writer.retractCalls))
	}
	if got, want := writer.retractCalls[0].evidenceSource, crossRepoEvidenceSource; got != want {
		t.Fatalf("retract evidence source = %q, want %q", got, want)
	}
	if got, want := writer.retractCalls[0].rows[0].RepositoryID, repoID; got != want {
		t.Fatalf("retract repo = %q, want %q", got, want)
	}
	if len(writer.writeCalls) != 0 {
		t.Fatalf("write calls = %d, want 0 for retract-only intent", len(writer.writeCalls))
	}
	if len(reader.marked) != 1 || reader.marked[0] != "retract-only" {
		t.Fatalf("marked intents = %v, want [retract-only]", reader.marked)
	}
}

func TestRepoDependencyProjectionRunnerRetractsThenRewritesSurvivingEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 20, 30, 0, 0, time.UTC)
	repoID := "repository:r_deploy"
	oldCompletedAt := now.Add(-time.Minute)
	oldRemoved := repoDependencyIntentRow(
		"old-removed",
		"git-repository-scope:repository:r_deploy",
		repoID,
		repoID,
		"repo_dependency:git-repository-scope:repository:r_deploy",
		"gen-old",
		now.Add(-2*time.Minute),
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:r_removed",
			"relationship_type": "DEPLOYS_FROM",
			"evidence_source":   crossRepoEvidenceSource,
		},
	)
	oldRemoved.CompletedAt = &oldCompletedAt
	oldSurvivor := repoDependencyIntentRow(
		"old-survivor",
		"git-repository-scope:repository:r_deploy",
		repoID,
		repoID,
		"repo_dependency:git-repository-scope:repository:r_deploy",
		"gen-old",
		now.Add(-2*time.Minute),
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:r_survivor",
			"relationship_type": "DISCOVERS_CONFIG_IN",
			"evidence_source":   crossRepoEvidenceSource,
		},
	)
	oldSurvivor.CompletedAt = &oldCompletedAt
	newSurvivor := repoDependencyIntentRow(
		"new-survivor",
		"git-repository-scope:repository:r_deploy",
		repoID,
		repoID,
		"repo_dependency:git-repository-scope:repository:r_deploy",
		"gen-new",
		now,
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:r_survivor",
			"relationship_type": "DISCOVERS_CONFIG_IN",
			"evidence_source":   crossRepoEvidenceSource,
		},
	)
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{newSurvivor},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{
			repoID: {oldRemoved, oldSurvivor, newSurvivor},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-new", true),
		Config:       RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := result.StaleIntents, 2; got != want {
		t.Fatalf("StaleIntents = %d, want %d", got, want)
	}
	if got, want := result.ActiveIntents, 1; got != want {
		t.Fatalf("ActiveIntents = %d, want %d", got, want)
	}
	if got, want := result.RetractedRows, 1; got != want {
		t.Fatalf("RetractedRows = %d, want %d", got, want)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retract calls = %d, want 1", len(writer.retractCalls))
	}
	if len(writer.writeCalls) != 1 || len(writer.writeCalls[0].rows) != 1 {
		t.Fatalf("write calls = %#v, want one survivor write", writer.writeCalls)
	}
	written := writer.writeCalls[0].rows[0]
	if got, want := stringValue(written.Payload["target_repo_id"]), "repository:r_survivor"; got != want {
		t.Fatalf("written target_repo_id = %q, want %q", got, want)
	}
	if got, want := stringValue(written.Payload["relationship_type"]), "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("written relationship_type = %q, want %q", got, want)
	}
	if len(reader.marked) != 3 {
		t.Fatalf("marked intents = %v, want old rows plus survivor completed", reader.marked)
	}
}
