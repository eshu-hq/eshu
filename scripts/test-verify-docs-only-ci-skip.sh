#!/usr/bin/env bash
#
# test-verify-docs-only-ci-skip.sh — test mirror for verify-docs-only-ci-skip.sh
# (#5818). Proves the two #5818 regression guards it added are non-vacuous:
#   1. the three duplicated `code:` dorny/paths-filter blocks (test.yml,
#      security-scan.yml, mcp-schema-drift.yml) must stay byte-identical;
#   2. no registry gate whose CI job is one of these workflows' code-gated jobs
#      may declare a trigger the filter's negations would swallow.
#
# Rather than hand-building synthetic fixture workflows that would also have to
# satisfy every pre-existing check in verify-docs-only-ci-skip.sh (the
# always-on-job checks, the umbrella guard-body checks, and so on), this test
# copies the real, currently-green repo files into a scratch tree — the script
# resolves repo_root from its own path, so running the copied script against
# the copied tree is a faithful hermetic rerun — then mutates one file at a
# time and asserts the specific guard fires. This is the same mutation the
# guards were proven against by hand while landing #5818's fix; this script
# pins that proof as a regression test.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script_rel="scripts/verify-docs-only-ci-skip.sh"

pass=0
fail=0
ok() { printf 'ok - %s\n' "$1"; pass=$((pass + 1)); }
no() { printf 'NOT OK - %s\n' "$1"; fail=$((fail + 1)); }

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# --- build the scratch tree: only the files verify-docs-only-ci-skip.sh reads. ---
mkdir -p "${tmp}/scripts" "${tmp}/.github/workflows" "${tmp}/specs"
cp "${repo_root}/${script_rel}" "${tmp}/scripts/verify-docs-only-ci-skip.sh"
for wf in build.yml security-scan.yml mcp-schema-drift.yml test.yml; do
	cp "${repo_root}/.github/workflows/${wf}" "${tmp}/.github/workflows/${wf}"
done
cp "${repo_root}/specs/ci-gates.v1.yaml" "${tmp}/specs/ci-gates.v1.yaml"
chmod +x "${tmp}/scripts/verify-docs-only-ci-skip.sh"

run_scratch() { (cd "${tmp}" && bash scripts/verify-docs-only-ci-skip.sh); }

# --- baseline: the real, unmutated repo state is green. ---
if out="$(run_scratch 2>&1)"; then
	ok "scratch copy of the real repo state passes verify-docs-only-ci-skip.sh"
else
	no "scratch copy of the real repo state should pass; got:"
	printf '%s\n' "${out}" >&2
fi

# --- guard 1 (byte-identity), failing case: drop the .agents/** negation from
# only ONE of the three copies (the exact #5818-shaped drift — a fix applied to
# one filter copy and not the other two). ---
python3 - "${tmp}/.github/workflows/mcp-schema-drift.yml" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = "              - '!.agents/**'\n"
assert content.count(needle) == 1, "expected exactly one !.agents/** negation line"
with open(path, "w") as f:
	f.write(content.replace(needle, "", 1))
PY
if out="$(run_scratch 2>&1)"; then
	no "guard 1 should fail when one of the three code: filters drifts from the other two"
else
	if rg -qF 'code: filter has drifted' <<<"${out}"; then
		ok "guard 1 fails and names the drift when one code: filter copy loses !.agents/**"
	else
		no "guard 1 failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/.github/workflows/mcp-schema-drift.yml" "${tmp}/.github/workflows/mcp-schema-drift.yml"
if run_scratch >/dev/null 2>&1; then
	ok "guard 1 passes again after restoring the mutated filter copy"
else
	no "guard 1 should pass again once the mutated filter copy is restored"
fi

# --- guard 2 (trigger superset), failing case: give a code-gated job's registry
# gate (go-fmt, ci.job "go-core" in test.yml) a trigger the filter's negations
# would swallow. ---
python3 - "${tmp}/specs/ci-gates.v1.yaml" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = (
	"  - id: go-fmt\n"
	"    name: Go gofumpt formatting\n"
	"    category: hygiene\n"
	"    tier: pre-commit\n"
	"    blocking: true\n"
	"    triggers:\n"
	'      - "go/**"\n'
)
assert needle in content, "go-fmt gate anchor not found — registry shape changed?"
mutated = needle.replace('      - "go/**"\n', '      - "go/**"\n      - ".agents/**"\n')
with open(path, "w") as f:
	f.write(content.replace(needle, mutated, 1))
PY
if out="$(run_scratch 2>&1)"; then
	no "guard 2 should fail when a code-gated gate's trigger is swallowed by the filter's negations"
else
	if rg -qF "swallowed by the code filter's own negations" <<<"${out}"; then
		ok "guard 2 fails and names the swallowed trigger when go-fmt gains a .agents/** trigger"
	else
		no "guard 2 failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/specs/ci-gates.v1.yaml" "${tmp}/specs/ci-gates.v1.yaml"
if run_scratch >/dev/null 2>&1; then
	ok "guard 2 passes again after restoring the mutated registry"
else
	no "guard 2 should pass again once the mutated registry is restored"
fi

printf '\n%d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
