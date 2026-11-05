// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The production fan-out port (cmd/reducer supplyChainImpactFanOutFor returns
// this store unwrapped) must satisfy the reader the marker branch asserts;
// without it every marker intent fails terminally on its first claim.
var _ supplychaincore.ManifestDependencyGenerationReader = SupplyChainImpactFanOutStore{}

// manifestGenerationColumns is the sixteen-column fact projection
// scanSupplyChainImpactFact decodes.
const manifestGenerationColumns = `
    fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind,
    fact.stable_fact_key, fact.schema_version, fact.collector_kind,
    fact.fencing_token, fact.source_confidence, fact.source_system,
    fact.source_fact_key, COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''), fact.observed_at,
    fact.is_tombstone, fact.payload`

// manifestGenerationRepositoryQuery reads one generation's repository fact:
// its repo_id anchors the dependency read, and a delta's copy names the
// changed and deleted paths. Served by the (scope_id, generation_id,
// fact_kind) prefix of fact_records_scope_generation_idx.
const manifestGenerationRepositoryQuery = `SELECT` + manifestGenerationColumns + `
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind IN ('repository', 'repositoryFact')
`

// manifestGenerationRepoIDsQuery returns the repo_id values one generation's
// repository fact carries, the anchor of the dependency read. It reads the
// same rows as manifestGenerationRepositoryQuery through the same index; the
// value is extracted in SQL so the store never reads a raw payload key.
const manifestGenerationRepoIDsQuery = `
SELECT DISTINCT BTRIM(fact.payload->>'repo_id')
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind IN ('repository', 'repositoryFact')
  AND COALESCE(BTRIM(fact.payload->>'repo_id'), '') <> ''
`

// manifestGenerationDependencyQuery reads one generation's live git dependency
// rows. The predicates are exactly the partial predicate of
// fact_records_content_entity_dependency_variable_repo_idx (migration 121), and
// repo_id = ANY($3) is its leading key, so the read seeks the generation's
// dependency rows instead of walking every content entity: 1.1 ms against
// 4.6 ms on a 3,000-entity generation in the #9874 shim.
const manifestGenerationDependencyQuery = `SELECT` + manifestGenerationColumns + `
FROM fact_records AS fact
WHERE (fact.payload->>'repo_id') = ANY($3::text[])
  AND fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind = 'content_entity'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'entity_type' = 'Variable'
  AND (fact.payload->'entity_metadata'->>'config_kind') = 'dependency'
`

// manifestGenerationTombstoneQuery reads one generation's tombstoned git
// dependency rows through fact_records_changed_since_tombstone_keys_idx
// (migration 195, partial on is_tombstone and non-reducer kinds).
const manifestGenerationTombstoneQuery = `SELECT` + manifestGenerationColumns + `
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.is_tombstone = TRUE
  AND fact.fact_kind NOT LIKE 'reducer\_%'
  AND fact.fact_kind = 'content_entity'
  AND fact.payload->>'entity_type' = 'Variable'
  AND (fact.payload->'entity_metadata'->>'config_kind') = 'dependency'
`

// manifestWatermarkQuery resolves the baseline W of a marker pass (#9874
// ruling D2): the newest generation before the intent's own row, in the
// activation order the projector supersedes by (ingested_at, generation_id),
// that ever activated and whose supply_chain_impact reducer item carries the
// completion stamp, whatever that item's status. A generation whose marker the
// runtime guard skipped, or that a claim sweep superseded, has no stamp and is
// passed over, so its pins are diffed as part of the chain instead. The stamp
// probe is an index range scan on fact_work_items_scope_generation_idx.
const manifestWatermarkQuery = `
SELECT prior.generation_id
FROM scope_generations AS intent
JOIN scope_generations AS prior
  ON prior.scope_id = intent.scope_id
WHERE intent.scope_id = $1
  AND intent.generation_id = $2
  AND prior.activated_at IS NOT NULL
  AND (prior.ingested_at, prior.generation_id) < (intent.ingested_at, intent.generation_id)
  AND EXISTS (
      SELECT 1
      FROM fact_work_items AS work
      WHERE work.scope_id = prior.scope_id
        AND work.generation_id = prior.generation_id
        AND work.stage = 'reducer'
        AND work.domain = 'supply_chain_impact'
        AND work.payload ? '` + supplychaincore.PayloadKeyManifestTriggerCompleted + `'
  )
ORDER BY prior.ingested_at DESC, prior.generation_id DESC
LIMIT 1
`

// manifestChainQuery lists the activated generations strictly between the
// baseline ($3, or the scope's start when empty) and the intent, oldest first,
// at most $4 rows. The caller passes the depth cap: (W, current] counts the
// current generation, so a full page of cap intermediates means the chain is
// over the cap.
const manifestChainQuery = `
SELECT chain.generation_id
FROM scope_generations AS intent
JOIN scope_generations AS chain
  ON chain.scope_id = intent.scope_id
LEFT JOIN scope_generations AS baseline
  ON baseline.scope_id = intent.scope_id
 AND baseline.generation_id = $3
WHERE intent.scope_id = $1
  AND intent.generation_id = $2
  AND chain.activated_at IS NOT NULL
  AND (chain.ingested_at, chain.generation_id) < (intent.ingested_at, intent.generation_id)
  AND ($3 = '' OR (chain.ingested_at, chain.generation_id) > (baseline.ingested_at, baseline.generation_id))
ORDER BY chain.ingested_at ASC, chain.generation_id ASC
LIMIT $4
`

// ReadManifestDependencyGeneration implements
// supplychaincore.ManifestDependencyGenerationReader (#9874). In one read-only
// transaction under the holder-read timeout it decides the activation state
// with the shared generation freshness check (equal: active; pending before
// activation: pending; anything older, failed, or pruned: superseded), then
// reads the full view of the current generation, of the baseline W (newest
// stamped activated prior), and of every activated generation between them.
// A delta view also carries its #7797 base rows, resolved by the same rules
// the delta union uses; a missing, pruned, or legacy base is BaseMissing,
// never guessed. A chain (W, current] longer than
// ManifestTriggerChainDepthCap (the current generation counted), or a
// baseline whose facts retention pruned, is Degraded; a pruned intermediate is
// skipped and reported as ChainPruned.
func (s SupplyChainImpactFanOutStore) ReadManifestDependencyGeneration(
	ctx context.Context,
	scopeID string,
	generationID string,
) (supplychaincore.ManifestDependencyGeneration, error) {
	var out supplychaincore.ManifestDependencyGeneration
	tx, err := s.beginBoundedRead(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()

	current, err := NewGenerationFreshnessCheck(tx)(ctx, scopeID, generationID)
	var notYet reducercontract.GenerationNotYetActiveError
	switch {
	case errors.As(err, &notYet):
		out.Activation = supplychaincore.ManifestGenerationPending
		return out, nil
	case err != nil:
		return out, classifyFanOutReadError(err)
	case !current:
		out.Activation = supplychaincore.ManifestGenerationSuperseded
		return out, nil
	}
	out.Activation = supplychaincore.ManifestGenerationActive
	store := FactStore{database: tx}
	// rows caches each generation's rows for the pass: a base full shared by
	// several delta views, or a baseline that is also the current delta's
	// base, is read once (review B2).
	rows := manifestRowCache{}
	if out.Current, err = readManifestView(ctx, tx, store, rows, scopeID, generationID); err != nil {
		return out, err
	}

	baselineID, err := queryManifestGenerationID(ctx, tx, manifestWatermarkQuery, scopeID, generationID)
	if err != nil {
		return out, err
	}
	chain, err := queryManifestGenerationIDs(ctx, tx, manifestChainQuery,
		scopeID, generationID, baselineID, supplychaincore.ManifestTriggerChainDepthCap)
	if err != nil {
		return out, err
	}
	// (W, current] is the intermediates plus the current generation.
	if len(chain)+1 > supplychaincore.ManifestTriggerChainDepthCap {
		out.Degraded = true
		return out, nil
	}
	if baselineID != "" {
		pruned, err := store.deltaUnionBasePruned(ctx, scopeID, baselineID)
		if err != nil {
			return out, classifyFanOutReadError(err)
		}
		if pruned {
			out.Degraded = true
			return out, nil
		}
		out.HasBaseline = true
		if out.Baseline, err = readManifestView(ctx, tx, store, rows, scopeID, baselineID); err != nil {
			return out, err
		}
	}
	for _, id := range chain {
		pruned, err := store.deltaUnionBasePruned(ctx, scopeID, id)
		if err != nil {
			return out, classifyFanOutReadError(err)
		}
		if pruned {
			out.ChainPruned = true
			continue
		}
		view, err := readManifestView(ctx, tx, store, rows, scopeID, id)
		if err != nil {
			return out, err
		}
		out.Chain = append(out.Chain, view)
	}
	return out, nil
}

// manifestRowCache holds each generation's rows for one marker pass, keyed by
// generation id.
type manifestRowCache map[string][]facts.Envelope

// rowsFor returns the generation's rows, reading them on first use.
func (c manifestRowCache) rowsFor(ctx context.Context, tx db.Queryer, scopeID, generationID string) ([]facts.Envelope, error) {
	if rows, ok := c[generationID]; ok {
		return rows, nil
	}
	rows, err := readManifestGenerationRows(ctx, tx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	c[generationID] = rows
	return rows, nil
}

// readManifestView reads one generation's dependency view and, for a delta,
// its base rows, both through the pass's row cache.
func readManifestView(
	ctx context.Context,
	tx db.Queryer,
	store FactStore,
	cache manifestRowCache,
	scopeID string,
	generationID string,
) (supplychaincore.ManifestDependencyView, error) {
	view := supplychaincore.ManifestDependencyView{GenerationID: generationID}
	var err error
	if view.Rows, err = cache.rowsFor(ctx, tx, scopeID, generationID); err != nil {
		return view, err
	}
	if !facts.ParseDeltaScope(view.Rows).IsDelta {
		return view, nil
	}
	baseID, _, wantUnion, err := store.resolveDeltaUnionBase(ctx, scopeID, generationID)
	var missing *projector.DeltaBaseMissingError
	switch {
	case errors.As(err, &missing), err == nil && (!wantUnion || strings.TrimSpace(baseID) == ""):
		view.BaseMissing = true
		return view, nil
	case err != nil:
		return view, classifyFanOutReadError(err)
	}
	view.Base, err = cache.rowsFor(ctx, tx, scopeID, baseID)
	return view, err
}

// readManifestGenerationRows reads one generation's repository fact, live
// dependency rows (anchored on the repository fact's repo_id), and tombstoned
// dependency rows. A generation with no repository fact has no repo_id anchor
// and no rows the marker can attribute, so it returns the tombstones alone.
func readManifestGenerationRows(ctx context.Context, tx db.Queryer, scopeID, generationID string) ([]facts.Envelope, error) {
	rows, err := queryManifestEnvelopes(ctx, tx, manifestGenerationRepositoryQuery, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	repoIDs, err := queryManifestRepoIDs(ctx, tx, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	if len(repoIDs) > 0 {
		live, err := queryManifestEnvelopes(ctx, tx, manifestGenerationDependencyQuery, scopeID, generationID, repoIDs)
		if err != nil {
			return nil, err
		}
		rows = append(rows, live...)
	}
	tombstones, err := queryManifestEnvelopes(ctx, tx, manifestGenerationTombstoneQuery, scopeID, generationID)
	if err != nil {
		return nil, err
	}
	return append(rows, tombstones...), nil
}

func queryManifestEnvelopes(ctx context.Context, tx db.Queryer, query string, args ...any) ([]facts.Envelope, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, classifyFanOutReadError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []facts.Envelope
	for rows.Next() {
		envelope, err := scanSupplyChainImpactFact(rows)
		if err != nil {
			return nil, fmt.Errorf("scan manifest dependency generation fact: %w", err)
		}
		out = append(out, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyFanOutReadError(err)
	}
	return out, nil
}

func queryManifestRepoIDs(ctx context.Context, tx db.Queryer, scopeID, generationID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, manifestGenerationRepoIDsQuery, scopeID, generationID)
	if err != nil {
		return nil, classifyFanOutReadError(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan manifest generation repo id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyFanOutReadError(err)
	}
	return ids, nil
}

func queryManifestGenerationID(ctx context.Context, tx db.Queryer, query string, args ...any) (string, error) {
	ids, err := queryManifestGenerationIDs(ctx, tx, query, args...)
	if err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0], nil
}

func queryManifestGenerationIDs(ctx context.Context, tx db.Queryer, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, classifyFanOutReadError(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan manifest generation id: %w", err)
		}
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, classifyFanOutReadError(err)
	}
	return ids, nil
}
