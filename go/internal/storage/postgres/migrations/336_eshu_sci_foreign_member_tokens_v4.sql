-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10643 PR 2: the foreign-member token function behind the member arm of
-- the key-range holder read. For a finding row it returns one token,
-- bucket || ':' || member, for every string jsonb ?| matches on
-- provenance.advisory_group_members (string elements of a top-level array,
-- keys of a top-level object, a top-level string scalar) that is not the
-- row's own cve_id or advisory_id; NULL when none remains. bucket is
-- strpos('0123456789abcdef', right(fact_id, 1)), 0 to 16. Migration 337
-- indexes it.
--
-- Only strings of at most 1,024 bytes (octet_length) become tokens, so no
-- stored value can make a GIN entry exceed its 2,712-byte limit and fail a
-- finding insert (#10643 PR 2 review F1, arbiter ruling 10643m). It mirrors
-- jsonb ?| for strings up to 1,024 bytes; a read whose lookup ids include a
-- longer one uses the shipped holder statement. The Go DDL is supplyChainImpactForeignMemberTokensFunctionSQL
-- in schema_fact_records_supply_chain_impact_indexes.go, byte for byte.
--
-- IMMUTABLE, STRICT, PARALLEL SAFE, built-ins only. The body is frozen once
-- released: an index stores its output, so a change is a new versioned
-- function, a new index and a new migration. CREATE OR REPLACE with the same
-- body keeps this file re-runnable; it takes no table lock.
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
