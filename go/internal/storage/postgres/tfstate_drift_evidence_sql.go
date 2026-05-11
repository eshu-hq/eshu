package postgres

// SQL constants consumed by tfstate_drift_evidence.go's PostgresDriftEvidenceLoader.
// Hoisted into a sibling file to keep the loader implementation under the
// CLAUDE.md 500-line cap; behavior and call sites are unchanged.
//
// Edit each query helper in tfstate_drift_evidence.go in lockstep with the
// constant it consumes — the rationale in each constant's comment block is
// the source of truth for what the helper may assume about the result shape.

// listConfigResourcesForCommitQuery returns the terraform_resources arrays
// emitted by the parser for one (scope_id, generation_id) — i.e. one sealed
// commit anchor — restricted to git-source file facts that actually contain
// resource blocks.
//
// The HCL parser's base payload (parser.go:103) always emits an empty
// terraform_resources array for every parsed file, so jsonb_typeof alone
// does NOT prune the scan. The array-length filter is the load-bearing
// predicate: it restricts the row set to .tf files that actually contain
// at least one `resource "<type>" "<name>" {}` block. Without it the loader
// scans every parsed file in the repo snapshot per drift intent.
//
// The CASE expression provides both the type guard and the length filter
// in one predicate. Postgres does NOT guarantee short-circuit evaluation
// of AND predicates — the planner can evaluate jsonb_array_length before
// any standalone jsonb_typeof guard, raising "cannot get array length of
// a scalar" (SQLSTATE 22023) on rows whose terraform_resources value is
// jsonb null or any other scalar. CASE branches are guaranteed not to
// evaluate unless their condition matches (PostgreSQL docs), so the CASE
// alone safely emits 0 for non-array values and the real length for
// arrays. Adding a separate `jsonb_typeof = 'array'` predicate next to
// this CASE would be redundant and would re-evaluate the type check per
// row. Regression test:
// TestPostgresDriftEvidenceLoaderSurvivesNullTerraformResourcesPath.
const listConfigResourcesForCommitQuery = `
SELECT
    fact.payload->'parsed_file_data'->'terraform_resources' AS terraform_resources
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind = 'file'
  AND fact.source_system = 'git'
  AND CASE
        WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_resources') = 'array'
        THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_resources')
        ELSE 0
      END > 0
ORDER BY fact.fact_id ASC
`

// activeStateSnapshotMetadataQuery returns the lineage and serial of the
// active terraform_state_snapshot fact for one state-snapshot scope, plus the
// generation_id (used to fetch the matching state-resource rows). The scope
// must have at most one active snapshot at any time; the LIMIT 1 protects
// against stray duplicates without hiding a real bug.
//
// The ORDER BY is deterministic: newest observed_at first, lexicographic
// fact_id tie-break. Without it, two snapshot facts sharing the active
// generation (e.g. a transient duplicate during recovery or an upstream
// re-emit) could yield different lineage/serial pairs on successive calls,
// silently shifting which prior generation the loader looks up next. Stable
// ordering is the only correctness gate the loader has against duplicate
// rows it cannot reject upstream.
const activeStateSnapshotMetadataQuery = `
SELECT
    fact.payload->>'lineage'                AS lineage,
    (fact.payload->>'serial')::bigint        AS serial,
    fact.generation_id                       AS generation_id
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE fact.scope_id = $1
  AND fact.fact_kind = 'terraform_state_snapshot'
ORDER BY fact.observed_at DESC, fact.fact_id DESC
LIMIT 1
`

// listStateResourcesForGenerationQuery returns the terraform_state_resource
// rows for one (scope_id, generation_id) pair. Used twice per call when a
// prior generation exists: once for the active generation and once for
// serial-1. Returns (address, payload_json) so the loader can decode
// attributes without joining additional fact records.
const listStateResourcesForGenerationQuery = `
SELECT
    fact.payload->>'address' AS address,
    fact.payload            AS payload
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind = 'terraform_state_resource'
ORDER BY fact.payload->>'address' ASC, fact.fact_id ASC
`

// priorStateSnapshotMetadataQuery returns the prior generation for one
// state-snapshot scope by matching serial = currentSerial - 1. The lineage
// is returned in addition to the generation_id so the loader can flag
// lineage rotations (different lineage than the current snapshot) and
// suppress removed_from_state per classify.go:73.
const priorStateSnapshotMetadataQuery = `
SELECT
    fact.payload->>'lineage'                AS lineage,
    (fact.payload->>'serial')::bigint        AS serial,
    fact.generation_id                       AS generation_id
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.fact_kind = 'terraform_state_snapshot'
  AND (fact.payload->>'serial')::bigint = $2
ORDER BY fact.observed_at DESC, fact.fact_id DESC
LIMIT 1
`
