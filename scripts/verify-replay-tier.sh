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
NORNICDB_IMAGE="timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74"
CONTAINER_NAME="eshu-replay-tier-nornicdb-$$"
HTTP_PORT="${ESHU_REPLAY_TIER_HTTP_PORT:-7474}"
BOLT_PORT="${ESHU_REPLAY_TIER_BOLT_PORT:-7687}"

log() { printf '[verify-replay-tier] %s\n' "$*"; }
die() { printf '[verify-replay-tier] ERROR: %s\n' "$*" >&2; exit 1; }

BLAST_LOG="${TMPDIR:-/tmp}/eshu-replay-tier-blast-$$.log"
TIER_LOG="${TMPDIR:-/tmp}/eshu-replay-tier-main-$$.log"

cleanup() {
	# Always tear the container down, on every exit path.
	docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
	rm -f "${BLAST_LOG}" "${TIER_LOG}"
}
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || die "docker is required"
# rg backs the non-vacuity check at the end of this script. #5974 spent months
# green because an assertion called a binary the runner did not have, and
# "command not found" looked the same as "the pattern did not match". Fail here
# instead.
command -v rg >/dev/null 2>&1 || die "rg is required for the blast-radius non-vacuity check"

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
# Both names of both variables are pinned, deliberately. The graph endpoint has
# a canonical ESHU_-prefixed name and a bare alias
# (go/internal/envregistry/entries.go), consumers disagree about which they
# read, and this container is the only endpoint any of them may reach. Pinning
# one name and leaving the other free lets an ambient value from a developer
# shell win: the test then runs against a stale endpoint or a database the tier
# never asserted against, inside this same gate run. CI never sees it because a
# clean runner leaves both unset, so it fails only on the machine of whoever
# has the variable set. Both holes were found on #6201 review, one round apart —
# the database first, then this mirror of it on the URI.
export ESHU_NEO4J_DATABASE="nornic"
export NEO4J_DATABASE="nornic"
export ESHU_NEO4J_URI="bolt://localhost:${BOLT_PORT}"
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
# set -e is on, and a failing ( ... ) exits the shell at once -- so without this
# guard `tier_status=$?` below was unreachable and the die message under it had
# never printed. The gate still failed, with the go test status and none of the
# wall-clock or diagnostic output. Guarded the same way as the blast-radius
# invocation at the end of this file, so the two blocks cannot drift.
set +e
(
	cd go
	# Both packages mutate the same live graph, so package test binaries must run
	# sequentially. Test-level parallelism remains available within each binary.
	go test -p=1 ./internal/replay/offlinetier/ ./internal/reducer/ \
		-run 'TestOfflineReplayTierGraphTruth|TestDeltaTombstone|TestDeltaEntityRetractGraphTruth|TestEntityRetractManifestBinding|TestDeltaSurvivorScopedRetractGraphTruth|TestDeltaEdgeRetractGraphTruth|TestDeltaFileRetractGraphTruth|TestReducerCodeCallEdgeRetractGraphTruth|TestReducerInheritanceEdgeRetractGraphTruth|TestReducerSQLRelationshipRetractGraphTruth|TestReducerRationaleEdgeRetractGraphTruth|TestReducerMetaclassEdgeRetractGraphTruth|TestReducerRepoDependencyEdgeRetractGraphTruth|TestReducerRuntimeEdgeRetractGraphTruth|TestReducerContentEdgeRetractGraphTruth|TestCodeInterprocTaintEdgeRetractGraphTruth|TestReducerCloudEdgeRetractGraphTruth|TestReducerSecurityGroupReachabilityEdgeRetractGraphTruth|TestReducerCanonicalGovernanceEdgeRetractGraphTruth|TestReducerWorkloadUsesEdgeRetractGraphTruth|TestReducerIAMEdgeRetractGraphTruth|TestReducerAWSCloudImageEdgeRetractGraphTruth|TestReducerSecretsIAMEdgeRetractGraphTruth|TestReducerSemanticVariableRetractGraphTruth|TestReducerKubernetesNamespaceEnvironmentRetractGraphTruth|TestReducerKubernetesNamespaceAbsentNodeRetractGraphTruth|TestReducerProvenanceReplayTombstoneGraphTruth|TestNornicDBFunctionProjectionEvaluatesAfterOptionalMatch|TestNornicDBChainedOptionalMatchPreservesExecutorBoundary' -count=1 -v
) >"${TIER_LOG}" 2>&1
tier_status=$?
set -e
tier_end="$(date +%s)"
tier_elapsed=$(( tier_end - tier_start ))
cat "${TIER_LOG}"

log "offline replay tier wall-clock: ${tier_elapsed}s (start=${tier_start} end=${tier_end})"
[[ ${tier_status} -eq 0 ]] || die "offline replay tier test failed (status ${tier_status})"
for projection_test in \
	TestNornicDBFunctionProjectionEvaluatesAfterOptionalMatch \
	TestNornicDBChainedOptionalMatchPreservesExecutorBoundary; do
	rg --quiet "^--- PASS: ${projection_test} " "${TIER_LOG}" \
		|| die "${projection_test} did not run: no '--- PASS: ${projection_test}' line, so -run matched nothing or the test skipped. A skip is not a pass."
done
rg --quiet "^--- PASS: TestReducerProvenanceReplayTombstoneGraphTruth " "${TIER_LOG}" \
	|| die "TestReducerProvenanceReplayTombstoneGraphTruth did not run: no exact PASS line, so the #6258 selector matched nothing or the test skipped. A skip is not a pass."
log "offline replay tier PASSED against real NornicDB"

# The sql_table blast-radius branch proof (#5409) lives in internal/query and
# skips unless ESHU_REPLAY_TIER_LIVE=1. This script is the only thing in the
# repo that sets that variable, and until #6182 internal/query was not in the
# package list above and neither test name was in its -run allowlist. The tests
# therefore ran nowhere automatic, and a skip reads as a pass in CI.
#
# It runs as its own invocation rather than joining the list above for two
# reasons. It seeds and deletes its own probe5409* nodes in the same shared
# graph, so it must not interleave with the exact node and edge assertions the
# tier makes. And a separate invocation attributes a failure to the branch
# proof rather than to the tier.
log "running sql_table blast-radius branch proof (#5409) against the same NornicDB"
blast_start="$(date +%s)"
set +e
(
	cd go
	go test -p=1 ./internal/query/ \
		-run 'TestSQLTableBlastRadiusEveryBranchContributesLive|TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive' \
		-count=1 -v
) >"${BLAST_LOG}" 2>&1
blast_status=$?
set -e
cat "${BLAST_LOG}"
log "sql_table blast-radius wall-clock: $(( $(date +%s) - blast_start ))s"
[[ ${blast_status} -eq 0 ]] || die "sql_table blast-radius branch proof failed (status ${blast_status})"

# go test -run exits 0 when its regex matches nothing, so renaming either test
# would turn this proof into a no-op that reports success. Require a PASS line
# per test: a skip is not a pass.
for required_test in \
	TestSQLTableBlastRadiusEveryBranchContributesLive \
	TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive; do
	rg --quiet "^--- PASS: ${required_test} " "${BLAST_LOG}" \
		|| die "${required_test} did not run: no '--- PASS: ${required_test}' line, so -run matched nothing or the test skipped. A skip is not a pass."
done
log "sql_table blast-radius branch proof PASSED (every UNION branch proven live)"
