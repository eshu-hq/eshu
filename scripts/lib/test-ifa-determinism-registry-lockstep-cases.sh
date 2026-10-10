#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# fail(), and the ${repo_root}/${registry}/${workflow} path variables are all
# defined by scripts/test-verify-ifa-determinism.sh before it sources this
# file; shellcheck cannot see that from this file alone.
# CI-gate registry/workflow lockstep mechanism cases for
# scripts/test-verify-ifa-determinism.sh, sourced so the top-level mirror
# stays below the repository's 500-line cap (mirroring the fault-injection
# sibling's per-mechanism case-module split, e.g.
# scripts/lib/test-ifa-fault-injection-marker-cases.sh). Proves, through the
# real ci-gates registry matcher rather than a text grep, that: every IFA
# proof-input seam the hand-maintained selector-cases table lists also
# retriggers the workflow AND is present in both the ifa-determinism and
# ifa-fault-injection registry entries (workflow superset-of-table); every
# trigger the registry actually declares also appears in the workflow, so a
# registry-only trigger can never leave a BLOCKING gate that GitHub never
# starts (registry subset-of-workflow); a fault-only input selects
# ifa-fault-injection but never ifa-determinism; and a determinism-only input
# (the mirror's own sourced case modules, classified by where they execute,
# not what they are about) selects ifa-determinism but never
# ifa-fault-injection -- guarding against over-triggering the fault gate's
# four-shard Docker matrix on an edit it cannot even observe. The self-check on
# `${BASH_SOURCE[0]}` below intentionally resolves to THIS file once sourced
# into a function (proven empirically: inside a bash function, BASH_SOURCE[0]
# is the file where the function is lexically defined, not the caller) --
# both the check and the `source` line it is checking for must stay together
# in this same file for that to keep working.
# Feed in-memory text through process substitution. The producer then lives
# outside the command's pipefail status, and Bash does not prefill a large
# here-input before starting rg (#4718/#5098).
_ifa_det_text_matches() {
	local text="$1"
	shift
	rg "$@" < <(printf '%s\n' "${text}")
}

_ifa_det_test_text_match_helper() {
	local hazard_window_tail hazard_window_text oversized_tail oversized_text
	printf -v hazard_window_tail '%*s' 32768 ''
	printf -v oversized_tail '%*s' 1048576 ''
	hazard_window_text='EARLY-MATCH'$'\n'"${hazard_window_tail}"
	oversized_text='EARLY-MATCH'$'\n'"${oversized_tail}"

	_ifa_det_text_matches 'literal[1]' --quiet --fixed-strings -- 'literal[1]' \
		|| fail "text matcher lost fixed-string semantics"
	_ifa_det_text_matches $'SELECTED\tifa-determinism reason' --quiet -- '^SELECTED[[:space:]]+ifa-determinism[[:space:]]' \
		|| fail "text matcher lost regex semantics"
	_ifa_det_text_matches $'prefix\n      - "trigger"\nsuffix' --quiet --fixed-strings --line-regexp -- '      - "trigger"' \
		|| fail "text matcher lost whole-line semantics"
	if _ifa_det_text_matches 'present' --quiet --fixed-strings -- 'absent'; then
		fail "text matcher reports a match for absent text"
	fi
	_ifa_det_text_matches "${hazard_window_text}" --quiet --fixed-strings -- 'EARLY-MATCH' \
		|| fail "text matcher fails inside the Bash large-here-input hazard window"
	_ifa_det_text_matches "${oversized_text}" --quiet --fixed-strings -- 'EARLY-MATCH' \
		|| fail "text matcher fails when an oversized input matches before the pipe-capacity boundary"
}

run_ifa_determinism_registry_lockstep_cases() {
_ifa_det_test_text_match_helper
determinism_registry="$(sed -n '/^  - id: ifa-determinism$/,/^  - id:/p' "${registry}")"
fault_registry="$(sed -n '/^  - id: ifa-fault-injection$/,/^  - id:/p' "${registry}")"
dead_letter_registry="$(sed -n '/^  - id: ifa-dead-letter-matrix$/,/^  - id:/p' "${registry}")"
static_mirror_registry="$(sed -n '/^  - id: ifa-static-mirror$/,/^  - id:/p' "${registry}")"
selector_cases_lib="${repo_root}/scripts/lib/ifa_live_gate_selector_cases.sh"
rg --quiet --fixed-strings --line-regexp -- 'source "${selector_cases_lib}"' "${BASH_SOURCE[0]}" \
	|| fail "selector cases must be sourced from scripts/lib/ifa_live_gate_selector_cases.sh"
# shellcheck source=scripts/lib/ifa_live_gate_selector_cases.sh
source "${selector_cases_lib}"
for seam in "${ifa_live_gate_common_seams[@]}"; do
	trigger="${seam%%|*}"
	concrete_path="${seam#*|}"
	_ifa_det_text_matches "${determinism_registry}" --fixed-strings --quiet -- "- \"${trigger}\"" \
		|| fail "ifa-determinism registry entry omits IFA proof input: ${trigger}"
	_ifa_det_text_matches "${fault_registry}" --fixed-strings --quiet -- "- \"${trigger}\"" \
		|| fail "ifa-fault-injection registry entry omits IFA proof input: ${trigger}"
	selection="$(printf '%s\n' "${concrete_path}" | (
		cd "${repo_root}/go" || exit
		go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain
	))"
	for gate in ifa-determinism ifa-fault-injection; do
		_ifa_det_text_matches "${selection}" --quiet -- "^SELECTED[[:space:]]+${gate}[[:space:]]" \
			|| fail "${concrete_path} does not select ${gate} through the real registry matcher"
	done
done

# A registry-only trigger makes a gate's job unstartable for a PR that touches
# only that path. The IFA-specific cigates test compares every CI-owned row in
# this workflow, including advisory static mirror, against parsed paths, so
# block- and flow-style YAML enforce the same contract. Its deletion fixture
# proves that a missing path is still rejected.
(
	cd "${repo_root}/go" || exit
	go test ./internal/cigates -run '^TestIfaDeterminismWorkflowPath' -count=1
) || fail "IFA registry triggers are missing from the workflow's pull_request.paths"

# The static mirror job runs the mirrors of all three live gates, and they read
# production sources as text (reducer_queue_replay.go in #7807). Its trigger
# list is therefore the union of the three rows' triggers: a path that arms any
# live Ifa gate must be in the mirror row's triggers too. The job itself runs
# whenever the workflow does (one shared paths: filter, no per-job if:), so
# today no command reads these triggers: `ci-gates select` reports a CI-only
# row on every path and `ci-gates await` reads blocking rows only. The union
# keeps a later flip to blocking sound, because await waits for a blocking row
# only on the paths its triggers match. The union is derived from the
# committed registry, never from a hand-kept list, so a trigger added to one
# row and not copied here fails this loop.
if [[ -z "${static_mirror_registry}" ]]; then
	fail "registry has no ifa-static-mirror row: the workflow's static mirror job would be an unowned job"
fi
static_mirror_triggers="$(printf '%s\n' "${static_mirror_registry}" \
	| sed -n '/^    triggers:$/,/^    [a-z_]*:$/p' \
	| rg --only-matching --replace '$1' -- '^\s+- "([^"]+)"\s*$')"
for live_block in "${determinism_registry}" "${dead_letter_registry}" "${fault_registry}"; do
	while IFS= read -r live_trigger; do
		[[ -n "${live_trigger}" ]] || continue
		_ifa_det_text_matches "${static_mirror_triggers}" --quiet --fixed-strings --line-regexp -- "${live_trigger}" \
			|| fail "ifa-static-mirror omits ${live_trigger}, which arms a live Ifa gate; if the row is made blocking, ci-gates await would not wait for the mirror on a change to it"
	done < <(printf '%s\n' "${live_block}" \
		| sed -n '/^    triggers:$/,/^    [a-z_]*:$/p' \
		| rg --only-matching --replace '$1' -- '^\s+- "([^"]+)"\s*$')
done

# Fault-only case data stays separate so the matcher proves these inputs do
# not accidentally broaden the determinism registry.
for seam in "${ifa_live_gate_fault_only_seams[@]}"; do
	trigger="${seam%%|*}"
	concrete_path="${seam#*|}"
	_ifa_det_text_matches "${fault_registry}" --quiet --fixed-strings --line-regexp -- "      - \"${trigger}\"" \
		|| fail "ifa-fault-injection registry entry omits fault-only input: ${trigger}"
	selection="$(printf '%s\n' "${concrete_path}" | (
		cd "${repo_root}/go" || exit
		go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain
	))"
	_ifa_det_text_matches "${selection}" --quiet '^SELECTED[[:space:]]+ifa-fault-injection[[:space:]]' \
		|| fail "${concrete_path} does not select ifa-fault-injection through the real registry matcher"
	if _ifa_det_text_matches "${selection}" --quiet '^SELECTED[[:space:]]+ifa-determinism[[:space:]]'; then
		fail "fault-only input must not select ifa-determinism: ${concrete_path}"
	fi
done

# Determinism-only case data is the mirror image of the fault-only loop
# above: these inputs (the mirror's own sourced case modules, classified by
# where they EXECUTE -- inside test-verify-ifa-determinism.sh -- not by what
# their content is about) must retrigger ifa-determinism but never
# ifa-fault-injection. Without this, a determinism-only test module could
# silently broaden the fault registry, costing every future family an
# unexercised fault-injection run on every edit. Scope that honestly: this
# constrains REGISTRY SELECTION (what `ci-gates select` returns, and therefore
# what `make pre-pr` runs locally). It does not by itself stop CI starting the
# fault shards -- ifa-determinism-gate.yml has one workflow-level on.paths and
# no per-job filter, so a determinism-only edit still starts all four.
for seam in "${ifa_live_gate_determinism_only_seams[@]}"; do
	trigger="${seam%%|*}"
	concrete_path="${seam#*|}"
	_ifa_det_text_matches "${determinism_registry}" --quiet --fixed-strings --line-regexp -- "      - \"${trigger}\"" \
		|| fail "ifa-determinism registry entry omits determinism-only input: ${trigger}"
	selection="$(printf '%s\n' "${concrete_path}" | (
		cd "${repo_root}/go" || exit
		go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain
	))"
	_ifa_det_text_matches "${selection}" --quiet '^SELECTED[[:space:]]+ifa-determinism[[:space:]]' \
		|| fail "${concrete_path} does not select ifa-determinism through the real registry matcher"
	if _ifa_det_text_matches "${selection}" --quiet '^SELECTED[[:space:]]+ifa-fault-injection[[:space:]]'; then
		fail "determinism-only input must not select ifa-fault-injection: ${concrete_path}"
	fi
done

# Negative controls (#6200). Every loop above asks whether a path STILL selects
# the gate it should, and none of them can catch the opposite failure: a
# trigger widened past what the gates actually observe. That went from
# theoretical to live when ~40 reducer filenames and six SDK entries were
# replaced by package globs -- 'go/internal/reducer/**' one keystroke from
# 'go/internal/**', and each of the two gates a Docker matrix. Over-triggering
# fails no assertion anywhere; it just spends CI.
#
# So: named unrelated paths that must select NEITHER live gate. The existence
# check is load-bearing, not defensive tidiness -- a renamed or deleted file
# would leave a control that passes because it tests nothing, which is exactly
# the false-green this issue exists to remove.
for negative_path in "${ifa_live_gate_negative_seams[@]}"; do
	[[ -e "${repo_root}/${negative_path}" ]] \
		|| fail "negative control names a path that no longer exists, so it proves nothing: ${negative_path}"
	selection="$(printf '%s\n' "${negative_path}" | (
		cd "${repo_root}/go" || exit
		go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain
	))"
	for gate in ifa-determinism ifa-fault-injection; do
		if printf '%s\n' "${selection}" | rg --quiet -- "^SELECTED[[:space:]]+${gate}[[:space:]]"; then
			fail "unrelated path must not arm the live ${gate} matrix: ${negative_path}"
		fi
	done
done

# Per-gate negative controls. The loop above covers paths that must select no
# live gate at all; this one covers a path that legitimately selects one gate
# and must not have been widened onto another. ifa-dead-letter-matrix is the
# only Ifá gate no loop in this file otherwise names, which is how it carried
# an over-wide scripts/lib glob through a full review round unchallenged.
#
# Each seam is path|required|forbidden, and the required half carries as much
# weight as the forbidden one. A negative-only control passes just as happily
# when the path stopped selecting anything at all -- and "asserted it still
# selects SOMETHING" would not have caught that either, because no-diff-
# fragments and no-ai-attribution trigger on "**" and so match every path in
# the repository. The gate has to be named.
for negative_gate_seam in "${ifa_live_gate_negative_gate_seams[@]}"; do
	IFS='|' read -r negative_path required_gate forbidden_gate <<<"${negative_gate_seam}"
	[[ -n "${negative_path}" && -n "${required_gate}" && -n "${forbidden_gate}" ]] \
		|| fail "per-gate negative control is not path|required|forbidden: ${negative_gate_seam}"
	[[ -e "${repo_root}/${negative_path}" ]] \
		|| fail "per-gate negative control names a path that no longer exists, so it proves nothing: ${negative_path}"
	selection="$(printf '%s\n' "${negative_path}" | (
		cd "${repo_root}/go" || exit
		go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain
	))"
	_ifa_det_text_matches "${selection}" --quiet -- "^SELECTED[[:space:]]+${required_gate}[[:space:]]" \
		|| fail "per-gate negative control no longer selects ${required_gate}, so its forbidden half proves nothing: ${negative_path}"
	if _ifa_det_text_matches "${selection}" --quiet -- "^SELECTED[[:space:]]+${forbidden_gate}[[:space:]]"; then
		fail "${negative_path} must not arm ${forbidden_gate}: that gate cannot observe the file"
	fi
done
}
