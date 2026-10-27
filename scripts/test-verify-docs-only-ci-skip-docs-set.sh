#!/usr/bin/env bash
#
# test-verify-docs-only-ci-skip-docs-set.sh — mutate a scratch copy of the live
# CI wiring to prove the #9200 docs-set guard rejects broken filters, fragments,
# and swallowed markdown triggers. Companion to
# scripts/test-verify-docs-only-ci-skip.sh, which owns the #5818/#5896/#5814
# cases; this file stays separate so neither mirror approaches the 500-line
# scripts cap. Wired next to it in the docs-only-ci-skip gate test_command and
# in the ci-docs-helm-hygiene action.
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
mkdir -p "${tmp}/scripts/lib" "${tmp}/.github/workflows" "${tmp}/specs"
cp "${repo_root}/${script_rel}" "${tmp}/scripts/verify-docs-only-ci-skip.sh"
cp "${repo_root}/scripts/lib/ci-gate-merge-group-checks.sh" "${tmp}/scripts/lib/ci-gate-merge-group-checks.sh"
cp "${repo_root}/scripts/lib/ci-gate-predicate-quantifier.sh" "${tmp}/scripts/lib/ci-gate-predicate-quantifier.sh"
cp "${repo_root}/scripts/lib/ci-gate-docs-planner-checks.sh" "${tmp}/scripts/lib/ci-gate-docs-planner-checks.sh"
cp "${repo_root}/scripts/lib/ci-gates-resolved-fixtures.sh" "${tmp}/scripts/lib/ci-gates-resolved-fixtures.sh"
mkdir -p "${tmp}/scripts/ci" "${tmp}/specs/ci-pr-workflows.d"
cp "${repo_root}/scripts/ci/assert-go-complete.sh" "${tmp}/scripts/ci/assert-go-complete.sh"
for frag in bench.yml build.yml e2e-tests.yml live-backend-tests.yml read-api-latency-gate.yml sdk-go.yml; do
	cp "${repo_root}/specs/ci-pr-workflows.d/${frag}" "${tmp}/specs/ci-pr-workflows.d/${frag}"
done
cp "${repo_root}/specs/ci-docs-lockstep-paths.txt" "${tmp}/specs/ci-docs-lockstep-paths.txt"
for wf in build.yml security-scan.yml mcp-schema-drift.yml test.yml; do
	cp "${repo_root}/.github/workflows/${wf}" "${tmp}/.github/workflows/${wf}"
done
. "${repo_root}/scripts/lib/ci-gates-resolved-fixtures.sh"
ci_gates_flat_view "${repo_root}/specs/ci-gates.v1.yaml" "${tmp}/specs/ci-gates.v1.yaml" "${repo_root}"
cp "${tmp}/specs/ci-gates.v1.yaml" "${tmp}/ci-gates-flat.original.yaml"
chmod +x "${tmp}/scripts/verify-docs-only-ci-skip.sh"

run_scratch() { (cd "${tmp}" && ESHU_CI_GATES_GO_REPO="${repo_root}" bash scripts/verify-docs-only-ci-skip.sh); }

# --- baseline: the real, unmutated repo state is green. ---
if out="$(run_scratch 2>&1)"; then
	ok "scratch copy of the real repo state passes verify-docs-only-ci-skip.sh"
else
	no "scratch copy of the real repo state should pass; got:"
	printf '%s\n' "${out}" >&2
fi

# --- #9200 positive proof, failing case: drop the '!**/*.md' negation from
# all three code: filters. Byte-identity still holds, so guard 1 stays green;
# the new docs-set check must fail instead. ---
for target_wf in test.yml security-scan.yml mcp-schema-drift.yml; do
python3 - "${tmp}/.github/workflows/${target_wf}" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = "              - '!**/*.md'\n"
assert content.count(needle) == 1, "expected exactly one '!**/*.md' negation line"
with open(path, "w") as f:
	f.write(content.replace(needle, "", 1))
PY
done
if out="$(run_scratch 2>&1)"; then
	no "missing nested-markdown negation in all three filters must fail"
else
	if rg -F 'code: filter lacks' < <(printf '%s\n' "${out}") >/dev/null; then
		ok "docs-set check fails and names the missing '!**/*.md' negation"
	else
		no "docs-set check failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
for target_wf in test.yml security-scan.yml mcp-schema-drift.yml; do
	cp "${repo_root}/.github/workflows/${target_wf}" "${tmp}/.github/workflows/${target_wf}"
done

# --- #9200 planner proof, failing case: drop the nested-markdown ignore from
# the build fragment. ---
python3 - "${tmp}/specs/ci-pr-workflows.d/build.yml" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = "  - '**/*.md'\n"
assert content.count(needle) == 1, "expected exactly one '**/*.md' ignore line"
with open(path, "w") as f:
	f.write(content.replace(needle, "", 1))
PY
if out="$(run_scratch 2>&1)"; then
	no "missing nested-markdown ignore in the build fragment must fail"
else
	if rg -F 'build.yml needs trusted fragment' < <(printf '%s\n' "${out}") >/dev/null; then
		ok "planner check fails and names the build fragment ignore"
	else
		no "planner check failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/specs/ci-pr-workflows.d/build.yml" "${tmp}/specs/ci-pr-workflows.d/build.yml"

# --- #9200 fragment proof, failing case: drop the markdown exclusion from the
# e2e fragment (representative of the five broad go/** fragments). ---
python3 - "${tmp}/specs/ci-pr-workflows.d/e2e-tests.yml" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = "  - '!**/*.md'\n"
assert content.count(needle) == 1, "expected exactly one '!**/*.md' exclusion line"
with open(path, "w") as f:
	f.write(content.replace(needle, "", 1))
PY
if out="$(run_scratch 2>&1)"; then
	no "missing markdown exclusion in the e2e fragment must fail"
else
	if rg -F "fragment lacks the '!**/*.md' exclusion" < <(printf '%s\n' "${out}") >/dev/null; then
		ok "fragment check fails and names the e2e exclusion"
	else
		no "fragment check failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/specs/ci-pr-workflows.d/e2e-tests.yml" "${tmp}/specs/ci-pr-workflows.d/e2e-tests.yml"

# --- last-match-wins order proof, failing case: a positive markdown path that
# is NOT a lockstep manifest entry, appended after the exclusion, would
# silently re-select markdown. Manifest entries after the exclusion are the
# reviewed re-includes and must keep passing. ---
python3 - "${tmp}/specs/ci-pr-workflows.d/e2e-tests.yml" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = "  - '!**/*.md'\n"
assert content.count(needle) == 1, "expected exactly one '!**/*.md' exclusion line"
with open(path, "w") as f:
	f.write(content.replace(needle, "  - '!**/*.md'\n  - 'docs/**/*.md'\n", 1))
PY
if out="$(run_scratch 2>&1)"; then
	no "a non-manifest markdown path after the exclusion must fail"
else
	if rg -F "is not a lockstep manifest entry" < <(printf '%s\n' "${out}") >/dev/null; then
		ok "order check fails and names the unlisted re-selecting path"
	else
		no "order check failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/specs/ci-pr-workflows.d/e2e-tests.yml" "${tmp}/specs/ci-pr-workflows.d/e2e-tests.yml"

# --- guard 2 LIVE-shape proof for '**/*.md': give a code-gated gate (go-fmt,
# ci.job "go-core") a literal nested-markdown trigger the live '!**/*.md'
# negation swallows whole. ---
python3 - "${tmp}/specs/ci-gates.v1.yaml" <<'PY'
import io,sys
p=sys.argv[1]
s=io.open(p).read()
i=s.index("  - id: go-fmt\n")
t='      - "go/**"\n'
j=s.index(t,i)+len(t)
io.open(p,"w").write(s[:j]+'      - "go/cmd/collector-cicd-run/README.md"\n'+s[j:])
PY
if out="$(run_scratch 2>&1)"; then
	no "guard 2 should fail when a code-gated trigger is swallowed by '!**/*.md'"
else
	if rg -F "trigger 'go/cmd/collector-cicd-run/README.md' is swallowed by the code filter's own negations" < <(printf '%s\n' "${out}") >/dev/null; then
		ok "guard 2 fails and names the swallowed nested-markdown trigger"
	else
		no "guard 2 failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${tmp}/ci-gates-flat.original.yaml" "${tmp}/specs/ci-gates.v1.yaml"
if run_scratch >/dev/null 2>&1; then
	ok "guard 2 passes again after restoring the mutated registry"
else
	no "guard 2 should pass again once the mutated registry is restored"
fi

printf '\n%d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
