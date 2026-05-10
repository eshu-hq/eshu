package collector

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	terraformStateCandidateSource  = "git_local_file"
	terraformStateCandidateBackend = "local"
	terraformStateCandidateWarning = "state_in_vcs"
)

// TerraformStateCandidate describes a repo-local Terraform state file without
// carrying raw state bytes or absolute filesystem paths.
type TerraformStateCandidate struct {
	RelativePath string
	PathHash     string
	FileSize     int64
}

func extractTerraformStateCandidates(
	repoPath string,
	files []string,
) ([]string, []TerraformStateCandidate) {
	candidates := make([]TerraformStateCandidate, 0)
	filtered := files[:0]
	for _, path := range files {
		if !isTerraformStateCandidateName(filepath.Base(path)) {
			filtered = append(filtered, path)
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		relativePath, err := filepath.Rel(repoPath, path)
		if err != nil {
			continue
		}
		relativePath = filepath.ToSlash(filepath.Clean(relativePath))
		candidates = append(candidates, TerraformStateCandidate{
			RelativePath: relativePath,
			PathHash: facts.StableID("TerraformStateCandidatePath", map[string]any{
				"relative_path": relativePath,
			}),
			FileSize: info.Size(),
		})
	}

	slices.SortFunc(candidates, func(a, b TerraformStateCandidate) int {
		return strings.Compare(a.RelativePath, b.RelativePath)
	})
	return filtered, candidates
}

func isTerraformStateCandidateName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".tfstate")
}

func terraformStateCandidateFactEnvelope(
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	candidate TerraformStateCandidate,
) facts.Envelope {
	payload := map[string]any{
		"candidate_source": terraformStateCandidateSource,
		"backend_kind":     terraformStateCandidateBackend,
		"repo_id":          repoID,
		"relative_path":    candidate.RelativePath,
		"path_hash":        candidate.PathHash,
		"file_size":        candidate.FileSize,
		"warning_flags":    []string{terraformStateCandidateWarning},
	}
	factKey := "terraform_state_candidate:" + repoID + ":" + candidate.PathHash
	sourceURI := "git://" + repoID + "/" + candidate.RelativePath

	return facts.Envelope{
		FactID: facts.StableID(
			"GoGitCollectorFact",
			map[string]any{
				"fact_key":      factKey,
				"fact_kind":     facts.TerraformStateCandidateFactKind,
				"generation_id": generationID,
				"scope_id":      scopeID,
			},
		),
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.TerraformStateCandidateFactKind,
		StableFactKey:    factKey,
		SchemaVersion:    facts.TerraformStateCandidateSchemaVersion,
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   "git",
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        factKey,
			SourceURI:      sourceURI,
			SourceRecordID: factKey,
		},
	}
}

func logTerraformStateCandidateDiscovery(
	ctx context.Context,
	s NativeRepositorySnapshotter,
	repoPath string,
	count int,
) {
	if s.Logger == nil || count == 0 {
		return
	}
	s.Logger.InfoContext(ctx, "terraform state candidates discovered",
		"collector_kind", "git",
		"repo_path", filepath.Base(repoPath),
		"candidate_source", terraformStateCandidateSource,
		"candidate_count", count,
	)
}
