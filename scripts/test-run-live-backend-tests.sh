#!/usr/bin/env bash
# Static test for scripts/run-live-backend-tests.sh (#6784 slice B).
# Exercises argument parsing (both --flag value and --flag=value forms),
# the pinned-image drift check against the compose files, the ledger
# target extraction, and the failure modes that must stay loud — all
# without Docker, via ESHU_LIVE_RUNNER_SELFTEST=1.
# Fast, credential-free, Docker-free, network-free.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/run-live-backend-tests.sh"
targets="${repo_root}/scripts/lib/live_backend_test_targets.py"
ledger="${repo_root}/specs/live-tests.v1.yaml"

fail() {
	printf 'test-run-live-backend-tests: %s\n' "$*" >&2
	exit 1
}

[[ "$(wc -l <"${BASH_SOURCE[0]}" | tr -d '[:space:]')" -lt 500 ]] ||
	fail "test script must stay under 500 lines"
[[ -x "${script}" ]] || fail "runner missing or not executable"
[[ -f "${targets}" ]] || fail "target extractor missing"
[[ -f "${ledger}" ]] || fail "ledger missing"
bash -n "${script}" || fail "runner fails bash -n"

probe() {
	ESHU_LIVE_RUNNER_SELFTEST=1 bash "${script}" "$@" 2>&1
}

# ── GREEN: defaults parse ────────────────────────────────────────────────
out="$(probe)" || fail "default invocation failed"
[[ "${out}" == *"backend=both tags=live_nornicdb_answer_truth keep=0 use_compose=1"* ]] ||
	fail "unexpected defaults: ${out}"

# ── GREEN: space form parses identically to equals form ──────────────────
out="$(probe --backend nornicdb --tags foo --keep --no-compose)" || fail "space-form args failed"
[[ "${out}" == *"backend=nornicdb tags=foo keep=1 use_compose=0"* ]] ||
	fail "unexpected space-form parse: ${out}"
out="$(probe --backend=nornicdb --tags=foo)" || fail "equals-form args failed"
[[ "${out}" == *"backend=nornicdb tags=foo keep=0 use_compose=1"* ]] ||
	fail "unexpected equals-form parse: ${out}"

# ── RED: unknown flag and bad backend stay loud ──────────────────────────
probe --bogus >/dev/null 2>&1 && fail "unknown argument passed"
probe --backend oracle >/dev/null 2>&1 && fail "bad backend passed"
probe --backend >/dev/null 2>&1 && fail "missing --backend value passed"

# ── RED: unexpected CI tag fails extraction, not silent green ─────────────
# (a ci row with a different build tag would make go test -run match zero
# tests and exit 0; the extractor rejects it so promotion stays a ledger edit)
seed_dir="$(mktemp -d)"
trap 'rm -rf "${seed_dir}"' EXIT
printf 'version: 1\nrows:\n  - file: go/cmd/golden-corpus-gate/graph_row_tokens_live_test.go\n    tag: bogus_future_tag\n    class: ci\n    reason: seeded RED for the tag guard\n' >"${seed_dir}/ledger.yaml"
tag_err="$(python3 "${targets}" "${seed_dir}/ledger.yaml" "${repo_root}" 2>&1)" && fail "wrong-tag ci row accepted"
[[ "${tag_err}" == *"unexpected tag"* ]] || fail "wrong-tag rejection names no tag: ${tag_err}"

# ── RED: extractor crash dies naming extraction, not "no targets" ─────────
# (mapfile succeeds even when the process substitution fails, so the runner
# must capture the extractor failure explicitly)
mkdir -p "${seed_dir}/fakebin"
printf '#!/usr/bin/env bash\nexit 1\n' >"${seed_dir}/fakebin/python3"
chmod +x "${seed_dir}/fakebin/python3"
extract_err="$(PATH="${seed_dir}/fakebin:${PATH}" ESHU_LIVE_RUNNER_SELFTEST=plan bash "${script}" --backend both 2>&1)" &&
	fail "extractor crash produced a plan"
[[ "${extract_err}" == *"could not extract live-test targets"* ]] ||
	fail "extractor crash misreported: ${extract_err}"

# ── Image pins match the canonical compose files (no silent drift) ───────
nornicdb_compose="$(probe | rg '^nornicdb_compose=' | cut -d= -f2-)"
neo4j_compose="$(probe | rg '^neo4j_compose=' | cut -d= -f2-)"
nornicdb_pin="$(rg '^    image: ' "${repo_root}/${nornicdb_compose}" | head -n 1 | sed 's/^    image: //')"
neo4j_pin="$(rg '^    image: ' "${repo_root}/${neo4j_compose}" | head -n 1 | sed 's/^    image: //')"
[[ -n "${nornicdb_pin}" && -n "${neo4j_pin}" ]] || fail "could not read image pins from live-backend compose files"
rg --fixed-strings --quiet -- "${nornicdb_pin}" "${repo_root}/docker-compose.yaml" ||
	fail "nornicdb image pin drifted from docker-compose.yaml: ${nornicdb_pin}"
rg --fixed-strings --quiet -- "${neo4j_pin}" "${repo_root}/docker-compose.neo4j.yml" ||
	fail "neo4j image pin drifted from docker-compose.neo4j.yml: ${neo4j_pin}"

# ── Targets: one line per CI ledger row, module-relative, named tests ────
mapfile -t lines < <(python3 "${targets}" "${ledger}" "${repo_root}")
[[ "${#lines[@]}" -gt 0 ]] || fail "no CI targets extracted"
for line in "${lines[@]}"; do
	# file|package|tests|backends; the tests field itself contains |
	# separators, so only the head (go/ path) and tail (pinned backends)
	# are asserted here.
	[[ "${line}" == go/* ]] || fail "malformed target line: ${line}"
	case "${line}" in
		*\|nornicdb|*\|neo4j|*\|both) ;;
		*) fail "target line pins no valid backends: ${line}" ;;
	esac
done
ci_rows="$(python3 - "${ledger}" <<'EOF'
import re, sys
print(sum(1 for _, cls in re.findall(r"^  - file: (\S+)\n    tag: .*\n    class: (\S+)\n", open(sys.argv[1]).read(), re.M) if cls == "ci"))
EOF
)"
[[ "${#lines[@]}" == "${ci_rows}" ]] ||
	fail "target count ${#lines[@]} != CI ledger rows ${ci_rows}"

# ── Plan: every CI row schedules on its pinned backends ─────────────────
# (guards the runs-loop field split: the tests field holds |-joined
# names, so a left-anchored split silently dropped multi-Test files)
mapfile -t plan < <(ESHU_LIVE_RUNNER_SELFTEST=plan bash "${script}" --backend both)
[[ "${#plan[@]}" == "38" ]] || fail "planned runs ${#plan[@]}, want 38 (22 nornicdb + 16 neo4j)"
mapfile -t plan_nornicdb < <(ESHU_LIVE_RUNNER_SELFTEST=plan bash "${script}" --backend nornicdb)
[[ "${#plan_nornicdb[@]}" == "22" ]] || fail "nornicdb planned runs ${#plan_nornicdb[@]}, want 22"
mapfile -t plan_neo4j < <(ESHU_LIVE_RUNNER_SELFTEST=plan bash "${script}" --backend neo4j)
[[ "${#plan_neo4j[@]}" == "16" ]] || fail "neo4j planned runs ${#plan_neo4j[@]}, want 16"
for multi in scoped_grant_live_test scoped_selector_live_test; do
	printf '%s\n' "${plan[@]}" | rg -q "^nornicdb\\|[^|]*\\|.*${multi}" || fail "${multi} missing from nornicdb plan"
	printf '%s\n' "${plan[@]}" | rg -q "^neo4j\\|[^|]*\\|.*${multi}" || fail "${multi} missing from neo4j plan"
done
for pinned in complexity_list_answer_truth nornicdb_incoming_anchor; do
	printf '%s\n' "${plan[@]}" | rg -q "^neo4j\\|[^|]*\\|.*${pinned}" &&
		fail "${pinned} scheduled on neo4j despite nornicdb pin"
done

printf 'test-run-live-backend-tests: static probes passed (%s targets, %s planned runs)\n' "${#lines[@]}" "${#plan[@]}"
