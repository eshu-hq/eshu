#!/bin/sh
# One-shot demo-corpus convergence orchestrator for docker-compose.demo.yaml.
#
# Approximates the ingester's continuous RunDeferredRelationshipMaintenance
# loop the same way scripts/verify-golden-corpus-gate.sh does: bootstrap-index
# -> drain -> deferred-relationship maintenance pass x2 (bootstrap-index rerun
# + drain) each. A single bootstrap+reducer pass is NOT sufficient to converge
# Q2 (KubernetesWorkload RUNS_IMAGE OciImageManifest, rc-4) or Q4 (cross-repo
# Repository DEPENDS_ON Repository, rc-3) — both need the reopened correlation
# domains a maintenance pass re-drives.
#
# Drain-to-terminal is read directly from Postgres with the same residual SQL
# scripts/verify-golden-corpus-gate.sh's eshu-golden-corpus-gate binary uses
# (go/cmd/golden-corpus-gate/drains.go): fact_work_items rows not in a clean
# terminal status, and shared_projection_intents rows with completed_at IS
# NULL. eshu-golden-corpus-gate itself is a gate-only binary (not part of the
# Dockerfile product image), so this script re-implements only the residual
# poll, not the gate's snapshot/graph/query assertions.
set -eu

: "${ESHU_POSTGRES_DSN:?ESHU_POSTGRES_DSN must be set}"
drain_timeout=${ESHU_DEMO_DRAIN_TIMEOUT_SECONDS:-600}
poll_interval=${ESHU_DEMO_DRAIN_POLL_SECONDS:-3}
# Number of distinct non-git collector source systems the demo corpus expects
# to have committed before bootstrap runs (the nine cassette collectors).
expected_collector_sources=${ESHU_DEMO_EXPECTED_COLLECTOR_SOURCES:-9}
# Settle window before the FIRST drain poll after starting reducer+projector.
# A poll that fires before the reducer has emitted any shared_projection_intents
# rows reads a false 0/0 "drained" on an unreduced pipeline — the same hazard
# scripts/verify-golden-corpus-gate.sh guards against with
# -require-populated-domains. Under concurrent host load (another heavy compose
# stack contending for CPU/IO) the reducer takes longer to reach its first
# emit, so this settle also absorbs that contention instead of racing it.
settle_seconds=${ESHU_DEMO_DRAIN_SETTLE_SECONDS:-60}

log() { printf '\n=== %s ===\n' "$*"; }

# The product image (Dockerfile) is Go-binaries-only and has no psql client.
# Install it once here rather than growing the shared Dockerfile for a
# demo-only diagnostic dependency; this is a package-manager fetch (like the
# other one-shot compose preflights in this repo), not a runtime credential.
log "install postgresql client for drain polling"
apk add --no-cache postgresql16-client >/dev/null

# psql_scalar runs a single-value SQL query against the demo Postgres.
psql_scalar() {
	PGPASSWORD_UNUSED=1 psql "$ESHU_POSTGRES_DSN" -tA -c "$1" 2>/dev/null | tr -d '[:space:]'
}

# wait_for_drain settles for settle_seconds (letting the reducer reach its
# first emit under contention), then polls until the reducer has quiesced. It
# takes a strictness mode:
#
#   intermediate — every ACTIVELY-processable fact_work_item (pending, claimed,
#     running) has cleared and every shared_projection_intents row is terminal.
#     `retrying` items are TOLERATED here: a cross-scope deferral such as
#     gcp_relationship_materialization waiting on canonical gcp nodes cannot
#     succeed until a later maintenance bootstrap replays the domain it depends
#     on, so requiring full residual=0 on an intermediate pass would block
#     forever. This mirrors the golden gate, whose intermediate drains do not
#     assert full drain.
#   final — additionally requires zero residual: no `retrying`, no
#     `dead_letter`. After the last maintenance pass every deferral must have
#     resolved, matching go/cmd/golden-corpus-gate/drains.go (any fact_work_item
#     not in ('succeeded','superseded') is residual, and a dead letter fails).
#
# Both modes require the populated-then-drained guard: at least one
# shared_projection_intents row must have been observed before a drained
# reading is accepted, so a poll racing ahead of the reducer's first emit never
# passes on an unreduced pipeline.
wait_for_drain() {
	label=$1
	mode=$2
	echo "[$label] settling ${settle_seconds}s before first drain poll"
	sleep "$settle_seconds"

	elapsed=0
	observed_populated=0
	while :; do
		active=$(psql_scalar "SELECT count(*) FROM fact_work_items WHERE status IN ('pending','claimed','running');")
		residual=$(psql_scalar "SELECT count(*) FROM fact_work_items WHERE status NOT IN ('succeeded','superseded');")
		intents_residual=$(psql_scalar "SELECT count(*) FROM shared_projection_intents WHERE completed_at IS NULL;")
		intents_seen=$(psql_scalar "SELECT count(*) FROM shared_projection_intents;")
		active=${active:-0}
		residual=${residual:-0}
		intents_residual=${intents_residual:-0}
		intents_seen=${intents_seen:-0}
		if [ "$intents_seen" != "0" ]; then
			observed_populated=1
		fi
		echo "[$label] active=$active residual=$residual intents_nonterminal=$intents_residual intents_seen=$intents_seen mode=$mode elapsed=${elapsed}s"
		if [ "$observed_populated" = "1" ] && [ "$active" = "0" ] && [ "$intents_residual" = "0" ]; then
			if [ "$mode" = "final" ]; then
				if [ "$residual" = "0" ]; then
					echo "[$label] fully drained (final: zero residual)"
					return 0
				fi
			else
				echo "[$label] quiesced (intermediate: no active work, retrying deferrals allowed)"
				return 0
			fi
		fi
		if [ "$elapsed" -ge "$drain_timeout" ]; then
			echo "demo corpus orchestrator failed: [$label] did not drain within ${drain_timeout}s (mode=$mode active=$active residual=$residual observed_populated=$observed_populated)" >&2
			return 1
		fi
		sleep "$poll_interval"
		elapsed=$((elapsed + poll_interval))
	done
}

# wait_for_collector_commits blocks until at least expected_collector_sources
# distinct non-git source systems have committed an ingestion scope, mirroring
# the golden gate's post-collector assertion
# (SELECT count(DISTINCT source_system) FROM ingestion_scopes WHERE
# source_system <> 'git'). The compose service_healthy gate only proves each
# collector's status server is up, not that its cassette commit landed, so this
# closes that race before bootstrap consumes a partial corpus.
wait_for_collector_commits() {
	elapsed=0
	while :; do
		sources=$(psql_scalar "SELECT count(DISTINCT source_system) FROM ingestion_scopes WHERE source_system <> 'git';")
		sources=${sources:-0}
		echo "[collector-commits] distinct_non_git_sources=$sources want>=$expected_collector_sources elapsed=${elapsed}s"
		if [ "$sources" -ge "$expected_collector_sources" ]; then
			echo "[collector-commits] all cassette collectors committed"
			return 0
		fi
		if [ "$elapsed" -ge "$drain_timeout" ]; then
			echo "demo corpus orchestrator failed: only ${sources} collector source(s) committed within ${drain_timeout}s (want >= ${expected_collector_sources})" >&2
			return 1
		fi
		sleep "$poll_interval"
		elapsed=$((elapsed + poll_interval))
	done
}

log "verify cassette collectors committed before bootstrap"
wait_for_collector_commits || exit 1

log "bootstrap-index over demo corpus (schema + filesystem facts + projection)"
/usr/local/bin/eshu-bootstrap-index

log "start reducer + projector for first drain pass"
/usr/local/bin/eshu-reducer &
reducer_pid=$!
/usr/local/bin/eshu-projector &
projector_pid=$!

wait_for_drain "first-drain" intermediate || { kill "$reducer_pid" "$projector_pid" 2>/dev/null || true; exit 1; }
kill "$reducer_pid" "$projector_pid" 2>/dev/null || true
wait "$reducer_pid" 2>/dev/null || true
wait "$projector_pid" 2>/dev/null || true

# The final maintenance pass asserts a full drain (zero residual, no retrying or
# dead letters); earlier passes tolerate cross-scope retrying deferrals.
pass=1
while [ "$pass" -le 2 ]; do
	if [ "$pass" -eq 2 ]; then
		drain_mode=final
	else
		drain_mode=intermediate
	fi
	log "deferred maintenance pass $pass: re-run bootstrap-index maintenance"
	/usr/local/bin/eshu-bootstrap-index

	log "drain pass $pass (reducer + projector)"
	/usr/local/bin/eshu-reducer &
	reducer_pid=$!
	/usr/local/bin/eshu-projector &
	projector_pid=$!

	wait_for_drain "maintenance-drain-$pass" "$drain_mode" || { kill "$reducer_pid" "$projector_pid" 2>/dev/null || true; exit 1; }
	kill "$reducer_pid" "$projector_pid" 2>/dev/null || true
	wait "$reducer_pid" 2>/dev/null || true
	wait "$projector_pid" 2>/dev/null || true

	pass=$((pass + 1))
done

log "demo corpus orchestrator PASS: corpus converged"
