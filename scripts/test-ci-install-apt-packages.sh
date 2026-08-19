#!/usr/bin/env bash
#
# test-ci-install-apt-packages.sh - hermetic checks for scripts/ci/install-apt-packages.sh.
#
# ripgrep is installed from a pinned, checksum-verified GitHub release binary
# instead of apt (apt-get intermittently hangs until job timeout on the
# runners; ripgrep was the only package that actually triggered apt -- jq is
# always preinstalled). Every other requested package still goes through the
# original apt path unchanged. These tests run entirely off the network: the
# ripgrep cases build their own fixture tarball and point the script at it via
# ESHU_CI_APT_RIPGREP_URL (curl fetches file:// URLs locally, no network
# needed), and the apt-path cases use ESHU_CI_APT_SKIP_INSTALL /
# ESHU_CI_APT_NO_SUDO so apt-get and sudo are never invoked.
# shellcheck disable=SC1091,SC2154  # helpers and tmp_root come from the lib sourced below; `shellcheck -x` is clean.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/ci/install-apt-packages.sh"

if [ ! -x "${helper}" ]; then
	echo "test-ci-install-apt-packages: missing executable helper at ${helper}" >&2
	exit 1
fi

# Fixtures + assertion helpers live in the sourced lib below (500-line cap).
# shellcheck source=scripts/lib/test-ci-install-apt-packages-fixtures.sh
source "${repo_root}/scripts/lib/test-ci-install-apt-packages-fixtures.sh"

# ---------------------------------------------------------------------------
# Case 1: an already-available package short-circuits -- no download, no apt.
# A fake `rg` on PATH stands in for the real (possibly host-installed) one so
# this case is independent of whether ripgrep happens to be on this machine.
# ---------------------------------------------------------------------------
test_already_available_short_circuits() {
	local case_dir="${tmp_root}/already-available"
	local fake_path="${case_dir}/bin"
	local bin_dir="${case_dir}/dest"
	mkdir -p "${fake_path}" "${bin_dir}"
	cat >"${fake_path}/rg" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
	chmod +x "${fake_path}/rg"

	local out="${case_dir}/out.log"
	local rc=0
	PATH="${fake_path}:${PATH}" \
		ESHU_CI_APT_BIN_DIR="${bin_dir}" \
		ESHU_CI_APT_NO_SUDO=1 \
		"${helper}" ripgrep >"${out}" 2>&1 || rc=$?
	assert_exit_zero "${rc}" "already-available: exits 0 without installing" \
		"already-available: exited nonzero. output:
$(cat "${out}")"

	assert_output_has "${out}" "ripgrep already available as rg" \
		"already-available: reports the short-circuit" \
		"already-available: missing short-circuit message. output:
$(cat "${out}")"

	assert_path_absent "${bin_dir}/rg" \
		"already-available: no download/install was attempted" \
		"already-available: rg was installed even though it was already on PATH"
}

# ---------------------------------------------------------------------------
# Case 2: pinned ripgrep installs from a LOCAL fixture tarball via the URL
# override, and rg lands in the destination with the fixture's exact bytes.
# ---------------------------------------------------------------------------
test_pinned_ripgrep_installs_from_local_fixture() {
	local case_dir="${tmp_root}/pinned-install"
	local bin_dir="${case_dir}/dest"
	mkdir -p "${case_dir}" "${bin_dir}"
	local sha256
	sha256="$(build_ripgrep_fixture "${case_dir}")"

	local out="${case_dir}/out.log"
	local rc=0
	ESHU_CI_APT_FORCE_INSTALL=1 \
		ESHU_CI_APT_NO_SUDO=1 \
		ESHU_CI_APT_BIN_DIR="${bin_dir}" \
		ESHU_CI_APT_RIPGREP_URL="file://${case_dir}/ripgrep.tar.gz" \
		ESHU_CI_APT_RIPGREP_SHA256="${sha256}" \
		"${helper}" ripgrep >"${out}" 2>&1 || rc=$?
	assert_exit_zero "${rc}" "pinned-install: exits 0" \
		"pinned-install: exited nonzero. output:
$(cat "${out}")"

	assert_path_present "${bin_dir}/rg" \
		"pinned-install: rg landed in the destination directory" \
		"pinned-install: ${bin_dir}/rg was not created. output:
$(cat "${out}")"

	if cmp -s "${bin_dir}/rg" "${case_dir}/stage/ripgrep-14.1.1-x86_64-unknown-linux-musl/rg"; then
		record_pass "pinned-install: installed rg has the fixture's exact bytes"
	else
		record_fail "pinned-install: installed rg content does not match the fixture"
	fi

	# GNU stat first, BSD second, and the order is load-bearing. On GNU/Linux
	# `stat -f` is --file-system: it SUCCEEDS and prints filesystem info, so a
	# BSD-first probe never falls through to the GNU form and compares that
	# text against "755". The reverse order fails cleanly on each platform --
	# GNU's -c is rejected by BSD stat, and vice versa.
	local mode=""
	mode="$(stat -c '%a' "${bin_dir}/rg" 2>/dev/null)" ||
		mode="$(stat -f '%Lp' "${bin_dir}/rg" 2>/dev/null)" ||
		mode=""
	if ! [[ "${mode}" =~ ^[0-7]{3,4}$ ]]; then
		record_fail "pinned-install: could not read a file mode for ${bin_dir}/rg (got \"${mode}\") -- neither GNU nor BSD stat produced one, so the 0755 assertion never ran"
	elif [ "${mode#0}" = "755" ]; then
		record_pass "pinned-install: installed rg is mode 0755"
	else
		record_fail "pinned-install: installed rg mode is ${mode}, wanted 755"
	fi
}

# ---------------------------------------------------------------------------
# Case 3: checksum MISMATCH fails closed -- nonzero exit, a message naming
# expected vs actual, and no binary written to the destination.
# ---------------------------------------------------------------------------
test_checksum_mismatch_fails_closed() {
	local case_dir="${tmp_root}/checksum-mismatch"
	local bin_dir="${case_dir}/dest"
	mkdir -p "${case_dir}" "${bin_dir}"
	build_ripgrep_fixture "${case_dir}" >/dev/null
	local wrong_sha256="0000000000000000000000000000000000000000000000000000000000000000"
	wrong_sha256="${wrong_sha256:0:64}"

	local out="${case_dir}/out.log"
	local rc=0
	ESHU_CI_APT_FORCE_INSTALL=1 \
		ESHU_CI_APT_NO_SUDO=1 \
		ESHU_CI_APT_BIN_DIR="${bin_dir}" \
		ESHU_CI_APT_RIPGREP_URL="file://${case_dir}/ripgrep.tar.gz" \
		ESHU_CI_APT_RIPGREP_SHA256="${wrong_sha256}" \
		"${helper}" ripgrep >"${out}" 2>&1 || rc=$?
	assert_exit_nonzero "${rc}" "checksum-mismatch: exits nonzero (rc=${rc})" \
		"checksum-mismatch: exited 0, wanted a failure. output:
$(cat "${out}")"

	assert_output_has_all "${out}" "checksum mismatch" "expected ${wrong_sha256}" \
		"checksum-mismatch: message names expected vs actual" \
		"checksum-mismatch: message missing expected/actual detail. output:
$(cat "${out}")"

	assert_path_absent "${bin_dir}/rg" \
		"checksum-mismatch: no binary was installed" \
		"checksum-mismatch: rg was installed despite the checksum failure"
}

# ---------------------------------------------------------------------------
# Case 4: a non-ripgrep package still takes the apt path. Asserted entirely
# through ESHU_CI_APT_SKIP_INSTALL / ESHU_CI_APT_NO_SUDO so apt-get is never
# actually invoked; the unstable-source-disabling side effect and the
# "Skipping apt install" message are the proof the apt branch ran.
# ---------------------------------------------------------------------------
test_non_ripgrep_package_still_takes_apt_path() {
	local case_dir="${tmp_root}/apt-path"
	local source_dir="${case_dir}/sources.list.d"
	mkdir -p "${source_dir}"
	touch "${source_dir}/microsoft-prod.list"

	local out="${case_dir}/out.log"
	local rc=0
	ESHU_CI_APT_FORCE_INSTALL=1 \
		ESHU_CI_APT_NO_SUDO=1 \
		ESHU_CI_APT_SKIP_INSTALL=1 \
		ESHU_CI_APT_SOURCES_DIR="${source_dir}" \
		"${helper}" jq >"${out}" 2>&1 || rc=$?
	assert_exit_zero "${rc}" "apt-path: exits 0" \
		"apt-path: exited nonzero. output:
$(cat "${out}")"

	assert_output_has "${out}" "Skipping apt install: jq" \
		"apt-path: jq was routed to the apt install path" \
		"apt-path: missing apt-skip message for jq. output:
$(cat "${out}")"

	assert_path_present "${source_dir}/microsoft-prod.list.eshu-disabled" \
		"apt-path: unstable runner source was disabled before the (skipped) apt install" \
		"apt-path: unstable runner source was not disabled"
}

# ---------------------------------------------------------------------------
# Case 5: ESHU_CI_APT_SKIP_INSTALL=1 still skips the pinned ripgrep install,
# not just the apt path. No download is attempted and nothing is written.
# ---------------------------------------------------------------------------
test_skip_install_skips_pinned_ripgrep() {
	local case_dir="${tmp_root}/skip-ripgrep"
	local bin_dir="${case_dir}/dest"
	mkdir -p "${case_dir}" "${bin_dir}"

	local out="${case_dir}/out.log"
	local rc=0
	# No ESHU_CI_APT_RIPGREP_URL override: if the skip check did not fire this
	# would try to reach the real GitHub release over the network and this
	# test would hang or fail for the wrong reason.
	ESHU_CI_APT_FORCE_INSTALL=1 \
		ESHU_CI_APT_NO_SUDO=1 \
		ESHU_CI_APT_SKIP_INSTALL=1 \
		ESHU_CI_APT_BIN_DIR="${bin_dir}" \
		"${helper}" ripgrep >"${out}" 2>&1 || rc=$?
	assert_exit_zero "${rc}" "skip-ripgrep: exits 0" \
		"skip-ripgrep: exited nonzero. output:
$(cat "${out}")"

	assert_output_has "${out}" "ESHU_CI_APT_SKIP_INSTALL=1" \
		"skip-ripgrep: reports the skip" \
		"skip-ripgrep: missing skip message. output:
$(cat "${out}")"

	assert_path_absent "${bin_dir}/rg" \
		"skip-ripgrep: no binary was installed" \
		"skip-ripgrep: rg was installed despite ESHU_CI_APT_SKIP_INSTALL=1"
}

# ---------------------------------------------------------------------------
# Case 6 (bonus, proves the bounded-retry requirement): a download source
# that never succeeds exhausts its bounded retries and fails loudly -- never
# silently falling back to apt. ESHU_CI_APT_RIPGREP_DOWNLOAD_RETRY_DELAY=0
# keeps this fast; the attempts count is still asserted in the message.
# ---------------------------------------------------------------------------
test_download_failure_exhausts_retries_and_fails_loudly() {
	local case_dir="${tmp_root}/download-fails"
	local bin_dir="${case_dir}/dest"
	mkdir -p "${case_dir}" "${bin_dir}"

	local out="${case_dir}/out.log"
	local rc=0
	ESHU_CI_APT_FORCE_INSTALL=1 \
		ESHU_CI_APT_NO_SUDO=1 \
		ESHU_CI_APT_BIN_DIR="${bin_dir}" \
		ESHU_CI_APT_RIPGREP_URL="file://${case_dir}/does-not-exist.tar.gz" \
		ESHU_CI_APT_RIPGREP_DOWNLOAD_ATTEMPTS=2 \
		ESHU_CI_APT_RIPGREP_DOWNLOAD_RETRY_DELAY=0 \
		"${helper}" ripgrep >"${out}" 2>&1 || rc=$?
	assert_exit_nonzero "${rc}" "download-fails: exits nonzero (rc=${rc})" \
		"download-fails: exited 0, wanted a failure. output:
$(cat "${out}")"

	assert_output_has "${out}" "failed after 2 attempt(s)" \
		"download-fails: message names the exhausted attempt count" \
		"download-fails: missing attempt-count message. output:
$(cat "${out}")"

	assert_output_lacks "${out}" "apt-get" \
		"download-fails: no silent apt-get fallback" \
		"download-fails: fell back to apt-get instead of failing loudly"

	assert_path_absent "${bin_dir}/rg" \
		"download-fails: no binary was installed" \
		"download-fails: rg was installed despite the download failure"
}

# ---------------------------------------------------------------------------
# Case 7: a machine whose architecture the pin was not built for must fail at
# INSTALL time, loudly, rather than installing a binary that cannot execute.
#
# This case exists because the guard is otherwise unreachable from this
# suite: every other ripgrep case sets ESHU_CI_APT_RIPGREP_URL, which is
# exactly the condition that makes assert_ripgrep_arch_supported return 0
# immediately. So it deliberately does NOT set the URL override, and relies
# on the guard running before the download (install_ripgrep_pinned_binary
# calls it immediately after mktemp, before curl) to stay hermetic -- no
# network is reached even though the real pinned URL is in play.
# ---------------------------------------------------------------------------
test_unsupported_arch_fails_before_download() {
	local case_dir="${tmp_root}/unsupported-arch"
	local bin_dir="${case_dir}/dest"
	local stub_bin="${case_dir}/stubbin"
	mkdir -p "${case_dir}" "${bin_dir}" "${stub_bin}"

	# Stub uname so the script believes it is on arm64. Everything else the
	# script needs before the guard resolves through the real PATH.
	cat >"${stub_bin}/uname" <<'STUB'
#!/usr/bin/env bash
if [ "${1:-}" = "-m" ]; then
	echo "aarch64"
else
	exec /usr/bin/uname "$@"
fi
STUB
	chmod +x "${stub_bin}/uname"

	local out="${case_dir}/out.log"
	local rc=0
	PATH="${stub_bin}:${PATH}" \
		ESHU_CI_APT_FORCE_INSTALL=1 \
		ESHU_CI_APT_NO_SUDO=1 \
		ESHU_CI_APT_BIN_DIR="${bin_dir}" \
		"${helper}" ripgrep >"${out}" 2>&1 || rc=$?
	assert_exit_nonzero "${rc}" "unsupported-arch: exits nonzero (rc=${rc})" \
		"unsupported-arch: exited 0 on aarch64 -- an x86_64 binary that cannot execute would have been installed. output:
$(cat "${out}")"

	assert_output_has_all "${out}" "aarch64" "x86_64-unknown-linux-musl" \
		"unsupported-arch: message names both the pin's arch and the machine's" \
		"unsupported-arch: message does not name the architecture mismatch. output:
$(cat "${out}")"

	assert_path_absent "${bin_dir}/rg" \
		"unsupported-arch: no binary was installed" \
		"unsupported-arch: rg was installed despite the architecture mismatch"

	# The guard must fire BEFORE the download, both so this case stays offline
	# and so the diagnostic is the architecture rather than a network error.
	assert_output_lacks "${out}" "installing ripgrep from pinned binary release" \
		"unsupported-arch: failed before attempting any download" \
		"unsupported-arch: reached the download stage before failing -- the arch guard must run before curl. output:
$(cat "${out}")"
}

# ---------------------------------------------------------------------------
# Case 8: with NO sha256 tool on PATH, verification must fail closed and say
# so. This is the case whose absence let a real defect through review:
# sha256_command's `exit 1` runs inside a command substitution, so it exits
# that subshell rather than the script, and `set -e` cannot rescue it because
# verify_ripgrep_checksum is called in an `if !` condition. Execution used to
# continue with an empty sha_cmd, which made the DOWNLOADED ARCHIVE the
# command word -- bash attempting to execute the unverified download, blocked
# only by curl -o creating it 0644. The archive must never be installed and
# the message must name the missing tool rather than reporting an empty
# "checksum mismatch".
# ---------------------------------------------------------------------------
test_missing_sha256_tool_fails_closed() {
	local case_dir="${tmp_root}/no-sha-tool"
	local bin_dir="${case_dir}/dest"
	local stub_bin="${case_dir}/stubbin"
	mkdir -p "${case_dir}" "${bin_dir}" "${stub_bin}"
	local sha256
	sha256="$(build_ripgrep_fixture "${case_dir}")"

	# A PATH holding only the utilities the script needs BEFORE hashing, with
	# sha256sum and shasum deliberately absent.
	local tool
	for tool in bash env curl tar mktemp rm mkdir install awk sed grep cat uname chmod cp seq sleep dirname basename tr head tail sort ln touch; do
		if command -v "${tool}" >/dev/null 2>&1; then
			ln -sf "$(command -v "${tool}")" "${stub_bin}/${tool}"
		fi
	done

	local out="${case_dir}/out.log"
	local rc=0
	PATH="${stub_bin}" \
		ESHU_CI_APT_FORCE_INSTALL=1 \
		ESHU_CI_APT_NO_SUDO=1 \
		ESHU_CI_APT_BIN_DIR="${bin_dir}" \
		ESHU_CI_APT_RIPGREP_URL="file://${case_dir}/ripgrep.tar.gz" \
		ESHU_CI_APT_RIPGREP_SHA256="${sha256}" \
		"${helper}" ripgrep >"${out}" 2>&1 || rc=$?
	assert_exit_nonzero "${rc}" "no-sha-tool: exits nonzero (rc=${rc})" \
		"no-sha-tool: exited 0 with no sha256 tool available -- an unverified archive would have been installed. output:
$(cat "${out}")"

	assert_path_absent "${bin_dir}/rg" \
		"no-sha-tool: no binary was installed" \
		"no-sha-tool: rg was installed without any checksum verification"

	# The assertions above pass with OR without the sha_cmd guards -- the script
	# fails closed either way, so outcome alone cannot tell the two apart. These
	# two pin the MECHANISM, which is what the guards actually change: without
	# them, execution continues past the missing tool, `${sha_cmd} "${archive}"`
	# makes the archive the command word (bash tries to EXECUTE the unverified
	# download), and an empty digest is then reported as a bogus
	# "checksum mismatch: got ". With the guards, verification returns before any
	# of that happens.
	assert_output_lacks "${out}" "checksum mismatch" \
		"no-sha-tool: no bogus empty-digest checksum mismatch reported" \
		"no-sha-tool: reported a checksum mismatch when no sha256 tool exists -- verification compared against an empty digest instead of returning early. output:
$(cat "${out}")"

	assert_output_lacks_any "${out}" "ripgrep.tar.gz: command not found" "ripgrep.tar.gz: Permission denied" \
		"no-sha-tool: never attempted to execute the downloaded archive" \
		"no-sha-tool: bash attempted to EXECUTE the downloaded archive as a command -- the sha_cmd guard did not fire. output:
$(cat "${out}")"

	if rg -q --fixed-strings "neither sha256sum nor shasum is available" "${out}"; then
		record_pass "no-sha-tool: message names the missing tool, not an empty checksum mismatch"
	elif rg -q --fixed-strings "no sha256 tool available" "${out}"; then
		record_pass "no-sha-tool: message names the missing tool (belt-and-braces guard fired)"
	else
		record_fail "no-sha-tool: message reports something other than the missing sha256 tool (an empty \"checksum mismatch: got \" here would mean the guards did not fire). output:
$(cat "${out}")"
	fi
}

# ---------------------------------------------------------------------------
# Case 10: this mirror must actually be RUN by CI, asserted by the mirror
# itself.
#
# The gate that selects this file is one of seven sharing test.yml's go-core
# job, and verify-ci-gates-registry.sh skips per-gate CI attribution for a
# shared bucket by design. So deleting the workflow step that runs this file
# leaves the registry row intact, the drift check green, and gate selection
# still passing on triggers -- while CI quietly stops running the only
# enforcement the installer's three guards have. That is precisely the defect
# this gate was added to fix, returning through a different door: everything
# looks wired and nothing runs.
#
# Nothing else in the repo closes that door, so the mirror closes it for
# itself. Self-referential on purpose: this file is the thing whose execution
# is in question, so it is the right place to assert its own wiring.
# ---------------------------------------------------------------------------
test_mirror_is_wired_into_ci() {
	local workflow="${repo_root}/.github/workflows/test.yml"

	if [ ! -f "${workflow}" ]; then
		record_fail "ci-wiring: ${workflow} not found -- this mirror asserts its own CI wiring and cannot locate the workflow"
		return
	fi

	assert_output_has "${workflow}" "bash scripts/test-ci-install-apt-packages.sh" \
		"ci-wiring: test.yml runs this mirror" \
		"ci-wiring: no step in .github/workflows/test.yml runs \"bash scripts/test-ci-install-apt-packages.sh\".
The ci-install-apt-packages registry row would still look correct and the drift
check would still pass -- it skips per-gate CI attribution for the shared go-core
job -- while CI silently stopped enforcing the installer's bounded-transfer,
fail-closed-checksum, and arch guards. Re-add the step or move the gate to a
dedicated ci.job."

	# The registry row is the other half: a workflow step with no row is not
	# selected locally by make pre-pr, and a row pointing at a job that does not
	# run the command is the same silence in the other direction.
	local registry="${repo_root}/specs/ci-gates.v1.yaml"
	assert_output_has "${registry}" "scripts/test-ci-install-apt-packages.sh" \
		"ci-wiring: ci-gates registry references this mirror" \
		"ci-wiring: specs/ci-gates.v1.yaml does not reference scripts/test-ci-install-apt-packages.sh, so make pre-pr will not select it"
}

# ---------------------------------------------------------------------------
# Case 9 (source-level regression, precedent: extractFuncBody in
# go/cmd/reducer/neo4j_wiring_test.go): --connect-timeout alone bounds
# connection SETUP only. If the GitHub release CDN accepts the connection and
# then stalls mid-body -- the same failure mode as the apt hang this script
# exists to remove, just on a different host -- curl blocks indefinitely, the
# retry loop never gets a turn, and the job rides to its 15-25 minute timeout.
# A behavioral test for "hangs forever" is impractical, so this reads the
# script's own source and asserts the download's curl invocation still
# carries a whole-transfer bound, not just --connect-timeout.
# ---------------------------------------------------------------------------
test_download_bounds_transfer_not_just_connect() {
	local curl_line
	curl_line="$(rg -m1 '^[[:space:]]*if curl -fsSL' "${helper}")"

	if [ -z "${curl_line}" ]; then
		record_fail "transfer-bound: could not find the ripgrep download curl invocation in ${helper}"
		return
	fi

	local has_max_time=0 has_speed_limit=0 has_speed_time=0
	case "${curl_line}" in *--max-time*) has_max_time=1 ;; esac
	case "${curl_line}" in *--speed-limit*) has_speed_limit=1 ;; esac
	case "${curl_line}" in *--speed-time*) has_speed_time=1 ;; esac

	if [ "${has_max_time}" -eq 1 ] && [ "${has_speed_limit}" -eq 1 ] && [ "${has_speed_time}" -eq 1 ]; then
		record_pass "transfer-bound: ripgrep download curl invocation bounds the whole transfer (--max-time/--speed-limit/--speed-time), not just connection setup"
	else
		record_fail "transfer-bound: ripgrep download curl invocation only bounds connection setup (--connect-timeout) -- a stalled mid-transfer download (GitHub CDN accepts the connection then stalls, same failure mode as the apt hang this script removes) would hang to the job timeout instead of retrying. Found: ${curl_line}"
	fi

	# Presence alone is not enough: --speed-limit 0 disables the abort, and a
	# --speed-time >= --max-time never gets a turn -- both keep every flag
	# present. --max-time is the "${max_time}" variable, not a literal, so its
	# value here is the script's default (ESHU_CI_APT_RIPGREP_DOWNLOAD_MAX_TIME:-120).
	local speed_limit_value speed_time_value max_time_default
	speed_limit_value="$(curl_flag_value "${curl_line}" '--speed-limit')"
	speed_time_value="$(curl_flag_value "${curl_line}" '--speed-time')"
	max_time_default="$(script_var_default "${helper}" 'local max_time=')"

	if [ "${speed_limit_value}" -ne 0 ]; then
		record_pass "transfer-bound: --speed-limit is nonzero (${speed_limit_value} bytes/s), so curl actually aborts a stalled-but-connected transfer"
	else
		record_fail "transfer-bound: --speed-limit is ${speed_limit_value} -- curl treats 0 as \"never abort on slow speed\", so the flag is present but inert and a stalled mid-transfer download would still hang to the job timeout. Found: ${curl_line}"
	fi

	if [ "${max_time_default}" -gt 0 ] && [ "${speed_time_value}" -lt "${max_time_default}" ]; then
		record_pass "transfer-bound: --speed-time (${speed_time_value}s) is less than --max-time's default (${max_time_default}s), so the stall-abort fires before the whole-transfer ceiling would"
	else
		record_fail "transfer-bound: --speed-time (${speed_time_value}s) is not less than --max-time's default (${max_time_default}s) -- a stalled transfer would ride to the whole-transfer ceiling instead of the faster --speed-limit/--speed-time abort, defeating the guard those flags exist to provide. curl line: ${curl_line}"
	fi
}

test_already_available_short_circuits
test_pinned_ripgrep_installs_from_local_fixture
test_checksum_mismatch_fails_closed
test_non_ripgrep_package_still_takes_apt_path
test_skip_install_skips_pinned_ripgrep
test_download_failure_exhausts_retries_and_fails_loudly
test_download_bounds_transfer_not_just_connect
test_mirror_is_wired_into_ci
test_missing_sha256_tool_fails_closed
test_unsupported_arch_fails_before_download

if [ "${FAIL}" -ne 0 ]; then
	printf 'test-ci-install-apt-packages: FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
	exit 1
fi

printf 'test-ci-install-apt-packages: all tests passed (%d/%d)\n' "${PASS}" "$((PASS + FAIL))"
