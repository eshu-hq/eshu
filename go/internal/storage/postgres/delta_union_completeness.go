// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// deltaUnionActiveGenerationsQuery lists the active generation of every
// in-filter scope: the generation the graph must reflect. For a delta it
// returns the commit its union base resolves at, the #8780 union-base
// commit COALESCEd to the baseline for a legacy delta, the same rule the
// union read applies. An empty filter covers every active generation; the
// LIMIT only guards the unfiltered call against a fleet-wide accident
// (delta-active scopes are few).
const deltaUnionActiveGenerationsQuery = `
SELECT scope_id, generation_id, is_delta,
       COALESCE(delta_union_base_commit_sha, delta_baseline_commit_sha, '')
FROM scope_generations
WHERE status = 'active'
  AND ($1 OR scope_id = ANY($2))
ORDER BY scope_id
LIMIT 2000
`

// deltaUnionActiveBaseQuery resolves, for each delta-active scope, the same
// base the union read resolves: the newest projected full generation at the
// union-base commit (#8780), intact statuses first. It is a deliberate second spelling
// of the union's base rule (deltaUnionBaseQuery), never a call into it, so
// one bug cannot hide in both.
const deltaUnionActiveBaseQuery = `
SELECT DISTINCT ON (a.scope_id)
    a.scope_id, g.generation_id
FROM (SELECT unnest($1::text[]) AS scope_id, unnest($2::text[]) AS baseline) AS a
JOIN scope_generations AS g
  ON g.scope_id = a.scope_id
 AND g.is_delta = false
 AND g.activated_at IS NOT NULL
 AND g.status IN ('active', 'completed', 'superseded')
 AND g.source_commit_sha = a.baseline
ORDER BY a.scope_id, (g.status IN ('active', 'completed')) DESC, g.ingested_at DESC, g.generation_id DESC
`

// deltaUnionActiveFactsQuery reads every fact of the wanted generations with
// the per-row file attribution the assembly needs. Tombstones ride along
// with their flag; the assembly excludes them from both sides. The Entity
// flag counts only the entity types the graph readers count: both readers
// count (f)-[:CONTAINS]->(e:Function) nodes, and the projection writes only
// function/Function content_entity rows as :Function nodes under the file
// (Module, Parameter, and Variable skip the generic phase-E entity write),
// so flagging every content_entity row would read correct graphs as Drifted.
// Both payload casings ride along: collectors emit function and Function.
const deltaUnionActiveFactsQuery = `
SELECT fr.scope_id,
       NULLIF(fr.payload ->> 'repo_id', ''),
       fr.generation_id,
       g.status,
       g.is_delta,
       fr.fact_kind,
       fr.stable_fact_key,
       CASE WHEN fr.fact_kind IN ('file', 'content_entity', 'parsed_file_data') THEN COALESCE(fr.payload ->> 'relative_path', '')
            WHEN fr.fact_kind = 'content' THEN COALESCE(fr.payload ->> 'content_path', '')
            ELSE '' END,
       fr.is_tombstone,
       (fr.fact_kind = 'content_entity' AND COALESCE(fr.payload ->> 'entity_type', '') IN ('function', 'Function'))
FROM (SELECT unnest($1::text[]) AS scope_id, unnest($2::text[]) AS generation_id) AS wanted
JOIN scope_generations AS g USING (scope_id, generation_id)
JOIN fact_records AS fr USING (scope_id, generation_id)
`

// deltaUnionActiveDeltaRepoQuery reads the active deltas' repository facts,
// the independent source of their touched paths and baselines.
const deltaUnionActiveDeltaRepoQuery = `
SELECT fr.scope_id,
       fr.payload -> 'delta_relative_paths',
       fr.payload -> 'delta_deleted_relative_paths'
FROM (SELECT unnest($1::text[]) AS scope_id, unnest($2::text[]) AS generation_id) AS wanted
JOIN fact_records AS fr USING (scope_id, generation_id)
WHERE fr.fact_kind = 'repository'
  AND COALESCE(fr.payload ->> 'delta_generation', 'false') = 'true'
`

// DeltaUnionActiveSnapshot is the ACTIVE side of the completeness check:
// the rows and delta paths goldengate.AssembleDeltaUnionExpectations turns
// into per-repo expectations.
type DeltaUnionActiveSnapshot struct {
	// Rows carries one row per fact of every wanted generation.
	Rows []goldengate.DeltaUnionActiveRow
	// Deltas carries touched paths by scope for active delta generations.
	Deltas map[string]goldengate.DeltaUnionDeltaPaths
}

// ReadDeltaUnionActive reads the ACTIVE side for the check: for every
// in-filter scope, its active generation plus, when the active generation is
// a delta, the union's base full generation at its baseline. An empty filter
// covers every active generation, bounded by the scope cap. It never calls
// the union overlay: the SQL re-derives the expected set independently.
// deltaUnionActiveGen is one in-filter active generation.
type deltaUnionActiveGen struct {
	scope, generation, baseline string
	isDelta                     bool
}

func (s FactStore) ReadDeltaUnionActive(
	ctx context.Context,
	scopeIDs []string,
) (DeltaUnionActiveSnapshot, error) {
	if s.database == nil {
		return DeltaUnionActiveSnapshot{}, fmt.Errorf("fact store database is required")
	}
	snapshot := DeltaUnionActiveSnapshot{Deltas: make(map[string]goldengate.DeltaUnionDeltaPaths)}
	var actives []deltaUnionActiveGen
	rows, err := s.database.QueryContext(ctx, deltaUnionActiveGenerationsQuery, len(scopeIDs) == 0, scopeIDs)
	if err != nil {
		return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union active generations: %w", err)
	}
	for rows.Next() {
		var gen deltaUnionActiveGen
		if err := rows.Scan(&gen.scope, &gen.generation, &gen.isDelta, &gen.baseline); err != nil {
			_ = rows.Close()
			return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union active generations scan: %w", err)
		}
		actives = append(actives, gen)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union active generations: %w", err)
	}
	_ = rows.Close()
	if len(actives) == 0 {
		return snapshot, nil
	}
	wantedScopes := make([]string, 0, len(actives))
	wantedGens := make([]string, 0, len(actives))
	var (
		deltaScopes    []string
		deltaBaselines []string
	)
	for _, gen := range actives {
		wantedScopes = append(wantedScopes, gen.scope)
		wantedGens = append(wantedGens, gen.generation)
		if gen.isDelta && strings.TrimSpace(gen.baseline) != "" {
			deltaScopes = append(deltaScopes, gen.scope)
			deltaBaselines = append(deltaBaselines, strings.TrimSpace(gen.baseline))
		}
	}
	if len(deltaScopes) != 0 {
		baseRows, err := s.database.QueryContext(ctx, deltaUnionActiveBaseQuery, deltaScopes, deltaBaselines)
		if err != nil {
			return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union base generations: %w", err)
		}
		for baseRows.Next() {
			var scope, generation string
			if err := baseRows.Scan(&scope, &generation); err != nil {
				_ = baseRows.Close()
				return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union base generations scan: %w", err)
			}
			wantedScopes = append(wantedScopes, scope)
			wantedGens = append(wantedGens, generation)
		}
		if err := baseRows.Err(); err != nil {
			_ = baseRows.Close()
			return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union base generations: %w", err)
		}
		_ = baseRows.Close()
	}
	factRows, err := s.database.QueryContext(ctx, deltaUnionActiveFactsQuery, wantedScopes, wantedGens)
	if err != nil {
		return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union facts: %w", err)
	}
	for factRows.Next() {
		var (
			row    goldengate.DeltaUnionActiveRow
			repoID *string
		)
		if err := factRows.Scan(&row.ScopeID, &repoID, &row.GenerationID, &row.GenerationStatus,
			&row.IsDelta, &row.Kind, &row.StableKey, &row.Path, &row.Tombstone, &row.Entity); err != nil {
			_ = factRows.Close()
			return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union facts scan: %w", err)
		}
		if repoID == nil || strings.TrimSpace(*repoID) == "" {
			continue
		}
		row.RepoID = strings.TrimSpace(*repoID)
		snapshot.Rows = append(snapshot.Rows, row)
	}
	if err := factRows.Err(); err != nil {
		_ = factRows.Close()
		return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union facts: %w", err)
	}
	_ = factRows.Close()
	if len(deltaScopes) != 0 {
		deltaGens := make([]string, 0, len(deltaScopes))
		byScope := make(map[string]deltaUnionActiveGen, len(actives))
		for _, gen := range actives {
			byScope[gen.scope] = gen
		}
		for _, scope := range deltaScopes {
			deltaGens = append(deltaGens, byScope[scope].generation)
		}
		repoRows, err := s.database.QueryContext(ctx, deltaUnionActiveDeltaRepoQuery, deltaScopes, deltaGens)
		if err != nil {
			return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union delta paths: %w", err)
		}
		for repoRows.Next() {
			var (
				scope      string
				changedRaw []byte
				deletedRaw []byte
			)
			if err := repoRows.Scan(&scope, &changedRaw, &deletedRaw); err != nil {
				_ = repoRows.Close()
				return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union delta paths scan: %w", err)
			}
			paths := goldengate.DeltaUnionDeltaPaths{}
			_ = json.Unmarshal(changedRaw, &paths.Changed)
			_ = json.Unmarshal(deletedRaw, &paths.Deleted)
			for i := range deltaScopes {
				if deltaScopes[i] == scope {
					paths.BaselineCommit = deltaBaselines[i]
					break
				}
			}
			snapshot.Deltas[scope] = paths
		}
		if err := repoRows.Err(); err != nil {
			_ = repoRows.Close()
			return DeltaUnionActiveSnapshot{}, fmt.Errorf("read delta union delta paths: %w", err)
		}
		_ = repoRows.Close()
	}
	return snapshot, nil
}
