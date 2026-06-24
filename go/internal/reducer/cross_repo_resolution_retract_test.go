// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestCrossRepoResolutionEmitsRetractIntentWhenGenerationHasNoEvidence(t *testing.T) {
	t.Parallel()

	intentWriter := &recordingRepoDependencyIntentWriter{}
	persister := &fakeResolutionPersister{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: nil},
		IntentWriter:   intentWriter,
		Persister:      persister,
	}

	count, err := handler.Resolve(
		context.Background(),
		"git-repository-scope:repository:r_deploy",
		"gen-removed-evidence",
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want one retract intent", count)
	}
	if len(persister.activatedGenerations) != 1 {
		t.Fatalf("activated generations = %d, want 1", len(persister.activatedGenerations))
	}
	if got, want := persister.activatedGenerations[0], "gen-removed-evidence"; got != want {
		t.Fatalf("activated generation = %q, want %q", got, want)
	}
	if len(intentWriter.rows) != 1 || len(intentWriter.rows[0]) != 1 {
		t.Fatalf("intent writes = %#v, want one retract row", intentWriter.rows)
	}

	row := intentWriter.rows[0][0]
	if got, want := row.RepositoryID, "repository:r_deploy"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := row.AcceptanceUnitID, "repository:r_deploy"; got != want {
		t.Fatalf("AcceptanceUnitID = %q, want %q", got, want)
	}
	if got, want := row.GenerationID, "gen-removed-evidence"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["repo_id"]), "repository:r_deploy"; got != want {
		t.Fatalf("payload repo_id = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["action"]), "retract"; got != want {
		t.Fatalf("payload action = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["evidence_source"]), crossRepoEvidenceSource; got != want {
		t.Fatalf("payload evidence_source = %q, want %q", got, want)
	}
	if got := stringValue(row.Payload["target_repo_id"]); got != "" {
		t.Fatalf("payload target_repo_id = %q, want blank for retract-only row", got)
	}
}

func TestCrossRepoResolutionEmitsRetractIntentWhenEvidenceNoLongerResolves(t *testing.T) {
	t.Parallel()

	evidence := []relationships.EvidenceFact{
		{
			EvidenceKind:     relationships.EvidenceKindTerraformConfigPath,
			RelationshipType: relationships.RelDiscoversConfigIn,
			SourceRepoID:     "repository:r_deploy",
			TargetRepoID:     "repository:r_config",
			Confidence:       0.50,
			Rationale:        "below admission threshold",
		},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{facts: evidence},
		IntentWriter:   intentWriter,
	}

	count, err := handler.Resolve(
		context.Background(),
		"git-repository-scope:repository:r_deploy",
		"gen-low-confidence",
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want one retract intent", count)
	}
	if len(intentWriter.rows) != 1 || len(intentWriter.rows[0]) != 1 {
		t.Fatalf("intent writes = %#v, want one retract row", intentWriter.rows)
	}
	row := intentWriter.rows[0][0]
	if got, want := stringValue(row.Payload["action"]), "retract"; got != want {
		t.Fatalf("payload action = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["relationship_type"]), ""; got != want {
		t.Fatalf("payload relationship_type = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["repo_id"]), "repository:r_deploy"; got != want {
		t.Fatalf("payload repo_id = %q, want %q", got, want)
	}
}
