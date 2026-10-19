#!/usr/bin/env bash
# Self-test for scripts/verify-scoped-route-catalog-sweep.sh (epic #5161 row
# #5167): the judge half of the live full-catalog personal-token sweep must
# accept a clean report and reject planted violations, without Docker, a
# stack, or network. Fast, credential-free, hermetic: ESHU_SWEEP_JUDGE_ONLY=1
# points the script at fixture report/policy JSON instead of booting
# docker-compose.e2e.yaml.
#
# RED/GREEN pairs:
#   GREEN: 2 allowlisted rows (strict ok, tolerant not_found), 1 shared_key_only
#     disclosed refusal, 1 pending_row_filtering disclosed refusal -> exit 0.
#     The pending row proves the judge reads classes from the Go-derived
#     policy instead of a hand-maintained "2 shared-key-only" list.
#   RED a: an allowlisted row answering route_denied_403 (undocumented 403).
#   RED b: a ledger row refused without disclosure.
#   RED c: a dropped result row (coverage mismatch with the policy).
#   RED d: a JSON null actual FAILs naming the row instead of raising.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-scoped-route-catalog-sweep.sh"

fail() {
	printf 'test-verify-scoped-route-catalog-sweep: %s\n' "$*" >&2
	exit 1
}

[[ "$(wc -l <"${BASH_SOURCE[0]}" | tr -d '[:space:]')" -lt 500 ]] ||
	fail "test script must stay under 500 lines"
[[ -x "${script}" ]] || fail "verify script missing or not executable"

fx="$(mktemp -d)"
trap 'rm -rf "${fx}"' EXIT

write_fixtures() {
	python3 "${repo_root}/scripts/write-sweep-judge-fixtures.py" "$fx"
}

judge() {
	ESHU_SWEEP_JUDGE_ONLY=1 \
		ESHU_SWEEP_REPORT_JSON="$fx/report.json" \
		ESHU_SWEEP_POLICY_JSON="$fx/policy.json" \
		"${script}" 2>"$fx/stderr.txt"
}

write_fixtures

# ── GREEN: the clean fixture passes with the documented counts ─────────────
out="$(judge)"; rc=$?
[[ $rc -eq 0 ]] || fail "judge rejected the clean fixture (exit ${rc}): $(cat "$fx/stderr.txt")"
[[ "${out}" =~ verify-scoped-route-catalog-sweep:\ PASS\ exercised=4\ routes=4\ refused=2\ strict=1\ tolerant=1\ errors=0 ]] ||
	fail "unexpected judge output: ${out}"

# ── RED a: an allowlisted row answering the route-policy 403 ────────────────
python3 - "$fx" <<'EOF'
import json, sys
fx = sys.argv[1]
rep = json.load(open(fx + "/report.json"))
rep["results"][1]["actual"] = "route_denied_403 (disclosed)"
rep["results"][1]["pass"] = False
open(fx + "/report.json", "w").write(json.dumps(rep))
EOF
rc=0; out="$(judge)" || rc=$?
[[ $rc -ne 0 ]] || fail "judge accepted an undocumented 403 on an allowlisted row: ${out}"
[[ "${out}$(cat "$fx/stderr.txt")" =~ get_entity_context ]] ||
	fail "rejection names no offending tool: ${out}"

# ── RED b: a ledger refusal without disclosure ─────────────────────────────
write_fixtures
python3 - "$fx" <<'EOF'
import json, sys
fx = sys.argv[1]
rep = json.load(open(fx + "/report.json"))
rep["results"][2]["actual"] = "route_denied_403 (undisclosed)"
rep["results"][2]["pass"] = False
open(fx + "/report.json", "w").write(json.dumps(rep))
EOF
rc=0; out="$(judge)" || rc=$?
[[ $rc -ne 0 ]] || fail "judge accepted an undisclosed ledger refusal: ${out}"

# ── RED c: a result row dropped (coverage mismatch) ─────────────────────────
write_fixtures
python3 - "$fx" <<'EOF'
import json, sys
fx = sys.argv[1]
rep = json.load(open(fx + "/report.json"))
rep["results"] = rep["results"][:3]
open(fx + "/report.json", "w").write(json.dumps(rep))
EOF
rc=0; out="$(judge)" || rc=$?
[[ $rc -ne 0 ]] || fail "judge accepted a report missing a policy row: ${out}"

# ── RED d: a malformed row (JSON null actual) FAILs naming the row ──────────
# A null actual must not raise AttributeError/TypeError out of the gate.
write_fixtures
python3 - "$fx" <<'EOF'
import json, sys
fx = sys.argv[1]
rep = json.load(open(fx + "/report.json"))
rep["results"][0]["actual"] = None
rep["results"][0]["pass"] = False
open(fx + "/report.json", "w").write(json.dumps(rep))
EOF
rc=0; out="$(judge)" || rc=$?
[[ $rc -ne 0 ]] || fail "judge accepted a row with null actual: ${out}"
combined="${out}$(cat "$fx/stderr.txt")"
[[ "${combined}" =~ list_indexed_repositories ]] ||
	fail "rejection names no offending tool: ${combined}"
[[ "${combined}" != *"Traceback"* ]] ||
	fail "judge raised instead of FAILing cleanly: ${combined}"

printf 'test-verify-scoped-route-catalog-sweep: PASS (1 green, 4 red pairs)\n'
