package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// WorkloadStageResult captures the output of the workload projection stage.
type WorkloadStageResult struct {
	Intents        []ReducerIntent
	RepositoryIDs  []string
	SourceRunPairs map[string]string // repo_id -> source_run_id
}

// ProjectWorkloadStage extracts workload-relevant metadata from repository
// facts and builds reducer intents for downstream workload materialization.
func ProjectWorkloadStage(envelopes []facts.Envelope) WorkloadStageResult {
	repoFacts := FilterRepositoryFacts(envelopes)
	result := WorkloadStageResult{
		SourceRunPairs: make(map[string]string, len(repoFacts)),
	}

	seenRepos := make(map[string]struct{}, len(repoFacts))
	for i := range repoFacts {
		repoID, _ := payloadString(repoFacts[i].Payload, "repo_id")
		if repoID == "" {
			continue
		}
		if _, ok := seenRepos[repoID]; ok {
			continue
		}
		seenRepos[repoID] = struct{}{}
		result.RepositoryIDs = append(result.RepositoryIDs, repoID)

		sourceRunID, _ := payloadString(repoFacts[i].Payload, "source_run_id")
		if sourceRunID != "" {
			result.SourceRunPairs[repoID] = sourceRunID
		}
	}

	// Collect reducer intents from all facts (workload/platform intents
	// may come from any fact kind that carries a reducer_domain key).
	seen := make(map[string]struct{}, len(envelopes))
	for i := range envelopes {
		if _, ok := seen[envelopes[i].FactID]; ok {
			continue
		}
		seen[envelopes[i].FactID] = struct{}{}

		if intent, ok := buildReducerIntent(envelopes[i]); ok {
			result.Intents = append(result.Intents, intent)
		}
	}

	return result
}
