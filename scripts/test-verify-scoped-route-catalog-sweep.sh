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
	python3 - "$fx" <<'EOF'
import json, sys
fx = sys.argv[1]
policy = {"disclosurePattern": "\\b403\\b", "disclosureFlags": "i", "rows": [
    {"tool": "list_indexed_repositories", "label": "default",
     "method": "GET", "path": "/api/v0/repositories", "class": "allowlisted",
     "arguments": {}, "accept": ["ok"]},
    {"tool": "get_entity_context", "label": "default",
     "method": "GET", "path": "/api/v0/entities/missing/context",
     "class": "allowlisted", "arguments": {}, "accept": ["ok", "not_found"]},
    {"tool": "execute_cypher_query", "label": "default",
     "method": "POST", "path": "/api/v0/code/cypher", "class": "shared_key_only",
     "arguments": {}, "accept": []},
    {"tool": "get_service_changed_since", "label": "default",
     "method": "GET", "path": "/api/v0/freshness/services/changed-since",
     "class": "pending_row_filtering", "arguments": {}, "accept": []},
]}
report = {"tools": 4, "calls": 4, "results": [
    {"tool": "list_indexed_repositories", "label": "default",
     "route": "GET /api/v0/repositories", "expected": "success (ok)",
     "actual": "ok", "pass": True, "detail": ""},
    {"tool": "get_entity_context", "label": "default",
     "route": "GET /api/v0/entities/missing/context",
     "expected": "success (ok|not_found)", "actual": "not_found",
     "pass": True, "detail": ""},
    {"tool": "execute_cypher_query", "label": "default",
     "route": "POST /api/v0/code/cypher", "expected": "403 disclosed (shared_key_only)",
     "actual": "route_denied_403 (disclosed)", "pass": True, "detail": "refused"},
    {"tool": "get_service_changed_since", "label": "default",
     "route": "GET /api/v0/freshness/services/changed-since",
     "expected": "403 disclosed (pending_row_filtering)",
     "actual": "route_denied_403 (disclosed)", "pass": True, "detail": "refused"},
]}
open(fx + "/policy.json", "w").write(json.dumps(policy))
open(fx + "/report.json", "w").write(json.dumps(report))
EOF
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

printf 'test-verify-scoped-route-catalog-sweep: PASS (1 green, 3 red pairs)\n'
