#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# The in-flight arms (arbiter ruling replay-go, decision (g)): the only way to
# exercise the admission-writer leg, because the pre-write consult releases a paced
# item before any write, so only an item that is already past it reaches the writer.
#
# IF  : thresholds set. IFC : thresholds unset, otherwise identical (the control).
#
# Mechanism, deterministic and identical in both arms:
#   1. seed the #10862 mix and source A's seeded generations, but NO aged A rows;
#   2. install a hold: a BEFORE UPDATE trigger on scope_generations that waits while
#      replay_hold.hold is true, for the update that stamps projection_write_started_at
#      (the projector's write marker, which runs after the pre-write consult and
#      before the first graph or content write);
#   3. start the binaries; the real ingester collects the fixtures; items are
#      claimed, pass the pre-write consult (no aged rows exist, the latch is clear)
#      and stall at the marker;
#   4. when a real-fixture projector item is claimed, insert A's aged rows
#      (production shape, 13,000), then release the hold;
#   5. the in-flight items finish their graph and content writes and reach the
#      admission writer, which now consults an engaged latch and defers them
#      (source_behind); later claims are released pre-write;
#   6. at the usual offset the harness completes A's aged rows through the
#      production queue call; the latch releases; the deferred items enqueue their
#      intents after the release and succeed.
# The hold is a test-venue trigger in the disposable replay database only.
# Needs env.sh, host.sh, proc.sh, sampler.sh, collect.sh, hosted.sh, arm.sh and
# REPLAY_SEEDER_IF (the seeder binary with ESHU_REPLAY_SKIP_MIX).

# replay_inflight_install_hold creates the hold table and the marker trigger.
replay_inflight_install_hold() {
  replay_psql -q <<'SQL'
CREATE TABLE replay_hold (hold boolean NOT NULL);
INSERT INTO replay_hold VALUES (true);
CREATE FUNCTION replay_hold_marker() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
  WHILE (SELECT hold FROM replay_hold) LOOP
    PERFORM pg_sleep(0.05);
  END LOOP;
  RETURN NEW;
END
$fn$;
CREATE TRIGGER replay_hold_marker BEFORE UPDATE OF projection_write_started_at ON scope_generations
  FOR EACH ROW WHEN (NEW.projection_write_started_at IS NOT NULL) EXECUTE FUNCTION replay_hold_marker();
SQL
}

# replay_inflight_trigger waits for a claimed real-fixture projector item, inserts
# A's aged rows while the hold is on, and releases the hold.
replay_inflight_trigger() {
  local i
  for ((i = 0; i < 1200; i++)); do
    if [[ "$(replay_psql -c "SELECT count(*) FROM fact_work_items WHERE stage = 'projector'
          AND status IN ('claimed', 'running') AND scope_id LIKE 'git-repository-scope:%'")" -gt 0 ]]; then
      replay_event inflight_claimed "$(replay_psql -c "SELECT count(*) FROM pg_stat_activity WHERE wait_event = 'PgSleep'")"
      ESHU_REPLAY_SKIP_MIX=1 replay_seed_with "$REPLAY_SEEDER_IF" TestReplaySeedBacklog \
        >"$REPLAY_RUN_DIR/seed-aged-a.log" 2>&1 || echo "aged A seed failed" >&2
      replay_event inflight_rows_seeded 0
      replay_psql -c "UPDATE replay_hold SET hold = false" >/dev/null
      replay_event inflight_released 0
      return 0
    fi
    sleep 0.1
  done
  replay_psql -c "UPDATE replay_hold SET hold = false" >/dev/null 2>&1 || true
  replay_event wait_timeout "a real-fixture projector item to be claimed" 120
  return 1
}

# replay_seed_with <seeder binary> <mode>: replay_seed with another seeder binary.
replay_seed_with() {
  local saved="$REPLAY_SEEDER"
  REPLAY_SEEDER="$1"
  shift
  replay_seed "$@"
  local rc=$?
  REPLAY_SEEDER="$saved"
  return $rc
}

# replay_inflight_export writes export/inflight.json: the release time from the latch
# row and the real reducer rows enqueued after it, by scope.
replay_inflight_export() {
  mkdir -p "$REPLAY_RUN_DIR/export"
  replay_psql -c "WITH rel AS (
      SELECT max(updated_at) AS at FROM workflow_claim_pacing
      WHERE signal = 'reducer_source' AND domain = '$REPLAY_SRC_A' AND NOT engaged)
    SELECT json_build_object(
      'release_epoch', (SELECT extract(epoch FROM at) FROM rel),
      'rows_after_release', (SELECT count(*) FROM fact_work_items w, rel WHERE w.stage = 'reducer'
          AND w.work_item_id NOT LIKE 'replay-%' AND w.created_at > rel.at),
      'scopes_after_release', COALESCE((SELECT json_agg(DISTINCT w.scope_id) FROM fact_work_items w, rel
          WHERE w.stage = 'reducer' AND w.work_item_id NOT LIKE 'replay-%' AND w.created_at > rel.at
            AND w.scope_id LIKE 'git-repository-scope:%'), '[]'::json))" >"$REPLAY_RUN_DIR/export/inflight.json"
}

# replay_run_inflight <IF|IFC> <rep> runs one in-flight run and assembles result.json.
replay_run_inflight() {
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
  REPLAY_A_ROWS_SAVED="$REPLAY_A_ROWS"
  replay_write_manifest "$arm" "$rep"
  replay_sampler_start
  trap 'replay_psql -c "UPDATE replay_hold SET hold = false" >/dev/null 2>&1 || true; replay_stop ingester; replay_stop projector; replay_sampler_stop' RETURN

  REPLAY_A_ROWS=0 replay_seed TestReplaySeedBacklog >"$REPLAY_RUN_DIR/seed-backlog.log" 2>&1 ||
    { echo "seed backlog failed" >&2; return 1; }
  replay_seed TestReplaySeedProjector >"$REPLAY_RUN_DIR/seed-projector.log" 2>&1 ||
    { echo "seed projector failed" >&2; return 1; }
  replay_inflight_install_hold
  local -a ing_env proj_env
  mapfile -t ing_env < <(replay_threshold_env ingester; replay_global_admission_env)
  mapfile -t proj_env < <(replay_threshold_env projector)
  replay_start ingester eshu-ingester 19501 "${ing_env[@]}" \
    ESHU_PROJECTOR_WORKERS="$REPLAY_INGESTER_PROJECTOR_WORKERS" ESHU_REPO_SOURCE_MODE=explicit \
    ESHU_REPOSITORY_RULES_JSON="$(replay_repository_rules)" ESHU_REPOS_DIR="$REPLAY_HOME/workspace" \
    GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="url.file://$REPLAY_FIXTURES/remotes/.insteadOf" \
    GIT_CONFIG_VALUE_0="https://github.com/"
  replay_start projector eshu-projector 19502 "${proj_env[@]}" ESHU_PROJECTOR_WORKERS="$REPLAY_PROJECTOR_WORKERS"
  replay_event processes_started 0
  replay_seed TestReplayFeedB >"$REPLAY_RUN_DIR/feed-b.log" 2>&1 &
  local feeder=$!
  replay_inflight_trigger || return 1

  sleep $((REPLAY_ENGAGED_WINDOW - 30))
  replay_census pre
  replay_scrape ingester engaged
  replay_scrape projector engaged
  replay_event catchup_start 0
  replay_seed TestReplayCompleteA >"$REPLAY_RUN_DIR/catchup.log" 2>&1
  replay_event catchup_done 0
  if [[ "$ARM_THRESHOLDS" == 1 ]]; then
    replay_wait_until 180 "latch release" replay_latch_released || true
  fi
  replay_fixture_commit after_release 3
  wait "$feeder" || true
  replay_wait_until 240 "projector queue to settle" replay_projector_settled || true
  sleep 40
  replay_wait_until 120 "projector queue to settle again" replay_projector_settled || true
  replay_census end
  replay_scrape ingester final
  replay_scrape projector final
  replay_sampler_stop
  replay_export_state
  replay_inflight_export
  replay_event run_done "$(replay_load1)"
  python3 -I "$here/assemble_result.py" "$REPLAY_RUN_DIR"
  python3 -I "$here/assemble_inflight.py" "$REPLAY_RUN_DIR"
  echo "run $arm rep $rep done: $REPLAY_RUN_DIR/result.json"
}
