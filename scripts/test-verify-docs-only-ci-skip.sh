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
mkdir -p "${tmp}/scripts/lib" "${tmp}/.github/workflows" "${tmp}/specs"
cp "${repo_root}/${script_rel}" "${tmp}/scripts/verify-docs-only-ci-skip.sh"
# The merge_group (#5814) checks live in this sourced lib, not inline (kept
# verify-docs-only-ci-skip.sh under the 500-line cap). Omitting this copy is
# exactly the "lib committed but never sourced by the scratch mirror" failure
# mode a stale test harness would hide — reproduced by hand while writing this
# copy: without it the scratch script aborts with a "did not define
# run_merge_group_checks" error the first time run_scratch is called, and
# every case below fails "for the wrong reason" instead of testing anything.
cp "${repo_root}/scripts/lib/ci-gate-merge-group-checks.sh" "${tmp}/scripts/lib/ci-gate-merge-group-checks.sh"
cp "${repo_root}/scripts/lib/ci-gate-predicate-quantifier.sh" "${tmp}/scripts/lib/ci-gate-predicate-quantifier.sh"
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
	if rg -F 'code: filter has drifted' < <(printf '%s\n' "${out}") >/dev/null; then
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
	if rg -F "swallowed by the code filter's own negations" < <(printf '%s\n' "${out}") >/dev/null; then
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

# --- guard 2 non-vacuity proof (#5841 P1): a NEW negation the filter does NOT
# carry today — not one of the five hard-coded shapes a prior version of this
# checker enumerated by name (docs/*, mkdocs.yml, .agents/*, .github/*.md,
# *.md) — added IDENTICALLY to all three code: filter copies (byte-identity
# stays intact, so guard 1 stays green) that happens to swallow an EXISTING
# code-gated gate's trigger whole: '!go/internal/query/**' fully covers
# query-plan-regression's "go/internal/query/**" trigger (ci.job
# "verify-contracts", code-gated in test.yml). A matcher hard-coded to
# today's five negation cases cannot see this — "go/internal/query/**"
# matches none of them and falls through to "not swallowed", the exact false
# "ok" the review flagged. The registry-driven matcher instead parses
# whatever negations the filter actually carries, so it catches this without
# needing a code change for the new negation shape. ---
for target_wf in test.yml security-scan.yml mcp-schema-drift.yml; do
	python3 - "${tmp}/.github/workflows/${target_wf}" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = "              - '!.agents/**'\n"
assert content.count(needle) == 1, "expected exactly one !.agents/** negation line"
mutated = needle + "              - '!go/internal/query/**'\n"
with open(path, "w") as f:
	f.write(content.replace(needle, mutated, 1))
PY
done
if out="$(run_scratch 2>&1)"; then
	no "guard 2 should fail when a NEW negation, added identically to all three filters, swallows an existing code-gated trigger whole"
else
	if rg -F "trigger 'go/internal/query/**' is swallowed by the code filter's own negations" < <(printf '%s\n' "${out}") >/dev/null; then
		ok "guard 2 fails and names the swallowed trigger for a NEW negation not in the old hard-coded case list"
	else
		no "guard 2 failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
for target_wf in test.yml security-scan.yml mcp-schema-drift.yml; do
	cp "${repo_root}/.github/workflows/${target_wf}" "${tmp}/.github/workflows/${target_wf}"
done
if run_scratch >/dev/null 2>&1; then
	ok "guard 2 passes again after restoring all three filters"
else
	no "guard 2 should pass again once all three filters are restored"
fi

# --- negation_swallows_trigger LIVE shape proof (#5841 P1): the code: filter
# ALREADY carries a fourth negation shape today — the directory-recursive +
# extension-anchored hybrid '!.github/**/*.md' — that the prior
# negation_swallows_trigger only handled for two shapes ("<dir>/**" and a bare
# "*.md"), neither of which matches "<dir>/**/*.<ext>". Unlike the guard above
# (which injects a brand-new negation to prove the parser is generic), this
# case needs NO filter mutation: it gives an existing code-gated gate
# (go-fmt, ci.job "go-core" in test.yml) a trigger that the LIVE,
# already-committed '!.github/**/*.md' negation swallows whole, so this proves
# today's filter — not a hypothetical one — was silently unchecked. ---
python3 - "${tmp}/specs/ci-gates.v1.yaml" <<'PY'
import io,sys
p=sys.argv[1]
s=io.open(p).read()
i=s.index("  - id: go-fmt\n")
t='      - "go/**"\n'
j=s.index(t,i)+len(t)
io.open(p,"w").write(s[:j]+'      - ".github/workflows/generated-notes.md"\n'+s[j:])
PY
if out="$(run_scratch 2>&1)"; then
	no "guard 2 should fail when a code-gated gate's trigger is swallowed by the LIVE '!.github/**/*.md' negation"
else
	if rg -F "trigger '.github/workflows/generated-notes.md' is swallowed by the code filter's own negations" < <(printf '%s\n' "${out}") >/dev/null; then
		ok "guard 2 fails and names the swallowed trigger for the live '<dir>/**/*.<ext>' hybrid negation shape"
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

# --- merge_group (#5814): a required-status-check umbrella must report on the
# merge_group event too, or enabling a GitHub merge queue later would strand
# every queued PR waiting on checks that never fire. Four independent guards
# in verify-docs-only-ci-skip.sh cover the shape; each is mutated separately
# here, following the same scratch-copy-and-mutate pattern as guards 1/2 above
# instead of a hand-run proof, so a future edit that quietly breaks one cannot
# slip back in unnoticed. ---

# --- merge_group case 1: drop the on: trigger itself. ---
python3 - "${tmp}/.github/workflows/test.yml" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = "  merge_group:\n    types: [checks_requested]\n"
assert content.count(needle) == 1, "expected exactly one merge_group trigger block"
with open(path, "w") as f:
	f.write(content.replace(needle, "", 1))
PY
if out="$(run_scratch 2>&1)"; then
	no "merge_group case 1 should fail when on: drops the merge_group trigger"
else
	if rg -F 'test.yml on: block must add a merge_group trigger' < <(printf '%s\n' "${out}") >/dev/null; then
		ok "merge_group case 1 fails and names the missing on: trigger"
	else
		no "merge_group case 1 failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/.github/workflows/test.yml" "${tmp}/.github/workflows/test.yml"
if run_scratch >/dev/null 2>&1; then
	ok "merge_group case 1 passes again after restoring the on: trigger"
else
	no "merge_group case 1 should pass again once the on: trigger is restored"
fi

# --- merge_group case 2: drop the Filter changed paths step's skip guard, so
# it would run dorny/paths-filter's unproven merge_group behavior against this
# job's shallow fetch-depth: 2 checkout. ---
python3 - "${tmp}/.github/workflows/test.yml" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = (
	"      - name: Filter changed paths\n"
	"        id: filter\n"
	"        if: ${{ github.event_name != 'merge_group' }}\n"
	"        uses: dorny/paths-filter@v3\n"
)
assert content.count(needle) == 1, "expected exactly one guarded Filter changed paths step"
mutated = needle.replace("        if: ${{ github.event_name != 'merge_group' }}\n", "")
with open(path, "w") as f:
	f.write(content.replace(needle, mutated, 1))
PY
if out="$(run_scratch 2>&1)"; then
	no "merge_group case 2 should fail when the Filter changed paths step loses its merge_group skip guard"
else
	if rg -F "Filter changed paths step must guard if" < <(printf '%s\n' "${out}") >/dev/null; then
		ok "merge_group case 2 fails and names the missing skip guard"
	else
		no "merge_group case 2 failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/.github/workflows/test.yml" "${tmp}/.github/workflows/test.yml"
if run_scratch >/dev/null 2>&1; then
	ok "merge_group case 2 passes again after restoring the skip guard"
else
	no "merge_group case 2 should pass again once the skip guard is restored"
fi

# --- merge_group case 3: drop the merge_group_code step entirely, so the
# changes job would FAIL (not skip) on merge_group once the filter step also
# skips itself — exactly the false-green-turned-jam this fix closes. ---
python3 - "${tmp}/.github/workflows/test.yml" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = (
	"      - name: Force code=true on merge_group\n"
	"        id: merge_group_code\n"
	"        if: ${{ github.event_name == 'merge_group' }}\n"
	"        run: echo \"code=true\" >> \"$GITHUB_OUTPUT\"\n"
	"\n"
)
assert content.count(needle) == 1, "expected exactly one Force code=true on merge_group step"
with open(path, "w") as f:
	f.write(content.replace(needle, "", 1))
PY
if out="$(run_scratch 2>&1)"; then
	no "merge_group case 3 should fail when the merge_group_code step is removed"
else
	if rg -F "must have a single step (id: merge_group_code) that sets code=true" < <(printf '%s\n' "${out}") >/dev/null; then
		ok "merge_group case 3 fails and names the missing merge_group_code step"
	else
		no "merge_group case 3 failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/.github/workflows/test.yml" "${tmp}/.github/workflows/test.yml"
if run_scratch >/dev/null 2>&1; then
	ok "merge_group case 3 passes again after restoring the merge_group_code step"
else
	no "merge_group case 3 should pass again once the merge_group_code step is restored"
fi

# --- merge_group case 4: drop the outputs.code || fallback, so the job's
# output silently reverts to only the (skipped-on-merge_group) filter step. ---
python3 - "${tmp}/.github/workflows/test.yml" <<'PY'
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
needle = "      code: ${{ steps.filter.outputs.code || steps.merge_group_code.outputs.code }}\n"
assert content.count(needle) == 1, "expected exactly one changes job outputs.code fallback line"
mutated = "      code: ${{ steps.filter.outputs.code }}\n"
with open(path, "w") as f:
	f.write(content.replace(needle, mutated, 1))
PY
if out="$(run_scratch 2>&1)"; then
	no "merge_group case 4 should fail when outputs.code drops the merge_group_code fallback"
else
	if rg -F "must fall back to the merge_group step's output" < <(printf '%s\n' "${out}") >/dev/null; then
		ok "merge_group case 4 fails and names the missing outputs.code fallback"
	else
		no "merge_group case 4 failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/.github/workflows/test.yml" "${tmp}/.github/workflows/test.yml"
if run_scratch >/dev/null 2>&1; then
	ok "merge_group case 4 passes again after restoring the outputs.code fallback"
else
	no "merge_group case 4 should pass again once the outputs.code fallback is restored"
fi

# --- guard 3 (#5896), failing case: drop `predicate-quantifier: 'every'` from
# one workflow. This is the regression that reads as harmless: the five `!`
# negations below it still LOOK like they exclude docs, but dorny@v3 falls back
# to `some`, `**` matches first and short-circuits, and the whole filter becomes
# `code: ['**']`. Deleting this line is how #5818's ~118 wasted runner-minutes
# come back, so it must fail loudly and name the file. ---
python3 - "${tmp}/.github/workflows/security-scan.yml" <<'PY'
import re, sys
path = sys.argv[1]
content = open(path).read()
pat = re.compile(r"^[ \t]*predicate-quantifier:.*\n", re.MULTILINE)
assert len(pat.findall(content)) == 1, "one line expected"
open(path, "w").write(pat.sub("", content, count=1))
PY
if out="$(run_scratch 2>&1)"; then
	no "guard 3 should fail when a code: filter loses predicate-quantifier: 'every'"
else
	if rg -F 'predicate-quantifier' < <(printf '%s\n' "${out}") >/dev/null && rg -F 'security-scan.yml' < <(printf '%s\n' "${out}") >/dev/null; then
		ok "guard 3 fails and names the workflow that lost predicate-quantifier: 'every'"
	else
		no "guard 3 failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/.github/workflows/security-scan.yml" "${tmp}/.github/workflows/security-scan.yml"
if run_scratch >/dev/null 2>&1; then
	ok "guard 3 passes again after restoring predicate-quantifier: 'every'"
else
	no "guard 3 should pass again once predicate-quantifier: 'every' is restored"
fi

# --- guard 3, wrong-value case: dorny validates the input against exactly
# {'every','some'} and silently falls back to `some` for anything else, so a
# plausible-looking typo ('all', which is what the README's prose suggests) is
# indistinguishable from deleting the line. The check must be literal. ---
python3 - "${tmp}/.github/workflows/test.yml" <<'PY'
import re
import sys
path = sys.argv[1]
with open(path) as f:
	content = f.read()
pattern = re.compile(r"^([ \t]*)predicate-quantifier:.*\n", re.MULTILINE)
assert len(pattern.findall(content)) == 1, "one line expected"
open(path, "w").write(pattern.sub(lambda m: m.group(1) + "predicate-quantifier: 'all'\n", content, count=1))
PY
if out="$(run_scratch 2>&1)"; then
	no "guard 3 should fail for predicate-quantifier: 'all', which dorny treats as the default"
else
	if rg -F 'predicate-quantifier' < <(printf '%s\n' "${out}") >/dev/null && rg -F 'test.yml' < <(printf '%s\n' "${out}") >/dev/null; then
		ok "guard 3 rejects a wrong quantifier value, not just a missing line"
	else
		no "guard 3 wrong-value case failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/.github/workflows/test.yml" "${tmp}/.github/workflows/test.yml"
if run_scratch >/dev/null 2>&1; then
	ok "guard 3 passes again after restoring the correct quantifier value"
else
	no "guard 3 should pass again once the correct quantifier value is restored"
fi

# --- guard 3, quoting case (#5956 review): YAML reads every, 'every' and
# "every" identically and dorny accepts all three, so the guard must too.
# Demanding one style would fail a PR whose filter is in fact correct. ---
for style in "every" '"every"'; do
	python3 - "${tmp}/.github/workflows/test.yml" "${style}" <<'PYQ'
import re
import sys
path, style = sys.argv[1], sys.argv[2]
with open(path) as f:
	content = f.read()
pattern = re.compile(r"^([ \t]*)predicate-quantifier:.*\n", re.MULTILINE)
assert len(pattern.findall(content)) == 1, "expected exactly one predicate-quantifier line"
with open(path, "w") as f:
	f.write(pattern.sub(lambda m: m.group(1) + "predicate-quantifier: " + style + "\n", content, count=1))
PYQ
	if run_scratch >/dev/null 2>&1; then
		ok "guard 3 accepts predicate-quantifier: ${style}"
	else
		no "guard 3 must accept the valid quoting style ${style}"
	fi
	cp "${repo_root}/.github/workflows/test.yml" "${tmp}/.github/workflows/test.yml"
done

# --- guard 3, relocation case (#5956 review, codex): a file-wide search would
# still pass if the line were moved out of the paths-filter step, or if a second
# dorny step carried it while the `code` filter reverted to the default. Move it
# to a sibling step in the same job and the guard must still fail. ---
python3 - "${tmp}/.github/workflows/test.yml" <<'PYS'
import re, sys
path = sys.argv[1]
c = open(path).read()
pat = re.compile(r"^[ \t]*predicate-quantifier:.*\n", re.MULTILINE)
assert len(pat.findall(c)) == 1, "one line expected"
c = pat.sub("", c, count=1)
a = '        run: echo "code=true" >> "$GITHUB_OUTPUT"\n'
assert c.count(a) == 1, "anchor missing"
open(path, "w").write(c.replace(a, a + "        # predicate-quantifier: 'every'\n", 1))
PYS
if out="$(run_scratch 2>&1)"; then
	no "guard 3 should fail when the quantifier is moved out of the paths-filter step"
else
	if rg -F 'predicate-quantifier' < <(printf '%s\n' "${out}") >/dev/null && rg -F 'test.yml' < <(printf '%s\n' "${out}") >/dev/null; then
		ok "guard 3 is scoped to the filter step, not the file — a relocated line still fails"
	else
		no "guard 3 relocation case failed for the wrong reason; got:"
		printf '%s\n' "${out}" >&2
	fi
fi
cp "${repo_root}/.github/workflows/test.yml" "${tmp}/.github/workflows/test.yml"
if run_scratch >/dev/null 2>&1; then
	ok "guard 3 passes again after restoring the quantifier to the filter step"
else
	no "guard 3 should pass again once the quantifier is back in the filter step"
fi

printf '\n%d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
