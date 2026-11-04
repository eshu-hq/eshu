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

// priorActivatedGenerationQuery resolves the newest generation before the
// intent's own row, in the activation order the projector supersedes by
// (ingested_at, generation_id), that ever activated. A failed or never-active
// predecessor never held active rows, so it is not a diff source. Both probes
// are index range scans on scope_generations_scope_latest_lookup_idx and
// scope_generations_scope_generation_idx.
const priorActivatedGenerationQuery = `
SELECT prior.generation_id
FROM scope_generations AS intent
JOIN scope_generations AS prior
  ON prior.scope_id = intent.scope_id
WHERE intent.scope_id = $1
  AND intent.generation_id = $2
  AND prior.activated_at IS NOT NULL
  AND prior.status IN ('active', 'completed', 'superseded')
  AND (prior.ingested_at, prior.generation_id) < (intent.ingested_at, intent.generation_id)
ORDER BY prior.ingested_at DESC, prior.generation_id DESC
LIMIT 1
`

// ReadManifestDependencyGeneration implements
// supplychaincore.ManifestDependencyGenerationReader (#9874). In one read-only
// transaction under the holder-read timeout it decides the activation state
// with the shared generation freshness check (equal: active; pending before
// activation: pending; anything older, failed, or pruned: superseded), then
// reads the generation's own dependency rows and those of the newest
// activated generation before it. A prior delta also returns its #7797 base
// rows, resolved by the same rules the delta union uses; a missing, pruned,
// or legacy base is reported as BaseMissing, never guessed. A prior whose
// facts retention pruned counts as no prior.
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
	if out.Current.Rows, err = readManifestGenerationRows(ctx, tx, scopeID, generationID); err != nil {
		return out, err
	}

	priorID, err := readPriorActivatedGeneration(ctx, tx, scopeID, generationID)
	if err != nil || priorID == "" {
		return out, err
	}
	store := FactStore{database: tx}
	if pruned, err := store.deltaUnionBasePruned(ctx, scopeID, priorID); err != nil {
		return out, classifyFanOutReadError(err)
	} else if pruned {
		return out, nil
	}
	out.HasPrior = true
	if out.Prior.Rows, err = readManifestGenerationRows(ctx, tx, scopeID, priorID); err != nil {
		return out, err
	}
	if !facts.ParseDeltaScope(out.Prior.Rows).IsDelta {
		return out, nil
	}
	baseID, _, wantUnion, err := store.resolveDeltaUnionBase(ctx, scopeID, priorID)
	var missing *projector.DeltaBaseMissingError
	switch {
	case errors.As(err, &missing), err == nil && (!wantUnion || strings.TrimSpace(baseID) == ""):
		out.Prior.BaseMissing = true
		return out, nil
	case err != nil:
		return out, classifyFanOutReadError(err)
	}
	out.Prior.Base, err = readManifestGenerationRows(ctx, tx, scopeID, baseID)
	return out, err
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

func readPriorActivatedGeneration(ctx context.Context, tx db.Queryer, scopeID, generationID string) (string, error) {
	rows, err := tx.QueryContext(ctx, priorActivatedGenerationQuery, scopeID, generationID)
	if err != nil {
		return "", classifyFanOutReadError(err)
	}
	defer func() { _ = rows.Close() }()
	var prior string
	if rows.Next() {
		if err := rows.Scan(&prior); err != nil {
			return "", fmt.Errorf("scan prior activated generation: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return "", classifyFanOutReadError(err)
	}
	return strings.TrimSpace(prior), nil
}
