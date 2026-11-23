#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Arm T2: the hosted family paced at the coordinator claim grant. The hosted
# collector is the built collector-component-extension binary running the
# offline synthetic zero-fact emitter (collector-synth-zero) on the in-tree claim
# path against the replay PostgreSQL. A tagged seeder in cmd/collector-synth-zero
# prepares the on-disk component registry and plants the pending workflow items
# through the real planner; the binary claims them through its real dispatcher,
# which consults per-source backpressure before every grant.
#
# What this arm does NOT show: the emitter commits no facts, so it proves claim
# pacing only (pending items stay pending, no attempt spent, each completes after
# release), not hosted fact commit. Needs env.sh, host.sh and proc.sh sourced and
# REPLAY_RUN_DIR set. The synth seeder binary is built by build.sh.

REPLAY_HOSTED_ITEMS="${REPLAY_HOSTED_ITEMS:-24}"
REPLAY_HOSTED_INSTANCE="synth-8877-cutover-instance-1"

# replay_hosted_prepare installs the component registry and plants the items.
replay_hosted_prepare() {
  # The seeder reads manifest.yaml and trust-grant.json relative to its package
  # directory, so it runs from there like `go test` would.
  (
    cd "$REPLAY_REPO_ROOT/go/cmd/collector-synth-zero"
    ESHU_REPLAY_DSN="$REPLAY_DSN" ESHU_REPLAY_COMPONENT_HOME="$REPLAY_RUN_DIR/component-home" \
      ESHU_REPLAY_SYNTH_BIN="$REPLAY_BIN/eshu-collector-synth-zero" ESHU_REPLAY_HOSTED_ITEMS="$REPLAY_HOSTED_ITEMS" \
      "$REPLAY_BIN/replayseed-synth.test" -test.run '^TestReplaySeedHostedFamily$' -test.v
  ) >"$REPLAY_RUN_DIR/seed-hosted.log" 2>&1
}

# replay_hosted_start starts the built collector-component-extension binary.
# $@ are extra NAME=value settings (the threshold values).
replay_hosted_start() {
  replay_start hosted eshu-collector-component-extension 19503 "$@" \
    ESHU_COMPONENT_HOME="$REPLAY_RUN_DIR/component-home" ESHU_COMPONENT_TRUST_MODE=allowlist \
    ESHU_COMPONENT_ALLOW_IDS="$REPLAY_HOSTED_SRC" ESHU_COMPONENT_ALLOW_PUBLISHERS=eshu-hq \
    ESHU_COMPONENT_COLLECTOR_INSTANCE_ID="$REPLAY_HOSTED_INSTANCE" ESHU_COMPONENT_COLLECTOR_OWNER_ID=replay-hosted-1 \
    ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL=1s
}

# replay_hosted_export <tag> writes the claim accounting of the hosted family:
# pending items, claims ever issued, total attempts, completed and expired.
replay_hosted_export() {
  mkdir -p "$REPLAY_RUN_DIR/export"
  replay_psql -c "SELECT json_build_object(
      'pending', count(*) FILTER (WHERE status = 'pending'),
      'claims', (SELECT count(*) FROM workflow_claims c JOIN workflow_work_items i USING (work_item_id)
                 WHERE i.collector_kind = '$REPLAY_HOSTED_COLLECTOR'),
      'attempts', COALESCE(sum(attempt_count), 0),
      'completed', count(*) FILTER (WHERE status = 'completed'),
      'expired', count(*) FILTER (WHERE status IN ('expired', 'failed_terminal', 'failed_retryable')),
      'planted', count(*))
    FROM workflow_work_items WHERE collector_kind = '$REPLAY_HOSTED_COLLECTOR'" >"$REPLAY_RUN_DIR/export/hosted-$1.json"
}

# replay_hosted_complete reports whether every planted item has completed.
replay_hosted_complete() {
  [[ "$(replay_psql -c "SELECT count(*) FROM workflow_work_items WHERE collector_kind = '$REPLAY_HOSTED_COLLECTOR' AND status <> 'completed'")" == 0 ]]
}
