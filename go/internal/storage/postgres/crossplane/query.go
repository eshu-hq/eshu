// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore

// crossplaneRedriveDerivedGroupExpr derives a K8sResource content_entity
// row's Kubernetes API group from its entity_metadata.api_version field,
// mirroring reducer.crossplaneAPIVersionGroup exactly: a core-group
// apiVersion (no "/", e.g. "v1") or an empty/malformed value yields an empty
// group. split_part returns the WHOLE string when there is no separator, so
// the CASE only takes the split_part branch when a "/" is actually present
// -- matching Go's `idx <= 0 -> ""` fallback (including a leading "/", where
// idx == 0 and position(...) == 1 > 0, so split_part("/...", '/', 1) is
// itself "").
const crossplaneRedriveDerivedGroupExpr = `(CASE
    WHEN position('/' IN COALESCE(fact.payload->'entity_metadata'->>'api_version', '')) > 0
    THEN split_part(fact.payload->'entity_metadata'->>'api_version', '/', 1)
    ELSE ''
END)`

// ListCrossplaneRedriveTargetScopesQuery discovers OTHER scopes' active
// K8sResource Claim candidates matching one XRD's (group, claim_kind) join
// key -- the cross-scope target-discovery query issue #5476 needs to close
// the XRD-lag false-negative window. It is the sweep's active leg, and it is
// exported because the parent postgres package derives the delta-base leg
// (#10094) from this exact text with deriveDeltaOverlayBaseQuery, binding
// the candidate base generation ids at
// CrossplaneRedriveDeltaBaseGenerationParam. The
// derivation swaps the active-scope fragment
// (AND scope.active_generation_id = fact.generation_id) and the active
// generation fragment (AND generation.status = 'active'), so both must stay
// written exactly once, as they are here. Placeholders: $1 group, $2 claim
// kind, $3 the XRD's own scope, $4 the keyset cursor, $5 the page size, the
// order CrossplaneRedriveTargetPage.QueryArgs binds.
//
// Shape (#10094): the fact_records branch is a DISTINCT (scope_id,
// generation_id) subquery aliased fact, so a scope's many matching claim
// rows collapse to one row per generation before any join. The scope and
// generation joins then run once per (scope, generation) pair instead of
// once per claim row. The output tuple (scope_id,
// scope.active_generation_id) is functionally dependent on scope_id
// (ingestion_scopes.scope_id is the primary key), so DISTINCT ON
// (fact.scope_id) ORDER BY fact.scope_id returns the same tuple per scope
// as the per-claim shape. Both columns are NOT NULL, duplicates collapse
// the same way, and keyset boundaries are identical.
//
// Fences, matching the design's three required fences:
//
//  1. Active-generation fence: scope.active_generation_id =
//     fact.generation_id restricts every candidate to the target scope's
//     CURRENT active generation (fact rows from a superseded generation are
//     invisible), and generation.status = 'active' requires that generation
//     row to be active.
//  2. Active-claim fence: same join -- a claim from a non-active generation
//     is never an active-leg target. The delta-base leg derived from this
//     text reaches the base full generation of a delta-active scope instead.
//  3. Already-satisfied-for-this-identity fence: the NOT EXISTS subquery
//     skips a target scope already recorded in
//     crossplane_satisfied_by_redrive_target_ledger for this EXACT (group,
//     claim_kind) identity -- i.e. it already had a re-drive chance against
//     this identity while some XRD advertising it was active, so re-driving
//     it again for the SAME identity is a deterministic no-op. This is keyed
//     by identity, not by a timestamp: an XRD fact's own observed_at strictly
//     advances on every resync of the platform repo even when the XRD's
//     (group, claim_kind) is unchanged, so a timestamp-anchored fence would
//     never actually skip anything across repeated platform-repo syncs (the
//     bug the ledger replaces). See migration 076's doc comment on the ledger
//     table for the full reasoning, including why a LATER, genuinely new
//     Claim generation in the target scope needs no ledger check at all (its
//     own projector-triggered intent already resolves against the
//     already-active XRD).
//
// fact.scope_id <> $3 excludes the XRD's own scope: the same-scope case is
// already ungated and resolved by the time this handler's own intent runs
// (issue #5347); this query only exists to close the CROSS-scope gap.
//
// Keyset-paginated on fact.scope_id (never generation_id, which is
// per-generation and would not converge across an unbounded number of
// distinct generations for one scope): callers page with
// `after_scope_id=""` for the first page and the last row's scope_id
// thereafter. The plan uses fact_records_active_k8s_claim_redrive_idx
// (migration 076); TestCrossplaneRedriveTargetDiscoveryQueryPlanLive pins it.
const ListCrossplaneRedriveTargetScopesQuery = `
SELECT DISTINCT ON (fact.scope_id)
    fact.scope_id,
    scope.active_generation_id
FROM (
    SELECT DISTINCT fact.scope_id, fact.generation_id
    FROM fact_records AS fact
    WHERE fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND fact.is_tombstone = FALSE
      AND (fact.payload->>'entity_type' = 'K8sResource' OR fact.payload->>'entity_kind' = 'K8sResource')
      AND ` + crossplaneRedriveDerivedGroupExpr + ` = $1
      AND (fact.payload->'entity_metadata'->>'kind') = $2
      AND fact.scope_id <> $3
      AND fact.scope_id > $4
) AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE NOT EXISTS (
      SELECT 1
      FROM crossplane_satisfied_by_redrive_target_ledger AS ledger
      WHERE ledger.target_scope_id = fact.scope_id
        AND ledger.xrd_group = $1
        AND ledger.xrd_claim_kind = $2
  )
  AND generation.status = 'active'
ORDER BY fact.scope_id ASC
LIMIT $5
`

// CrossplaneRedriveDeltaBaseGenerationParam is the placeholder the
// delta-base leg binds its candidate base generation ids at: the first one
// ListCrossplaneRedriveTargetScopesQuery leaves free.
const CrossplaneRedriveDeltaBaseGenerationParam = 6
