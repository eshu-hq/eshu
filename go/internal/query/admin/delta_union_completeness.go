// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// defaultDeltaUnionMaxRepos bounds one completeness check to 500
// repositories: one anchored graph query each. Past that the caller narrows
// the scope filter instead of paging silently.
const defaultDeltaUnionMaxRepos = 500

// DeltaUnionActiveSource reads the ACTIVE side of the completeness check:
// every in-filter scope's active generation plus its union base.
type DeltaUnionActiveSource interface {
	ReadDeltaUnionActive(ctx context.Context, scopeIDs []string) (pgstatus.DeltaUnionActiveSnapshot, error)
}

// DeltaUnionGraphRunner runs one read-only Cypher statement. The query-layer
// graph reader satisfies it; the check issues one anchored per-repo query,
// never a whole-graph traversal.
type DeltaUnionGraphRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// deltaUnionGraphFilesCypher reads one repository's GRAPH side: its file set
// with per-path Function counts. The Repository anchor uses id (the
// canonical node key), the shape mirrors the production deadcode surface,
// and it must stay valid on both supported backends: validate against the
// pinned NornicDB binary before claiming both dialects.
const deltaUnionGraphFilesCypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
OPTIONAL MATCH (f)-[:CONTAINS]->(e:Function)
RETURN f.relative_path AS rel, count(e) AS entities`

// DeltaUnionCompletenessChecker is the read-only prod completeness check
// (#7797): per repository, the graph's file set against the ACTIVE set. It
// never repairs; a mismatch names the diff and the generations to re-parse.
type DeltaUnionCompletenessChecker struct {
	// Active reads the ACTIVE side from Postgres.
	Active DeltaUnionActiveSource
	// Graph reads the GRAPH side. Both nil make Check report unconfigured.
	Graph DeltaUnionGraphRunner
	// MaxRepos bounds the repositories one call compares; non-positive
	// selects the default.
	MaxRepos int
}

// Check runs the completeness check over the scope filter (empty covers
// every active generation, bounded by the ACTIVE read's scope cap).
func (c *DeltaUnionCompletenessChecker) Check(
	ctx context.Context,
	scopeIDs []string,
) (recovery.DeltaUnionCompletenessReport, error) {
	if c == nil || c.Active == nil || c.Graph == nil {
		return recovery.DeltaUnionCompletenessReport{}, fmt.Errorf("delta union completeness check is not configured")
	}
	maxRepos := c.MaxRepos
	if maxRepos <= 0 {
		maxRepos = defaultDeltaUnionMaxRepos
	}
	snapshot, err := c.Active.ReadDeltaUnionActive(ctx, scopeIDs)
	if err != nil {
		return recovery.DeltaUnionCompletenessReport{}, fmt.Errorf("read delta union active: %w", err)
	}
	expected := goldengate.AssembleDeltaUnionExpectations(snapshot.Rows, snapshot.Deltas)
	var comparable []goldengate.DeltaUnionExpectation
	for _, exp := range expected {
		if exp.ActiveGenerationID == "" {
			continue
		}
		comparable = append(comparable, exp)
	}
	if len(comparable) > maxRepos {
		return recovery.DeltaUnionCompletenessReport{}, fmt.Errorf(
			"delta union completeness covers %d repositories, past the %d cap: narrow the scope filter", len(comparable), maxRepos)
	}
	repos := make([]recovery.DeltaUnionRepoReport, 0, len(expected))
	for _, exp := range expected {
		if exp.ActiveGenerationID == "" {
			repos = append(repos, recovery.DeltaUnionRepoReport{
				RepoID: exp.RepoID, ScopeID: exp.ScopeID,
				Outcome: recovery.DeltaUnionRepoSkipped,
				Active:  recovery.DeltaUnionGeneration{},
				Annotation: fmt.Sprintf("repo=%s scope=%s: no active generation",
					exp.RepoID, exp.ScopeID),
			})
			continue
		}
		observation, err := c.readGraphRepo(ctx, exp.RepoID)
		if err != nil {
			return recovery.DeltaUnionCompletenessReport{}, err
		}
		result := goldengate.CompareDeltaUnionRepo(exp, observation)
		repos = append(repos, recovery.DeltaUnionRepoReport{
			RepoID:  result.RepoID,
			ScopeID: result.ScopeID,
			Outcome: recovery.DeltaUnionRepoOutcome(result.Outcome),
			Base: recovery.DeltaUnionGeneration{
				GenerationID: exp.BaseGenerationID, Status: exp.BaseStatus,
			},
			Active: recovery.DeltaUnionGeneration{
				GenerationID: exp.ActiveGenerationID, Status: "active", IsDelta: exp.ActiveIsDelta,
			},
			GraphFiles:  len(observation.Files),
			ActiveFiles: len(exp.Files),
			Missing:     result.Missing,
			Extra:       result.Extra,
			Drifted:     result.Drifted,
			Annotation:  result.Annotation,
		})
	}
	return recovery.SummarizeDeltaUnionCompleteness(repos), nil
}

// readGraphRepo reads one repository's GRAPH side.
func (c *DeltaUnionCompletenessChecker) readGraphRepo(
	ctx context.Context,
	repoID string,
) (goldengate.DeltaUnionObservation, error) {
	observation := goldengate.DeltaUnionObservation{RepoID: repoID, Files: map[string]int64{}}
	rows, err := c.Graph.Run(ctx, deltaUnionGraphFilesCypher, map[string]any{"repo_id": repoID})
	if err != nil {
		return goldengate.DeltaUnionObservation{}, fmt.Errorf("read delta union graph for repo %s: %w", repoID, err)
	}
	for _, row := range rows {
		rel, _ := row["rel"].(string)
		if rel == "" {
			continue
		}
		observation.Files[rel] = deltaUnionGraphCount(row["entities"])
	}
	return observation, nil
}

// deltaUnionGraphCount converts a Bolt count cell to int64 across driver
// numeric spellings.
func deltaUnionGraphCount(value any) int64 {
	switch count := value.(type) {
	case int64:
		return count
	case int:
		return int64(count)
	case float64:
		return int64(count)
	default:
		return 0
	}
}

// deltaUnionCompleteness serves GET /api/v0/admin/delta-union/completeness:
// the read-only prod completeness report over ?scope= filters (empty covers
// every active generation). It never repairs.
func (h *Handler) deltaUnionCompleteness(w http.ResponseWriter, r *http.Request) {
	if h.Completeness == nil {
		querycontract.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable",
			"detail": "The delta-union completeness check is not configured on this API instance.",
		})
		return
	}
	report, err := h.Completeness.Check(r.Context(), r.URL.Query()["scope"])
	if err != nil {
		querycontract.WriteJSON(w, http.StatusInternalServerError, map[string]any{
			"status": "error",
			"detail": err.Error(),
		})
		return
	}
	querycontract.WriteJSON(w, http.StatusOK, report)
}
