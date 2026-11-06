#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# verify-replay-tier.sh runs the R-5 offline replay gate tier (epic #4102,
# issue #4107) against a REAL single-container Neo4j started with plain
# `docker run` — NOT Docker Compose. It replays the committed cassette through
# the production canonical projection writer into the live graph and asserts
# node/edge truth over Bolt.
#
# This is deliberately lean: one Neo4j container, no Postgres, no full
# pipeline. It is the fast backend gate that catches backend-specific
# projection bugs (#4019 nested-directory drop, commit-time MERGE races,
# backend MATCH quirks) that a fake graph cannot reproduce.
#
# The tier ran against NornicDB until #10223: Neo4j is the supported backend,
# so the container, credentials, and health check below target Neo4j.
#
# The full Compose B-7 golden-corpus gate (scripts/verify-golden-corpus-gate.sh)
# is unchanged and remains the belt-and-suspenders full-corpus check.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

# Pinned Neo4j image (digest-locked for reproducibility): the same CI mirror
# the golden-corpus-gate-neo4j leg proves on, so the tier shares one declared
# engine profile with the blocking truth gate.
NEO4J_IMAGE="ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f"
CONTAINER_NAME="eshu-replay-tier-neo4j-$$"
HTTP_PORT="${ESHU_REPLAY_TIER_HTTP_PORT:-7474}"
BOLT_PORT="${ESHU_REPLAY_TIER_BOLT_PORT:-7687}"

log() { printf '[verify-replay-tier] %s\n' "$*"; }
die() { printf '[verify-replay-tier] ERROR: %s\n' "$*" >&2; exit 1; }

BLAST_LOG="${TMPDIR:-/tmp}/eshu-replay-tier-blast-$$.log"
TIER_LOG="${TMPDIR:-/tmp}/eshu-replay-tier-main-$$.log"
TIER_JSON="${TMPDIR:-/tmp}/eshu-replay-tier-main-$$.jsonl"
TIER_ERR="${TMPDIR:-/tmp}/eshu-replay-tier-main-$$.stderr"
BLAST_JSON="${TMPDIR:-/tmp}/eshu-replay-tier-blast-$$.jsonl"
BLAST_ERR="${TMPDIR:-/tmp}/eshu-replay-tier-blast-$$.stderr"

cleanup() {
	# Always tear the container down, on every exit path.
	docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
	rm -f "${BLAST_LOG}" "${TIER_LOG}" "${TIER_JSON}" "${TIER_ERR}" "${BLAST_JSON}" "${BLAST_ERR}"
}
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || die "docker is required"
# rg backs the non-vacuity check at the end of this script. #5974 spent months
# green because an assertion called a binary the runner did not have, and
# "command not found" looked the same as "the pattern did not match". Fail here
# instead.
command -v rg >/dev/null 2>&1 || die "rg is required for the blast-radius non-vacuity check"

python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" --reviewed-source
graph_password="${NEO4J_PASSWORD:-change-me}"
export ESHU_NEO4J_USERNAME="neo4j"
export NEO4J_USERNAME="neo4j"
export ESHU_NEO4J_PASSWORD="${graph_password}"
export NEO4J_PASSWORD="${graph_password}"
log "starting lean Neo4j container ${CONTAINER_NAME} (plain docker run, no compose)"
docker run -d --name "${CONTAINER_NAME}" \
	-p "${HTTP_PORT}:7474" \
	-p "${BOLT_PORT}:7687" \
	-e "NEO4J_AUTH=neo4j/${graph_password}" \
	"${NEO4J_IMAGE}" >/dev/null \
	|| die "docker run failed"

log "waiting for Neo4j health on http://localhost:${HTTP_PORT}/"
ready=false
for _ in $(seq 1 60); do
	if curl -fsS "http://localhost:${HTTP_PORT}/" >/dev/null 2>&1; then
		ready=true
		break
	fi
	# Fall back to wget if curl is unavailable.
	if command -v wget >/dev/null 2>&1 && wget --spider -q "http://localhost:${HTTP_PORT}/" >/dev/null 2>&1; then
		ready=true
		break
	fi
	sleep 2
done
[[ "${ready}" == "true" ]] || { docker logs "${CONTAINER_NAME}" 2>&1 | tail -40; die "Neo4j did not become healthy"; }
log "Neo4j healthy"

# Real-backend environment for the gated go test.
export ESHU_GRAPH_BACKEND="neo4j"
# Every name of every endpoint and credential variable is pinned, deliberately.
# The graph endpoint has a canonical ESHU_-prefixed name and a bare alias
# (go/internal/envregistry/entries.go), consumers disagree about which they
# read, and this container is the only endpoint any of them may reach. Pinning
# one name and leaving the other free lets an ambient value from a developer
# shell win: the test then runs against a stale endpoint, a database the tier
# never asserted against, or credentials the container does not know, inside
# this same gate run. CI never sees it because a clean runner leaves every
# name unset, so it fails only on the machine of whoever has the variable set.
# The endpoint holes were found on #6201 review, one round apart — the database
# first, then this mirror of it on the URI — and the credential pair rides the
# same pin for the same reason now that the backend enforces auth. The
# container auth derives from the pinned password above, so the gate never
# splits the password the server requires from the one the driver sends.
export ESHU_NEO4J_DATABASE="neo4j"
export NEO4J_DATABASE="neo4j"
export ESHU_NEO4J_URI="bolt://localhost:${BOLT_PORT}"
export NEO4J_URI="bolt://localhost:${BOLT_PORT}"
export ESHU_REPLAY_TIER_LIVE=1
# Per-worktree build cache isolation (house rule).
export GOCACHE="${repo_root}/.gocache"

python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" --reviewed-source
log "running focused offline replay tier tests (R-5 graph truth + R-17 delta/tombstone) against real Neo4j"
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
	go test -p=1 ./internal/replay/offlinetier/ ./internal/reducer/... \
		-run 'TestOfflineReplayTierGraphTruth|TestDeltaTombstone|TestDeltaEntityRetractGraphTruth|TestEntityRetractManifestBinding|TestDeltaSurvivorScopedRetractGraphTruth|TestDeltaEdgeRetractGraphTruth|TestDeltaFileRetractGraphTruth|TestReducerCodeCallEdgeRetractGraphTruth|TestReducerInheritanceEdgeRetractGraphTruth|TestReducerSQLRelationshipRetractGraphTruth|TestReducerRationaleEdgeRetractGraphTruth|TestReducerMetaclassEdgeRetractGraphTruth|TestReducerRepoDependencyEdgeRetractGraphTruth|TestReducerRuntimeEdgeRetractGraphTruth|TestReducerContentEdgeRetractGraphTruth|TestCodeInterprocTaintEdgeRetractGraphTruth|TestReducerCloudEdgeRetractGraphTruth|TestReducerSecurityGroupReachabilityEdgeRetractGraphTruth|TestReducerCanonicalGovernanceEdgeRetractGraphTruth|TestReducerWorkloadUsesEdgeRetractGraphTruth|TestReducerIAMEdgeRetractGraphTruth|TestReducerAWSCloudImageEdgeRetractGraphTruth|TestReducerSecretsIAMEdgeRetractGraphTruth|TestReducerSemanticVariableRetractGraphTruth|TestReducerSSMHostRetractGraphTruth|TestReducerKubernetesNamespaceEnvironmentRetractGraphTruth|TestReducerKubernetesNamespaceAbsentNodeRetractGraphTruth|TestReducerProvenanceReplayTombstoneGraphTruth|TestNornicDBFunctionProjectionEvaluatesAfterOptionalMatch|TestNornicDBChainedOptionalMatchEvaluatesSecondHop|TestGovScopeMaterializationEntitySourcesHaveFiles' -count=1 -v -json
) >"${TIER_JSON}" 2>"${TIER_ERR}"
tier_status=$?
set -e
tier_end="$(date +%s)"
tier_elapsed=$(( tier_end - tier_start ))
cat "${TIER_ERR}" >&2
cat "${TIER_JSON}"
python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" --events "${TIER_JSON}" --go-exit "${tier_status}" >"${TIER_LOG}" || die "replay tier event contract failed"
python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" --reviewed-source
cat "${TIER_LOG}"

log "offline replay tier wall-clock: ${tier_elapsed}s (start=${tier_start} end=${tier_end})"
[[ ${tier_status} -eq 0 ]] || die "offline replay tier test failed (status ${tier_status})"
for projection_test in \
	TestNornicDBFunctionProjectionEvaluatesAfterOptionalMatch \
	TestNornicDBChainedOptionalMatchEvaluatesSecondHop; do
	rg --quiet "^--- PASS: ${projection_test} " "${TIER_LOG}" \
		|| die "${projection_test} did not run: no '--- PASS: ${projection_test}' line, so -run matched nothing or the test skipped. A skip is not a pass."
done
rg --quiet "^--- PASS: TestReducerProvenanceReplayTombstoneGraphTruth " "${TIER_LOG}" \
	|| die "TestReducerProvenanceReplayTombstoneGraphTruth did not run: no exact PASS line, so the #6258 selector matched nothing or the test skipped. A skip is not a pass."
log "offline replay tier PASSED against real Neo4j"

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
# TestSQLTableBlastRadiusReportsUnseededBranchMissingLive is the #6204 bite
# proof. The other two prove the branches match; only this one proves the
# dead-branch DETECTION still works, by seeding eight of nine branches and
# asserting the ninth comes back named. Before it existed, a refactor that
# accumulated the missing list from the fixture table instead of the returned
# rows would have kept both other tests green and gutted the detection -- which
# is what the test #6201 renamed actually did.
log "running sql_table blast-radius branch proof (#5409) and bite proof (#6204) against the same Neo4j"
python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" --reviewed-source
blast_start="$(date +%s)"
set +e
(
	cd go
	go test -p=1 ./internal/query/ \
		-run 'TestSQLTableBlastRadiusEveryBranchContributesLive|TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive|TestSQLTableBlastRadiusReportsUnseededBranchMissingLive' \
		-count=1 -v -json
) >"${BLAST_JSON}" 2>"${BLAST_ERR}"
blast_status=$?
set -e
cat "${BLAST_ERR}" >&2
cat "${BLAST_JSON}"
python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" --events "${BLAST_JSON}" --go-exit "${blast_status}" --blast >"${BLAST_LOG}" || die "blast-radius event contract failed"
python3 scripts/lib/replay_tier_enrollment.py --root "${repo_root}" --reviewed-source
cat "${BLAST_LOG}"
log "sql_table blast-radius wall-clock: $(( $(date +%s) - blast_start ))s"
[[ ${blast_status} -eq 0 ]] || die "sql_table blast-radius branch proof failed (status ${blast_status})"

# go test -run exits 0 when its regex matches nothing, so renaming either test
# would turn this proof into a no-op that reports success. Require a PASS line
# per test: a skip is not a pass.
for required_test in \
	TestSQLTableBlastRadiusEveryBranchContributesLive \
	TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive \
	TestSQLTableBlastRadiusReportsUnseededBranchMissingLive; do
	rg --quiet "^--- PASS: ${required_test} " "${BLAST_LOG}" \
		|| die "${required_test} did not run: no '--- PASS: ${required_test}' line, so -run matched nothing or the test skipped. A skip is not a pass."
done
log "sql_table blast-radius branch proof PASSED (every UNION branch proven live)"
