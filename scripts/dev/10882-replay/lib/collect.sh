#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# End-of-arm exports for one run: the final committed state (generations, real
# fixture fact sets, graph counts, reducer rows of every generation, projector
# items, content rows, the latch rows), source B's
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
  # The full real-fixture fact dump of the active generations (fact id, stable
  # key, payload text), kept as evidence so the field-level normalisation in
  # assemble_result.py can be audited against the raw rows.
  replay_psql -F $'\t' -c "SELECT f.scope_id, f.fact_id, f.stable_fact_key, f.payload::text
      FROM fact_records f JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
      WHERE f.scope_id LIKE 'git-repository-scope:%' ORDER BY f.scope_id, f.fact_id" >"$out/real_facts.tsv"
  # Every fact of every generation of the real git scopes, in ingest order. The
  # active generation alone is not comparable between arms: a paced first
  # generation is superseded unprojected, so the next one is a full re-observation
  # in the treatment arm and a delta in the control. The effective fact set (the
  # latest version of each stable fact key across the chain) is.
  replay_psql -F $'\t' -c "SELECT f.scope_id, extract(epoch FROM g.ingested_at)::numeric(16,3), f.is_tombstone,
      f.stable_fact_key, f.payload::text
      FROM fact_records f JOIN scope_generations g ON g.generation_id = f.generation_id AND g.scope_id = f.scope_id
      WHERE f.scope_id LIKE 'git-repository-scope:%' ORDER BY f.scope_id, g.ingested_at, f.stable_fact_key" \
    >"$out/real_facts_chain.tsv"
  # Reducer intents and projector items of EVERY generation, not only the active
  # ones: a paced generation overtaken by a newer commit is superseded before it
  # is projected and enqueues nothing, so the active-generation set and the union
  # over all generations are compared separately (assemble_result.py). One row per
  # real reducer row (seeded replay-% rows excluded): scope, domain, generation,
  # whether the generation is the active one, status and source.
  replay_psql -F $'\t' -c "SELECT w.scope_id, w.domain, w.generation_id, (w.generation_id = s.active_generation_id),
      w.status, COALESCE(NULLIF(BTRIM(w.payload->>'source_system'), ''), s.source_system)
      FROM fact_work_items w JOIN ingestion_scopes s USING (scope_id)
      WHERE w.stage = 'reducer' AND w.work_item_id NOT LIKE 'replay-%' ORDER BY 1, 2, 3, 5" >"$out/reducer_rows.tsv"
  replay_psql -F $'\t' -c "SELECT w.scope_id, w.generation_id, COALESCE(NULLIF(g.source_commit_sha, ''), ''),
      (w.generation_id = s.active_generation_id), w.status, w.attempt_count, w.source_paced_waits, s.source_system,
      w.admission_wait_attempts, COALESCE(w.failure_class, '')
      FROM fact_work_items w JOIN ingestion_scopes s USING (scope_id)
      LEFT JOIN scope_generations g ON g.generation_id = w.generation_id AND g.scope_id = w.scope_id
      WHERE w.stage = 'projector' ORDER BY 1, 2" >"$out/projector_items.tsv"
  # Every generation of the real git scopes, with the time it was ingested: the
  # selection delay of a pushed commit is its generation's ingested_at minus the
  # push time, matched by source commit sha.
  replay_psql -F $'\t' -c "SELECT g.scope_id, g.generation_id, COALESCE(g.source_commit_sha, ''),
      extract(epoch FROM g.ingested_at)::numeric(16,3), g.is_delta, g.status, (g.generation_id = s.active_generation_id)
      FROM scope_generations g JOIN ingestion_scopes s ON s.scope_id = g.scope_id
      WHERE g.scope_id LIKE 'git-repository-scope:%' ORDER BY 1, 4" >"$out/generations_all.tsv"
  # The latch rows themselves, for the engage and release times and for the
  # "no latch row for A" reading of the T3 arms.
  replay_psql -F $'\t' -c "SELECT signal, domain, engaged, COALESCE(extract(epoch FROM engaged_at)::numeric(16,3)::text, ''),
      extract(epoch FROM updated_at)::numeric(16,3) FROM workflow_claim_pacing ORDER BY 1, 2" >"$out/latch.tsv"
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
  # When source A's projector work finished: the arbiter's section 6 finding
  # (a paced release sets PreviousGenerationExists) is read as A's post-catch-up
  # drain time against C's.
  replay_psql -c "SELECT json_build_object('items', count(*), 'done_ts', COALESCE(extract(epoch FROM max(w.updated_at)), 0))
    FROM fact_work_items w JOIN ingestion_scopes s USING (scope_id)
    WHERE w.stage = 'projector' AND w.status = 'succeeded' AND s.source_system = '$REPLAY_SRC_A'" >"$out/a_drain.json"
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
  # A failed read leaves graph_counts.tsv empty and the assembler fails the run
  # on it; it must not look like an empty graph.
  replay_cypher "MATCH (n) UNWIND labels(n) AS l RETURN l, count(*) ORDER BY l" 2>"$out/graph_counts.err" |
    tail -n +2 | tr -d '"' >"$out/graph_counts.tsv" || true
}

# replay_census <tag> runs the not-takeable census for source A and saves it.
replay_census() {
  local tag="$1" here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  PGPASSWORD="$REPLAY_PG_PASSWORD" "$here/census.sh" "$REPLAY_DSN" "$REPLAY_SRC_A" "${REPLAY_ENGAGE_AGE_SECONDS:-3600}" \
    >"$REPLAY_RUN_DIR/census-$tag.txt" 2>&1 || true
}
