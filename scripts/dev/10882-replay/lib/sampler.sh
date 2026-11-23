#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Background state sampler for one arm. Every REPLAY_SAMPLE_SECONDS (plus the cost
# of the two reads, about 3 s in practice) it records one row of database and graph
# state to samples.tsv in the run directory. The AC4 checks read it: the latch
# row's own engaged_at and updated_at (the engage and release times come from the
# row, not from when a sample happened to see it), source A's graph, content and
# projection writes from a real Neo4j, K, parked rows, the real fixture generation
# count and the host load1 of every sample.
#
# State, not instruments: the consult duration and deferral counters come from
# the product binaries' /metrics, never from here (arbiter item 13). A missing
# graph reading is written as NA, never as 0, and the assembler fails the run on
# it: a Neo4j that cannot be read must not look like a zero-write proof.
# Needs env.sh, host.sh and these variables from the arm: REPLAY_SRC_A,
# REPLAY_A_REPO_PRED (SQL predicate on content repo_id), REPLAY_A_CYPHER_PRED
# (Cypher predicate on node n), REPLAY_RUN_DIR.

REPLAY_SAMPLE_SECONDS="${REPLAY_SAMPLE_SECONDS:-2}"

# replay_graph_count prints source A's node count, retrying once; NA on failure.
replay_graph_count() {
  local out try
  for try in 1 2; do
    out="$(replay_cypher "MATCH (n) WHERE $REPLAY_A_CYPHER_PRED RETURN count(n)" 2>/dev/null | tail -1)"
    if [[ "$out" =~ ^[0-9]+$ ]]; then
      printf '%s' "$out"
      return 0
    fi
  done
  printf 'NA'
}

# replay_sample_once prints one tab-separated sample line.
replay_sample_once() {
  local pg graph
  pg="$(replay_psql -F $'\t' -c "
SELECT extract(epoch FROM clock_timestamp())::numeric(16,3),
  COALESCE((SELECT bool_or(engaged) FROM workflow_claim_pacing
            WHERE signal = 'reducer_source' AND domain = '$REPLAY_SRC_A'), false),
  COALESCE((SELECT extract(epoch FROM max(engaged_at))::numeric(16,3)::text FROM workflow_claim_pacing
            WHERE signal = 'reducer_source' AND domain = '$REPLAY_SRC_A'), 'NA'),
  COALESCE((SELECT extract(epoch FROM max(updated_at))::numeric(16,3)::text FROM workflow_claim_pacing
            WHERE signal = 'reducer_source' AND domain = '$REPLAY_SRC_A'), 'NA'),
  (SELECT count(*) FROM content_files WHERE $REPLAY_A_REPO_PRED),
  (SELECT count(*) FROM content_entities WHERE $REPLAY_A_REPO_PRED),
  (SELECT count(*) FROM fact_work_items w JOIN ingestion_scopes s USING (scope_id)
    WHERE w.stage = 'projector' AND w.status = 'succeeded' AND s.source_system = '$REPLAY_SRC_A'),
  (SELECT count(*) FROM fact_work_items w JOIN ingestion_scopes s USING (scope_id)
    WHERE w.stage = 'reducer' AND w.status = 'retrying' AND (w.visible_at IS NULL OR w.visible_at <= now())
      AND COALESCE(NULLIF(BTRIM(w.payload->>'source_system'), ''), s.source_system) = '$REPLAY_SRC_A'),
  (SELECT count(*) FROM reducer_waiting_items),
  (SELECT count(*) FROM scope_generations WHERE scope_id LIKE 'git-repository-scope:%')" 2>/dev/null || true)"
  graph="$(replay_graph_count)"
  [[ -n "$pg" ]] && printf '%s\t%s\t%s\n' "$pg" "$graph" "$(replay_load1)"
}

# replay_sampler_start launches the sampler loop in the background.
replay_sampler_start() {
  printf 'ts\tlatch_engaged\tlatch_engaged_at\tlatch_updated_at\tcontent_files_a\tcontent_entities_a\tprojector_succeeded_a\tk_visible_retrying_a\twait_rows\treal_generations\tgraph_nodes_a\tload1\n' \
    >"$REPLAY_RUN_DIR/samples.tsv"
  (
    while :; do
      replay_sample_once >>"$REPLAY_RUN_DIR/samples.tsv"
      sleep "$REPLAY_SAMPLE_SECONDS"
    done
  ) &
  echo $! >"$REPLAY_RUN_DIR/sampler.pid"
}

# replay_sampler_stop ends the sampler.
replay_sampler_stop() {
  [[ -f "$REPLAY_RUN_DIR/sampler.pid" ]] || return 0
  kill "$(cat "$REPLAY_RUN_DIR/sampler.pid")" 2>/dev/null || true
}
