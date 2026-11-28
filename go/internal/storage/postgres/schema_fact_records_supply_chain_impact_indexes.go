// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const supplyChainImpactFactRecordReadIndexesSQL = `
-- #10408: the advisory arm of the holder fan-out lookup. A re-key retires
-- group and member ids no replacement row names; a holder keyed by such an
-- id (an advisory-keyed GHSA-only row, a holder whose deciding record names
-- the fragment) matches payload->>'advisory_id' when payload->>'cve_id'
-- misses. Same shape as the CVE arm so the planner can serve both arms
-- from matching partial indexes.
CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_advisory_lookup_idx
    ON fact_records (
        (payload->>'advisory_id'),
        (payload->>'impact_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

-- #10408: the member-set arm of the holder fan-out lookup. A holder whose
-- group id the committing pass never saw (a private advisory id under a
-- divergent alias closure) still carries the shared member in its stored
-- advisory_group_members, so the lookup matches the stored set against the
-- fanned-out ids with ?|. GIN like the sibling
-- fact_records_security_alert_cve_ids_idx array arm.
CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_group_members_idx
    ON fact_records USING GIN ((payload #> '{provenance,advisory_group_members}'))
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

-- #9975: the keyed winners resweep probe. The package triple plus the
-- coalesced c0 (cve_id, else advisory_id) and advisory_id expressions the
-- probe matches per dirty identifier, so all five equalities are index
-- conditions and the heap fetches only kept facts instead of every candidate
-- of a touched triple. fact_id and generation_id ride along like the sibling
-- impact indexes. Migration 291 builds it concurrently on existing
-- deployments; the expressions must stay byte-identical to the probe in
-- supply_chain_impact_winners_keyed_sql.go or the planner cannot use it.
CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_winners_keyed_idx
    ON fact_records (
        (payload->>'package_id'),
        (payload->>'repository_id'),
        (payload->>'subject_digest'),
        (COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '')),
        (COALESCE(payload->>'advisory_id', '')),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;

-- #11376: the ecosystem arm of the package-keyed holder read
-- (FindPackageHolderCandidatesQuery). The folded ecosystem expression leads
-- so the requested spellings ride as an Index Cond instead of a heap
-- filter; scope_id and generation_id ride along for the active-generation
-- join. Migration 347 builds it concurrently on existing deployments; the
-- expressions must stay byte-identical to the query predicate or the
-- planner cannot use it.
CREATE INDEX IF NOT EXISTS fact_records_vuln_pkg_eco_scope_gen_active_idx
    ON fact_records (
        ((LOWER(BTRIM(payload->>'ecosystem') COLLATE "C")) COLLATE "default"),
        scope_id,
        generation_id
    )
    WHERE fact_kind = 'vulnerability.affected_package'
      AND is_tombstone = FALSE;
` + supplyChainImpactForeignMemberTokensFunctionSQL + supplyChainImpactForeignMemberTokensIndexSQL

// SupplyChainImpactForeignMemberTokensFunction is the versioned name of the
// foreign-member token function (#10643 PR 2).
// #nosec G101 -- the constant names a Postgres function or index, not a credential; the word token is the GIN lookup token.
const SupplyChainImpactForeignMemberTokensFunction = "eshu_sci_foreign_member_tokens_v4"

// SupplyChainImpactForeignMemberTokensIndex is the GIN index on
// SupplyChainImpactForeignMemberTokensFunction (#10643 PR 2).
const SupplyChainImpactForeignMemberTokensIndex = "fact_records_sci_foreign_member_tokens_v4_idx"

// supplyChainImpactForeignMemberTokensFunctionSQL is the foreign-member token
// function (#10643 PR 2), function v4 of
// #10643 (retired evidence record). For a finding
// row it returns one token, bucket || ':' || member, for every string the
// jsonb ?| operator matches on
// provenance.advisory_group_members (the string elements of a top-level
// array, the keys of a top-level object, a top-level string scalar) that is
// not the row's own cve_id or advisory_id, and NULL when none remains.
// bucket is strpos('0123456789abcdef', right(fact_id, 1)): 1 to 16 for a
// lower-case hex last character, 0 for any other. The members value is read
// once; missing, JSON null, number and boolean return NULL before any
// subquery (the CASE has no ELSE), which keeps the per-row write cost low.
//
// The function is total: it tokenizes a string only when octet_length is at
// most HolderMemberTokenMaxBytes (1,024 bytes; arbiter ruling 10643m). A GIN
// array_ops entry cannot exceed 2,712 bytes, so an untruncated token of a
// longer member would fail the finding insert ("index row size ... exceeds
// maximum 2712"). It mirrors jsonb ?| for strings up to 1,024 bytes; longer
// strings are not tokenized, and a read whose lookup ids include one uses the
// shipped statement (HolderLookupExceedsTokenBytes). No hash and no
// truncation: either would need a recheck against the payload.
//
// The body is frozen once released: an index is built on it, so altering it
// would leave stored entries that the new body no longer produces. A change
// is a new versioned function, a new index and a new migration, proven again
// with the F1 shape matrix and a whole-fixture identity scan. CREATE OR REPLACE with
// the same body keeps the migration re-runnable; the body digest is pinned
// by TestForeignMemberTokensFunctionIsTheFrozenV4 and, as stored in
// pg_proc, by TestHolderKeyRangeTokenIndexValidFromMigrationsLive.
// Migration 336 applies it on existing deployments.
// #nosec G101 -- the SQL text names a Postgres function or index, not a credential; the word token is the GIN lookup token.
const supplyChainImpactForeignMemberTokensFunctionSQL = `
CREATE OR REPLACE FUNCTION eshu_sci_foreign_member_tokens_v4(p_fact_id text, p_payload jsonb) RETURNS text[]
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT CASE jsonb_typeof(m.v)
    WHEN 'array' THEN (
      SELECT array_agg(DISTINCT o.b || ':' || (e #>> '{}'))
      FROM (SELECT p_payload->>'cve_id' AS c, p_payload->>'advisory_id' AS a,
                   strpos('0123456789abcdef', right(p_fact_id, 1))::text AS b) AS o,
           jsonb_array_elements(m.v) AS e
      WHERE jsonb_typeof(e) = 'string'
        AND octet_length(e #>> '{}') <= 1024
        AND (e #>> '{}') IS DISTINCT FROM o.c
        AND (e #>> '{}') IS DISTINCT FROM o.a)
    WHEN 'object' THEN (
      SELECT array_agg(DISTINCT o.b || ':' || k)
      FROM (SELECT p_payload->>'cve_id' AS c, p_payload->>'advisory_id' AS a,
                   strpos('0123456789abcdef', right(p_fact_id, 1))::text AS b) AS o,
           jsonb_object_keys(m.v) AS k
      WHERE octet_length(k) <= 1024
        AND k IS DISTINCT FROM o.c
        AND k IS DISTINCT FROM o.a)
    WHEN 'string' THEN (
      SELECT CASE WHEN octet_length(z.s) <= 1024
                   AND z.s IS DISTINCT FROM (p_payload->>'cve_id') AND z.s IS DISTINCT FROM (p_payload->>'advisory_id')
                  THEN ARRAY[strpos('0123456789abcdef', right(p_fact_id, 1))::text || ':' || z.s] END
      FROM (SELECT m.v #>> '{}' AS s) AS z)
  END
  FROM (SELECT p_payload #> '{provenance,advisory_group_members}' AS v) AS m
$$;
`

// supplyChainImpactForeignMemberTokensIndexSQL is the GIN array_ops index on
// the token function, partial on the finding kind and live rows, the variant
// the #10643 PR 2 T3 and T4r proofs measured. The member arm of each
// key-range holder statement reads it with && on that statement's lookup
// tokens (HolderMemberTokens). Migration 337 builds it concurrently on
// existing deployments.
// #nosec G101 -- the SQL text names a Postgres function or index, not a credential; the word token is the GIN lookup token.
const supplyChainImpactForeignMemberTokensIndexSQL = `
CREATE INDEX IF NOT EXISTS fact_records_sci_foreign_member_tokens_v4_idx
    ON fact_records USING GIN (eshu_sci_foreign_member_tokens_v4(fact_id, payload))
    WHERE fact_kind = 'reducer_supply_chain_impact_finding'
      AND is_tombstone = FALSE;
`
