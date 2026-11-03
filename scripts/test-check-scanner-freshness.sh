#!/usr/bin/env bash
#
# test-check-scanner-freshness.sh - hermetic tests for
# scripts/check-scanner-freshness.sh (#9943).
# No network, no gh auth: `gh` resolves to a PATH stub serving fixture
# workflow-run JSON, and manifests are tmp fixtures mirroring the pin leaf.
#
# The seeded RED/GREEN pair:
#   GREEN a fixture manifest whose anchor equals the stub release head:
#         exit 0 printing FRESH.
#   RED   a fixture manifest whose anchor lags the stub release head:
#         exit 1 naming both SHAs and the release run.
# Fail-closed cases: a missing or malformed Scanner-Publish-SHA line, a
# missing or malformed Scanner-Publish-Repo line, zero successful releases,
# a gh transport failure, and malformed release JSON must all exit 2,
# never 0 and never 1. Unknown is not fresh and not stale.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
checker="${repo_root}/scripts/check-scanner-freshness.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

ANCHOR="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
NEWER="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
PUBLISH_REPO="eshu-hq/eshu-scanner-worker"
RUN_ID="41401606694"

# --- PATH stub --------------------------------------------------------------
stubbin="${tmp_root}/stubbin"
mkdir -p "${stubbin}"
# Stub gh serves the runs API from env: GH_STUB_RELEASE_HEAD holds the
# head_sha to report (or "none" for zero successful releases), and
# GH_STUB_API_FAIL=1 fails the call like a transport error.
cat >"${stubbin}/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == api && "${2:-}" == repos/*/actions/workflows/*/runs* ]]; then
	# Tripwire: the checker must scope the query to successful runs. A
	# query change that drops status=success fails closed here, not green.
	if [[ "${2:-}" != *status=success* ]]; then
		echo "stub gh: expected status=success in ${2:-}" >&2
		exit 2
	fi
	if [[ "${GH_STUB_API_FAIL:-}" == 1 ]]; then
		echo "stub gh: dial tcp: connection refused" >&2
		exit 1
	fi
STUB
# Split into two heredocs: one body would exceed the 512-byte
# heredoc budget and deadlock macOS Homebrew bash (#5074).
cat >>"${stubbin}/gh" <<'STUB'
	if [[ "${GH_STUB_BAD_JSON:-}" == 1 ]]; then
		echo "stub gh: {not json"
		exit 0
	fi
	if [[ "${GH_STUB_RELEASE_HEAD:-}" == none ]]; then
		echo '{"total_count":0,"workflow_runs":[]}'
		exit 0
	fi
	head="${GH_STUB_RELEASE_HEAD:?need GH_STUB_RELEASE_HEAD}"
	printf '{"total_count":1,"workflow_runs":[{"head_sha":"%s","id":41401606694}]}\n' "${head}"
	exit 0
fi
echo "stub gh: unexpected args: $*" >&2
exit 2
STUB
chmod +x "${stubbin}/gh"

# make_fixture DIR [MODE] writes a fixture manifest with the anchor block.
# MODE: valid, missing-sha, malformed-sha, missing-repo. Prints the path.
make_fixture() {
	local dir="$1" mode="${2:-valid}"
	mkdir -p "${dir}/go/internal/collector/scannercomponent"
	local sha_line="// Scanner-Publish-SHA: ${ANCHOR}"
	local repo_line="// Scanner-Publish-Repo: ${PUBLISH_REPO}"
	case "${mode}" in
		missing-sha) sha_line="// no anchor here" ;;
		malformed-sha) sha_line="// Scanner-Publish-SHA: xyz" ;;
		missing-repo) repo_line="// no publish repo here" ;;
	esac
	cat >"${dir}/go/internal/collector/scannercomponent/manifest.go" <<EOF
package scannercomponent

${sha_line}
${repo_line}
EOF
	printf '%s\n' "${dir}/go/internal/collector/scannercomponent/manifest.go"
}

# run_checker MANIFEST -> sets rc and out_file. gh resolves via PATH.
run_checker() {
	out_file="${tmp_root}/out.txt"
	set +e
	env PATH="${stubbin}:${PATH}" GH_BIN=gh \
		GH_STUB_RELEASE_HEAD="${GH_STUB_RELEASE_HEAD:-}" \
		GH_STUB_API_FAIL="${GH_STUB_API_FAIL:-}" \
		GH_STUB_BAD_JSON="${GH_STUB_BAD_JSON:-}" \
		bash "${checker}" --manifest "$1" >"${out_file}" 2>&1
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

# --- GREEN: anchor equals the stub release head. -----------------------------
fx="${tmp_root}/green"
manifest="$(make_fixture "${fx}")"
GH_STUB_RELEASE_HEAD="${ANCHOR}" run_checker "${manifest}"
expect_rc 0 "GREEN: anchor equal to the release head passes"
expect_output "FRESH" "GREEN prints the FRESH summary"
expect_output "${ANCHOR:0:7}" "GREEN names the anchor"

# --- RED: anchor lags the stub release head. ---------------------------------
fx="${tmp_root}/stale"
manifest="$(make_fixture "${fx}")"
GH_STUB_RELEASE_HEAD="${NEWER}" run_checker "${manifest}"
expect_rc 1 "RED: a lagging anchor fails"
expect_output "STALE" "RED prints the STALE summary"
expect_output "${NEWER:0:7}" "RED names the release head"
expect_output "${RUN_ID}" "RED names the release run"
expect_output "${ANCHOR:0:7}" "RED names the lagging anchor"

# --- Fail-closed: zero successful releases. ----------------------------------
fx="${tmp_root}/noruns"
manifest="$(make_fixture "${fx}")"
GH_STUB_RELEASE_HEAD="none" run_checker "${manifest}"
expect_rc 2 "zero successful releases exits 2, never 0 or 1"
expect_output "FAIL-CLOSED" "zero successful releases says FAIL-CLOSED"

# --- Fail-closed: gh transport failure. --------------------------------------
fx="${tmp_root}/transport"
manifest="$(make_fixture "${fx}")"
GH_STUB_RELEASE_HEAD="${ANCHOR}" GH_STUB_API_FAIL=1 run_checker "${manifest}"
expect_rc 2 "a gh transport failure exits 2, never 0"
expect_output "FAIL-CLOSED" "a gh transport failure says FAIL-CLOSED"

# --- Fail-closed: malformed release JSON. ------------------------------------
fx="${tmp_root}/badjson"
manifest="$(make_fixture "${fx}")"
GH_STUB_RELEASE_HEAD="${ANCHOR}" GH_STUB_BAD_JSON=1 run_checker "${manifest}"
expect_rc 2 "malformed release JSON exits 2, never 0"

# --- Fail-closed: malformed manifest. ----------------------------------------
for mode in missing-sha malformed-sha missing-repo; do
	fx="${tmp_root}/${mode}"
	manifest="$(make_fixture "${fx}" "${mode}")"
	GH_STUB_RELEASE_HEAD="${ANCHOR}" run_checker "${manifest}"
	expect_rc 2 "a ${mode} manifest exits 2"
	expect_output "FAIL-CLOSED" "a ${mode} manifest says FAIL-CLOSED"
done

printf '%s passed, %s failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
