// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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

// ErrDeltaUnionScopeRequired reports a completeness check call without an
// explicit scope filter. An empty filter fans out over every active
// generation plus one anchored graph query per repository (#7797: p95
// 1.648s past the 1.5s default budget), so callers must name their scopes.
// The read-api latency gate's no-arg sweep answers this with a fast 400 and
// stays unexercised instead of sampling the fan-out; a named latency/work
// budget for scoped calls must be rendered from GREEN gate reports via
// scripts/refresh-read-api-work-budgets.sh, never hand-written.
var ErrDeltaUnionScopeRequired = errors.New("delta union completeness requires at least one explicit scope filter")

// Check runs the completeness check over an explicit scope filter: at
// least one scope is required (see ErrDeltaUnionScopeRequired). A filter
// wider than MaxRepos fails before the ACTIVE fact fan-out; scopes with
// no active generation, or whose active generation is not a delta, are
// reported skipped without a graph read; delta-active scopes past
// MaxRepos fail loudly instead of paging silently.
func (c *DeltaUnionCompletenessChecker) Check(
	ctx context.Context,
	scopeIDs []string,
) (recovery.DeltaUnionCompletenessReport, error) {
	if c == nil || c.Active == nil || c.Graph == nil {
		return recovery.DeltaUnionCompletenessReport{}, fmt.Errorf("delta union completeness check is not configured")
	}
	scopes := make([]string, 0, len(scopeIDs))
	for _, id := range scopeIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		scopes = append(scopes, id)
	}
	if len(scopes) == 0 {
		return recovery.DeltaUnionCompletenessReport{}, fmt.Errorf("%w: pass at least one ?scope= query value", ErrDeltaUnionScopeRequired)
	}
	maxRepos := c.MaxRepos
	if maxRepos <= 0 {
		maxRepos = defaultDeltaUnionMaxRepos
	}
	// Reject an over-cap filter before the ACTIVE fact fan-out: the read
	// loads every fact of every in-filter generation plus their bases, so
	// a filter wider than the cap must fail before that work, not after.
	// The post-assembly cap check below stays as defense in depth for the
	// delta-active subset.
	if len(scopes) > maxRepos {
		return recovery.DeltaUnionCompletenessReport{}, fmt.Errorf(
			"delta union completeness covers %d scopes, past the %d cap: narrow the scope filter", len(scopes), maxRepos)
	}
	snapshot, err := c.Active.ReadDeltaUnionActive(ctx, scopes)
	if err != nil {
		return recovery.DeltaUnionCompletenessReport{}, fmt.Errorf("read delta union active: %w", err)
	}
	expected := goldengate.AssembleDeltaUnionExpectations(snapshot.Rows, snapshot.Deltas)
	var comparable []goldengate.DeltaUnionExpectation
	for _, exp := range expected {
		// Only delta-active scopes issue graph reads: the check covers
		// delta-active scopes, so a full active generation is out of
		// this comparison's scope and skips without a graph read.
		if exp.ActiveGenerationID == "" || !exp.ActiveIsDelta {
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
		if exp.ActiveGenerationID == "" || !exp.ActiveIsDelta {
			annotation := fmt.Sprintf("repo=%s scope=%s: no active generation",
				exp.RepoID, exp.ScopeID)
			if exp.ActiveGenerationID != "" {
				annotation = fmt.Sprintf("repo=%s scope=%s: active generation is not a delta",
					exp.RepoID, exp.ScopeID)
			}
			repos = append(repos, recovery.DeltaUnionRepoReport{
				RepoID: exp.RepoID, ScopeID: exp.ScopeID,
				Outcome:    recovery.DeltaUnionRepoSkipped,
				Active:     recovery.DeltaUnionGeneration{},
				Annotation: annotation,
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
	// A requested scope with no rows and no delta entry assembles no
	// expectation, so without this it would vanish into an all-clear.
	// Report each such scope skipped with a reason, in the same shape as
	// the no-active-generation skips above; skipped entries never count
	// as mismatches (see SummarizeDeltaUnionCompleteness).
	seen := make(map[string]struct{}, len(expected))
	for _, exp := range expected {
		seen[exp.ScopeID] = struct{}{}
	}
	for _, scopeID := range scopes {
		if _, ok := seen[scopeID]; ok {
			continue
		}
		seen[scopeID] = struct{}{}
		repos = append(repos, recovery.DeltaUnionRepoReport{
			ScopeID: scopeID,
			Outcome: recovery.DeltaUnionRepoSkipped,
			Active:  recovery.DeltaUnionGeneration{},
			Annotation: fmt.Sprintf("repo=%s scope=%s: no active generation",
				"", scopeID),
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
// the read-only prod completeness report over explicit ?scope= filters (at
// least one is required; an empty filter fans out and is rejected with
// 400). It never repairs.
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
		if errors.Is(err, ErrDeltaUnionScopeRequired) {
			querycontract.WriteJSON(w, http.StatusBadRequest, map[string]any{
				"status": "error",
				"detail": err.Error(),
			})
			return
		}
		querycontract.WriteJSON(w, http.StatusInternalServerError, map[string]any{
			"status": "error",
			"detail": err.Error(),
		})
		return
	}
	querycontract.WriteJSON(w, http.StatusOK, report)
}
