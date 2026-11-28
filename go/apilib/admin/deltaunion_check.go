// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// DeltaUnionActiveSource reads the ACTIVE side of the completeness check:
// every in-filter scope's active generation plus its union base.
//
// Implementations own their storage reads; apilib exports no SQL. The
// documented semantics a conforming implementation must reproduce (core's
// FactStore.ReadDeltaUnionActive is the reference):
//
//  1. List active generations for the filter; for a delta, its union-base
//     commit is COALESCE(delta_union_base_commit_sha,
//     delta_baseline_commit_sha, ”).
//  2. Resolve each delta's base as the newest projected full generation at
//     that commit (activated, status active/completed/superseded, intact
//     statuses first, then ingested_at desc, generation_id desc) — a
//     deliberate second spelling of the union's base rule, never a call
//     into the union overlay.
//  3. Read every fact of the wanted generations with file attribution
//     (file/content_entity/parsed_file_data via relative_path, content via
//     content_path, else ""), tombstones included with their flag, and
//     Entity set only for content_entity rows whose entity_type is
//     function/Function (the :Function population graph readers count).
//     Rows with no repo_id are dropped.
//  4. Read each active delta's repository fact (delta_generation=true) for
//     its changed/deleted relative paths, keyed by scope, with the baseline
//     commit from step 1.
type DeltaUnionActiveSource interface {
	ReadDeltaUnionActive(ctx context.Context, scopeIDs []string) (DeltaUnionActiveSnapshot, error)
}

// DeltaUnionGraphRunner runs one read-only Cypher statement. The check
// issues one anchored per-repo query, never a whole-graph traversal.
// Implementations should wrap availability/deadline failures with
// ErrGraphUnavailable / ErrGraphReadDeadline (or classify via
// ClassifyDeltaUnionGraphReadError) so DeltaUnionGraphErrorHandler can
// answer the shared verdicts.
type DeltaUnionGraphRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// DeltaUnionGraphFilesCypher reads one repository's GRAPH side: its file set
// with per-path Function counts. The Repository anchor uses id (the
// canonical node key) and it must stay valid on both supported backends:
// validate against the pinned NornicDB binary before claiming both dialects.
const DeltaUnionGraphFilesCypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
OPTIONAL MATCH (f)-[:CONTAINS]->(e:Function)
RETURN f.relative_path AS rel, count(e) AS entities`

// ErrDeltaUnionScopeRequired reports a check call without an explicit scope
// filter. An empty filter fans out over every active generation plus one
// anchored graph query per repository, so callers must name their scopes.
var ErrDeltaUnionScopeRequired = errors.New("delta union completeness requires at least one explicit scope filter")

// DeltaUnionCompletenessChecker is the read-only completeness check: per
// repository, the graph's file set against the ACTIVE set. It never
// repairs; a mismatch names the diff and the generations to re-parse. It
// satisfies DeltaUnionStore, so ServeDeltaUnionCompleteness and
// MountDeltaUnionCompleteness run it directly with
// DeltaUnionGraphErrorHandler as the non-nil store-error hook.
type DeltaUnionCompletenessChecker struct {
	// Active reads the ACTIVE side.
	Active DeltaUnionActiveSource
	// Graph reads the GRAPH side. Either nil makes Check report unconfigured.
	Graph DeltaUnionGraphRunner
	// MaxRepos bounds the repositories one call compares; non-positive
	// selects the 500 default.
	MaxRepos int
}

// Compile-time proof the checker satisfies the served store port.
var _ DeltaUnionStore = (*DeltaUnionCompletenessChecker)(nil)

// CheckDeltaUnionCompleteness runs Check; it is the DeltaUnionStore method.
func (c *DeltaUnionCompletenessChecker) CheckDeltaUnionCompleteness(ctx context.Context, scopeIDs []string) (DeltaUnionCompletenessReport, error) {
	return c.Check(ctx, scopeIDs)
}

// Check runs the completeness check over an explicit scope filter: at least
// one scope is required. A filter wider than MaxRepos fails before the
// ACTIVE read; scopes with no active generation, or whose active generation
// is not a delta, are reported skipped without a graph read; delta-active
// scopes past MaxRepos fail loudly. A graph failure propagates and never
// answers a partial report. Cap failures return *DeltaUnionCapError.
func (c *DeltaUnionCompletenessChecker) Check(
	ctx context.Context,
	scopeIDs []string,
) (DeltaUnionCompletenessReport, error) {
	if c == nil || c.Active == nil || c.Graph == nil {
		return DeltaUnionCompletenessReport{}, fmt.Errorf("delta union completeness check is not configured")
	}
	scopes := make([]string, 0, len(scopeIDs))
	for _, id := range scopeIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		scopes = append(scopes, id)
	}
	if len(scopes) == 0 {
		return DeltaUnionCompletenessReport{}, fmt.Errorf("%w: pass at least one ?scope= query value", ErrDeltaUnionScopeRequired)
	}
	maxRepos := c.MaxRepos
	if maxRepos <= 0 {
		maxRepos = defaultDeltaUnionMaxScopes
	}
	if len(scopes) > maxRepos {
		return DeltaUnionCompletenessReport{}, &DeltaUnionCapError{Count: len(scopes), Limit: maxRepos, Unit: "scopes"}
	}
	snapshot, err := c.Active.ReadDeltaUnionActive(ctx, scopes)
	if err != nil {
		return DeltaUnionCompletenessReport{}, fmt.Errorf("read delta union active: %w", err)
	}
	expected := AssembleDeltaUnionExpectations(snapshot.Rows, snapshot.Deltas)
	comparable := 0
	for _, exp := range expected {
		if exp.ActiveGenerationID == "" || !exp.ActiveIsDelta {
			continue
		}
		comparable++
	}
	if comparable > maxRepos {
		return DeltaUnionCompletenessReport{}, &DeltaUnionCapError{Count: comparable, Limit: maxRepos, Unit: "repositories"}
	}
	repos, err := c.compareExpected(ctx, expected)
	if err != nil {
		return DeltaUnionCompletenessReport{}, err
	}
	repos = appendUnseenScopeSkips(repos, expected, scopes)
	return SummarizeDeltaUnionCompleteness(repos), nil
}

// compareExpected compares every expectation, skipping non-delta scopes
// without a graph read.
func (c *DeltaUnionCompletenessChecker) compareExpected(ctx context.Context, expected []DeltaUnionExpectation) ([]DeltaUnionRepoReport, error) {
	repos := make([]DeltaUnionRepoReport, 0, len(expected))
	for _, exp := range expected {
		if exp.ActiveGenerationID == "" || !exp.ActiveIsDelta {
			annotation := fmt.Sprintf("repo=%s scope=%s: no active generation", exp.RepoID, exp.ScopeID)
			if exp.ActiveGenerationID != "" {
				annotation = fmt.Sprintf("repo=%s scope=%s: active generation is not a delta", exp.RepoID, exp.ScopeID)
			}
			repos = append(repos, DeltaUnionRepoReport{
				RepoID: exp.RepoID, ScopeID: exp.ScopeID,
				Outcome:    DeltaUnionRepoSkipped,
				Active:     DeltaUnionGeneration{},
				Annotation: annotation,
			})
			continue
		}
		observation, err := c.readGraphRepo(ctx, exp.RepoID)
		if err != nil {
			return nil, err
		}
		result := CompareDeltaUnionRepo(exp, observation)
		repos = append(repos, DeltaUnionRepoReport{
			RepoID:  result.RepoID,
			ScopeID: result.ScopeID,
			Outcome: result.Outcome,
			Base: DeltaUnionGeneration{
				GenerationID: exp.BaseGenerationID, Status: exp.BaseStatus,
			},
			Active: DeltaUnionGeneration{
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
	return repos, nil
}

// appendUnseenScopeSkips reports each requested scope that assembled no
// expectation as skipped, so an unknown scope never vanishes into an
// all-clear.
func appendUnseenScopeSkips(repos []DeltaUnionRepoReport, expected []DeltaUnionExpectation, scopes []string) []DeltaUnionRepoReport {
	seen := make(map[string]struct{}, len(expected))
	for _, exp := range expected {
		seen[exp.ScopeID] = struct{}{}
	}
	for _, scopeID := range scopes {
		if _, ok := seen[scopeID]; ok {
			continue
		}
		seen[scopeID] = struct{}{}
		repos = append(repos, DeltaUnionRepoReport{
			ScopeID: scopeID,
			Outcome: DeltaUnionRepoSkipped,
			Active:  DeltaUnionGeneration{},
			Annotation: fmt.Sprintf("repo=%s scope=%s: no active generation",
				"", scopeID),
		})
	}
	return repos
}

// readGraphRepo reads one repository's GRAPH side via the anchored Cypher.
func (c *DeltaUnionCompletenessChecker) readGraphRepo(
	ctx context.Context,
	repoID string,
) (DeltaUnionObservation, error) {
	observation := DeltaUnionObservation{RepoID: repoID, Files: map[string]int64{}}
	rows, err := c.Graph.Run(ctx, DeltaUnionGraphFilesCypher, map[string]any{"repo_id": repoID})
	if err != nil {
		return DeltaUnionObservation{}, fmt.Errorf("read delta union graph for repo %s: %w", repoID, err)
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
