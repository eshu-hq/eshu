#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# verify-replay-tier.sh runs the R-5 offline replay gate tier (epic #4102,
# issue #4107) against a REAL single-container NornicDB started with plain
# `docker run` — NOT Docker Compose. It replays the committed cassette through
# the production canonical projection writer into the live graph and asserts
# node/edge truth over Bolt.
#
# This is deliberately lean: one NornicDB container, no Postgres, no full
# pipeline. It is the fast credential-free backend gate that catches
# backend-specific projection bugs (#4019 nested-directory drop, commit-time
# MERGE races, NornicDB MATCH quirks) that a fake graph cannot reproduce.
#
# The full Compose B-7 golden-corpus gate (scripts/verify-golden-corpus-gate.sh)
# is unchanged and remains the belt-and-suspenders full-corpus check.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

# Pinned NornicDB image (digest-locked for reproducibility).
NORNICDB_IMAGE="timothyswt/nornicdb-cpu-bge:v1.1.11@sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090"
CONTAINER_NAME="eshu-replay-tier-nornicdb-$$"
HTTP_PORT="${ESHU_REPLAY_TIER_HTTP_PORT:-7474}"
BOLT_PORT="${ESHU_REPLAY_TIER_BOLT_PORT:-7687}"

log() { printf '[verify-replay-tier] %s\n' "$*"; }
die() { printf '[verify-replay-tier] ERROR: %s\n' "$*" >&2; exit 1; }

cleanup() {
	# Always tear the container down, on every exit path.
	docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || die "docker is required"

log "starting lean NornicDB container ${CONTAINER_NAME} (plain docker run, no compose)"
docker run -d --name "${CONTAINER_NAME}" \
	-p "${HTTP_PORT}:7474" \
	-p "${BOLT_PORT}:7687" \
	-e NORNICDB_NO_AUTH=true \
	-e NORNICDB_DATA_DIR=/data \
	-e NORNICDB_HTTP_PORT=7474 \
	-e NORNICDB_BOLT_PORT=7687 \
	-e NORNICDB_ASYNC_WRITES_ENABLED=false \
	-e NORNICDB_HEIMDALL_ENABLED=false \
	-e NORNICDB_EMBEDDING_ENABLED=false \
	-e NORNICDB_SEARCH_BM25_ENABLED=false \
	-e NORNICDB_SEARCH_VECTOR_ENABLED=false \
	"${NORNICDB_IMAGE}" >/dev/null \
	|| die "docker run failed"

log "waiting for NornicDB health on http://localhost:${HTTP_PORT}/health"
ready=false
for _ in $(seq 1 60); do
	if curl -fsS "http://localhost:${HTTP_PORT}/health" >/dev/null 2>&1; then
		ready=true
		break
	fi
	# Fall back to wget if curl is unavailable.
	if command -v wget >/dev/null 2>&1 && wget --spider -q "http://localhost:${HTTP_PORT}/health" >/dev/null 2>&1; then
		ready=true
		break
	fi
	sleep 2
done
[[ "${ready}" == "true" ]] || { docker logs "${CONTAINER_NAME}" 2>&1 | tail -40; die "NornicDB did not become healthy"; }
log "NornicDB healthy"

# Real-backend environment for the gated go test.
export ESHU_GRAPH_BACKEND="nornicdb"
export ESHU_NEO4J_DATABASE="nornic"
export NEO4J_URI="bolt://localhost:${BOLT_PORT}"
# NornicDB runs with NORNICDB_NO_AUTH=true, but the shared Bolt driver config
# requires non-empty username/password, so supply placeholders the backend
# ignores.
export NEO4J_USERNAME="${NEO4J_USERNAME:-neo4j}"
export NEO4J_PASSWORD="${NEO4J_PASSWORD:-change-me}"
export ESHU_REPLAY_TIER_LIVE=1
# Per-worktree build cache isolation (house rule).
export GOCACHE="${repo_root}/.gocache"

log "running focused offline replay tier tests (R-5 graph truth + R-17 delta/tombstone) against real NornicDB"
tier_start="$(date +%s)"
(
	cd go
	go test ./internal/replay/offlinetier/ \
		-run 'TestOfflineReplayTierGraphTruth|TestDeltaTombstone|TestDeltaEntityRetractGraphTruth|TestEntityRetractManifestBinding|TestDeltaSurvivorScopedRetractGraphTruth|TestDeltaEdgeRetractGraphTruth|TestDeltaFileRetractGraphTruth|TestReducerCodeCallEdgeRetractGraphTruth|TestReducerInheritanceEdgeRetractGraphTruth|TestReducerSQLRelationshipRetractGraphTruth|TestReducerRationaleEdgeRetractGraphTruth|TestReducerMetaclassEdgeRetractGraphTruth|TestReducerRepoDependencyEdgeRetractGraphTruth|TestReducerRuntimeEdgeRetractGraphTruth|TestReducerContentEdgeRetractGraphTruth|TestCodeInterprocTaintEdgeRetractGraphTruth|TestReducerCloudEdgeRetractGraphTruth|TestReducerSecurityGroupReachabilityEdgeRetractGraphTruth|TestReducerCanonicalGovernanceEdgeRetractGraphTruth|TestReducerWorkloadUsesEdgeRetractGraphTruth|TestReducerIAMEdgeRetractGraphTruth|TestReducerSecretsIAMEdgeRetractGraphTruth|TestReducerSemanticVariableRetractGraphTruth|TestReducerKubernetesNamespaceEnvironmentRetractGraphTruth' -count=1 -v
)
tier_status=$?
tier_end="$(date +%s)"
tier_elapsed=$(( tier_end - tier_start ))

log "offline replay tier wall-clock: ${tier_elapsed}s (start=${tier_start} end=${tier_end})"
[[ ${tier_status} -eq 0 ]] || die "offline replay tier test failed (status ${tier_status})"
log "offline replay tier PASSED against real NornicDB"
