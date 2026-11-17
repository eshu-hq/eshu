#!/usr/bin/env bash
# test-verify-npm-audit-exceptions.sh — contract mirror for
# verify-npm-audit-exceptions.sh. Feeds seeded `npm audit --json`
# fixtures (no network, no install) and asserts the gate fails closed:
# only the single allowed picomatch GHSA-fc5q-39xc-vjpf high passes
# (including string-only inheritance chains); a second high, a critical,
# a moderate node carrying a critical advisory, an advisory without a
# URL or the wrong name, an npm error report, a missing/null
# vulnerabilities object, and dangling references all fail.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-npm-audit-exceptions.sh"

fail=0
note() { printf 'test-verify-npm-audit-exceptions: %s\n' "$*" >&2; }
check() { if [[ "$2" -ne 0 ]]; then note "FAIL: $1"; fail=1; fi; }

if [[ ! -f "${script}" ]]; then note "FAIL: verify-npm-audit-exceptions.sh missing"; fail=1; fi
if [[ ! -x "${script}" ]]; then note "FAIL: verify-npm-audit-exceptions.sh not executable"; fail=1; fi
bash -n "${script}"; check "wrapper syntax" $?

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# GREEN fixture: only the allowed GHSA high, plus a moderate (ignored).
cat >"${tmp}/green.json" <<'EOF'
{"vulnerabilities": {
  "picomatch": {"severity": "high", "via": [{"name": "picomatch", "url": "https://github.com/advisories/GHSA-fc5q-39xc-vjpf", "severity": "high"}]},
  "minimatch": {"severity": "moderate", "via": [{"name": "minimatch", "url": "https://github.com/advisories/GHSA-g3hv-75wg-m249", "severity": "moderate"}]}
}}
EOF
# RED fixture 1: a second, different high GHSA (all strings resolvable so
# the failure comes from the advisory check, not the dangling guard).
cat >"${tmp}/red-second-high.json" <<'EOF'
{"vulnerabilities": {
  "picomatch": {"severity": "high", "via": [{"name": "picomatch", "url": "https://github.com/advisories/GHSA-fc5q-39xc-vjpf", "severity": "high"}]},
  "vite": {"severity": "high", "via": ["picomatch"]},
  "left-pad": {"severity": "high", "via": [{"name": "left-pad", "url": "https://github.com/advisories/GHSA-example-second-high", "severity": "high"}]}
}}
EOF
# RED fixture 2: a critical.
cat >"${tmp}/red-critical.json" <<'EOF'
{"vulnerabilities": {
  "tinypool": {"severity": "critical", "via": [{"name": "tinypool", "url": "https://github.com/advisories/GHSA-example-critical", "severity": "critical"}]}
}}
EOF
# RED fixture 3: a high whose advisory object names no URL (fail closed).
cat >"${tmp}/red-no-url.json" <<'EOF'
{"vulnerabilities": {
  "mystery": {"severity": "high", "via": ["some-dep", {"name": "mystery", "severity": "high"}]},
  "some-dep": {"severity": "low", "via": []}
}}
EOF
# RED fixture 4: a moderate node carrying a critical advisory for another GHSA.
cat >"${tmp}/red-moderate-critical-dict.json" <<'EOF'
{"vulnerabilities": {
  "left-pad": {"severity": "moderate", "via": [{"name": "left-pad", "url": "https://github.com/advisories/GHSA-example-critical", "severity": "critical"}]}
}}
EOF
# RED fixture 5: an npm error report (e.g. registry failure) fails closed.
cat >"${tmp}/red-error-doc.json" <<'EOF'
{"error": {"code": "ENOLOCK", "summary": "No lockfile found"}}
EOF
# RED fixture 6: a missing vulnerabilities object fails closed.
cat >"${tmp}/red-no-vulns.json" <<'EOF'
{"auditReportVersion": 2, "metadata": {}}
EOF
# RED fixture 7: a null vulnerabilities object fails closed.
cat >"${tmp}/red-null-vulns.json" <<'EOF'
{"vulnerabilities": null}
EOF
# RED fixture 8: a high with a dangling string reference fails closed.
cat >"${tmp}/red-dangling.json" <<'EOF'
{"vulnerabilities": {
  "mystery": {"severity": "high", "via": ["ghost-dep"]}
}}
EOF
# RED fixture 9: picomatch with a *different* GHSA fails (name allowlists don't).
cat >"${tmp}/red-picomatch-other-ghsa.json" <<'EOF'
{"vulnerabilities": {
  "picomatch": {"severity": "high", "via": [{"name": "picomatch", "url": "https://github.com/advisories/GHSA-example-other", "severity": "high"}]}
}}
EOF
# GREEN fixture 2: string-only inheritance (the dominant real shape: highs
# that name only dependency names inherit the checked picomatch advisory).
cat >"${tmp}/green-inherited.json" <<'EOF'
{"vulnerabilities": {
  "picomatch": {"severity": "high", "via": [{"name": "picomatch", "url": "https://github.com/advisories/GHSA-fc5q-39xc-vjpf", "severity": "high"}]},
  "vite": {"severity": "high", "via": ["picomatch"]},
  "tinyglobby": {"severity": "high", "via": ["fdir", "picomatch"]},
  "fdir": {"severity": "high", "via": ["picomatch"]}
}}
EOF
# RED fixture 10: a dependency cycle fails closed.
cat >"${tmp}/red-cycle.json" <<'EOF'
{"vulnerabilities": {
  "aaa": {"severity": "high", "via": ["bbb"]},
  "bbb": {"severity": "high", "via": ["aaa"]}
}}
EOF
# RED fixture 11: a high with an empty via list fails closed.
cat >"${tmp}/red-empty-via.json" <<'EOF'
{"vulnerabilities": {
  "mystery": {"severity": "high", "via": []}
}}
EOF
# RED fixture 12: a critical node whose only resolved advisory is the
# allowed high fails closed (severity coverage, not mere existence).
cat >"${tmp}/red-critical-via-high.json" <<'EOF'
{"vulnerabilities": {
  "picomatch": {"severity": "high", "via": [{"name": "picomatch", "url": "https://github.com/advisories/GHSA-fc5q-39xc-vjpf", "severity": "high"}]},
  "tinypool": {"severity": "critical", "via": ["picomatch"]}
}}
EOF
# RED fixture 13: a high whose advisory dict has no severity fails closed.
cat >"${tmp}/red-no-severity-dict.json" <<'EOF'
{"vulnerabilities": {
  "mystery": {"severity": "high", "via": [{"name": "mystery", "url": "https://github.com/advisories/GHSA-example-x"}]}
}}
EOF
# GREEN fixture 3: info severity is ignored, not a failure.
cat >"${tmp}/green-info.json" <<'EOF'
{"vulnerabilities": {
  "picomatch": {"severity": "high", "via": [{"name": "picomatch", "url": "https://github.com/advisories/GHSA-fc5q-39xc-vjpf", "severity": "high"}]},
  "tinyglobby": {"severity": "info", "via": ["picomatch"]}
}}
EOF
printf 'not-valid-json' >"${tmp}/red-malformed.json"

"${script}" . "${tmp}/green.json" >/dev/null 2>&1
check "allowed-GHSA-only fixture passes" $?

if "${script}" . "${tmp}/red-second-high.json" >/dev/null 2>&1; then
	note "FAIL: second-high fixture passes (must fail)"; fail=1
fi

if "${script}" . "${tmp}/red-critical.json" >/dev/null 2>&1; then
	note "FAIL: critical fixture passes (must fail)"; fail=1
fi

expect_fail() {
	local fixture="$1" want_token="$2"
	local out rc=0
	out="$("${script}" . "${tmp}/${fixture}.json" 2>&1)" || rc=$?
	if [[ "${rc}" -eq 0 ]]; then
		note "FAIL: ${fixture} fixture passes (must fail)"; fail=1; return
	fi
	if [[ -n "${want_token}" ]] && ! grep -qF "${want_token}" <<<"${out}"; then
		note "FAIL: ${fixture} output misses '${want_token}' (must name the advisory)"; fail=1
	fi
}
# Advisory violations must fail naming the expected advisory token.
expect_fail red-second-high "GHSA-example-second-high"
expect_fail red-critical "GHSA-example-critical"
expect_fail red-no-url "<no-advisory-url>"
expect_fail red-moderate-critical-dict "GHSA-example-critical"
expect_fail red-picomatch-other-ghsa "GHSA-example-other"
expect_fail red-empty-via "<no-via-entries>"
expect_fail red-critical-via-high "<severity-not-covered-by-resolved-advisories>"
expect_fail red-no-severity-dict "<unclassifiable-severity>"
# Structural failures (bad input shape, cycles, dangling refs, missing
# report) must fail with a non-zero exit; their message names the shape
# problem instead of the FAIL advisory listing.
for fixture in red-dangling red-error-doc red-no-vulns red-null-vulns red-malformed red-cycle; do
	expect_fail "${fixture}" ""
done

if ! "${script}" . "${tmp}/green-inherited.json" >/dev/null 2>&1; then
	note "FAIL: string-inheritance green fixture fails (must pass)"; fail=1
fi

if ! "${script}" . "${tmp}/green-info.json" >/dev/null 2>&1; then
	note "FAIL: info-severity green fixture fails (must pass)"; fail=1
fi

if [[ "${fail}" -ne 0 ]]; then
	note "RED"
	exit 1
fi
note "GREEN"
