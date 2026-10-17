// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"fmt"
	"strings"
)

// The PR-3c ledger read statements (#7127). Every one is a plain SELECT: no
// row lock, no advisory lock, no write. The reader takes only AccessShareLock
// on the ledger relations and never waits on the link writer's per-scope
// fence. Every link predicate names the full three-column key (scope_id,
// generation_id, prior_generation_id); no statement aggregates or joins by
// generation_id alone, so an orphan row naming another prior can never leak
// into a read (PR-3d arbiter review addition for PR-3c).

// ReadCursorQuery reads the scope's writer fence and the generation its
// state describes. An empty state generation means nothing was ever linked.
//
// $1 scope_id.
const ReadCursorQuery = `
SELECT COALESCE(state_generation_id, ''), state_activation_seq, digest_version
FROM changed_since_scope_cursor
WHERE scope_id = $1
`

// ReadChainQuery walks the link chain from the requested prior ($2) toward
// the current generation ($3) following prior_generation_id pointers. It
// returns every link on the walk in chain order with its digest version, its
// stored delta-row count, and the linked generation's live key counts. The
// caller validates contiguity (first prior is $2, each prior names the
// previous generation, last generation is $3), one row per step, every
// digest, and the delta-row budget; anything else is a fallback. The walk
// stops expanding past the current generation and is capped at ChainMaxLinks
// steps.
//
// $1 scope_id, $2 prior generation, $3 current generation.
const ReadChainQuery = `
WITH RECURSIVE chain AS (
    SELECT link.generation_id, link.prior_generation_id, link.link_kind, link.digest_version,
           link.delta_rows, link.files_keys, link.content_entities_keys, link.facts_keys, 1 AS depth
    FROM changed_since_links AS link
    WHERE link.scope_id = $1 AND link.prior_generation_id = $2
    UNION ALL
    SELECT link.generation_id, link.prior_generation_id, link.link_kind, link.digest_version,
           link.delta_rows, link.files_keys, link.content_entities_keys, link.facts_keys, chain.depth + 1
    FROM changed_since_links AS link
    JOIN chain ON link.scope_id = $1 AND link.prior_generation_id = chain.generation_id
    WHERE chain.depth < 64 AND chain.generation_id <> $3
)
SELECT generation_id, prior_generation_id, link_kind, digest_version, delta_rows,
       files_keys, content_entities_keys, facts_keys
FROM chain
ORDER BY depth
`

// ReadCurrentLinkExistsQuery reports whether the current generation has any
// link row at all. It runs only when the chain walk from the prior comes
// back empty, to tell an unlinked current generation (the writer has not
// reached it) from a broken chain.
//
// $1 scope_id, $2 current generation.
const ReadCurrentLinkExistsQuery = `
SELECT 1
FROM changed_since_links
WHERE scope_id = $1 AND generation_id = $2
LIMIT 1
`

// ReadLinkDeltasQuery reads every stored delta row of one link by its full
// three-column key, in a deterministic order. The caller needs the whole
// changed set: counts are exact, only the samples served upstream are
// bounded, and the fetched row count must equal the link's recorded
// delta_rows (anything else is a retention-pruned or half-committed chain
// and falls back).
//
// $1 scope_id, $2 generation_id, $3 prior_generation_id.
const ReadLinkDeltasQuery = `
SELECT fact_category, classification, stable_fact_key,
       prior_fact_kind, current_fact_kind, prior_state, current_state, current_tombstoned
FROM changed_since_link_deltas
WHERE scope_id = $1 AND generation_id = $2 AND prior_generation_id = $3
ORDER BY fact_category, classification, stable_fact_key
`

// readUnchangedSamplesSelect is the fixed head of the unchanged-samples
// read: the live keys of one category the composed changed set does not
// contain, ordered exactly as the legacy lateral samples order them. Each
// exclusion clause pins one link by its full three-column key; a key with a
// changed row (added, updated, retired, superseded) in any composed link is
// not unchanged. Kind-changed but digest-equal keys ('unchanged' delta rows)
// and tombstones of never-live keys ('dropped' delta rows) stay out of the
// exclusion, matching the legacy buckets, which count them as unchanged or
// drop them.
const readUnchangedSamplesSelect = `
SELECT state.stable_fact_key, state.fact_kind
FROM changed_since_key_state AS state
WHERE state.scope_id = $1 AND state.fact_category = $2
`

// readUnchangedExclusionClause excludes the changed keys of one link from
// the unchanged samples. $1/$2 are the outer scope and category; the
// generation and prior binds follow.
const readUnchangedExclusionClause = `AND NOT EXISTS (
    SELECT 1
    FROM changed_since_link_deltas AS delta
    WHERE delta.scope_id = $1 AND delta.generation_id = $%d AND delta.prior_generation_id = $%d
      AND delta.fact_category = $2 AND delta.stable_fact_key = state.stable_fact_key
      AND delta.classification IN ('added', 'updated', 'retired', 'superseded')
)`

// ReadUnchangedSamplesQuery is the single-link unchanged-samples read: the
// live keys of one category outside the link's changed set.
//
// $1 scope_id, $2 fact_category, $3 generation_id, $4 prior_generation_id,
// $5 sample fetch limit.
var ReadUnchangedSamplesQuery = mustBuildChainUnchangedSamplesQuery(1)

// BuildChainUnchangedSamplesQuery returns the unchanged-samples read for a
// chain of links links long: the state keys outside every composed link's
// changed set. Binds are $1 scope_id, $2 fact_category, then one
// (generation_id, prior_generation_id) pair per link in chain order, then
// the sample fetch limit. Every link clause names the full three-column key;
// the statement never filters by generation_id alone.
func BuildChainUnchangedSamplesQuery(links int) (string, error) {
	if links < 1 || links > ChainMaxLinks {
		return "", fmt.Errorf("changed-since ledger read: chain links = %d, want 1..%d", links, ChainMaxLinks)
	}
	var query strings.Builder
	query.WriteString(readUnchangedSamplesSelect)
	bind := 3
	for link := 0; link < links; link++ {
		fmt.Fprintf(&query, readUnchangedExclusionClause, bind, bind+1)
		query.WriteString("\n")
		bind += 2
	}
	fmt.Fprintf(&query, "ORDER BY state.stable_fact_key ASC\nLIMIT $%d\n", bind)
	return query.String(), nil
}

func mustBuildChainUnchangedSamplesQuery(links int) string {
	query, err := BuildChainUnchangedSamplesQuery(links)
	if err != nil {
		panic(err)
	}
	return query
}

// ReadTombstoneProbeQuery settles retired against superseded for keys the
// composed chain removed: the tombstone rows the current (final) generation
// still carries for exactly those keys. The caller matches a probe row's
// fact_kind category against the key's prior category, mirroring the legacy
// tombstone join; a removed key with no same-category tombstone in the final
// generation is superseded, not retired. Reducer-derived kinds stay out,
// mirroring the legacy tombstone scan.
//
// $1 scope_id, $2 final generation_id, $3 stable_fact_key set.
const ReadTombstoneProbeQuery = `
SELECT stable_fact_key, fact_kind
FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND is_tombstone = TRUE
  AND ` + ExcludeReducerDerivedKinds + `
  AND stable_fact_key = ANY($3)
GROUP BY stable_fact_key, fact_kind
`
