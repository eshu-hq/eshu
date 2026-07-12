#!/usr/bin/env bash
# generate-ci-gates-doc.sh - regenerate docs/public/reference/ci-gates.md from
# specs/ci-gates.v1.yaml, the single source of truth for Eshu's CI gate
# registry. To change a gate's documented shape, edit the registry (or, for
# parsing and rendering, scripts/lib/ci-gates-doc-parse.awk), re-run this
# script, and commit the regenerated output. The test mirror
# scripts/test-generate-ci-gates-doc.sh asserts the committed file is
# byte-identical to a fresh run (idempotency/drift) and carries the expected
# headline gates and row count.
#
# Parsing AND per-row rendering both happen in
# scripts/lib/ci-gates-doc-parse.awk (a real state machine over the
# registry's three record shapes — full gate, CI-only gate, and alias entry)
# rather than in this shell script. That is deliberate, not just a style
# choice: an earlier draft parsed in awk and rendered each row in a bash
# `while read` loop, and hit a real bash 3.2.57 defect (the system bash this
# repo's scripts must run under) where `read` silently mis-splits fields on
# any delimiter that is not one of its three built-in IFS-whitespace
# characters. Keeping every row's data inside awk's own variables from parse
# to print avoids that entire class of bug. Every body this script itself
# emits is built with printf, never a heredoc, so it cannot hit the bash
# >=5.1 heredoc-pipe deadlock (#5019/#5074) either.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
registry="${repo_root}/specs/ci-gates.v1.yaml"
lib_dir="${repo_root}/scripts/lib"
output_path="${ESHU_CI_GATES_DOC_OUTPUT_PATH:-${repo_root}/docs/public/reference/ci-gates.md}"

[[ -f "${registry}" ]] || {
	printf 'generate-ci-gates-doc: registry not found: %s\n' "${registry}" >&2
	exit 1
}

gate_count="$(rg -c '^  - id: ' "${registry}")"

{
	printf '# CI Gates Reference\n\n'
	printf '<!-- Generated from specs/ci-gates.v1.yaml. Do not edit by hand; regenerate with `bash scripts/generate-ci-gates-doc.sh`. -->\n\n'
	printf 'This reference is generated from the CI gate registry, the single source\n'
	printf 'of truth mapping a changed path to the local and CI checks it requires. See\n'
	printf '[Run the proof suite](../guides/run-the-proof-suite.md) for how `make pre-pr`\n'
	printf 'and `make prove` select from this table, and\n'
	printf '[Local Testing](local-testing.md) for the full verification map.\n\n'
	printf 'The registry currently defines %s gates. A row with no local command is\n' "${gate_count}"
	printf 'CI-only (it needs a credential, a service container, or hosted infrastructure\n'
	printf 'a laptop does not have); a row marked as an alias shares its check with the\n'
	printf 'gate its reason names, under a different git hook stage.\n\n'
	printf '| Gate id | Name | Category | Tier | Blocking | Local command | CI workflow / job | Triggers |\n'
	printf '| --- | --- | --- | --- | --- | --- | --- | --- |\n'

	awk -f "${lib_dir}/ci-gates-doc-parse.awk" "${registry}"
} >"${output_path}"

printf 'generate-ci-gates-doc: wrote %s (%s gates)\n' "${output_path}" "${gate_count}"
