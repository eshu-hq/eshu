#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# End-of-arm exports for one run: the final committed state (generations, real
# fixture fact sets, graph counts, reducer intents, content rows), source B's
# queue wait and claim rate, and the not-takeable census. Each export is one
# small file in the run directory; assemble_result.py turns them into
# result.json. Needs env.sh and host.sh, and these arm variables: REPLAY_SRC_A,
# REPLAY_SRC_B, REPLAY_RUN_DIR.

# replay_export_state writes the final-state exports.
replay_export_state() {
  local out="$REPLAY_RUN_DIR/export"
  mkdir -p "$out"
  # Generation ids and fact ids embed the observation time, so they differ
  # between runs of identical content. The comparable identity of a committed
  # generation is the source commit (seeded generations have none, so their
  # deterministic id stands in); the comparable identity of a fact is its stable
  # fact key plus its payload hash.
  replay_psql -c "SELECT COALESCE(json_object_agg(s.scope_id, COALESCE(NULLIF(g.source_commit_sha, ''), g.generation_id) ORDER BY s.scope_id), '{}'::json)
    FROM ingestion_scopes s JOIN scope_generations g ON g.scope_id = s.scope_id AND g.generation_id = s.active_generation_id
    WHERE s.scope_id LIKE 'replay-%' OR s.scope_id LIKE 'git-repository-scope:%'" >"$out/generations.json"
  # Real fixture fact sets of each repository's active generation. Seeded
  # generations are excluded on purpose: their fact equality holds by
  # construction and proves nothing (arbiter item 5).
  replay_psql -c "SELECT COALESCE(json_object_agg(scope_id, h ORDER BY scope_id), '{}'::json) FROM (
      SELECT f.scope_id, md5(string_agg(f.stable_fact_key || ':' || md5(f.payload::text), ',' ORDER BY f.stable_fact_key)) AS h
      FROM fact_records f JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
      WHERE f.scope_id LIKE 'git-repository-scope:%' GROUP BY f.scope_id) t" >"$out/real_fact_sets.json"
  # Reducer intents enqueued for A and B by projection: the distinct (scope,
  # domain) pairs of the active generations. A generation superseded before it
  # was projected (a paced one overtaken by a newer commit) enqueues nothing, so
  # intents of non-active generations are not compared.
  replay_psql -c "SELECT json_build_object(
      'A', json_build_object('count', count(*) FILTER (WHERE src = '$REPLAY_SRC_A'),
           'keys_hash', COALESCE(md5(string_agg(k, ',' ORDER BY k) FILTER (WHERE src = '$REPLAY_SRC_A')), '')),
      'B', json_build_object('count', count(*) FILTER (WHERE src = '$REPLAY_SRC_B'),
           'keys_hash', COALESCE(md5(string_agg(k, ',' ORDER BY k) FILTER (WHERE src = '$REPLAY_SRC_B')), '')))
    FROM (SELECT DISTINCT w.scope_id || ':' || w.domain AS k,
                 COALESCE(NULLIF(BTRIM(w.payload->>'source_system'), ''), s.source_system) AS src
          FROM fact_work_items w JOIN ingestion_scopes s USING (scope_id)
          WHERE w.stage = 'reducer' AND w.work_item_id NOT LIKE 'replay-%'
            AND w.generation_id = s.active_generation_id) t" >"$out/reducer_intents.json"
  replay_psql -c "SELECT json_build_object(
      'content_rows', (SELECT count(*) FROM content_files) + (SELECT count(*) FROM content_entities),
      'projector_unsettled', (SELECT count(*) FROM fact_work_items WHERE stage = 'projector' AND status NOT IN ('succeeded', 'superseded')),
      'projector_superseded', (SELECT count(*) FROM fact_work_items WHERE stage = 'projector' AND status = 'superseded'),
      'dead_letters', (SELECT count(*) FROM fact_work_items WHERE status = 'dead_letter'),
      'admission_timeouts', (SELECT count(*) FROM fact_work_items WHERE failure_class = 'admission_timeout' OR admission_wait_attempts > 0),
      'attempt_count_changes', (SELECT count(*) FROM fact_work_items WHERE stage = 'projector' AND status = 'succeeded'
                                  AND attempt_count - source_paced_waits <> 1),
      'paced_waits_total', (SELECT COALESCE(sum(source_paced_waits), 0) FROM fact_work_items WHERE stage = 'projector'))" \
    >"$out/counters.json"
  # Source B: queue wait is claim time minus enqueue time of its projector
  # items, read from the queue's own timestamps.
  replay_psql -c "SELECT json_build_object(
      'items', count(*),
      'queue_wait_p50_s', COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY wait), 0),
      'queue_wait_p95_s', COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY wait), 0),
      'claims_per_s', COALESCE(count(*) / NULLIF(extract(epoch FROM max(claimed) - min(claimed)), 0), 0))
    FROM (SELECT extract(epoch FROM w.last_attempt_at - w.created_at) AS wait, w.last_attempt_at AS claimed
          FROM fact_work_items w JOIN ingestion_scopes s USING (scope_id)
          WHERE w.stage = 'projector' AND w.status = 'succeeded' AND s.source_system = '$REPLAY_SRC_B') t" >"$out/b_stats.json"
  replay_cypher "MATCH (n) UNWIND labels(n) AS l RETURN l, count(*) ORDER BY l" 2>/dev/null |
    tail -n +2 | tr -d '"' >"$out/graph_counts.tsv" || true
}

# replay_census <tag> runs the not-takeable census for source A and saves it.
replay_census() {
  local tag="$1" here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  PGPASSWORD="$REPLAY_PG_PASSWORD" "$here/census.sh" "$REPLAY_DSN" "$REPLAY_SRC_A" "${REPLAY_ENGAGE_AGE_SECONDS:-3600}" \
    >"$REPLAY_RUN_DIR/census-$tag.txt" 2>&1 || true
}
