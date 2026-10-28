#!/usr/bin/env bash
# Static structural test for the CI gate registry (#4213): the verify script,
# the registry YAML, and the committed specs/ci-gates.v1.yaml. Fast,
# credential-free, Docker-free, network-free.
#
# This test runs:
#   1. Structural checks on the verify script itself.
#   2. Existence and syntax checks on the registry YAML.
#   3. The real validate command against the committed registry, so every gate
#      entry's script and workflow references are proven live.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ci-gates-registry.sh"
registry="${repo_root}/specs/ci-gates.v1.yaml"
static_contract_workflow="${repo_root}/.github/workflows/static-contract-gates.yml"
build_test_workflow="${repo_root}/.github/workflows/test.yml"
frontend_workflow="${repo_root}/.github/workflows/frontend.yml"
e2e_workflow="${repo_root}/.github/workflows/e2e-tests.yml"
frontend_inventory="${repo_root}/specs/ci-pr-workflows.d/frontend.yml"
e2e_inventory="${repo_root}/specs/ci-pr-workflows.d/e2e-tests.yml"
registry_workflow="${repo_root}/.github/workflows/verify-ci-gate-registry.yml"
# Existing row-shape assertions consume a validated, temporary flat view.
registry_root="${registry}"
static_contract_workflow_root="${static_contract_workflow}"
resolved_dir="$(mktemp -d)"
trap 'rm -rf "${resolved_dir}"' EXIT
# shellcheck source=scripts/lib/ci-gates-resolved-fixtures.sh
. "${repo_root}/scripts/lib/ci-gates-resolved-fixtures.sh"
ci_gates_flat_view "${registry_root}" "${resolved_dir}/ci-gates.v1.yaml" "${repo_root}"
ci_gates_static_workflow_view "${static_contract_workflow_root}" "${resolved_dir}/static-contract-gates.yml" "${repo_root}"
registry="${resolved_dir}/ci-gates.v1.yaml"
static_contract_workflow="${resolved_dir}/static-contract-gates.yml"

fail() {
	printf 'test-verify-ci-gates-registry: %s\n' "$*" >&2
	exit 1
}

require() {
	# Matches the needle only as LIVE CODE: a commented-out occurrence does not
	# count. Without this, commenting out a pinned workflow step leaves the pin
	# green while the step stops running -- proven twice on this branch, once
	# for the ifa contract-layer append_gate and once for docsclienvrefs.
	local label="$1" needle="$2" file="$3" line stripped
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
		[[ "${stripped}" == "#"* ]] && continue
		[[ "${line}" == *"${needle}"* ]] && return 0
	done < "${file}"
	fail "missing ${label} (${needle}) in ${file} as live code (a commented-out occurrence does not count)"
}

[[ "$(wc -l <"${BASH_SOURCE[0]}" | tr -d '[:space:]')" -lt 500 ]] ||
	fail "test-verify-ci-gates-registry.sh must stay under 500 lines"

# ── 1. Verify script structural checks ─────────────────────────────────────

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-ci-gates-registry.sh must be executable"
bash -n "${script}" || fail "verify-ci-gates-registry.sh has a syntax error"

require "strict mode"    "set -euo pipefail"          "${script}"
require "validate call"  "go run ./cmd/ci-gates validate" "${script}"
require "registry arg"   "--registry"                  "${script}"
require "repo-root arg"  "--repo-root"                 "${script}"
require "drift flag"     "--drift"                     "${script}"

# ── 2. Registry YAML structural checks ─────────────────────────────────────

[[ -f "${registry}" ]] || fail "missing ${registry}"
require "schema version"    "version: v1"     "${registry}"
require "gates section"     "gates:"          "${registry}"
require "id field present"  "  - id:"         "${registry}"
require "triggers present"  "    triggers:"   "${registry}"
require "ci_only_reason"    "ci_only_reason:" "${registry}"

# The local runner executes test_command, while the registry validator only
# proves its scripts exist. Keep the CI mirror explicit so the cache-isolation
# regression runs in both local promotion and the workflow that claims it.
[[ -f "${registry_workflow}" ]] || fail "missing ${registry_workflow}"
require "pre-pr cache-isolation CI mirror" \
	"scripts/test-pre-pr-whole-module-gates.sh" \
	"${registry_workflow}"
require "generated CI-gates doc CI mirror" \
	"scripts/test-generate-ci-gates-doc.sh" \
	"${registry_workflow}"
require "cigates Go test CI mirror" \
	"go test ./internal/cigates" \
	"${registry_workflow}"

# A registry gate's own test inputs must select that gate. Otherwise changing a
# regression test can silently skip the check that is supposed to execute it.
ci_gate_registry="$(
	sed -n '/^  - id: ci-gate-registry$/,/^  - id:/p' "${registry}"
)"
ci_gate_registry_test_command="$(
	printf '%s\n' "${ci_gate_registry}" |
		sed -n 's/^[[:space:]]*test_command: "\(.*\)"$/\1/p'
)"
[[ -n "${ci_gate_registry_test_command}" ]] ||
	fail "ci-gate-registry has no local test_command"
while IFS= read -r registry_test_input; do
	selection="$(
		printf '%s\n' "${registry_test_input}" |
			(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
				--registry "${registry}" --tier pre-pr --paths-from - --explain)
	)"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+ci-gate-registry[[:space:]]' >/dev/null ||
		fail "ci-gate-registry test input does not select its gate (${registry_test_input})"
done < <(
	printf '%s\n' "${ci_gate_registry_test_command}" |
		rg --only-matching 'scripts/[[:alnum:]_./-]+\.sh' |
		sort -u
)

# Retained-console SQL fixtures are executable proof inputs. A fixture-only
# change must select the same frontend gate in both GitHub and local parity.
[[ -f "${frontend_workflow}" ]] || fail "missing ${frontend_workflow}"
# shellcheck source=scripts/lib/test-verify-ci-gates-registry-path-assertions.sh
. "${repo_root}/scripts/lib/test-verify-ci-gates-registry-path-assertions.sh"
# shellcheck source=scripts/lib/test-verify-ci-gates-registry-parser-relationship-cases.sh
. "${repo_root}/scripts/lib/test-verify-ci-gates-registry-parser-relationship-cases.sh"
check_parser_relationship_trigger_parity

[[ -f "${frontend_inventory}" ]] || fail "missing ${frontend_inventory}"
require "frontend PR inventory identity" "- file: frontend.yml" "${frontend_inventory}"
frontend_pull_request_paths="$(sed -n '/^  paths:/,/^  jobs:/p' "${frontend_inventory}")"

# A change to the live backend-conformance driver must schedule the E2E
# workflow that executes it and select the same registered CI-heavy gate.
[[ -f "${e2e_workflow}" ]] || fail "missing ${e2e_workflow}"
[[ -f "${e2e_inventory}" ]] || fail "missing ${e2e_inventory}"
require "e2e PR inventory identity" "- file: e2e-tests.yml" "${e2e_inventory}"
require "trusted e2e caller" "uses: ./.github/workflows/e2e-tests.yml" "${build_test_workflow}"
e2e_pull_request_paths="$(sed -n '/^  paths:/,/^  jobs:/p' "${e2e_inventory}")"
e2e_gate="$(
	sed -n '/^  - id: e2e-tests$/,/^  - id:/p' "${registry}"
)"
backend_conformance_script='scripts/verify_backend_conformance_live.sh'
require_inventory_path_line "${e2e_pull_request_paths}" "${backend_conformance_script}" \
	"e2e-tests pull_request paths omit the live backend-conformance driver"
mutated_e2e_paths="$(printf '%s\n' "${e2e_pull_request_paths}" | rg --invert-match --fixed-strings -- "  - ${backend_conformance_script}")"
if (require_inventory_path_line "${mutated_e2e_paths}" "${backend_conformance_script}" "seeded missing E2E path" >/dev/null 2>&1); then
	fail "e2e inventory path assertion accepted a planted missing driver"
fi
require_path_line "${e2e_gate}" "${backend_conformance_script}" \
	"e2e-tests registry triggers omit the live backend-conformance driver"

# Unregistered-trigger false-green class (#5538/#5546, #5814): five gates
# depend on a scripts/lib helper their own triggers omit, so a change touching
# only that helper selected NO gate and CI became first discovery.
# Membership assertions only, never an exact
# SELECTED count: heredoc-budget's "scripts/**/*.sh" trigger legitimately also
# matches every one of these paths, and other gates may come to match later.
# The list is fed by heredoc (not a pipe) so `fail` exits this script rather
# than a subshell; it is kept well under the 512-byte heredoc budget.
#
# "Depends on" deliberately covers more than `source`. maturity-drift-guard
# never sources its helper: extract_corpus_fixtures() awk-parses the
# corpus_fixtures=( ... ) array straight out of
# scripts/lib/golden-corpus-fixtures.sh, so the helper's CONTENT decides which
# fixture languages get graded. That inventory was split out of the gate
# orchestrator to respect the 500-line file cap, which is precisely how its
# trigger came to be missing — the same cap-driven extraction that created the
# merge_group lib. A read-as-data dependency false-greens like a sourced one.
# The fourth field is the trigger STRING doing the selecting, not always the
# helper's own path: #6200 globbed the scripts/lib/ Ifá surface, and a row that
# could only name a literal would have pinned one filename in the registry to
# keep this test green -- the per-file list this guard retires, inside the
# guard. Both halves bite: the trigger must be declared verbatim in the gate
# block, and the helper path must select that gate through it alone.
while IFS='|' read -r sourced_gate sourced_lib sourced_tier sourced_trigger; do
	[[ -n "${sourced_gate}" ]] || continue
	[[ -n "${sourced_trigger}" ]] || sourced_trigger="${sourced_lib}"
	sourced_gate_block="$(
		sed -n "/^  - id: ${sourced_gate}\$/,/^  - id: /p" "${registry}"
	)"
	require_path_line "${sourced_gate_block}" "${sourced_trigger}" \
		"${sourced_gate} registry triggers omit the trigger covering a scripts/lib helper it uses"
	sourced_selection="$(
		printf '%s\n' "${sourced_lib}" |
			(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
				--registry "${registry}" --tier "${sourced_tier}" --paths-from - --explain)
	)"
	printf '%s\n' "${sourced_selection}" |
		rg "^SELECTED[[:space:]]+${sourced_gate}[[:space:]]" >/dev/null ||
		fail "${sourced_lib} did not select ${sourced_gate}"
	printf '%s\n' "${sourced_selection}" |
		rg --fixed-strings -- "matched trigger \"${sourced_trigger}\" on path \"${sourced_lib}\"" >/dev/null ||
		fail "${sourced_gate} selected for the wrong reason (${sourced_lib} via ${sourced_trigger})"
done <<'SOURCED_LIB_GATES'
parser-relationship-kit|scripts/lib/parser_relationship_language_ledger.sh|pre-pr|
ifa-determinism|scripts/lib/ifa_sql_delta_live.sh|pre-pr|scripts/lib/ifa_*_live*.sh
ifa-fault-injection|scripts/lib/ifa_determinism_common.sh|pre-pr|
docs-build-changed|scripts/lib/test-verify-docs-build-changed-fake-uv.sh|pre-push|
maturity-drift-guard|scripts/lib/golden-corpus-fixtures.sh|pre-pr
ci-gate-registry|scripts/lib/test-verify-ci-gates-registry-telemetry-cases.sh|pre-pr
SOURCED_LIB_GATES

# shellcheck source=scripts/lib/test-verify-ci-gates-registry-telemetry-cases.sh
. "${repo_root}/scripts/lib/test-verify-ci-gates-registry-telemetry-cases.sh"
check_telemetry_coverage_trigger_parity

# shellcheck source=scripts/lib/test-verify-ci-gates-registry-performance-cases.sh
. "${repo_root}/scripts/lib/test-verify-ci-gates-registry-performance-cases.sh"
check_performance_evidence_trigger_parity

# shellcheck source=scripts/lib/test-verify-ci-gates-registry-docs-cli-env-cases.sh
. "${repo_root}/scripts/lib/test-verify-ci-gates-registry-docs-cli-env-cases.sh"
check_docs_cli_env_refs_trigger_parity

# shellcheck source=scripts/lib/test-verify-ci-gates-registry-ifa-filter-cases.sh
. "${repo_root}/scripts/lib/test-verify-ci-gates-registry-ifa-filter-cases.sh"
run_ci_gates_registry_ifa_filter_cases

# The number of gates that trigger on every path ("**"). Derived by asking the
# selector what a path no surface gate matches selects, rather than hardcoding
# it: this file previously assumed exactly one such gate (the AI-attribution
# check) in five separate places, so adding a second PR-wide gate broke all
# five with a number, not a reason (#5612).
pr_wide_gates="$(
	printf 'zz-pr-wide-probe.unmatched\n' |
		(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
			--registry "${registry}" --tier pre-push --paths-from - --explain) |
		rg --count '^SELECTED[[:space:]]+' || true
)"
[[ "${pr_wide_gates}" -ge 1 ]] ||
	fail "no PR-wide gates found; the counts below would assert nothing"

# A path under scripts/ also selects scripts-file-cap (#8221), so script-tree
# fixtures select their surface gate plus every PR-wide gate plus that one.
scripts_tree_gates="$(
	printf 'scripts/zz-scripts-tree-probe.unmatched\n' |
		(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
			--registry "${registry}" --tier pre-push --paths-from - --explain) |
		rg --count '^SELECTED[[:space:]]+' || true
)"
[[ "${scripts_tree_gates}" -gt "${pr_wide_gates}" ]] ||
	fail "a scripts/ path must select more gates than a PR-wide path (scripts-file-cap)"

for sql_fixture in \
	'scripts/lib/console-retained-create-proof-schema.sql' \
	'scripts/lib/console-retained-verify-public-identity.sql'; do
	require_inventory_path_line "${frontend_pull_request_paths}" "${sql_fixture}" \
		"frontend pull_request paths omit retained SQL fixture"

	selection="$(
		printf '%s\n' "${sql_fixture}" |
			(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
				--registry "${registry}" --tier pre-push --paths-from - --explain)
	)"
	[[ "$(printf '%s\n' "${selection}" | rg --count '^SELECTED[[:space:]]+' || true)" == "$((1 + scripts_tree_gates))" ]] ||
		fail "retained SQL fixture must select its surface gate plus the ${scripts_tree_gates} scripts-tree gate(s) (${sql_fixture})"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+frontend-console-checks[[:space:]]' >/dev/null ||
		fail "retained SQL fixture did not select frontend-console-checks (${sql_fixture})"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+no-ai-attribution[[:space:]]' >/dev/null ||
		fail "retained SQL fixture did not select the PR-wide attribution gate (${sql_fixture})"
	printf '%s\n' "${selection}" |
		rg --fixed-strings -- "matched trigger \"${sql_fixture}\" on path \"${sql_fixture}\"" >/dev/null ||
		fail "frontend-console-checks selected for the wrong reason (${sql_fixture})"
done

# #5798: a PR touching only the Cloudflare Pages build-Node pin, its runbook,
# or the console bundle-budget scripts/types previously ran no CI at all and
# selected no local gate either. Assert each of the six inputs is present in
# BOTH the workflow filter and every gate whose blast radius it falls in, AND
# that it genuinely selects those gates (not string presence alone) - so
# deleting or commenting out any one of the six from either source of truth
# fails this test.
frontend_site_gate="$(
	sed -n '/^  - id: frontend-site$/,/^  - id:/p' "${registry}"
)"
frontend_console_checks_gate="$(
	sed -n '/^  - id: frontend-console-checks$/,/^  - id:/p' "${registry}"
)"
frontend_eslint_gate="$(
	sed -n '/^  - id: frontend-eslint$/,/^  - id:/p' "${registry}"
)"

select_explain() {
	printf '%s\n' "$1" |
		(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
			--registry "${registry}" --tier pre-push --paths-from - --explain)
}

for cloudflare_input in '.nvmrc' 'CLOUDFLARE_PAGES.md'; do
	require_inventory_path_line "${frontend_pull_request_paths}" "${cloudflare_input}" \
		"frontend pull_request paths omit Cloudflare Pages input"
	require_path_line "${frontend_site_gate}" "${cloudflare_input}" \
		"frontend-site registry triggers omit Cloudflare Pages input"

	selection="$(select_explain "${cloudflare_input}")"
	[[ "$(printf '%s\n' "${selection}" | rg --count '^SELECTED[[:space:]]+' || true)" == "$((1 + pr_wide_gates))" ]] ||
		fail "Cloudflare Pages input must select its surface gate plus the ${pr_wide_gates} PR-wide gate(s) (${cloudflare_input})"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+frontend-site[[:space:]]' >/dev/null ||
		fail "Cloudflare Pages input did not select frontend-site (${cloudflare_input})"
	printf '%s\n' "${selection}" |
		rg --fixed-strings -- "matched trigger \"${cloudflare_input}\" on path \"${cloudflare_input}\"" >/dev/null ||
		fail "frontend-site selected for the wrong reason (${cloudflare_input})"
done

for bundle_input in \
	'scripts/console-bundle-budget.mjs' \
	'scripts/console-bundle-budget.d.mts' \
	'scripts/console-bundle-report.mjs' \
	'scripts/console-bundle-report.d.mts'; do
	require_inventory_path_line "${frontend_pull_request_paths}" "${bundle_input}" \
		"frontend pull_request paths omit console bundle-budget input"
	require_path_line "${frontend_console_checks_gate}" "${bundle_input}" \
		"frontend-console-checks registry triggers omit console bundle-budget input"

	# The .mjs pair is ALSO linted: verify-eslint-config.sh runs `eslint .`
	# repo-wide and eslint.config.js carries an explicit
	# files: ["scripts/**/*.{js,mjs,cjs,ts,mts,cts}"] block. The .d.mts pair is
	# not - eslint.config.js ignores "**/*.d.mts". Asserting a flat "exactly one
	# gate" here would have cemented that missing eslint trigger as correct.
	expected_gates=1
	if [[ "${bundle_input}" == *.mjs ]]; then
		expected_gates=2
		require_path_line "${frontend_eslint_gate}" 'scripts/**/*.mjs' \
			"frontend-eslint registry triggers omit the linted-scripts glob"
	fi

	selection="$(select_explain "${bundle_input}")"
	expected_with_pr_wide="$((expected_gates + scripts_tree_gates))"
	[[ "$(printf '%s\n' "${selection}" | rg --count '^SELECTED[[:space:]]+' || true)" == "${expected_with_pr_wide}" ]] ||
		fail "console bundle-budget input must select ${expected_gates} surface gate(s) plus the ${scripts_tree_gates} scripts-tree gate(s) (${bundle_input})"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+frontend-console-checks[[:space:]]' >/dev/null ||
		fail "console bundle-budget input did not select frontend-console-checks (${bundle_input})"
	printf '%s\n' "${selection}" |
		rg --fixed-strings -- "matched trigger \"${bundle_input}\" on path \"${bundle_input}\"" >/dev/null ||
		fail "frontend-console-checks selected for the wrong reason (${bundle_input})"
	if [[ "${expected_gates}" -eq 2 ]]; then
		printf '%s\n' "${selection}" |
			rg '^SELECTED[[:space:]]+frontend-eslint[[:space:]]' >/dev/null ||
			fail "linted bundle script did not select frontend-eslint (${bundle_input})"
	fi
done

# Same #5798 class, found by sweeping the sibling scripts: root vite.config.ts
# sets test.exclude with NO include override, so vitest's default pattern
# collects scripts/marketing-review-runtime.test.mjs from the repo root - and
# `npm test` is frontend-site's own command. All three files ran no CI and
# selected no gate at all, four lines away from the pins this issue is about.
for marketing_input in \
	'scripts/marketing-review.mjs' \
	'scripts/marketing-review-runtime.mjs' \
	'scripts/marketing-review-runtime.test.mjs'; do
	require_inventory_path_line "${frontend_pull_request_paths}" "${marketing_input}" \
		"frontend pull_request paths omit marketing-review input"
	require_path_line "${frontend_site_gate}" "${marketing_input}" \
		"frontend-site registry triggers omit marketing-review input"

	selection="$(select_explain "${marketing_input}")"
	[[ "$(printf '%s\n' "${selection}" | rg --count '^SELECTED[[:space:]]+' || true)" == "$((2 + scripts_tree_gates))" ]] ||
		fail "marketing-review input must select two surface gates plus the ${scripts_tree_gates} scripts-tree gate(s) (${marketing_input})"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+frontend-site[[:space:]]' >/dev/null ||
		fail "marketing-review input did not select frontend-site (${marketing_input})"
	printf '%s\n' "${selection}" |
		rg --fixed-strings -- "matched trigger \"${marketing_input}\" on path \"${marketing_input}\"" >/dev/null ||
		fail "frontend-site selected for the wrong reason (${marketing_input})"
	printf '%s\n' "${selection}" |
		rg '^SELECTED[[:space:]]+frontend-eslint[[:space:]]' >/dev/null ||
		fail "marketing-review input did not select frontend-eslint (${marketing_input})"
done

# The registry matches linted scripts as a CLASS ("scripts/**/*.mjs") while the
# workflow has to enumerate them, because GitHub's ** semantics are not the Go
# matcher's. Nothing else reconciles the two lists, so a new scripts/foo.mjs
# would get the local gate and ZERO CI - #5798 again, on the CI side. Drive the
# check off the tracked files so it also covers scripts that do not exist yet.
# Materialize the list first: `set -e` does NOT propagate a failure out of a
# process substitution, so `done < <(git ls-files ...)` would iterate zero times
# and report PASS if git or rg failed - a silent false green in the one check
# that reconciles the class glob against the enumerated workflow.
linted_scripts="$(
	cd "${repo_root}" && git ls-files scripts | rg '\.(mjs|cjs|js|ts|mts|cts)$' | sort -u
)" || fail "could not enumerate tracked linted scripts (git/rg failed)"
[[ -n "${linted_scripts}" ]] ||
	fail "no tracked linted scripts enumerated - the parity check would be vacuous"
# NOTE: no herestring (<<<) here: it deadlocks on bash >= 5.3. The list is
# already materialized and validated non-empty above, so the printf-fed
# process substitution keeps the no-silent-false-green property.
while IFS= read -r linted_script; do
	[[ -n "${linted_script}" ]] || continue
	require_inventory_path_line "${frontend_pull_request_paths}" "${linted_script}" \
		"frontend pull_request paths omit linted script"
done < <(printf '%s\n' "${linted_scripts}")

# The published Cloudflare Pages assets. Vite has no publicDir/root override, so
# public/ is copied verbatim into build.outDir and shipped - a direct input to
# frontend-site's own `npm run build`, yet it selected no gate at all. Asserted
# as a class glob, and proven through a real asset so the glob cannot be vacuous.
require_inventory_path_line "${frontend_pull_request_paths}" 'public/**' \
	"frontend pull_request paths omit the published public assets"
require_path_line "${frontend_site_gate}" 'public/**' \
	"frontend-site registry triggers omit the published public assets"
public_assets="$(cd "${repo_root}" && git ls-files public)" ||
	fail "could not enumerate tracked public/ assets (git failed)"
public_asset="${public_assets%%$'\n'*}"
[[ -n "${public_asset}" ]] ||
	fail "no tracked public/ asset found - the public/** assertion would be vacuous"
selection="$(select_explain "${public_asset}")"
[[ "$(printf '%s\n' "${selection}" | rg --count '^SELECTED[[:space:]]+' || true)" == "$((1 + pr_wide_gates))" ]] ||
	fail "a published public asset must select its surface gate plus the ${pr_wide_gates} PR-wide gate(s) (${public_asset})"
printf '%s\n' "${selection}" |
	rg '^SELECTED[[:space:]]+frontend-site[[:space:]]' >/dev/null ||
	fail "public asset did not select frontend-site (${public_asset})"

# The exact-source auth CLI helper is shared by both fresh-stack auth gates.
# CI-heavy gates are never selected in the local lane (select.go enforces that
# before trigger matching), so prove parity directly in both sources of truth;
# the live validate --drift below proves the resulting registry is valid.
auth_mcp_gate="$(
	sed -n '/^  - id: auth-mcp-e2e$/,/^  - id:/p' "${registry}"
)"
for auth_cli_path in \
	'scripts/lib/auth_e2e_cli.sh' \
	'scripts/test-auth-e2e-cli.sh'; do
	require_path_line "${auth_mcp_gate}" "${auth_cli_path}" \
		"auth-mcp-e2e registry triggers omit the auth CLI helper"
	require_inventory_path_line "${frontend_pull_request_paths}" "${auth_cli_path}" \
		"frontend pull_request paths omit the auth CLI helper"
done

# Every gate must declare a tier. Spot-check the enumerated tiers.
require "pre-commit tier"  "tier: pre-commit" "${registry}"
require "pre-push tier"    "tier: pre-push"   "${registry}"
require "pre-pr tier"      "tier: pre-pr"     "${registry}"
require "ci-heavy tier"    "tier: ci-heavy"   "${registry}"

# #4220 drift surfaces: hook mappings + the two reconciliation allowlists.
require "hook_id mapping"     "hook_id:"            "${registry}"
require "hygiene_hooks list"  "hygiene_hooks:"      "${registry}"
require "non_gate_workflows"  "non_gate_workflows:" "${registry}"

# shellcheck source=scripts/lib/test-verify-ci-gates-registry-workflow-shape-cases.sh
. "${repo_root}/scripts/lib/test-verify-ci-gates-registry-workflow-shape-cases.sh"
check_ci_gate_workflow_shapes

# ── 3. Live validate + drift — proves every script + workflow ref exists AND
#       that .pre-commit-config.yaml / .github/workflows stay in lockstep ─────

printf 'test-verify-ci-gates-registry: running live validate --drift on committed registry...\n'
(cd "${repo_root}/go" && go run ./cmd/ci-gates validate \
	--registry "${registry}" \
	--repo-root "${repo_root}" \
	--drift) || fail "live validate --drift failed — see errors above"

printf 'PASS: ci-gates registry static contract + drift\n'
