#!/usr/bin/env bash
#
# test-regen-conflicts.sh - prove scripts/dev/regen-conflicts.sh regenerates
# conflicted generated files from the gate registry's regenerate: map and
# stages them, without touching uncovered conflicts. Hermetic: bash, git, awk,
# sort, cmp, rg only — no Go toolchain, no Postgres, no network.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/dev/regen-conflicts.sh"

command -v rg >/dev/null 2>&1 || {
	echo 'test-regen-conflicts: rg is required' >&2
	exit 1
}

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT
tab="$(printf '\t')"

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# Case 1: the helper exists and prints its usage.
if [[ -x "${helper}" ]] && "${helper}" --help >/dev/null 2>&1; then
	record_pass "helper exists, is executable, and prints usage"
else
	record_fail "helper is missing, not executable, or has no usage"
fi

# Case 2: --print-map on a fixture registry yields the exact TSV map.
fixture="${tmp_root}/fixture"
mkdir -p "${fixture}/specs/ci-gates.d"
printf '%s\n' 'version: v1' 'gate_fragments:' '  - ci-gates.d/a.yaml' '  - ci-gates.d/b.yaml' \
	>"${fixture}/specs/ci-gates.v1.yaml"
printf '%s\n' 'version: v1' 'gates:' \
	'  - id: doc-gate' '    name: Doc' '    category: hygiene' '    tier: pre-pr' \
	'    blocking: true' '    triggers:' '      - "gen/out.md"' '    regenerate:' \
	'      "gen/out.md": "bash scripts/gen-out.sh"' \
	>"${fixture}/specs/ci-gates.d/a.yaml"
printf '%s\n' 'version: v1' 'gates:' \
	'  - id: multi-gate' '    name: Multi' '    category: hygiene' '    tier: pre-pr' \
	'    blocking: true' '    triggers:' '      - "gen/a/x.py"' '    regenerate:' \
	'      "gen/a/x.py": "python3 scripts/gen-a.py"' \
	'      "gen/b/*.py": "python3 scripts/gen-a.py"' \
	>"${fixture}/specs/ci-gates.d/b.yaml"
map_out="${tmp_root}/map.tsv"
if REPO_ROOT="${fixture}" "${helper}" --print-map >"${map_out}" 2>/dev/null; then
	printf 'gen/a/x.py\tpython3 scripts/gen-a.py\ngen/b/*.py\tpython3 scripts/gen-a.py\ngen/out.md\tbash scripts/gen-out.sh\n' \
		>"${tmp_root}/map.want"
	if cmp -s "${map_out}" "${tmp_root}/map.want"; then
		record_pass "--print-map extracts the exact path-to-command map"
	else
		record_fail "--print-map output differs from the fixture registry map"
	fi
else
	record_fail "--print-map failed on the fixture registry"
fi

# Case 3: --print-map on the real registry carries every covered artifact.
real_map="${tmp_root}/real-map.tsv"
if "${helper}" --print-map >"${real_map}" 2>/dev/null; then
	missing=0
	for want_path in 'docs/public/reference/ci-gates.md' \
		'docs/public/reference/env-registry.md' \
		'docs/public/reference/env-registry/*.md' \
		'specs/live-tests.v1.yaml' \
		'specs/live-tests.d/live-tests-readiness.generated.yaml' \
		'scripts/lib/live_postgres_readiness_inventory_generated.py' \
		'scripts/lib/live_postgres_readiness_inventory_gen_*.py' \
		'scripts/lib/live_postgres_readiness_inventory_helpers.py' \
		'scripts/lib/live_postgres_readiness_env.sh' \
		'.github/workflows/live-postgres-readiness.yml' \
		'go/internal/collector/repo/git/epoch/ledger.tsv'; do
		if ! awk -F"${tab}" '$1==p{found=1} END{exit !found}' p="${want_path}" "${real_map}"; then
			missing=$((missing + 1))
			printf '  missing map entry: %s\n' "${want_path}" >&2
		fi
	done
	if [[ "${missing}" -eq 0 ]]; then
		record_pass "--print-map on the live registry covers every artifact"
	else
		record_fail "${missing} covered artifact(s) missing from the live map"
	fi
else
	record_fail "--print-map failed on the live registry"
fi

# Case 4: the readiness keys match the drift script's GENERATED list, so the
# registry copy cannot drift from the generator's own list silently.
drift_script="${repo_root}/scripts/verify-live-postgres-readiness-drift.sh"
gen_list="${tmp_root}/generated.txt"
sed -n '/^GENERATED=(/,/^)/p' "${drift_script}" | sed '1d;$d' | tr -d ' ' | LC_ALL=C sort >"${gen_list}"
regen_list="${tmp_root}/regen-readiness.txt"
awk -F"${tab}" '$1 ~ /live-tests|live_postgres_readiness|live-postgres-readiness/{print $1}' "${real_map}" \
	| LC_ALL=C sort >"${regen_list}"
if cmp -s "${gen_list}" "${regen_list}"; then
	record_pass "registry readiness keys match the drift script GENERATED list"
else
	record_fail "registry readiness keys drifted from the drift script GENERATED list"
fi

# Case 5: end to end on a fixture repo — a covered conflict regenerates and
# stages, an uncovered conflict is left alone and named, and the exit codes
# tell the two states apart.
stage="${tmp_root}/stage"
mkdir -p "${stage}/specs/ci-gates.d" "${stage}/scripts" "${stage}/gen"
printf '%s\n' 'version: v1' 'gate_fragments:' '  - ci-gates.d/a.yaml' \
	>"${stage}/specs/ci-gates.v1.yaml"
printf '%s\n' 'version: v1' 'gates:' \
	'  - id: doc-gate' '    name: Doc' '    category: hygiene' '    tier: pre-pr' \
	'    blocking: true' '    triggers:' '      - "gen/out.md"' '    regenerate:' \
	'      "gen/out.md": "bash scripts/gen-out.sh"' \
	>"${stage}/specs/ci-gates.d/a.yaml"
printf '%s\n' '#!/usr/bin/env bash' \
	'printf "regen-base=%s run=%s\\n" "${ESHU_REGEN_BASE:?unset}" "$(cat scripts/runcount 2>/dev/null || echo 0)" > gen/out.md' \
	'printf "%s\\n" "$(( $(cat scripts/runcount 2>/dev/null || echo 0) + 1 ))" > scripts/runcount' \
	>"${stage}/scripts/gen-out.sh"
chmod +x "${stage}/scripts/gen-out.sh"
printf '0\n' >"${stage}/scripts/runcount"
git -C "${stage}" init -q
git -C "${stage}" config user.email t@t
git -C "${stage}" config user.name t
git -C "${stage}" add -A
git -C "${stage}" commit -qm base
base_sha="$(git -C "${stage}" rev-parse HEAD)"
git -C "${stage}" checkout -qb side-a
printf 'side a\n' >"${stage}/gen/out.md"
git -C "${stage}" add gen/out.md
git -C "${stage}" commit -qm a
git -C "${stage}" checkout -qb side-b "${base_sha}"
mkdir -p "${stage}/gen"
printf 'side b\n' >"${stage}/gen/out.md"
git -C "${stage}" add gen/out.md
git -C "${stage}" commit -qm b
git -C "${stage}" checkout -q side-a
if git -C "${stage}" merge side-b >/dev/null 2>&1; then
	record_fail "fixture setup did not produce a conflict"
else
	if REPO_ROOT="${stage}" "${helper}" --base side-b >"${tmp_root}/run1.log" 2>&1; then
		if git -C "${stage}" diff --name-only --diff-filter=U | rg -q .; then
			record_fail "helper exited 0 with unmerged paths remaining"
		elif rg -q '^regen-base=side-b run=0$' "${stage}/gen/out.md" \
			&& git -C "${stage}" diff --cached --name-only | rg -qx 'gen/out.md'; then
			record_pass "covered conflict regenerates with the base and stages"
		else
			record_fail "covered conflict was not regenerated and staged"
		fi
	else
		record_fail "helper failed on a covered-only conflict"
	fi
fi

# Case 6: rerunning with nothing conflicted exits 0 and changes nothing.
before="$(git -C "${stage}" status --porcelain)"
if REPO_ROOT="${stage}" "${helper}" >"${tmp_root}/run2.log" 2>&1 \
	&& [[ "$(git -C "${stage}" status --porcelain)" == "${before}" ]] \
	&& [[ "$(cat "${stage}/scripts/runcount")" == "1" ]]; then
	record_pass "clean rerun exits 0 without rerunning generators"
else
	record_fail "clean rerun changed the tree or reran a generator"
fi
git -C "${stage}" merge --abort >/dev/null 2>&1 || true

# Case 7: an uncovered conflict survives with a nonzero exit that names it.
git -C "${stage}" checkout -q side-a
printf 'plain a\n' >"${stage}/gen/plain.md"
git -C "${stage}" add gen/plain.md
git -C "${stage}" commit -qm plain-a
git -C "${stage}" checkout -qb side-c
printf 'plain c\n' >"${stage}/gen/plain.md"
git -C "${stage}" commit -qam plain-c
git -C "${stage}" checkout -q side-a
printf 'plain a2\n' >"${stage}/gen/plain.md"
git -C "${stage}" commit -qam plain-a2
git -C "${stage}" merge side-c >/dev/null 2>&1 || true
if REPO_ROOT="${stage}" "${helper}" >"${tmp_root}/run3.log" 2>&1; then
	record_fail "helper exited 0 with an uncovered conflict remaining"
elif git -C "${stage}" diff --name-only --diff-filter=U | rg -qx 'gen/plain.md' \
	&& rg -q 'gen/plain.md' "${tmp_root}/run3.log"; then
	record_pass "uncovered conflict is left unmerged and named"
else
	record_fail "uncovered conflict was touched or not reported"
fi
git -C "${stage}" merge --abort >/dev/null 2>&1 || true

# Case 8: no regenerate value in the live registry needs a YAML escape. The
# helper parses raw fragment text while Go's Load sees the decoded value, so
# a backslash escape would run one command locally and validate another.
escape_hits="$(awk '/^    regenerate:/{in_regen=1; next} /^    [a-z_]+:/{in_regen=0; next} in_regen && /\\/{print FILENAME ":" $0}' "${repo_root}"/specs/ci-gates.d/*.yaml || true)"
if [[ -z "${escape_hits}" ]]; then
	record_pass "no regenerate value needs a YAML escape"
else
	printf '  escaped regenerate lines:\n%s\n' "${escape_hits}" >&2
	record_fail "regenerate values carry backslash escapes the helper would misread"
fi

if [[ "${FAIL}" -ne 0 ]]; then
	printf 'test-regen-conflicts FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
	exit 1
fi

printf 'test-regen-conflicts passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
