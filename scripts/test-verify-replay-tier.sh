#!/usr/bin/env bash
# Static contract test for verify-replay-tier.sh. The live verifier owns one
# shared NornicDB, so separate package test binaries must not mutate it in
# parallel.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-replay-tier.sh"
workflow="${repo_root}/.github/workflows/verify-replay-tier.yml"
ci_gates="${repo_root}/specs/ci-gates.v1.yaml"
prepr="${repo_root}/scripts/dev/pre-pr.sh"

fail() { printf 'test-verify-replay-tier: %s\n' "$*" >&2; exit 1; }

has_serialized_package_command() {
	rg --quiet \
		'^[[:space:]]*go test -p=1 \./internal/replay/offlinetier/ \./internal/reducer/ \\$' \
		"$1"
}

# has_blast_radius_command checks the gate also runs the sql_table blast-radius
# branch proof (#5409). Those two tests skip unless ESHU_REPLAY_TIER_LIVE=1, and
# until #6182 the only thing setting it was this gate, whose package list and
# -run allowlist named neither. A skipped test is indistinguishable from a
# passing one in any summary a reviewer reads.
has_blast_radius_command() {
	rg --quiet '^[[:space:]]*go test -p=1 \./internal/query/ \\$' "$1" &&
		rg --quiet "^[[:space:]]*-run 'TestSQLTableBlastRadiusEveryBranchContributesLive\|TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive' \\\\$" "$1"
}

# has_blast_radius_nonvacuity_guard checks the gate asserts both tests actually
# RAN. `go test -run` exits 0 when its regex matches nothing, so renaming a test
# turns the proof into a no-op that reports success. That is #5974's shape,
# where an assertion which never fired read as green for months -- and it failed
# there because it called a binary the runner did not have, so the guard must
# also refuse to run without rg rather than treat a missing tool as no-match.
#
# Every pattern below is anchored at line start through tab indentation only, so
# a `# ` prefix breaks it. An unanchored whole-file search would keep passing
# after the assertion was commented out, which is the shape that has let
# load-bearing lines be deleted while their mirror stayed green.
has_blast_radius_nonvacuity_guard() {
	local name
	for name in TestSQLTableBlastRadiusEveryBranchContributesLive \
		TestSQLTableBlastRadiusMatchesNothingForUnknownTableLive; do
		rg --quiet "^\t${name}[; \\\\]" "$1" || return 1
	done
	rg --quiet '^\trg --quiet "\^--- PASS: \$\{required_test\} "' "$1" || return 1
	rg --quiet '^command -v rg >/dev/null 2>&1 \|\| die' "$1"
}

# has_graph_endpoint_pins checks the gate pins every name of the graph endpoint
# to its OWN container, not merely that an export line exists.
#
# Two rounds of #6201 review closed holes exactly here: the database first, then
# the URI mirror of it. Both were the same shape — one name of a pair pinned,
# the other left free for an ambient value from a developer shell to win, which
# CI never sees because a clean runner leaves both unset. Nothing in this mirror
# guarded the pins, so a third round could have deleted one and stayed green.
#
# The value is asserted, not just the assignment. `export ESHU_NEO4J_URI="$OTHER"`
# would satisfy an existence check while reopening the exact hole, and a guard
# that passes for the wrong reason is what this whole gate exists to stop.
has_graph_endpoint_pins() {
	local var
	for var in ESHU_NEO4J_URI NEO4J_URI; do
		rg --quiet "^export ${var}=\"bolt://localhost:\\\$\\{BOLT_PORT\\}\"$" "$1" || return 1
	done
	for var in ESHU_NEO4J_DATABASE NEO4J_DATABASE; do
		rg --quiet "^export ${var}=\"nornic\"$" "$1" || return 1
	done
}

has_workflow_wiring() {
	local install_line test_line
	install_line="$(rg --line-number --no-heading \
		'^[[:space:]]*run: scripts/ci/install-apt-packages\.sh ripgrep$' "$1" | cut -d: -f1)"
	test_line="$(rg --line-number --no-heading \
		'^[[:space:]]*run: bash scripts/test-verify-replay-tier\.sh$' "$1" | cut -d: -f1)"
	[[ -n "${install_line}" && -n "${test_line}" && "${install_line}" -lt "${test_line}" ]] &&
		rg --quiet "^[[:space:]]*- 'go/internal/query/\\*\\*'$" "$1" &&
		rg --quiet "^[[:space:]]*- 'scripts/test-verify-replay-tier\\.sh'$" "$1" &&
		rg --quiet "^[[:space:]]*- 'scripts/dev/pre-pr\\.sh'$" "$1" &&
		rg --quiet "^[[:space:]]*- 'scripts/ci/install-apt-packages\\.sh'$" "$1"
}

# replay_registry_block prints only the replay-tier gate's rows. Twelve other
# gates in the same registry also trigger on go/internal/query/**, so a
# whole-file search for that line reports success whether or not THIS gate
# carries it -- verified by deleting the trigger and watching an unscoped check
# stay green.
replay_registry_block() {
	awk '/^  - id: / { inside = ($0 == "  - id: replay-tier") } inside { print }' "$1"
}

has_registry_wiring() {
	replay_registry_block "$1" |
		rg --quiet '^[[:space:]]*- "go/internal/query/\*\*"$' &&
		rg --quiet '^[[:space:]]*- "scripts/test-verify-replay-tier\.sh"$' "$1" &&
		rg --quiet '^[[:space:]]*- "scripts/dev/pre-pr\.sh"$' "$1" &&
		rg --quiet '^[[:space:]]*- "scripts/ci/install-apt-packages\.sh"$' "$1" &&
		rg --quiet '^[[:space:]]*test_command: "bash scripts/test-verify-replay-tier\.sh"$' "$1"
}

replay_prepr_trigger() {
	sed -n '/^[[:space:]]*run_or_defer replay-tier \\$/ { n; p; q; }' "$1"
}

has_prepr_selector_parity() {
	local trigger
	trigger="$(replay_prepr_trigger "$1")"
	rg --quiet "^[[:space:]]+'\\^\\(.*\\)'[[:space:]]+\\\\$" <<<"${trigger}" || return 1
	for required_path in \
		'go/cmd/(ingester|projector)/' \
		'go/internal/(query|replay|reducer|storage/cypher|storage/nornicdb|projector|graph|runtime)/' \
		'testdata/cassettes/(replayoffline|replaydelta)/' \
		'scripts/(verify-replay-tier|test-verify-replay-tier)\.sh' \
		'scripts/dev/pre-pr\.sh' \
		'scripts/ci/install-apt-packages\.sh' \
		'\.github/workflows/verify-replay-tier\.yml'; do
		rg --fixed-strings --quiet "${required_path}" <<<"${trigger}" || return 1
	done
}

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-replay-tier.sh must be executable"
bash -n "${script}" || fail "verify-replay-tier.sh has a syntax error"
command -v rg >/dev/null 2>&1 || fail "missing required tool: rg"
[[ "$(wc -l <"${script}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "verify-replay-tier.sh must stay under 500 lines"

has_serialized_package_command "${script}" \
	|| fail "shared-graph package test binaries must run sequentially with go test -p=1"
has_blast_radius_command "${script}" \
	|| fail "gate must run the sql_table blast-radius branch proof (#5409), not only the replay tier (#6182)"
has_blast_radius_nonvacuity_guard "${script}" \
	|| fail "gate must assert both blast-radius tests RAN; go test -run exits 0 on a regex matching nothing"
has_graph_endpoint_pins "${script}" \
	|| fail "gate must pin every graph-endpoint name to its own container; an unpinned name lets an ambient developer value win (#6201)"

[[ -f "${workflow}" ]] || fail "missing ${workflow}"
has_workflow_wiring "${workflow}" \
	|| fail "workflow must actively run and trigger on this contract test"
[[ -f "${ci_gates}" ]] || fail "missing ${ci_gates}"
has_registry_wiring "${ci_gates}" \
	|| fail "CI gate registry must actively trigger on and run this contract test"

[[ -f "${prepr}" ]] || fail "missing ${prepr}"
has_prepr_selector_parity "${prepr}" \
	|| fail "active pre-pr replay selector does not mirror the workflow and registry"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
sed 's/^[[:space:]]*go test -p=1 /# go test -p=1 /' "${script}" >"${tmp}/script"
if has_serialized_package_command "${tmp}/script"; then
	fail "commented-out package serialization must not satisfy the guard"
fi
if has_blast_radius_command "${tmp}/script"; then
	fail "commented-out blast-radius invocation must not satisfy the guard"
fi
sed '/rg --quiet "\^--- PASS: \${required_test} "/s/^/# /' "${script}" >"${tmp}/script-vacuous"
if has_blast_radius_nonvacuity_guard "${tmp}/script-vacuous"; then
	fail "commented-out non-vacuity assertion must not satisfy the guard"
fi
sed '/^command -v rg >\/dev\/null 2>&1 || die/s/^/# /' "${script}" >"${tmp}/script-no-rg"
if has_blast_radius_nonvacuity_guard "${tmp}/script-no-rg"; then
	fail "gate must refuse to run without rg rather than read a missing tool as no-match (#5974)"
fi
# One negation per pinned name: deleting any single export must fail, which is
# the drift that produced both #6201 findings.
for pinned_var in ESHU_NEO4J_URI NEO4J_URI ESHU_NEO4J_DATABASE NEO4J_DATABASE; do
	sed "/^export ${pinned_var}=/d" "${script}" >"${tmp}/script-no-${pinned_var}"
	if has_graph_endpoint_pins "${tmp}/script-no-${pinned_var}"; then
		fail "a dropped ${pinned_var} pin must not satisfy the guard"
	fi
done
# Repointing a pin away from this gate's own container must fail too: an
# existence-only check would pass here and leave the hole open.
sed 's|^export ESHU_NEO4J_URI=.*|export ESHU_NEO4J_URI="bolt://elsewhere:7687"|' \
	"${script}" >"${tmp}/script-repointed"
if has_graph_endpoint_pins "${tmp}/script-repointed"; then
	fail "a pin repointed away from this gate's container must not satisfy the guard"
fi
sed -e "/^[[:space:]]*- 'scripts\\/test-verify-replay-tier\\.sh'$/s/^/# /" \
	-e '/^[[:space:]]*run: bash scripts\/test-verify-replay-tier\.sh$/s/^/# /' \
	"${workflow}" >"${tmp}/workflow"
if has_workflow_wiring "${tmp}/workflow"; then
	fail "commented-out workflow wiring must not satisfy the guard"
fi
sed '/^[[:space:]]*run: scripts\/ci\/install-apt-packages\.sh ripgrep$/s/^/# /' \
	"${workflow}" >"${tmp}/workflow-no-rg"
if has_workflow_wiring "${tmp}/workflow-no-rg"; then
	fail "workflow must install ripgrep before running the contract test"
fi
sed "/^[[:space:]]*- 'scripts\\/ci\\/install-apt-packages\\.sh'$/s/^/# /" \
	"${workflow}" >"${tmp}/workflow-no-installer-trigger"
if has_workflow_wiring "${tmp}/workflow-no-installer-trigger"; then
	fail "workflow must trigger on its shared installer"
fi
sed -e '/^[[:space:]]*- "scripts\/test-verify-replay-tier\.sh"$/s/^/# /' \
	-e '/^[[:space:]]*test_command: "bash scripts\/test-verify-replay-tier\.sh"$/s/^/# /' \
	"${ci_gates}" >"${tmp}/ci-gates"
if has_registry_wiring "${tmp}/ci-gates"; then
	fail "commented-out registry wiring must not satisfy the guard"
fi
sed '/^[[:space:]]*- "scripts\/ci\/install-apt-packages\.sh"$/s/^/# /' \
	"${ci_gates}" >"${tmp}/ci-gates-no-installer-trigger"
if has_registry_wiring "${tmp}/ci-gates-no-installer-trigger"; then
	fail "CI gate registry must trigger on the shared installer"
fi
sed '/^[[:space:]]*run_or_defer replay-tier \\$/s/^/# /' \
	"${prepr}" >"${tmp}/pre-pr"
if has_prepr_selector_parity "${tmp}/pre-pr"; then
	fail "commented-out pre-pr selector must not satisfy the guard"
fi

printf 'test-verify-replay-tier: PASS\n'
