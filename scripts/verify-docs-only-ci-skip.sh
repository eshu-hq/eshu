#!/usr/bin/env bash
#
# verify-docs-only-ci-skip.sh — static mirror for the docs-only CI carve-out.
#
# A docs-only PR (docs/**, root markdown, mkdocs config) must skip the heavy Go
# lanes while the cheap due-diligence gates still run. That behavior lives in
# .github/workflows YAML and is only exercised by GitHub Actions, so this script
# is the local guard that the WIRING is intact — it fails closed if a future edit
# removes the path filter, drops the `changes` gate from a heavy job, or lets the
# go-race umbrella reject a legitimately-skipped matrix. It does not re-implement
# dorny/paths-filter's matcher (CI owns the actual path evaluation); it asserts
# the structure that makes the skip both correct and required-check-safe.
#
# Runs in the always-on docs-helm-hygiene job, so it guards the carve-out even on
# the docs-only PRs the carve-out is for.
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
wf="${repo_root}/.github/workflows"
fail=0

ok()   { printf 'ok - %s\n' "$1"; }
bad()  { printf 'not ok - %s\n' "$1"; fail=1; }

# has_block <file> <needle> — needle present anywhere in file.
has() { rg -qF -- "$2" "$1"; }

# The three fully-heavy workflows must skip on a docs-only PR via paths-ignore on
# the pull_request trigger. paths-ignore skips the run only when EVERY changed
# file matches, so a mixed docs+code PR still runs — the safe direction.
for f in security-scan.yml mcp-schema-drift.yml build.yml; do
	p="${wf}/${f}"
	if [[ ! -f "${p}" ]]; then bad "${f} exists"; continue; fi
	if has "${p}" 'paths-ignore:' && has "${p}" "- 'docs/**'" && has "${p}" "- '*.md'"; then
		ok "${f} skips docs-only PRs (paths-ignore: docs/**, *.md)"
	else
		bad "${f} has a pull_request paths-ignore covering docs/** and *.md"
	fi
done

# test.yml is mixed: the heavy lanes gate on a `changes` job; docs-helm-hygiene
# stays always-on.
t="${wf}/test.yml"
if has "${t}" 'code: ${{ steps.filter.outputs.code }}' && has "${t}" 'dorny/paths-filter'; then
	ok "test.yml has a changes job exporting the code filter"
else
	bad "test.yml exposes a changes.outputs.code from dorny/paths-filter"
fi

# Each of the three heavy jobs (verify-contracts, go-core, go-race) must depend
# on `changes` and short-circuit to run on non-PR events (push/schedule/dispatch)
# so main is never left unverified. Count occurrences rather than windowing each
# job — robust to the long comment blocks that separate a header from its wiring.
needs_n="$(rg -cF 'needs: changes' "${t}" || true)"; needs_n="${needs_n:-0}"
guard_n="$(rg -cF "github.event_name != 'pull_request' || needs.changes.outputs.code == 'true'" "${t}" || true)"; guard_n="${guard_n:-0}"
if [[ "${needs_n}" -ge 3 && "${guard_n}" -ge 3 ]]; then
	ok "all 3 heavy jobs gate on the changes job (needs×${needs_n}, event-guard×${guard_n}; run on non-PR events regardless)"
else
	bad "verify-contracts, go-core and go-race each carry needs:changes + the event-guarded if (needs×${needs_n}, guard×${guard_n}, want ≥3 each)"
fi

# The go-race umbrella must treat a SKIPPED matrix (docs-only PR) as green, so its
# stable check name never strands a docs-only PR if marked required later.
if rg -qF '!= "skipped"' "${t}"; then
	ok "go-race-complete accepts a skipped matrix as pass (required-check-safe)"
else
	bad "go-race-complete treats result==skipped as pass"
fi

if [[ "${fail}" -ne 0 ]]; then
	printf '\nverify-docs-only-ci-skip: docs-only CI carve-out wiring drifted — see failures above.\n' >&2
	exit 1
fi
printf '\nverify-docs-only-ci-skip: docs-only CI carve-out wiring intact.\n'
