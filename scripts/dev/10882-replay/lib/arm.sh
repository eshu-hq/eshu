#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# One replay run (one arm, one repetition). Needs env.sh, host.sh, proc.sh,
# sampler.sh and collect.sh sourced, REPLAY_SEEDER (the replayseed test binary),
# REPLAY_FIXTURES (the git fixture root) and REPLAY_RESULTS (results root).
#
# Phases, all at fixed offsets so every arm sees the same schedule:
#   reset -> seed backlog and source A's projector items -> start binaries ->
#   t+30s fixture commit (selection probe while engaged) -> t+120s catch-up
#   (the harness completes A's aged reducer rows through the production queue
#   call) -> release -> fixture commit (selection probe after release) -> drain
#   -> final exports.
# What the schedule does not do: run a reducer, or run any hosted collector
# other than the T2 stand-in described in the design record.

REPLAY_HOSTED_SRC="dev.eshu.collector.synth-8877-cutover"
REPLAY_HOSTED_COLLECTOR="synth_zero_fact"
REPLAY_ENGAGED_WINDOW="${REPLAY_ENGAGED_WINDOW:-120}"
REPLAY_B_COUNT="${REPLAY_B_COUNT:-150}"
REPLAY_B_CADENCE="${REPLAY_B_CADENCE:-1s}"
REPLAY_A_GENERATIONS="${REPLAY_A_GENERATIONS:-48}"
REPLAY_A_ROWS="${REPLAY_A_ROWS:-13000}"
REPLAY_CANDIDATE_TTL="${REPLAY_CANDIDATE_TTL:-5s}"
REPLAY_ENGAGE_AGE_SECONDS=3600

# replay_arm_config <arm> sets the per-arm switches.
#   C        control, thresholds unset, A=git B=hosted
#   C0       control without the completion step (arbiter F1 compensating run)
#   T1       treatment at signal cache TTL 0
#   T1TTL    treatment at the candidate TTL
#   C2       control for T2: thresholds unset, A hosted, B=git, hosted collector running
#   T2       treatment, A hosted paced at the claim grant, B=git
#   T3       treatment, A's aged rows all not takeable (#11187), TTL 0
#   T3TTL    T3 at the candidate TTL (consult cost with parked rows present)
#   CF       reference for T1 shapes: thresholds unset, both fixture commits pushed BEFORE
#            the binaries start, so every fixture scope ends with a FULL active generation
#            at the final sha (no latch row); one repetition, judged reference
#   CG       one repetition of C at the DEFAULT global admission marks, reported, not judged
#   IF, IFC  the in-flight arm and its control (lib/inflight.sh), run by run_inflight.sh
replay_arm_config() {
  ARM_THRESHOLDS=1 ARM_TTL=0s ARM_COMPLETE=1 ARM_A_KIND=git ARM_A_SHAPE=takeable ARM_HOSTED=0 ARM_GLOBAL_MARKS=raised ARM_PREPUSH=0
  case "$1" in
    C) ARM_THRESHOLDS=0 ;;
    C0) ARM_THRESHOLDS=0 ARM_COMPLETE=0 ;;
    T1) ;;
    T1TTL) ARM_TTL="$REPLAY_CANDIDATE_TTL" ;;
    C2) ARM_THRESHOLDS=0 ARM_A_KIND=hosted ARM_HOSTED=1 ;;
    T2) ARM_A_KIND=hosted ARM_HOSTED=1 ;;
    T3) ARM_COMPLETE=0 ARM_A_SHAPE=notakeable ;;
    T3TTL) ARM_COMPLETE=0 ARM_A_SHAPE=notakeable ARM_TTL="$REPLAY_CANDIDATE_TTL" ;;
    CG) ARM_THRESHOLDS=0 ARM_GLOBAL_MARKS=default ;;
    CF) ARM_THRESHOLDS=0 ARM_PREPUSH=1 ;;
    IF) ;; # in-flight arm, see lib/inflight.sh
    IFC) ARM_THRESHOLDS=0 ;;
    *) echo "unknown arm $1" >&2; return 1 ;;
  esac
  if [[ "$ARM_A_KIND" == git ]]; then
    REPLAY_SRC_A=git REPLAY_COLLECTOR_A=git REPLAY_SRC_B="$REPLAY_HOSTED_SRC" REPLAY_COLLECTOR_B="$REPLAY_HOSTED_COLLECTOR"
    REPLAY_A_REPO_PRED="(repo_id LIKE 'repo-replay-a-%' OR repo_id LIKE 'repository:r\\_%')"
    REPLAY_A_CYPHER_PRED="(n.scope_id STARTS WITH 'replay-a-' OR n.scope_id STARTS WITH 'git-repository-scope:')"
  else
    REPLAY_SRC_A="$REPLAY_HOSTED_SRC" REPLAY_COLLECTOR_A="$REPLAY_HOSTED_COLLECTOR" REPLAY_SRC_B=git REPLAY_COLLECTOR_B=git
    REPLAY_A_REPO_PRED="repo_id LIKE 'repo-replay-a-%'"
    REPLAY_A_CYPHER_PRED="n.scope_id STARTS WITH 'replay-a-'"
  fi
  export REPLAY_SRC_A REPLAY_COLLECTOR_A REPLAY_SRC_B REPLAY_COLLECTOR_B REPLAY_A_REPO_PRED REPLAY_A_CYPHER_PRED
}

# replay_threshold_env prints the ESHU_SOURCE_BACKPRESSURE_* values of a
# treatment arm, one NAME=value per line. The values are the design record's
# Proposed defaults (engage 1h, release 15m, floor 25) plus a grant delay and a
# max engagement longer than the replay.
replay_threshold_env() {
  [[ "$ARM_THRESHOLDS" == 1 ]] || return 0
  cat <<ENV
ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE=1h
ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE=15m
ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR=25
ESHU_SOURCE_BACKPRESSURE_GRANT_DELAY=5s
ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT=6h
ENV
  [[ "$ARM_TTL" == 0s ]] || echo "ESHU_SOURCE_BACKPRESSURE_SIGNAL_CACHE_TTL=$ARM_TTL"
  if [[ "$1" == ingester && "$ARM_A_KIND" == git ]]; then
    echo "ESHU_GIT_SELECTION_MIN_RESYNC_INTERVAL=20s"
    echo "ESHU_GIT_SELECTION_STRETCH_MULTIPLIER=4"
  fi
}

# replay_repository_rules prints the exact-repository rules for the fixtures.
replay_repository_rules() {
  local n sep="" out="["
  for n in $(seq 1 6); do
    out+="${sep}{\"kind\":\"exact\",\"value\":\"replay/fixture-$(printf '%02d' "$n")\"}"
    sep=","
  done
  printf '%s]' "$out"
}

# replay_global_admission_env prints the global database-protection marks of the
# reducer-intent admission gate, raised above the replay corpus in EVERY arm. The
# corpus (8,713 mix rows plus A's 13,000) is past the ingester defaults (10,000
# outstanding, 500 retrying), so the default global gate would defer all intents,
# B's included, and time them out in the control arm too. That backstop is not
# what this replay tests; the per-source leg is. Recorded in every manifest. Only
# the ingester gets them: the standalone projector runs no global gate at its
# product default (reduceradmission/reducer_admission.go), so a mark there would
# turn one on. Arm CG runs C at the default marks, reported and not judged.
replay_global_admission_env() {
  [[ "$ARM_GLOBAL_MARKS" == raised ]] || return 0
  cat <<ENV
ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK=1000000
ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK=1000000
ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK=500000
ENV
}

# replay_event appends one timestamped event to the run's events.tsv.
replay_event() {
  printf '%s\t%s\t%s\t%s\t%s\n' "$(date +%s.%N)" "$1" "${2:-}" "${3:-}" "${4:-}" >>"$REPLAY_RUN_DIR/events.tsv"
}

# replay_fixture_commit <label> <day> adds one fixed-content commit, at a fixed
# date, to every fixture repository. Identical in every arm.
replay_fixture_commit() {
  local label="$1" day="$2" repo
  for repo in "$REPLAY_FIXTURES"/work/replay-fixture-*; do
    printf '// change %s\n' "$label" >>"$repo/src/mod1.go"
    GIT_AUTHOR_NAME=replay GIT_AUTHOR_EMAIL=replay@example.invalid GIT_COMMITTER_NAME=replay \
      GIT_COMMITTER_EMAIL=replay@example.invalid GIT_AUTHOR_DATE="2026-01-0${day}T00:00:00Z" \
      GIT_COMMITTER_DATE="2026-01-0${day}T00:00:00Z" GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null \
      git -C "$repo" commit -q -am "replay $label"
    git -C "$repo" push -q "$REPLAY_FIXTURES/remotes/replay/fixture-${repo##*-}.git" HEAD:main
    # One push event per repository: the pushed HEAD sha is what the selection
    # delay is measured against (a generation is matched to its commit by sha).
    replay_event fixture_push "replay/fixture-${repo##*-}" "$label" "$(git -C "$repo" rev-parse HEAD)"
  done
  replay_event fixture_commit "$(ls -d "$REPLAY_FIXTURES"/work/replay-fixture-* | wc -l)" "$label"
}

# replay_seed runs one mode of the seeder with the arm's identities.
replay_seed() {
  local mode="$1"
  shift
  ESHU_REPLAY_DSN="$REPLAY_DSN" ESHU_REPLAY_SOURCE_A="$REPLAY_SRC_A" ESHU_REPLAY_COLLECTOR_A="$REPLAY_COLLECTOR_A" \
    ESHU_REPLAY_SOURCE_B="$REPLAY_SRC_B" ESHU_REPLAY_COLLECTOR_B="$REPLAY_COLLECTOR_B" \
    ESHU_REPLAY_A_ROWS="$REPLAY_A_ROWS" ESHU_REPLAY_A_GENERATIONS="$REPLAY_A_GENERATIONS" \
    ESHU_REPLAY_A_SHAPE="$ARM_A_SHAPE" ESHU_REPLAY_B_COUNT="$REPLAY_B_COUNT" ESHU_REPLAY_B_CADENCE="$REPLAY_B_CADENCE" \
    ESHU_REPLAY_COMPLETE_CHUNK=2000 ESHU_REPLAY_COMPLETE_EVERY=1s "$@" \
    "$REPLAY_SEEDER" -test.run "^${mode}\$" -test.v
}

# replay_write_manifest records everything that must be equal, or differ on
# purpose, between arms.
replay_write_manifest() {
  local arm="$1" rep="$2" thresholds sel_base=None
  thresholds="$(replay_threshold_env projector | paste -sd, -)"
  if [[ "$ARM_THRESHOLDS" == 1 && "$ARM_A_KIND" == git ]]; then
    sel_base=20 # ESHU_GIT_SELECTION_MIN_RESYNC_INTERVAL seconds, see replay_threshold_env
  fi
  python3 -I - "$REPLAY_RUN_DIR/manifest.json" <<PY
import hashlib, json, os, sys
bins = {n: hashlib.sha256(open(os.path.join("$REPLAY_BIN", n), "rb").read()).hexdigest()
        for n in sorted(os.listdir("$REPLAY_BIN")) if n.startswith("eshu-")}
json.dump({"binaries": bins,
  "arm": "$arm", "rep": $rep, "ttl": "$ARM_TTL", "completion_step": bool($ARM_COMPLETE),
  "thresholds": "$thresholds", "global_admission": "$(replay_global_admission_env | paste -sd, -)", "global_marks": "$ARM_GLOBAL_MARKS", "source_a": "$REPLAY_SRC_A", "source_b": "$REPLAY_SRC_B",
  "a_shape": "$ARM_A_SHAPE", "a_reducer_rows": $REPLAY_A_ROWS, "a_projector_generations": $REPLAY_A_GENERATIONS,
  "b_generations": $REPLAY_B_COUNT, "b_cadence": "$REPLAY_B_CADENCE",
  "workers": {"projector": $REPLAY_PROJECTOR_WORKERS, "ingester_projector": $REPLAY_INGESTER_PROJECTOR_WORKERS},
  "engaged_window_s": $REPLAY_ENGAGED_WINDOW, "selection_base_s": $sel_base, "load1_at_start": "$(replay_load1)",
  "postgres": "$REPLAY_PG_IMAGE", "neo4j": "$REPLAY_NEO4J_IMAGE",
  "commit": "$(cat "$REPLAY_RESULTS/build.commit")", "started": "$(date -u +%FT%TZ)",
}, open(sys.argv[1], "w"), indent=2, sort_keys=True)
PY
}

# replay_wait_until <seconds> <description> <command...> polls a command once a
# second until it succeeds; it reports and returns 1 on timeout.
replay_wait_until() {
  local limit="$1" what="$2"
  shift 2
  local i
  for ((i = 0; i < limit; i++)); do
    if "$@"; then return 0; fi
    sleep 1
  done
  echo "timeout after ${limit}s waiting for $what" >&2
  replay_event wait_timeout "$what" "$limit"
  return 1
}

replay_latch_released() {
  [[ "$(replay_psql -c "SELECT COALESCE(bool_or(engaged), false) FROM workflow_claim_pacing WHERE signal = 'reducer_source' AND domain = '$REPLAY_SRC_A'")" == f ]]
}

replay_projector_settled() {
  # superseded is a settled state: a generation a newer one replaced before it
  # was projected (a paced generation overtaken by the next commit).
  [[ "$(replay_psql -c "SELECT count(*) FROM fact_work_items WHERE stage = 'projector' AND status NOT IN ('succeeded', 'superseded')")" == 0 ]]
}

# replay_run_arm <arm> <rep> runs one arm end to end and assembles result.json.
replay_run_arm() {
  local arm="$1" rep="$2" here
  here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  replay_arm_config "$arm" || return 1
  REPLAY_RUN_DIR="$REPLAY_RESULTS/$arm-rep$rep"
  rm -rf "$REPLAY_RUN_DIR"
  mkdir -p "$REPLAY_RUN_DIR"
  export REPLAY_RUN_DIR

  "$here/lib/reset.sh" >/dev/null
  rm -rf "$REPLAY_HOME/workspace"
  "$here/make_fixtures.sh" "$REPLAY_FIXTURES" 6 >/dev/null
  if [[ "$ARM_PREPUSH" == 1 ]]; then
    replay_fixture_commit engaged 2
    replay_fixture_commit after_release 3
  fi
  replay_write_manifest "$arm" "$rep"
  replay_sampler_start
  trap 'replay_stop ingester; replay_stop projector; replay_stop hosted; replay_sampler_stop' RETURN

  replay_seed TestReplaySeedBacklog >"$REPLAY_RUN_DIR/seed-backlog.log" 2>&1 ||
    { echo "seed backlog failed: $REPLAY_RUN_DIR/seed-backlog.log" >&2; return 1; }
  replay_seed TestReplaySeedProjector >"$REPLAY_RUN_DIR/seed-projector.log" 2>&1 ||
    { echo "seed projector failed: $REPLAY_RUN_DIR/seed-projector.log" >&2; return 1; }
  if [[ "$ARM_HOSTED" == 1 ]]; then
    replay_hosted_prepare || { echo "hosted prepare failed: $REPLAY_RUN_DIR/seed-hosted.log" >&2; return 1; }
  fi
  local -a ing_env proj_env
  mapfile -t ing_env < <(replay_threshold_env ingester; replay_global_admission_env)
  mapfile -t proj_env < <(replay_threshold_env projector)
  replay_start ingester eshu-ingester 19501 "${ing_env[@]}" \
    ESHU_PROJECTOR_WORKERS="$REPLAY_INGESTER_PROJECTOR_WORKERS" ESHU_REPO_SOURCE_MODE=explicit \
    ESHU_REPOSITORY_RULES_JSON="$(replay_repository_rules)" ESHU_REPOS_DIR="$REPLAY_HOME/workspace" \
    GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="url.file://$REPLAY_FIXTURES/remotes/.insteadOf" \
    GIT_CONFIG_VALUE_0="https://github.com/"
  replay_start projector eshu-projector 19502 "${proj_env[@]}" ESHU_PROJECTOR_WORKERS="$REPLAY_PROJECTOR_WORKERS"
  if [[ "$ARM_HOSTED" == 1 ]]; then
    local -a hosted_env
    mapfile -t hosted_env < <(replay_threshold_env hosted)
    replay_hosted_start "${hosted_env[@]}"
  fi
  replay_event processes_started 0
  replay_seed TestReplayFeedB >"$REPLAY_RUN_DIR/feed-b.log" 2>&1 &
  local feeder=$!

  sleep 30
  [[ "$ARM_PREPUSH" == 1 ]] || replay_fixture_commit engaged 2
  sleep $((REPLAY_ENGAGED_WINDOW - 30))
  replay_census pre
  replay_scrape ingester engaged
  replay_scrape projector engaged
  if [[ "$ARM_HOSTED" == 1 ]]; then
    replay_scrape hosted engaged
    replay_hosted_export at_engagement_end
  fi
  if [[ "$ARM_COMPLETE" == 1 ]]; then
    replay_event catchup_start 0
    replay_seed TestReplayCompleteA >"$REPLAY_RUN_DIR/catchup.log" 2>&1
    replay_event catchup_done 0
  fi
  if [[ "$ARM_THRESHOLDS" == 1 && "$ARM_COMPLETE" == 1 ]]; then
    replay_wait_until 180 "latch release" replay_latch_released || true
  fi
  [[ "$ARM_PREPUSH" == 1 ]] || replay_fixture_commit after_release 3
  if [[ "$ARM_HOSTED" == 1 ]]; then
    replay_wait_until 180 "hosted items to complete" replay_hosted_complete || true
  fi
  wait "$feeder" || true
  replay_wait_until 240 "projector queue to settle" replay_projector_settled || true
  sleep 40 # let the post-release fixture commit be selected and projected
  replay_wait_until 120 "projector queue to settle again" replay_projector_settled || true
  replay_census end
  replay_scrape ingester final
  replay_scrape projector final
  if [[ "$ARM_HOSTED" == 1 ]]; then
    replay_scrape hosted final
    replay_hosted_export final
  fi
  replay_sampler_stop
  replay_export_state
  replay_event run_done "$(replay_load1)"
  python3 -I "$here/assemble_result.py" "$REPLAY_RUN_DIR"
  echo "run $arm rep $rep done: $REPLAY_RUN_DIR/result.json"
}
