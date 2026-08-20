#!/usr/bin/env bash
# shellcheck shell=bash
# Committed materialized-edge family fixtures shared by the two Ifá live gates
# (scripts/verify-ifa-determinism.sh and scripts/verify-ifa-fault-injection.sh).
#
# Both gates drive the SAME committed family cassettes and assert against the
# SAME expected-set files, and both had byte-identical copies of these paths
# and of the fail-fast existence guards below. Two sibling families landing at
# once (#5993 deployable_unit_edges and #5998 rationale_edges) pushed both
# copies past the repo's 500-line file cap, which is what surfaced the
# duplication. One definition means a new family is registered once and cannot
# drift between the determinism gate and the fault-injection gate.
#
# Sourcing contract: source this AFTER `repo_root` is set. Every path below is
# resolved at source time, exactly as the inline blocks did.

# SQL relationship family cassette (#5351): a committed cassette (unlike the
# generated synth cassette) that exercises the reducer's SQL relationship edge
# materialization across all nine materialized edge types (QUERIES_TABLE,
# READS_FROM, HAS_COLUMN, TRIGGERS, EXECUTES, INDEXES, MIGRATES,
# REFERENCES_TABLE, WRITES_TO). It is driven into every cell of both gates
# alongside the demo-org + synth-multiscope cassettes, and the
# materialized_edges:sql_relationships manifest row's proof_gate claim is
# backed by an ADDITIONAL per-cell absolute-set assertion (the `ifa
# assert-edges` call after each drain): digest equality across N, or against a
# fault-free baseline, cannot catch a family silently empty in ALL cells; the
# absolute expected set can.
sql_cassette="${repo_root}/testdata/cassettes/sqlrelationships/ifa-sql-family.json"
sql_expected_edges="${repo_root}/go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
sql_delta_cassette="${repo_root}/testdata/cassettes/sqlrelationships/ifa-sql-family-delta.json"
sql_delta_expected_edges="${repo_root}/go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-delta-live-expected-edges.json"

# code_call_edges family cassette (#5991): the CALLS edge family, exact-set
# asserted at five edges by both gates.
code_call_cassette="${repo_root}/testdata/cassettes/codecalls/ifa-code-call-family.json"
code_call_expected_edges="${repo_root}/go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"

# rationale_edges family cassette (#5998): the EXPLAINS edge family. Driven
# into every cell of both gates; the delta cassette carries the generation-2
# retract that must converge to exact-one EXPLAINS plus the exact surviving
# Charge node, asserted against the full expected-record set (not a digest).
rationale_cassette="${repo_root}/testdata/cassettes/rationale/ifa-rationale-family.json"
rationale_expected_edges="${repo_root}/go/internal/ifa/testdata/rationale/ifa-rationale-family-expected-edges.json"
rationale_delta_cassette="${repo_root}/testdata/cassettes/rationale/ifa-rationale-family-delta.json"
rationale_delta_expected_records="${repo_root}/go/internal/ifa/testdata/rationale/ifa-rationale-family-delta-live-expected-records.json"

# documentation_edges family cassette (#5994): a committed cassette exercising
# the reducer's DOCUMENTS edge materialization, including the SqlTable-target
# case (batchCanonicalDocumentationEntityEdgeCypher's MATCH label alternation).
# Driven into every cell alongside the SQL + code-call families; on the
# fault-injection gate cell_killworker_documentation and
# cell_failgraphwrite_documentation back its manifest row's proof_gate claim.
documentation_cassette="${repo_root}/testdata/cassettes/documentation/ifa-documentation-family.json"
documentation_expected_edges="${repo_root}/go/internal/ifa/testdata/documentation/ifa-documentation-family-live-expected-edges.json"

# deployable_unit_edges family cassette (#5993). On the determinism gate this
# is driven in its OWN standalone cell after the N-loop, not into the shared
# N={1,2,4} cells -- see scripts/lib/ifa_deployable_unit_live.sh's header for
# why (a bootstrap-index maintenance pass is required for this family to
# materialize anything, and folding that into the shared loop would move every
# other family's digest terminal for a reason unrelated to what that loop
# tests). On the fault-injection gate it is driven only by the three
# deployable_unit-targeted cells, never by drive_all_cassettes.
deployable_unit_cassette="${repo_root}/testdata/cassettes/deployableunit/ifa-deployable-unit-family.json"
deployable_unit_expected_edges="${repo_root}/go/internal/ifa/testdata/deployableunit/ifa-deployable-unit-family-expected-edges.json"

# codeowners_ownership_edges family cassette (#5992): the DECLARES_CODEOWNER
# edge family, exact-set asserted at five edges by both gates. Driven uniformly
# into every N-cell on the determinism gate (a plain reducer family needing no
# maintenance pass), and only by the three codeowners-targeted cells on the
# fault-injection gate -- never by drive_all_cassettes, per that driver's rule
# that a new family cassette must not join the shared drive.
codeowners_cassette="${repo_root}/testdata/cassettes/codeowners/ifa-codeowners-family.json"
codeowners_expected_edges="${repo_root}/go/internal/ifa/testdata/codeowners/ifa-codeowners-family-expected-edges.json"

# repo_dependency is resolver-owned and requires a fixture-only Platform
# prerequisite after its first, deliberately gated drain.
repo_dependency_cassette="${repo_root}/testdata/cassettes/repodependency/ifa-repo-dependency-family.json"
repo_dependency_expected_edges="${repo_root}/go/internal/ifa/testdata/repodependency/ifa-repo-dependency-family-expected-edges.json"

# ifa_family_fixtures_require fails fast, before any Compose stack is started,
# when a committed fixture is missing. Each message names the specific fixture
# so a missing file is identifiable from the failure line alone; "$1" is the
# calling gate's name so the prefix matches the gate that aborted.
ifa_family_fixtures_require() {
	local gate="$1"
	[[ -f "${sql_cassette}" ]] || { echo "${gate}: SQL cassette not found: ${sql_cassette}" >&2; exit 1; }
	[[ -f "${sql_expected_edges}" ]] || { echo "${gate}: SQL expected-edge set not found: ${sql_expected_edges}" >&2; exit 1; }
	[[ -f "${sql_delta_cassette}" ]] || { echo "${gate}: SQL delta cassette not found: ${sql_delta_cassette}" >&2; exit 1; }
	[[ -f "${sql_delta_expected_edges}" ]] || { echo "${gate}: SQL delta expected-edge set not found: ${sql_delta_expected_edges}" >&2; exit 1; }
	[[ -f "${code_call_cassette}" ]] || { echo "${gate}: code-call cassette not found: ${code_call_cassette}" >&2; exit 1; }
	[[ -f "${code_call_expected_edges}" ]] || { echo "${gate}: code-call expected-edge set not found: ${code_call_expected_edges}" >&2; exit 1; }
	[[ -f "${documentation_cassette}" ]] || { echo "${gate}: documentation cassette not found: ${documentation_cassette}" >&2; exit 1; }
	[[ -f "${documentation_expected_edges}" ]] || { echo "${gate}: documentation expected-edge set not found: ${documentation_expected_edges}" >&2; exit 1; }
	[[ -f "${deployable_unit_cassette}" ]] || { echo "${gate}: deployable-unit cassette not found: ${deployable_unit_cassette}" >&2; exit 1; }
	[[ -f "${deployable_unit_expected_edges}" ]] || { echo "${gate}: deployable-unit expected-edge set not found: ${deployable_unit_expected_edges}" >&2; exit 1; }
	[[ -f "${rationale_cassette}" ]] || { echo "${gate}: rationale cassette not found: ${rationale_cassette}" >&2; exit 1; }
	[[ -f "${rationale_expected_edges}" ]] || { echo "${gate}: rationale expected-edge set not found: ${rationale_expected_edges}" >&2; exit 1; }
	[[ -f "${rationale_delta_cassette}" ]] || { echo "${gate}: rationale delta cassette not found: ${rationale_delta_cassette}" >&2; exit 1; }
	[[ -f "${rationale_delta_expected_records}" ]] || { echo "${gate}: rationale delta expected-record set not found: ${rationale_delta_expected_records}" >&2; exit 1; }
	[[ -f "${codeowners_cassette}" ]] || { echo "${gate}: codeowners cassette not found: ${codeowners_cassette}" >&2; exit 1; }
	[[ -f "${codeowners_expected_edges}" ]] || { echo "${gate}: codeowners expected-edge set not found: ${codeowners_expected_edges}" >&2; exit 1; }
	[[ -f "${repo_dependency_cassette}" ]] || { echo "${gate}: repo-dependency cassette not found: ${repo_dependency_cassette}" >&2; exit 1; }
	[[ -f "${repo_dependency_expected_edges}" ]] || { echo "${gate}: repo-dependency expected-edge set not found: ${repo_dependency_expected_edges}" >&2; exit 1; }
}
