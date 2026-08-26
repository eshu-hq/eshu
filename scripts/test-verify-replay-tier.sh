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

# has_nornicdb_v123_image_pin binds the live gate to the released multi-arch
# artifact whose tag resolves to NornicDB main d9b76ae82334. A tag-only check
# would let Docker Hub retarget the proof without a repository change.
has_nornicdb_v123_image_pin() {
	rg --quiet \
		'^NORNICDB_IMAGE="timothyswt/nornicdb-cpu-bge:v1\.2\.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74"$' \
		"$1"
}

# workflow_gate_step_block prints the workflow step that runs the live gate,
# from its `- name:` line up to the next step.
workflow_gate_step_block() {
	awk '
		/^      - name: / { inside = 0 }
		/^      - name: Run offline replay tier against real NornicDB$/ { inside = 1 }
		inside { print }
	' "$1"
}

# gate_step_is_active checks the workflow HAS the gate step, RUNS it, and lets
# it BLOCK.
#
# A whole-file search for the `run:` line is not enough, in two different ways,
# and #6205 review found them one after the other:
#
#   - `if: ${{ false }}` makes GitHub skip the step while the line is still
#     present and this mirror still green. Skip reads as pass.
#   - `continue-on-error: true` lets the step fail and the job still succeed.
#     Fail reads as pass — the same hole with the opposite trigger.
#
# Both are rejected outright rather than inspected for a "safe" value. This gate
# is unconditional and blocking; a future change to either property should
# require a deliberate edit here, not quietly stop the blast-radius proof from
# gating merges.
gate_step_is_active() {
	local block key
	block="$(workflow_gate_step_block "$1")"
	rg --quiet '^[[:space:]]*run: bash scripts/verify-replay-tier\.sh$' <<<"${block}" || return 1
	for key in 'if:' 'continue-on-error:'; do
		if rg --quiet "^[[:space:]]*${key}" <<<"${block}"; then
			return 1
		fi
	done
}

# workflow_gate_job_block prints the replay-tier job, from its key to the next
# job key at the same indent.
workflow_gate_job_block() {
	awk '
		/^  [A-Za-z0-9_-]+:$/ { inside = 0 }
		/^  replay-tier:$/ { inside = 1 }
		inside { print }
	' "$1"
}

# gate_job_is_active applies the same two rejections one level up.
#
# gate_step_is_active reads only the step, so `jobs.replay-tier.if: ${{ false }}`
# skips the install, the contract test AND the gate while every `run:` line is
# still present — the step-level guard passes and nothing runs. Its
# `continue-on-error:` twin lets the whole job fail without blocking. Found on
# #6205 review after the step-level pair was already closed, which is the point:
# the disabling vector moved up a level and the guard did not follow.
gate_job_is_active() {
	local job key
	job="$(workflow_gate_job_block "$1")"
	rg --quiet '^    name: Offline replay tier vs real NornicDB$' <<<"${job}" || return 1
	for key in 'if:' 'continue-on-error:'; do
		if rg --quiet "^    ${key}" <<<"${job}"; then
			return 1
		fi
	done
}

has_workflow_wiring() {
	local install_line test_line
	install_line="$(rg --line-number --no-heading \
		'^[[:space:]]*run: scripts/ci/install-apt-packages\.sh ripgrep$' "$1" | cut -d: -f1)"
	test_line="$(rg --line-number --no-heading \
		'^[[:space:]]*run: bash scripts/test-verify-replay-tier\.sh$' "$1" | cut -d: -f1)"
	gate_step_is_active "$1" || return 1
	gate_job_is_active "$1" || return 1
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
has_nornicdb_v123_image_pin "${script}" \
	|| fail "gate must pin the exact published NornicDB v1.2.3 multi-arch digest (#6262)"

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
# A commented pin or a tag with no digest must fail. These mutations keep the
# image name visible, which defeats a whole-file substring check.
sed '/^NORNICDB_IMAGE=/s/^/# /' "${script}" >"${tmp}/script-no-image-pin"
if has_nornicdb_v123_image_pin "${tmp}/script-no-image-pin"; then
	fail "a commented NornicDB image pin must not satisfy the guard"
fi
sed 's/@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74//' \
	"${script}" >"${tmp}/script-tag-only-image"
if has_nornicdb_v123_image_pin "${tmp}/script-tag-only-image"; then
	fail "a tag-only NornicDB image must not satisfy the immutable digest guard"
fi
# Repointing a pin away from this gate's own container must fail too, for EVERY
# name. Deletion cases alone cannot catch the guard regressing from asserting
# the value to asserting only the assignment: a weakened `^export ${var}=`
# check still fails every deletion above while letting a repointed pin through.
repoint_pin() {
	case "$1" in
	*_URI) printf 'bolt://elsewhere:7687\n' ;;
	*) printf 'not-nornic\n' ;;
	esac
}
for pinned_var in ESHU_NEO4J_URI NEO4J_URI ESHU_NEO4J_DATABASE NEO4J_DATABASE; do
	sed "s|^export ${pinned_var}=.*|export ${pinned_var}=\"$(repoint_pin "${pinned_var}")\"|" \
		"${script}" >"${tmp}/script-repointed-${pinned_var}"
	if has_graph_endpoint_pins "${tmp}/script-repointed-${pinned_var}"; then
		fail "${pinned_var} repointed away from this gate's container must not satisfy the guard"
	fi
done
sed -e "/^[[:space:]]*- 'scripts\\/test-verify-replay-tier\\.sh'$/s/^/# /" \
	-e '/^[[:space:]]*run: bash scripts\/test-verify-replay-tier\.sh$/s/^/# /' \
	"${workflow}" >"${tmp}/workflow"
if has_workflow_wiring "${tmp}/workflow"; then
	fail "commented-out workflow wiring must not satisfy the guard"
fi
# The gate step gets the same standing negation every other guard here has. The
# evidence for it used to be a one-time manual mutation, which is not a guard.
sed '/^[[:space:]]*run: bash scripts\/verify-replay-tier\.sh$/s/^/# /' \
	"${workflow}" >"${tmp}/workflow-no-gate-step"
if has_workflow_wiring "${tmp}/workflow-no-gate-step"; then
	fail "workflow must run the live gate step (scripts/verify-replay-tier.sh)"
fi
# Present but skipped is the harder case: GitHub runs nothing, the run: line is
# still there, and a whole-file regex stays green (#6205, found by codex).
sed '/^[[:space:]]*run: bash scripts\/verify-replay-tier\.sh$/i\
        if: ${{ false }}
' "${workflow}" >"${tmp}/workflow-disabled-gate-step"
if has_workflow_wiring "${tmp}/workflow-disabled-gate-step"; then
	fail "a disabled (if:-gated) live gate step must not satisfy the guard"
fi
# Non-blocking is the twin: the step runs, it fails, and the job passes anyway.
sed '/^[[:space:]]*run: bash scripts\/verify-replay-tier\.sh$/i\
        continue-on-error: true
' "${workflow}" >"${tmp}/workflow-nonblocking-gate-step"
if has_workflow_wiring "${tmp}/workflow-nonblocking-gate-step"; then
	fail "a continue-on-error live gate step must not satisfy the guard"
fi
# The same two vectors one level up: disabling or de-blocking the whole job
# skips the gate while every run: line stays present.
sed '/^  replay-tier:$/a\
    if: ${{ false }}
' "${workflow}" >"${tmp}/workflow-disabled-gate-job"
if has_workflow_wiring "${tmp}/workflow-disabled-gate-job"; then
	fail "a disabled (if:-gated) replay-tier JOB must not satisfy the guard"
fi
sed '/^  replay-tier:$/a\
    continue-on-error: true
' "${workflow}" >"${tmp}/workflow-nonblocking-gate-job"
if has_workflow_wiring "${tmp}/workflow-nonblocking-gate-job"; then
	fail "a continue-on-error replay-tier JOB must not satisfy the guard"
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
