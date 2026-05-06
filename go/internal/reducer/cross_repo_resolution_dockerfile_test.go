package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestCrossRepoResolutionPromotesDockerfileSourceLabelToCanonicalDeploysFrom(t *testing.T) {
	t.Parallel()

	intentWriter := &recordingRepoDependencyIntentWriter{}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: &fakeEvidenceFactLoader{
			facts: []relationships.EvidenceFact{
				{
					EvidenceKind:     relationships.EvidenceKindDockerfileSourceLabel,
					RelationshipType: relationships.RelDeploysFrom,
					SourceRepoID:     "repo-runtime",
					TargetRepoID:     "repo-payments",
					Confidence:       0.93,
				},
			},
		},
		IntentWriter: intentWriter,
	}

	count, err := handler.Resolve(context.Background(), "scope-dockerfile", "gen-dockerfile")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Resolve() = %d, want 1", count)
	}
	if len(intentWriter.rows) != 1 || len(intentWriter.rows[0]) != 1 {
		t.Fatalf("intent writes = %#v, want 1 row", intentWriter.rows)
	}

	row := intentWriter.rows[0][0]
	if got, want := stringValue(row.Payload["relationship_type"]), string(relationships.RelDeploysFrom); got != want {
		t.Fatalf("relationship_type = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["target_repo_id"]), "repo-payments"; got != want {
		t.Fatalf("target_repo_id = %q, want %q", got, want)
	}
	if got, want := stringValue(row.Payload["evidence_type"]), "dockerfile_source_label"; got != want {
		t.Fatalf("evidence_type = %q, want %q", got, want)
	}
}
