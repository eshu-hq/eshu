#!/usr/bin/env bash
#
# test-verify-scanner-digest-pins.sh - hermetic tests for
# scripts/verify-scanner-digest-pins.sh (#9491, satellite semantics #8878).
# No network, no Docker, no gh auth: `crane` and `gh` resolve to PATH stubs
# under mktemp, and git runs against tmp fixture repos that mirror the
# relocated pin leaf.
#
# The seeded RED/GREEN pair:
#   GREEN a fixture manifest whose pins equal what the stub registry serves,
#         with a known anchor commit in the publish repo: exit 0.
#   RED   a one-character typo in any pin, a MANIFEST_UNKNOWN tag (404), or a
#         failing attestation: exit 1 naming the component and the expected
#         digest.
# Fail-closed cases: a missing or malformed Scanner-Publish-SHA line, a
# missing or malformed Scanner-Publish-Repo line, an unknown anchor commit,
# a missing pin, and an unreadable manifest must all exit 2, never 0.
# A network-shaped crane failure (not MANIFEST_UNKNOWN) also exits 2.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-scanner-digest-pins.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

PIN_IMAGE="d2967a1ab642747ec5bafed484b4a5384839b90b59c9562ee38e67b3fa547d49"
PIN_SBOM="cbafeeeb2bae99902838e5159bd4003e210f5cfe41891b50b0caf502b2d99dec"
PIN_OS="e302cf4e398f23974210f69988a3c53510738100ae7525ead6d11d992d7be3a8"
ANCHOR="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
PUBLISH_REPO="eshu-hq/eshu-scanner-worker"

# --- PATH stubs -----------------------------------------------------------
stubbin="${tmp_root}/stubbin"
mkdir -p "${stubbin}"
# Stub crane serves digests from $CRANE_STUB_DIR/digest-<comp> files holding
# "sha256:<hex>"; a $CRANE_STUB_DIR/fail-<comp> file makes that component
# 404 with a MANIFEST_UNKNOWN message like the real crane; a
# $CRANE_STUB_DIR/broken file fails every call without that marker.
cat >"${stubbin}/crane" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == digest && $# -eq 2 ]] || exit 2
comp="$(printf '%s' "$2"|sed -n 's|.*/scanner-\([a-z]*\):sha-[0-9a-f]*$|\1|p')"
[[ -n "${comp}" ]] || exit 2
dir="${CRANE_STUB_DIR:?need CRANE_STUB_DIR}"
[[ -e "${dir}/broken" ]] && { echo dial tcp: connection refused >&2; exit 1; }
[[ -e "${dir}/fail-${comp}" ]] && { echo "MANIFEST_UNKNOWN: ${comp}" >&2; exit 1; }
cat "${dir}/digest-${comp}"
STUB
# Stub gh passes attestation unless GH_STUB_ATTEST_FAIL names the component,
# and resolves anchor commits unless GH_STUB_UNKNOWN_ANCHOR is set.
cat >"${stubbin}/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == attestation && "${2:-}" == verify ]]; then
	ref="$3"
	comp="$(printf '%s' "${ref}" | sed -n 's|.*scanner-\([a-z]*\)@sha256:[0-9a-f]*|\1|p')"
	[[ -n "${comp}" ]] || { echo "stub gh: cannot parse component from ${ref}" >&2; exit 2; }
	if [[ "${GH_STUB_ATTEST_FAIL:-}" == "${comp}" ]]; then
		echo "stub gh: no attestation found for ${ref}" >&2
		exit 1
	fi
	exit 0
fi
if [[ "${1:-}" == api && "${2:-}" == repos/*/commits/* ]]; then
	if [[ "${GH_STUB_UNKNOWN_ANCHOR:-}" == 1 ]]; then
		echo "stub gh: 404 commit not found" >&2
		exit 1
	fi
	printf '{"sha":"%s"}\n' "${2##*/}"
	exit 0
fi
echo "stub gh: unexpected args: $*" >&2
exit 2
STUB
chmod +x "${stubbin}/crane" "${stubbin}/gh"

# make_fixture DIR [MODE] writes a fixture repo whose manifest pins the
# three PIN_* digests at the relocated leaf path with the anchor lines.
# MODE selects the anchor block: valid, missing-sha, malformed-sha,
# missing-repo, or malformed-repo. Prints the fixture manifest path.
make_fixture() {
	local dir="$1" mode="${2:-valid}"
	mkdir -p "${dir}/go/internal/collector/scannercomponent"
	local sha_line="// Scanner-Publish-SHA: ${ANCHOR}"
	local repo_line="// Scanner-Publish-Repo: ${PUBLISH_REPO}"
	case "${mode}" in
		missing-sha) sha_line="// no anchor here" ;;
		malformed-sha) sha_line="// Scanner-Publish-SHA: xyz" ;;
		missing-repo) repo_line="// no publish repo here" ;;
		malformed-repo) repo_line="// Scanner-Publish-Repo: not a repo" ;;
	esac
	cat >"${dir}/go/internal/collector/scannercomponent/manifest.go" <<EOF
package scannercomponent

${sha_line}
${repo_line}

var artifactDigests = map[string]string{
	IDImage:     "${PIN_IMAGE}",
	IDSBOM:      "${PIN_SBOM}",
	IDOSRuntime: "${PIN_OS}",
}
EOF
	git -C "${dir}" init -q -b main 2>/dev/null || true
	printf '%s\n' "${dir}/go/internal/collector/scannercomponent/manifest.go"
}

# serve_all DIR writes stub digest files matching the fixture pins.
serve_all() {
	mkdir -p "$1/stub"
	printf 'sha256:%s\n' "${PIN_IMAGE}" >"$1/stub/digest-image"
	printf 'sha256:%s\n' "${PIN_SBOM}" >"$1/stub/digest-sbom"
	printf 'sha256:%s\n' "${PIN_OS}" >"$1/stub/digest-osruntime"
	printf '%s/stub\n' "$1"
}

# run_verifier MANIFEST STUBDIR -> sets rc and out_file. The stubs resolve
# via PATH; CRANE_BIN/GH_BIN default to the stub names.
run_verifier() {
	out_file="${tmp_root}/out.txt"
	set +e
	env PATH="${stubbin}:${PATH}" CRANE_BIN=crane GH_BIN=gh \
		CRANE_STUB_DIR="$2" GH_STUB_ATTEST_FAIL="${GH_STUB_ATTEST_FAIL:-}" \
		GH_STUB_UNKNOWN_ANCHOR="${GH_STUB_UNKNOWN_ANCHOR:-}" \
		bash "${verifier}" --manifest "$1" >"${out_file}" 2>&1
	rc=$?
	set -e
}

expect_rc() {
	local want="$1" label="$2"
	if [[ "${rc}" -eq "${want}" ]]; then
		record_pass "${label} (rc=${rc})"
	else
		record_fail "${label}: want rc=${want}, got rc=${rc}"
		cat "${out_file}" >&2
	fi
}

expect_output() {
	local needle="$1" label="$2"
	if [[ "$(<"${out_file}")" == *"${needle}"* ]]; then
		record_pass "${label}"
	else
		record_fail "${label} (expected to find: ${needle})"
		cat "${out_file}" >&2
	fi
}

# --- GREEN: pins match the stub registry, anchor known. --------------------
fx="${tmp_root}/green"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
run_verifier "${manifest}" "${stub}"
expect_rc 0 "GREEN: matching pins with a known anchor pass"
expect_output "PASS" "GREEN prints the PASS summary"

# A comment-only manifest edit stays green.
printf '// follow-up pin bump\n' >>"${manifest}"
run_verifier "${manifest}" "${stub}"
expect_rc 0 "GREEN: a comment-only manifest edit passes"

# --- RED: one-character typo in any pin. ------------------------------------
for comp_pin in "image:${PIN_IMAGE}" "sbom:${PIN_SBOM}" "osruntime:${PIN_OS}"; do
	comp="${comp_pin%%:*}"
	fx="${tmp_root}/typo-${comp}"
	manifest="$(make_fixture "${fx}")"
	stub="$(serve_all "${fx}")"
	python3 - "${manifest}" "${comp_pin%%:*}" <<'PY'
import sys
path, comp = sys.argv[1], sys.argv[2]
text = open(path).read()
old = {"image": "d2967a1a", "sbom": "cbafeeeb", "osruntime": "e302cf4e"}[comp]
new = {"image": "d2967a1b", "sbom": "cbafeeec", "osruntime": "e302cf4f"}[comp]
assert text.count(old) == 1, (comp, text.count(old))
open(path, "w").write(text.replace(old, new))
PY

	run_verifier "${manifest}" "${stub}"
	expect_rc 1 "RED: a typo in the ${comp} pin fails"
	expect_output "scanner-${comp}" "RED names the stale component scanner-${comp}"
	expect_output "expected digest:" "RED names the expected digest"
done

# --- RED: the tag is gone (404). --------------------------------------------
fx="${tmp_root}/gone"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
touch "${stub}/fail-sbom"
run_verifier "${manifest}" "${stub}"
expect_rc 1 "RED: a missing sha tag fails"
expect_output "scanner-sbom" "RED names the stale component scanner-sbom"
expect_output "expected digest: ${PIN_SBOM}" "RED names the expected sbom digest"

# --- RED: attestation re-verification fails. ---------------------------------
fx="${tmp_root}/attest"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
GH_STUB_ATTEST_FAIL=osruntime run_verifier "${manifest}" "${stub}"
expect_rc 1 "RED: a failing attestation fails"
expect_output "scanner-osruntime" "RED names the component with no attestation"
expect_output "expected digest: ${PIN_OS}" "RED names the expected osruntime digest"

# --- Fail closed: bad anchor lines, unknown anchors, missing pins. ----------
fx="${tmp_root}/noanchor"
manifest="$(make_fixture "${fx}" missing-sha)"
stub="$(serve_all "${fx}")"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a missing anchor line exits 2"
expect_output "FAIL-CLOSED" "a missing anchor line says FAIL-CLOSED"

fx="${tmp_root}/badanchor"
manifest="$(make_fixture "${fx}" malformed-sha)"
stub="$(serve_all "${fx}")"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a malformed anchor line exits 2"

fx="${tmp_root}/norepo"
manifest="$(make_fixture "${fx}" missing-repo)"
stub="$(serve_all "${fx}")"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a missing publish-repo line exits 2"
expect_output "FAIL-CLOSED" "a missing publish-repo line says FAIL-CLOSED"

fx="${tmp_root}/badrepo"
manifest="$(make_fixture "${fx}" malformed-repo)"
stub="$(serve_all "${fx}")"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a malformed publish-repo line exits 2"

fx="${tmp_root}/unknown"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
GH_STUB_UNKNOWN_ANCHOR=1 run_verifier "${manifest}" "${stub}"
expect_rc 2 "an unknown anchor commit exits 2"
expect_output "FAIL-CLOSED" "an unknown anchor says FAIL-CLOSED"

# A pin that is not 64-hex fails closed, never passes.
fx="${tmp_root}/shortpin"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
# -i.bak is the in-place form both GNU and BSD (macOS) sed accept (#9626).
sed -i.bak 's/"cbafeeeb2bae99902838e5159bd4003e210f5cfe41891b50b0caf502b2d99dec"/"cbaf"/' "${manifest}" && rm -f "${manifest}.bak"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a truncated pin exits 2"

# A network-shaped crane failure (no MANIFEST_UNKNOWN marker) fails closed.
fx="${tmp_root}/broken"
manifest="$(make_fixture "${fx}")"
stub="$(serve_all "${fx}")"
touch "${stub}/broken"
run_verifier "${manifest}" "${stub}"
expect_rc 2 "a crane transport failure exits 2, never 0"

# The verifier must not edit the manifest.
before="$(cksum <"${manifest}")"
stub2="$(serve_all "${tmp_root}/readonly")"
run_verifier "${manifest}" "${stub2}" || true
after="$(cksum <"${manifest}")"
if [[ "${before}" == "${after}" ]]; then record_pass "the verifier leaves the manifest unchanged"; else record_fail "the verifier modified the manifest"; fi

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
