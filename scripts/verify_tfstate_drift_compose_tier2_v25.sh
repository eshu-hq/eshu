#!/usr/bin/env bash
#
# scripts/verify_tfstate_drift_compose_tier2_v25.sh
#
# Tier-2 v2.5 E2E compose proof for the Terraform config-vs-state drift
# handler (issue #209). Where the Tier-2 v1 verifier proves buckets A, B,
# D, and E from a single collector pass, v2.5 proves buckets C
# (`removed_from_state`) and F (`removed_from_config`) by running two
# distinct terraform_state collector instances back-to-back against gen-1
# and gen-2 fixtures.
#
# Orchestration (Option B — dual collector instance):
#
#   Pass 1
#     - compose up with repos_gen1 + state_gen1 mounted.
#     - collector-terraform-state-gen1 has claims_enabled=true.
#     - collector-terraform-state-gen2 has claims_enabled=false.
#     - bootstrap-index Pass 1 collects repos_gen1 (creates active gen-1
#       scope_generations rows for the bucket-C and bucket-F repos).
#     - Coordinator plans terraform_state work items keyed against the
#       gen-1 instance; collector-gen1 claims and drains.
#     - Phase 3.5 runs once (no drift expected yet because there is no
#       prior generation in Postgres).
#
#   Mutate
#     - Overwrite the MinIO objects in eshu-drift-c and eshu-drift-f with
#       the gen-2 .tfstate via `docker compose --profile gen2 run --rm
#       minio-init-gen2`. Bucket C state rotates to serial=2 with the
#       resource removed; bucket F state stays empty.
#     - Recreate bootstrap-index/ingester/resolution-engine with
#       ESHU_TIER2_V25_REPOS_DIR pointing at repos_gen2 so the next ingest
#       sees the gen-2 .tf files.
#     - Flip the collector-instance claims_enabled toggles:
#       gen1 -> false, gen2 -> true; recreate workflow-coordinator and
#       both collector containers so the new env takes effect.
#
#   Pass 2
#     - bootstrap-index Pass 2 collects repos_gen2 (gen-1 scope_generations
#       rows are marked superseded; new active gen-2 rows are published).
#     - Coordinator plans fresh terraform_state work items against the
#       gen-2 instance (the per-RunID idempotency at
#       coordinator/tfstate_scheduler.go:129 is scoped to one instance, so
#       gen-2's RunID is distinct from gen-1's).
#     - collector-gen2 claims and drains; emits the serial=2 snapshot for
#       bucket C and the empty serial=2 snapshot for bucket F.
#     - Phase 3.5 runs again. The prior-state query finds gen-1's serial=1
#       snapshot for bucket C and emits drift_kind="removed_from_state".
#       The prior-config query finds gen-1's superseded scope_generations
#       row for bucket F's repo and emits drift_kind="removed_from_config".
#
# Assertions: non-zero counter deltas for drift_kind=removed_from_state AND
# drift_kind=removed_from_config on the
# eshu_dp_correlation_drift_detected_total counter, plus structured-log
# assertions for the admitted candidates.
#
# Usage:
#   bash scripts/verify_tfstate_drift_compose_tier2_v25.sh
#
# Environment knobs:
#   ESHU_KEEP_COMPOSE_STACK=true   Skip `docker compose down -v` at the end.
#   ESHU_TFSTATE_DRIFT_V25_PROOF_OUT   Write captured artifacts to this file.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

RUNTIME_LIB="$REPO_ROOT/scripts/lib/compose_verification_runtime_common.sh"
DRIFT_LIB="$REPO_ROOT/scripts/lib/tfstate_drift_compose_common.sh"
TIER2_LIB="$REPO_ROOT/scripts/lib/tfstate_drift_tier2_compose_common.sh"

source "$RUNTIME_LIB"
source "$DRIFT_LIB"
source "$TIER2_LIB"

TMP_DIR="$(mktemp -d)"
METRICS_BEFORE_FILE="$TMP_DIR/metrics-before.txt"
METRICS_AFTER_PASS1_FILE="$TMP_DIR/metrics-after-pass1.txt"
METRICS_AFTER_PASS2_FILE="$TMP_DIR/metrics-after-pass2.txt"
PHASE_35_PASS1_LOG="$TMP_DIR/phase-3-5-pass1.log"
PHASE_35_PASS2_LOG="$TMP_DIR/phase-3-5-pass2.log"
DRIFT_LOGS_FILE="$TMP_DIR/drift-logs.json"

KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
PROOF_OUT="${ESHU_TFSTATE_DRIFT_V25_PROOF_OUT:-}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-eshu-tfstate-drift-tier2-v25-209-$$}"
export COMPOSE_PROJECT_NAME

COMPOSE_CMD=()
COMPOSE_DISPLAY=""
RESERVED_HOST_PORTS=()
PICKED_PORT=""

LABEL_PACK='pack="terraform_config_state_drift"'
LABEL_REMOVED_FROM_STATE="drift_kind=\"removed_from_state\""
LABEL_REMOVED_FROM_CONFIG="drift_kind=\"removed_from_config\""

# Canonical repository ID for a filesystem-mode repo is
# `repository:r_<first 8 hex chars of sha1(localPath)>`. localPath inside the
# bootstrap-index/ingester container resolves to /data/repos/<folder> (the
# fixture tree is bind-mounted at /fixtures but staged into /data/repos by the
# Git collector). The v1 Tier-2 overlay documents the same convention at
# docker-compose.tier2-tfstate.yaml:31. compute_repo_id is a deterministic
# substitute the v25 collector-instances env var consumes; the coordinator
# matches scope_generations rows keyed against this exact ID.
compute_repo_id() {
    local folder="$1"
    local in_container_path="/data/repos/${folder}"
    local hash
    if command -v sha1sum >/dev/null 2>&1; then
        hash="$(printf '%s' "$in_container_path" | sha1sum | head -c8)"
    else
        hash="$(printf '%s' "$in_container_path" | shasum -a 1 | head -c8)"
    fi
    printf 'repository:r_%s' "$hash"
}

require_compose() {
    for candidate in "docker compose" "docker-compose"; do
        # shellcheck disable=SC2206
        local cmd_array=($candidate)
        if "${cmd_array[@]}" version >/dev/null 2>&1; then
            COMPOSE_CMD=("${cmd_array[@]}")
            COMPOSE_DISPLAY="$candidate"
            return 0
        fi
    done
    echo "docker compose not found on PATH" >&2
    return 1
}

eshu_require_tool docker
eshu_require_tool curl
eshu_require_tool jq
eshu_require_tool nc
require_compose

COMPOSE_CMD+=(-f docker-compose.yaml -f docker-compose.tier2-tfstate-v25.yaml)

pick_port() {
    local start_port="$1" port
    for ((port = start_port; port < start_port + 200; port++)); do
        if [[ " ${RESERVED_HOST_PORTS[*]} " == *" $port "* ]]; then
            continue
        fi
        nc -z 127.0.0.1 "$port" >/dev/null 2>&1 || {
            RESERVED_HOST_PORTS+=("$port")
            PICKED_PORT="$port"
            return 0
        }
    done
    echo "no free port found near $start_port" >&2
    return 1
}

assign_port() {
    local name="$1" start_port="$2"
    pick_port "$start_port"
    printf -v "$name" '%s' "$PICKED_PORT"
    export "$name"
}

configure_ports() {
    RESERVED_HOST_PORTS=()
    # v2.5 port range bumped up another +1000 from Tier-2 v1 so all three
    # verifier flavors (Tier-1, Tier-2 v1, Tier-2 v2.5) can run concurrently
    # without colliding.
    assign_port NEO4J_HTTP_PORT "${NEO4J_HTTP_PORT:-57474}"
    assign_port NEO4J_BOLT_PORT "${NEO4J_BOLT_PORT:-57687}"
    assign_port ESHU_POSTGRES_PORT "${ESHU_POSTGRES_PORT:-55432}"
    assign_port ESHU_HTTP_PORT "${ESHU_HTTP_PORT:-58080}"
    assign_port ESHU_MCP_PORT "${ESHU_MCP_PORT:-58081}"
    assign_port ESHU_API_METRICS_PORT "${ESHU_API_METRICS_PORT:-51464}"
    assign_port ESHU_BOOTSTRAP_METRICS_PORT "${ESHU_BOOTSTRAP_METRICS_PORT:-51467}"
    assign_port ESHU_MCP_METRICS_PORT "${ESHU_MCP_METRICS_PORT:-51468}"
    assign_port ESHU_INGESTER_METRICS_PORT "${ESHU_INGESTER_METRICS_PORT:-51465}"
    assign_port ESHU_RESOLUTION_ENGINE_METRICS_PORT "${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-51466}"
    assign_port ESHU_WORKFLOW_COORDINATOR_HTTP_PORT "${ESHU_WORKFLOW_COORDINATOR_HTTP_PORT:-58082}"
    assign_port ESHU_WORKFLOW_COORDINATOR_METRICS_PORT "${ESHU_WORKFLOW_COORDINATOR_METRICS_PORT:-51469}"
    assign_port ESHU_COLLECTOR_TFSTATE_GEN1_METRICS_PORT "${ESHU_COLLECTOR_TFSTATE_GEN1_METRICS_PORT:-51470}"
    assign_port ESHU_COLLECTOR_TFSTATE_GEN2_METRICS_PORT "${ESHU_COLLECTOR_TFSTATE_GEN2_METRICS_PORT:-51471}"
    assign_port MINIO_PORT "${MINIO_PORT:-59000}"
    assign_port MINIO_CONSOLE_PORT "${MINIO_CONSOLE_PORT:-59001}"
}

cleanup() {
    local exit_code=$?
    if [[ "$exit_code" -ne 0 ]]; then
        echo
        echo "tier-2 v2.5 tfstate drift compose verification failed (exit=$exit_code)."
        echo "Compose project: $COMPOSE_PROJECT_NAME"
        "${COMPOSE_CMD[@]}" ps || true
        tier2_dump_failure_logs
        [[ -f "$DRIFT_LOGS_FILE" ]] && {
            echo "Drift logs captured at $DRIFT_LOGS_FILE (tail 20):"
            tail -n 20 "$DRIFT_LOGS_FILE" || true
        }
        [[ -f "$METRICS_AFTER_PASS2_FILE" ]] && {
            echo "Metrics scraped at $METRICS_AFTER_PASS2_FILE:"
            cat "$METRICS_AFTER_PASS2_FILE" || true
        }
    fi
    if [[ "$KEEP_STACK" != "true" ]]; then
        "${COMPOSE_CMD[@]}" --profile gen2 down -v >/dev/null 2>&1 || true
    fi
    rm -rf "$TMP_DIR"
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

cd "$REPO_ROOT"

echo "==> Computing canonical repository IDs for v2.5 fixture repos"
ESHU_TIER2_V25_REPO_C_ID="$(compute_repo_id drift-c-tf-config)"
ESHU_TIER2_V25_REPO_F_ID="$(compute_repo_id drift-f-tf-config)"
export ESHU_TIER2_V25_REPO_C_ID ESHU_TIER2_V25_REPO_F_ID
echo "  bucket C repo_id: $ESHU_TIER2_V25_REPO_C_ID"
echo "  bucket F repo_id: $ESHU_TIER2_V25_REPO_F_ID"

echo "==> Configuring host port assignments"
configure_ports

# Pass 1 fixture binding + collector toggles
export ESHU_TIER2_V25_REPOS_DIR="./tests/fixtures/tfstate_drift_tier2/v25/repos_gen1"
export ESHU_TIER2_V25_GEN1_CLAIMS=true
export ESHU_TIER2_V25_GEN2_CLAIMS=false

echo "==> Pass 1: bringing up v2.5 compose stack with gen-1 fixtures"
"${COMPOSE_CMD[@]}" up --build -d \
    postgres nornicdb db-migrate workspace-setup \
    minio minio-init-gen1 \
    bootstrap-index \
    resolution-engine \
    workflow-coordinator \
    collector-terraform-state-gen1 \
    collector-terraform-state-gen2 \
    eshu

echo "==> Waiting for db-migrate to complete"
eshu_compose_wait_for_named_exit_tier2 "db-migrate" 180

echo "==> Waiting for minio-init-gen1 to upload gen-1 .tfstate objects"
eshu_compose_wait_for_named_exit_tier2 "minio-init-gen1" 90

echo "==> Waiting for bootstrap-index Pass 1 to complete"
eshu_compose_wait_for_named_exit_tier2 "bootstrap-index" 240

echo "==> Asserting Git collector emitted terraform_backends facts for C+F"
tier2_assert_terraform_backend_facts 2 60

echo "==> Asserting workflow-coordinator planned >=2 terraform_state work items for the gen-1 instance"
tier2_wait_for_terraform_state_work_items 2 180

echo "==> Waiting for collector-gen1 to drain Pass 1 work items"
tier2_wait_for_terraform_state_work_drained 240 2

echo "==> Asserting Pass 1 terraform_state_snapshot facts landed (>=2: C-serial1, F-serial1)"
tier2_assert_terraform_state_snapshot_facts 2

echo "==> Capturing metrics baseline before mid-run mutation"
tfstate_drift_scrape_counters \
    "localhost:${ESHU_RESOLUTION_ENGINE_METRICS_PORT}" \
    "$METRICS_BEFORE_FILE" \
    || true

echo "==> Phase 3.5 Pass 1 (no drift expected; bootstraps gen-1 generations in Postgres)"
"${COMPOSE_CMD[@]}" run --rm bootstrap-index >"$PHASE_35_PASS1_LOG" 2>&1 \
    || {
        echo "bootstrap-index Pass 1 rerun failed; tail of output:" >&2
        tail -n 60 "$PHASE_35_PASS1_LOG" >&2
        exit 1
    }

echo "==> Mid-run mutation: overwriting MinIO objects with gen-2 state"
"${COMPOSE_CMD[@]}" --profile gen2 run --rm minio-init-gen2 \
    || {
        echo "minio-init-gen2 failed" >&2
        exit 1
    }

echo "==> Swapping fixture repos volume to gen-2 and flipping collector claims toggles"
export ESHU_TIER2_V25_REPOS_DIR="./tests/fixtures/tfstate_drift_tier2/v25/repos_gen2"
export ESHU_TIER2_V25_GEN1_CLAIMS=false
export ESHU_TIER2_V25_GEN2_CLAIMS=true

# Force recreate the affected services so the new env vars and bind mounts
# take effect. workflow-coordinator must restart so the new
# ESHU_COLLECTOR_INSTANCES_JSON gets parsed; both collectors must restart so
# the new claims_enabled flag takes effect.
#
# bootstrap-index is intentionally NOT in this list. The Pass 1 bootstrap-
# index container has already exited; the `run --rm bootstrap-index` Pass 2
# invocation below picks up the new ESHU_TIER2_V25_REPOS_DIR at exec time
# and binds repos_gen2.
#
# ingester stays in the running set: the canonical writer's commit-time
# UNIQUE-on-MERGE retry path (RetryingExecutor.ExecuteGroup classifying
# isNornicDBCommitTimeUniqueConflict + allStatementsAreMerge as retryable,
# go/internal/storage/cypher/retrying_executor.go) absorbs concurrent
# writers on the same File.[path] uid without serializing here. Per the
# project rule "Serialization Is Not A Fix" (CLAUDE.md / AGENTS.md), a
# concurrent writer must not be stopped purely to silence a MERGE race.
"${COMPOSE_CMD[@]}" up -d --force-recreate --no-deps \
    resolution-engine eshu \
    workflow-coordinator \
    collector-terraform-state-gen1 \
    collector-terraform-state-gen2

echo "==> Pass 2: re-running bootstrap-index against gen-2 repos"
# bootstrap-index defaults to min(NumCPU, 8) projection workers. With the
# concurrent-MERGE retry now landed in RetryingExecutor.ExecuteGroup,
# multi-worker projection on gen-2 facts that share a TerraformResource
# uid with Pass 1 is self-healing: the first commit succeeds, racers
# retry-and-match. Worker-knob serialization (ESHU_PROJECTION_WORKERS=1)
# is intentionally NOT set — see "Serialization Is Not A Fix" in
# CLAUDE.md / AGENTS.md.
"${COMPOSE_CMD[@]}" run --rm bootstrap-index >"$PHASE_35_PASS2_LOG" 2>&1 \
    || {
        echo "bootstrap-index Pass 2 failed; tail of output:" >&2
        tail -n 60 "$PHASE_35_PASS2_LOG" >&2
        exit 1
    }

echo "==> Waiting for Pass 2 terraform_state work items (need additional rows for gen-2 instance)"
tier2_wait_for_terraform_state_work_items 4 240

echo "==> Waiting for collector-gen2 to drain Pass 2 work items"
tier2_wait_for_terraform_state_work_drained 300 4

echo "==> Asserting Pass 2 terraform_state_snapshot facts landed (>=4: C-s1, C-s2, F-s1, F-s2)"
tier2_assert_terraform_state_snapshot_facts 4

echo "==> Confirming Phase 3.5 Pass 2 enqueued drift intents for the gen-2 snapshots"
grep -E 'config_state_drift_intents_enqueued count=[0-9]+' "$PHASE_35_PASS2_LOG" \
    | tail -n 1 \
    | tee -a "$PHASE_35_PASS2_LOG.summary"
phase35_count="$(
    grep -E 'config_state_drift_intents_enqueued count=[0-9]+' "$PHASE_35_PASS2_LOG" \
    | tail -n 1 \
    | sed -E 's/.*count=([0-9]+).*/\1/'
)"
if [[ -z "$phase35_count" || "$phase35_count" -lt 2 ]]; then
    echo "Phase 3.5 Pass 2 enqueued count=${phase35_count:-<missing>}, expected >=2 (C, F)" >&2
    tail -n 60 "$PHASE_35_PASS2_LOG" >&2
    exit 1
fi

echo "==> Waiting for reducer to drain Pass 2 config_state_drift intents"
tfstate_drift_wait_for_reducer_drain 240

sleep 5

echo "==> Scraping metrics after Pass 2 drain"
tfstate_drift_scrape_counters \
    "localhost:${ESHU_RESOLUTION_ENGINE_METRICS_PORT}" \
    "$METRICS_AFTER_PASS2_FILE"

echo "==> Extracting drift log lines from resolution-engine"
tfstate_drift_extract_drift_logs "$DRIFT_LOGS_FILE"

# -- assertions --------------------------------------------------------------

echo "==> Asserting per-kind counter deltas for C and F via ${COMPOSE_DISPLAY}"
for entry in \
    "removed_from_state:$LABEL_REMOVED_FROM_STATE" \
    "removed_from_config:$LABEL_REMOVED_FROM_CONFIG"; do
    kind="${entry%%:*}"
    kind_label="${entry#*:}"
    value_after="$(
        tfstate_drift_counter_value "$METRICS_AFTER_PASS2_FILE" \
            '^eshu_dp_correlation_drift_detected_total\{' \
            "$LABEL_PACK" \
            "$kind_label"
    )"
    value_before="$(
        tfstate_drift_counter_value "$METRICS_BEFORE_FILE" \
            '^eshu_dp_correlation_drift_detected_total\{' \
            "$LABEL_PACK" \
            "$kind_label"
    )"
    delta=$((value_after - value_before))
    echo "  drift_kind=$kind before=$value_before after=$value_after delta=$delta"
    if [[ "$delta" -lt 1 ]]; then
        echo "Counter delta for drift_kind=$kind was $delta, expected >=1" >&2
        echo "Captured metrics:" >&2
        cat "$METRICS_AFTER_PASS2_FILE" >&2
        exit 1
    fi
done

echo "==> Asserting structured-log entries for each admitted drift kind"
for kind in removed_from_state removed_from_config; do
    tfstate_drift_assert_log_line "$DRIFT_LOGS_FILE" \
        "\"drift.kind\":\"$kind\"" \
        "drift candidate admitted log for drift_kind=$kind"
done

echo "==> Asserting drift admission carries high-cardinality address field"
for address in aws_s3_bucket.cached aws_s3_bucket.legacy; do
    tfstate_drift_assert_log_line "$DRIFT_LOGS_FILE" \
        "\"drift.address\":\"$address\"" \
        "drift.address=$address in admitted-candidate log"
done

# -- proof artifact ----------------------------------------------------------

if [[ -n "$PROOF_OUT" ]]; then
    echo "==> Writing proof artifact to $PROOF_OUT"
    proof_dir="$(dirname "$PROOF_OUT")"
    mkdir -p "$proof_dir"
    {
        echo "# Tier-2 v2.5 tfstate drift compose proof matrix"
        echo
        echo "Captured: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
        echo "Compose project: \`$COMPOSE_PROJECT_NAME\`"
        echo "Worktree HEAD: \`$(git rev-parse --short HEAD)\` on $(git rev-parse --abbrev-ref HEAD)"
        echo
        echo "## Phase 3.5 Pass 2 enqueue log"
        echo
        echo '```'
        grep -E 'config_state_drift_intents_enqueued count=[0-9]+' "$PHASE_35_PASS2_LOG" | tail -n 1
        echo '```'
        echo
        echo "## Counter snapshot (after Pass 2 drain)"
        echo
        echo '```'
        cat "$METRICS_AFTER_PASS2_FILE"
        echo '```'
        echo
        echo "## Structured log excerpts"
        echo
        echo '```json'
        cat "$DRIFT_LOGS_FILE"
        echo '```'
    } >"$PROOF_OUT"
fi

echo
echo "OK — Tier-2 v2.5 tfstate drift compose proof matrix passed."
