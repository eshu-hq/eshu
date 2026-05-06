package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestWorkloadStageExtractsRepositoryIDs(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:   "repo-1",
			FactKind: "repository",
			Payload: map[string]any{
				"repo_id":       "repository:r_payments",
				"source_run_id": "run-42",
			},
		},
	}

	result := ProjectWorkloadStage(envelopes)
	if len(result.RepositoryIDs) != 1 {
		t.Fatalf("RepositoryIDs len = %d, want 1", len(result.RepositoryIDs))
	}
	if result.RepositoryIDs[0] != "repository:r_payments" {
		t.Errorf("RepositoryIDs[0] = %q", result.RepositoryIDs[0])
	}
	if result.SourceRunPairs["repository:r_payments"] != "run-42" {
		t.Errorf("SourceRunPairs missing run-42")
	}
}

func TestWorkloadStageDeduplicatesRepos(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactID: "r-1", FactKind: "repository", Payload: map[string]any{"repo_id": "repo:r1"}},
		{FactID: "r-2", FactKind: "repository", Payload: map[string]any{"repo_id": "repo:r1"}},
	}

	result := ProjectWorkloadStage(envelopes)
	if len(result.RepositoryIDs) != 1 {
		t.Errorf("RepositoryIDs len = %d, want 1 (deduped)", len(result.RepositoryIDs))
	}
}

func TestWorkloadStageCollectsReducerIntents(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:       "w-1",
			FactKind:     "file",
			ScopeID:      "scope-1",
			GenerationID: "gen-1",
			Payload: map[string]any{
				"reducer_domain": "cloud_asset_resolution",
				"entity_key":     "platform:aws:ecs",
			},
		},
	}

	result := ProjectWorkloadStage(envelopes)
	if len(result.Intents) != 1 {
		t.Fatalf("Intents len = %d, want 1", len(result.Intents))
	}
}

func TestWorkloadStageSkipsEmptyRepoID(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactID: "r-1", FactKind: "repository", Payload: map[string]any{"repo_id": ""}},
		{FactID: "r-2", FactKind: "repository", Payload: map[string]any{}},
	}

	result := ProjectWorkloadStage(envelopes)
	if len(result.RepositoryIDs) != 0 {
		t.Errorf("RepositoryIDs len = %d, want 0", len(result.RepositoryIDs))
	}
}
