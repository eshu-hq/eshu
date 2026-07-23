#!/usr/bin/env bash
#
# verify-docs-only-ci-skip.sh — static mirror for the docs-only CI carve-out.
#
# A docs-only PR (docs/**, root markdown, mkdocs config) must skip the heavy Go
# lanes while the cheap due-diligence gates — and the docs-relevant guards that
# happen to live inside otherwise-heavy workflows — still run. That behavior
# lives in .github/workflows YAML and is only exercised by GitHub Actions, so
# this script is the local guard that the WIRING is intact. It fails closed if a
# future edit removes a path filter, drops a heavy job's `changes` gate, lets the
# go-race umbrella reject a legitimately-skipped matrix, or — the subtle one the
# review caught — silences a docs-relevant guard (Trivy's secret scan, the
# capability `-mode docs` check) by gating the wrong job.
#
# It does not re-implement dorny/paths-filter's matcher (CI owns the actual path
# evaluation); it asserts the structure that makes the skip correct, complete,
# and required-check-safe. Runs in the always-on docs-helm-hygiene job.
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
wf="${repo_root}/.github/workflows"
fail=0

ok()  { printf 'ok - %s\n' "$1"; }
bad() { printf 'not ok - %s\n' "$1"; fail=1; }
has() { rg -qF -- "$2" "$1"; }

# job_block <file> <job> — the YAML lines of one top-level job, header through the
# line before the next 2-space job key.
job_block() { awk -v j="  $2:" '$0==j{f=1;print;next} f&&/^  [A-Za-z]/{exit} f{print}' "$1"; }
# job_gated <file> <job> — true if the job carries a `needs: changes` code gate.
job_gated()    { job_block "$1" "$2" | rg -qF 'needs: changes'; }
job_alwayson() { ! job_gated "$1" "$2"; }

# --- build.yml: pure binary build, no docs-relevant job — skip the whole run. ---
b="${wf}/build.yml"
if has "${b}" 'paths-ignore:' && has "${b}" "- 'docs/**'" && has "${b}" "- '*.md'"; then
	ok "build.yml skips docs-only PRs (paths-ignore: docs/**, *.md)"
else
	bad "build.yml has a pull_request paths-ignore covering docs/** and *.md"
fi

# --- security-scan.yml: keep the secret scan on, gate the Go scanners. ---
s="${wf}/security-scan.yml"
if has "${s}" 'dorny/paths-filter' && job_block "${s}" changes | rg -qF 'code: ${{ steps.filter.outputs.code }}'; then
	ok "security-scan.yml has a changes job exporting the code filter"
else
	bad "security-scan.yml has a changes job exporting code"
fi
if job_alwayson "${s}" trivy-fs; then
	ok "security-scan.yml keeps trivy-fs (secret + IaC scan) always-on for docs-only PRs"
else
	bad "security-scan.yml trivy-fs must NOT be code-gated (it is the only secret scan)"
fi
for j in govulncheck gosec nancy; do
	if job_gated "${s}" "${j}"; then ok "security-scan.yml ${j} (Go scanner) is code-gated"
	else bad "security-scan.yml ${j} carries needs:changes"; fi
done

# --- mcp-schema-drift.yml: keep the docs guard on, gate the Go drift jobs. ---
m="${wf}/mcp-schema-drift.yml"
if has "${m}" 'dorny/paths-filter' && job_block "${m}" changes | rg -qF 'code: ${{ steps.filter.outputs.code }}'; then
	ok "mcp-schema-drift.yml has a changes job exporting the code filter"
else
	bad "mcp-schema-drift.yml has a changes job exporting code"
fi
if job_alwayson "${m}" capability-verify; then
	ok "mcp-schema-drift.yml keeps capability-verify (-mode docs guard) always-on for docs-only PRs"
else
	bad "mcp-schema-drift.yml capability-verify must NOT be code-gated (its -mode docs is the only docs guard)"
fi
for j in mcp-tool-count mcp-test-suite; do
	if job_gated "${m}" "${j}"; then ok "mcp-schema-drift.yml ${j} (Go drift job) is code-gated"
	else bad "mcp-schema-drift.yml ${j} carries needs:changes"; fi
done

# --- test.yml: heavy Go lanes gated; docs build stays always-on. ---
t="${wf}/test.yml"
if has "${t}" 'code: ${{ steps.filter.outputs.code }}' && has "${t}" 'dorny/paths-filter'; then
	ok "test.yml has a changes job exporting the code filter"
else
	bad "test.yml exposes a changes.outputs.code from dorny/paths-filter"
fi
needs_n="$(rg -cF 'needs: changes' "${t}" || true)"; needs_n="${needs_n:-0}"
guard_n="$(rg -cF "github.event_name != 'pull_request' || needs.changes.outputs.code == 'true'" "${t}" || true)"; guard_n="${guard_n:-0}"
if [[ "${needs_n}" -ge 3 && "${guard_n}" -ge 3 ]]; then
	ok "all 3 heavy test.yml jobs gate on the changes job (needs×${needs_n}, guard×${guard_n})"
else
	bad "verify-contracts, go-core and go-race each carry needs:changes + the event-guarded if (needs×${needs_n}, guard×${guard_n}, want ≥3 each)"
fi
if job_alwayson "${t}" docs-helm-hygiene; then
	ok "test.yml keeps docs-helm-hygiene (docs build) always-on for docs-only PRs"
else
	bad "test.yml docs-helm-hygiene must NOT be code-gated (it is the docs build)"
fi
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
