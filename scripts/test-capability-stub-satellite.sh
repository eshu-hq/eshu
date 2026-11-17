#!/usr/bin/env bash
# test-capability-stub-satellite.sh — behavioral test for the offline double
# harness in scripts/lib/capability-stub-satellite.sh.
#
# Vectors: the stub gates GET /api/v0/openapi.json behind the bearer (200
# with the configured token, 401 with a wrong or missing one), 404s anything
# else, serves a JSON document with a paths table; the wrapper exports both
# satellite variables to its child, passes a live ESHU_API_BASE_URL through
# untouched (never shadowing an operator backend), honors a preset
# ESHU_API_KEY end to end, propagates the child exit status, and rejects an
# empty invocation.
#
# The stub lives only for its wrapper child, so every HTTP probe runs inside
# a wrapper child that records results to a file the test asserts on after
# the wrapper (and its server) exits.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
harness="${repo_root}/scripts/lib/capability-stub-satellite.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
fail() {
	FAIL=$((FAIL + 1))
	printf 'not ok - %s\n' "$1"
	[[ $# -gt 1 ]] && printf '  detail: %s\n' "$2"
}

# probe_child.sh runs under the wrapper: it records the exported satellite
# variables plus one curl per RequireAuth/document-shape vector into
# RESULT_FILE, so assertions below survive the server's exit.
cat >"${tmp_root}/probe_child.sh" <<'PROBE_EOF'
#!/usr/bin/env bash
set -u
printf 'base_url=%s\n' "${ESHU_API_BASE_URL}" >>"${RESULT_FILE}"
printf 'api_key=%s\n' "${ESHU_API_KEY}" >>"${RESULT_FILE}"
code="$(curl -s -o "${TMP_ROOT}/doc.json" -w '%{http_code}' \
	-H "Authorization: Bearer ${ESHU_API_KEY}" \
	"${ESHU_API_BASE_URL}/api/v0/openapi.json" || true)"
printf 'tokened=%s\n' "${code}" >>"${RESULT_FILE}"
PROBE_EOF
cat >>"${tmp_root}/probe_child.sh" <<'PROBE_EOF2'
code="$(curl -s -o /dev/null -w '%{http_code}' \
	"${ESHU_API_BASE_URL}/api/v0/openapi.json" || true)"
printf 'untokened=%s\n' "${code}" >>"${RESULT_FILE}"
code="$(curl -s -o /dev/null -w '%{http_code}' \
	-H "Authorization: Bearer wrong-token" \
	"${ESHU_API_BASE_URL}/api/v0/openapi.json" || true)"
printf 'wrongtoken=%s\n' "${code}" >>"${RESULT_FILE}"
PROBE_EOF2
cat >>"${tmp_root}/probe_child.sh" <<'PROBE_EOF3'
code="$(curl -s -o /dev/null -w '%{http_code}' \
	-H "Authorization: Bearer ${ESHU_API_KEY}" \
	"${ESHU_API_BASE_URL}/nope" || true)"
printf 'wrongpath=%s\n' "${code}" >>"${RESULT_FILE}"
PROBE_EOF3
chmod +x "${tmp_root}/probe_child.sh"

# run_probe [preset-key]: one wrapper run with ESHU_API_BASE_URL unset (and
# optionally a preset ESHU_API_KEY), leaving RESULT_FILE + doc.json behind.
run_probe() {
	local preset_key="${1:-}"
	rm -f "${tmp_root}/results" "${tmp_root}/doc.json"
	if [[ -n "${preset_key}" ]]; then
		RESULT_FILE="${tmp_root}/results" TMP_ROOT="${tmp_root}" \
			env -u ESHU_API_BASE_URL "ESHU_API_KEY=${preset_key}" \
			bash "${harness}" bash "${tmp_root}/probe_child.sh" \
			2>"${tmp_root}/stderr"
	else
		RESULT_FILE="${tmp_root}/results" TMP_ROOT="${tmp_root}" \
			env -u ESHU_API_BASE_URL -u ESHU_API_KEY \
			bash "${harness}" bash "${tmp_root}/probe_child.sh" \
			2>"${tmp_root}/stderr"
	fi
}

result() { grep "^$1=" "${tmp_root}/results" | cut -d= -f2-; }

# --- 1-4. default dummy token: exports, RequireAuth shape, document shape.
if run_probe; then
	base_url="$(result base_url)"
	api_key="$(result api_key)"
	case "${base_url}" in http://127.0.0.1:*) pass "default exports loopback stub URL (got ${base_url})" ;; *)
		fail "default exports loopback stub URL" "got ${base_url}"
		;;
	esac
	[[ "${api_key}" == "admin-all-scope-token" ]] \
		&& pass "default exports the dummy bearer" \
		|| fail "default exports the dummy bearer" "got ${api_key}"
	if [[ "$(result tokened)" == "200" ]] && grep -q '"paths"' "${tmp_root}/doc.json" \
		&& grep -q '"/api/v0/repositories"' "${tmp_root}/doc.json"; then
		pass "tokened fetch returns the OpenAPI document"
	else
		fail "tokened fetch returns the OpenAPI document" "tokened=$(result tokened)"
	fi
	[[ "$(result untokened)" == "401" ]] && pass "untokened fetch 401s" \
		|| fail "untokened fetch 401s" "got $(result untokened)"
	[[ "$(result wrongtoken)" == "401" ]] && pass "wrong bearer 401s" \
		|| fail "wrong bearer 401s" "got $(result wrongtoken)"
	[[ "$(result wrongpath)" == "404" ]] && pass "unknown path 404s" \
		|| fail "unknown path 404s" "got $(result wrongpath)"
else
	for label in "default exports loopback stub URL" "default exports the dummy bearer" \
		"tokened fetch returns the OpenAPI document" "untokened fetch 401s" \
		"wrong bearer 401s" "unknown path 404s"; do
		fail "${label}" "wrapper run failed; see stderr"
	done
fi

# --- 5. preset ESHU_API_KEY is honored end to end (stub expects it, child sends it).
if run_probe "custom-key-1"; then
	if [[ "$(result api_key)" == "custom-key-1" && "$(result tokened)" == "200" ]]; then
		pass "preset key works end to end"
	else
		fail "preset key works end to end" "key=$(result api_key) tokened=$(result tokened)"
	fi
else
	fail "preset key works end to end" "wrapper run failed"
fi

# --- 6. live satellite passthrough: preset base URL is never shadowed.
passthrough_out="$(ESHU_API_BASE_URL="https://live.example.invalid" ESHU_API_KEY="live-key" \
	bash "${harness}" bash -c 'printf "%s\n%s\n" "${ESHU_API_BASE_URL}" "${ESHU_API_KEY}"')"
if [[ "${passthrough_out}" == "$(printf 'https://live.example.invalid\nlive-key')" ]]; then
	pass "preset base URL passes through untouched"
else
	fail "preset base URL passes through untouched" "got ${passthrough_out}"
fi

# --- 7. exit status propagation.
status=0
env -u ESHU_API_BASE_URL -u ESHU_API_KEY bash "${harness}" true || status=$?
[[ "${status}" == "0" ]] && pass "child status 0 propagates" \
	|| fail "child status 0 propagates" "status=${status}"
status=0
env -u ESHU_API_BASE_URL -u ESHU_API_KEY bash "${harness}" bash -c 'exit 3' || status=$?
[[ "${status}" == "3" ]] && pass "child status 3 propagates" \
	|| fail "child status 3 propagates" "status=${status}"

# --- 8. empty invocation is a usage error.
if bash "${harness}" >/dev/null 2>&1; then
	fail "empty invocation fails" "harness exited 0"
else
	pass "empty invocation fails"
fi

printf 'stub-satellite harness: %d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" == "0" ]]
