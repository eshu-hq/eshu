// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const listPendingValueFlowProgramInputsSQL = `
SELECT acceptance.scope_id,
       acceptance.acceptance_unit_id AS repository_id,
       acceptance.source_run_id,
       acceptance.generation_id,
       max(intent.completed_at) AS completed_at
FROM shared_projection_acceptance AS acceptance
JOIN ingestion_scopes AS scope
  ON scope.scope_id = acceptance.scope_id
 AND scope.active_generation_id = acceptance.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = acceptance.generation_id
 AND generation.status = 'active'
JOIN shared_projection_intents AS intent
  ON intent.scope_id = acceptance.scope_id
 AND intent.acceptance_unit_id = acceptance.acceptance_unit_id
 AND intent.source_run_id = acceptance.source_run_id
 AND intent.generation_id = acceptance.generation_id
 AND intent.projection_domain = 'code_calls'
 AND intent.completed_at IS NOT NULL
WHERE NOT EXISTS (
    SELECT 1
    FROM shared_projection_intents AS pending_intent
    WHERE pending_intent.scope_id = acceptance.scope_id
      AND pending_intent.acceptance_unit_id = acceptance.acceptance_unit_id
      AND pending_intent.source_run_id = acceptance.source_run_id
      AND pending_intent.generation_id = acceptance.generation_id
      AND pending_intent.projection_domain = 'code_calls'
      AND pending_intent.completed_at IS NULL
)
  AND NOT EXISTS (
    SELECT 1
    FROM scope_generations AS newer_generation
    WHERE newer_generation.scope_id = acceptance.scope_id
      AND newer_generation.ingested_at > generation.ingested_at
      AND newer_generation.status <> 'superseded'
)
GROUP BY acceptance.scope_id, acceptance.acceptance_unit_id,
         acceptance.source_run_id, acceptance.generation_id
ORDER BY completed_at ASC, repository_id ASC
LIMIT $1
`

const listValueFlowProgramCallEdgesSQL = `
SELECT intent.payload->>'caller_entity_id' AS caller_entity_id,
       intent.payload->>'callee_entity_id' AS callee_entity_id,
       coalesce(nullif(intent.payload->>'relationship_type', ''), 'CALLS') AS relationship_type,
       coalesce(nullif(intent.payload->>'resolution_method', ''), 'unspecified') AS resolution_method,
       coalesce(caller_function.repo, '') AS caller_repo_id,
       coalesce(caller_function.function_id, '') AS caller_function_id,
       coalesce(callee_function.repo, '') AS callee_repo_id,
       coalesce(callee_function.function_id, '') AS callee_function_id
FROM shared_projection_intents AS intent
LEFT JOIN function_graph_ids AS caller_function
  ON caller_function.uid = intent.payload->>'caller_entity_id'
LEFT JOIN function_graph_ids AS callee_function
  ON callee_function.uid = intent.payload->>'callee_entity_id'
WHERE intent.scope_id = $1
  AND intent.acceptance_unit_id = $2
  AND intent.source_run_id = $3
  AND intent.generation_id = $4
  AND intent.projection_domain = 'code_calls'
  AND intent.completed_at IS NOT NULL
  AND intent.payload->>'caller_entity_id' <> ''
  AND intent.payload->>'callee_entity_id' <> ''
ORDER BY caller_entity_id ASC, callee_entity_id ASC, relationship_type ASC
`

const listValueFlowProgramSourcesSQL = `
SELECT function_id,
       param_index,
       kind
FROM function_sources
WHERE repo = $1
ORDER BY function_id ASC, param_index ASC, kind ASC
`

// ValueFlowProgramInputStore loads bounded runtime inputs for value-flow Program
// assembly from active code-call projection state and persisted summaries.
type ValueFlowProgramInputStore struct {
	db ExecQueryer
}

// NewValueFlowProgramInputStore constructs a Postgres-backed value-flow Program
// input loader.
func NewValueFlowProgramInputStore(db ExecQueryer) ValueFlowProgramInputStore {
	return ValueFlowProgramInputStore{db: db}
}

// LoadPendingValueFlowProgramInputs loads active-generation value-flow Program
// inputs after code_calls shared projection intents have completed.
func (s ValueFlowProgramInputStore) LoadPendingValueFlowProgramInputs(
	ctx context.Context,
	limit int,
) ([]reducer.ValueFlowProgramInput, error) {
	if s.db == nil {
		return nil, fmt.Errorf("value-flow program input store database is required")
	}
	if limit <= 0 {
		limit = 10
	}
	candidates, err := s.loadValueFlowProgramCandidates(ctx, limit)
	if err != nil {
		return nil, err
	}
	inputs := make([]reducer.ValueFlowProgramInput, 0, len(candidates))
	for _, candidate := range candidates {
		input, err := s.loadValueFlowProgramInput(ctx, candidate)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

type valueFlowProgramCandidate struct {
	scopeID      string
	repositoryID string
	sourceRunID  string
	generationID string
	completedAt  time.Time
}

func (s ValueFlowProgramInputStore) loadValueFlowProgramCandidates(
	ctx context.Context,
	limit int,
) ([]valueFlowProgramCandidate, error) {
	rows, err := s.db.QueryContext(ctx, listPendingValueFlowProgramInputsSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending value-flow program inputs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	candidates := make([]valueFlowProgramCandidate, 0, limit)
	for rows.Next() {
		var candidate valueFlowProgramCandidate
		if err := rows.Scan(
			&candidate.scopeID,
			&candidate.repositoryID,
			&candidate.sourceRunID,
			&candidate.generationID,
			&candidate.completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending value-flow program input: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query pending value-flow program inputs: %w", err)
	}
	return candidates, nil
}

func (s ValueFlowProgramInputStore) loadValueFlowProgramInput(
	ctx context.Context,
	candidate valueFlowProgramCandidate,
) (reducer.ValueFlowProgramInput, error) {
	edges, skipped, repos, err := s.loadValueFlowProgramCallEdges(ctx, candidate)
	if err != nil {
		return reducer.ValueFlowProgramInput{}, err
	}
	summaries, err := s.loadValueFlowProgramSummaries(ctx, repos)
	if err != nil {
		return reducer.ValueFlowProgramInput{}, err
	}
	sources, err := s.loadValueFlowProgramSources(ctx, repos)
	if err != nil {
		return reducer.ValueFlowProgramInput{}, err
	}
	return reducer.ValueFlowProgramInput{
		ScopeID:                candidate.scopeID,
		GenerationID:           candidate.generationID,
		RepositoryID:           candidate.repositoryID,
		SourceRunID:            candidate.sourceRunID,
		Summaries:              summaries,
		CallEdges:              edges,
		Sources:                sources,
		SkippedMissingIdentity: skipped,
	}, nil
}

func (s ValueFlowProgramInputStore) loadValueFlowProgramCallEdges(
	ctx context.Context,
	candidate valueFlowProgramCandidate,
) ([]reducer.ValueFlowCallEdge, int, map[string]struct{}, error) {
	rows, err := s.db.QueryContext(
		ctx,
		listValueFlowProgramCallEdgesSQL,
		candidate.scopeID,
		candidate.repositoryID,
		candidate.sourceRunID,
		candidate.generationID,
	)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("query value-flow program call edges: %w", err)
	}
	defer func() { _ = rows.Close() }()

	edges := make([]reducer.ValueFlowCallEdge, 0)
	repos := make(map[string]struct{})
	skipped := 0
	for rows.Next() {
		row := valueFlowProgramCallEdgeRow{}
		if err := row.scan(rows); err != nil {
			return nil, 0, nil, err
		}
		callerID := summary.FunctionID(strings.TrimSpace(row.caller.functionID))
		calleeID := summary.FunctionID(strings.TrimSpace(row.callee.functionID))
		if callerID == "" || calleeID == "" {
			skipped++
			continue
		}
		repos[row.caller.repo()] = struct{}{}
		repos[row.callee.repo()] = struct{}{}
		edges = append(edges, reducer.ValueFlowCallEdge{
			CallerEntityID:   row.callerEntityID,
			CalleeEntityID:   row.calleeEntityID,
			CallerFunctionID: callerID,
			CalleeFunctionID: calleeID,
			RelationshipType: strings.ToUpper(strings.TrimSpace(row.relationshipType)),
			ResolutionMethod: row.resolutionMethod,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, fmt.Errorf("query value-flow program call edges: %w", err)
	}
	return edges, skipped, repos, nil
}

func (s ValueFlowProgramInputStore) loadValueFlowProgramSummaries(
	ctx context.Context,
	repos map[string]struct{},
) (map[summary.FunctionID]summary.Effects, error) {
	out := make(map[summary.FunctionID]summary.Effects)
	store := NewFunctionSummaryStore(s.db)
	for _, repo := range sortedValueFlowProgramRepos(repos) {
		snap, err := store.LoadRepoSnapshot(ctx, repo)
		if err != nil {
			return nil, err
		}
		for _, fn := range snap.Functions {
			out[fn.ID] = fn.Effects
		}
	}
	return out, nil
}

func (s ValueFlowProgramInputStore) loadValueFlowProgramSources(
	ctx context.Context,
	repos map[string]struct{},
) ([]interproc.Source, error) {
	var out []interproc.Source
	for _, repo := range sortedValueFlowProgramRepos(repos) {
		rows, err := s.db.QueryContext(ctx, listValueFlowProgramSourcesSQL, repo)
		if err != nil {
			return nil, fmt.Errorf("query value-flow program sources: %w", err)
		}
		for rows.Next() {
			var functionID string
			var paramIndex int
			var kind string
			if err := rows.Scan(&functionID, &paramIndex, &kind); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan value-flow program source: %w", err)
			}
			if strings.TrimSpace(functionID) == "" || strings.TrimSpace(kind) == "" || paramIndex < 0 {
				continue
			}
			out = append(out, interproc.Source{
				Port: interproc.Port{
					Func: interproc.FunctionID(functionID),
					Slot: interproc.Slot{Kind: interproc.SlotParam, Index: paramIndex},
				},
				Kind: strings.TrimSpace(kind),
			})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("query value-flow program sources: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close value-flow program sources: %w", err)
		}
	}
	return out, nil
}

type valueFlowProgramCallEdgeRow struct {
	callerEntityID   string
	calleeEntityID   string
	relationshipType string
	resolutionMethod string
	caller           valueFlowProgramFunctionIdentity
	callee           valueFlowProgramFunctionIdentity
}

func (r *valueFlowProgramCallEdgeRow) scan(rows Rows) error {
	if err := rows.Scan(
		&r.callerEntityID,
		&r.calleeEntityID,
		&r.relationshipType,
		&r.resolutionMethod,
		&r.caller.repoID,
		&r.caller.functionID,
		&r.callee.repoID,
		&r.callee.functionID,
	); err != nil {
		return fmt.Errorf("scan value-flow program call edge: %w", err)
	}
	return nil
}

type valueFlowProgramFunctionIdentity struct {
	repoID     string
	functionID string
}

func (i valueFlowProgramFunctionIdentity) repo() string {
	if repo := strings.TrimSpace(i.repoID); repo != "" {
		return repo
	}
	return functionIDRepo(strings.TrimSpace(i.functionID))
}

func sortedValueFlowProgramRepos(repos map[string]struct{}) []string {
	out := make([]string, 0, len(repos))
	for repo := range repos {
		if strings.TrimSpace(repo) != "" {
			out = append(out, repo)
		}
	}
	sort.Strings(out)
	return out
}
