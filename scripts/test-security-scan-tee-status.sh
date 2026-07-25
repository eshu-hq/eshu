#!/usr/bin/env bash
#
# Executable regression test for scripts/ci/run-scan-with-tee.sh (#5813).
#
# security-scan.yml's govulncheck and nancy steps pipe a scanner through
# `tee <artifact>` under `set -euo pipefail` so the scan output is both
# streamed to the job log and saved as a workflow artifact. Before 41e77ee33,
# the error-handling branch read only ${PIPESTATUS[0]} (the scanner's own
# status), so a scanner that SUCCEEDED while `tee` FAILED to write its
# artifact (full disk, unwritable working directory) still exited 0 -- a
# green scan with no output file ever written. This test runs the REAL
# scripts/ci/run-scan-with-tee.sh end-to-end, with stub scanner binaries on
# PATH, and asserts actual exit codes and artifact contents for the three
# cases the helper must tell apart.
#
# The tee failure (case b) is induced via ENOTDIR -- the artifact path is
# "<regular-file>/out", i.e. a path that tries to use a plain file as a
# directory component. That fails the same way under an unprivileged user AND
# under root (CI containers frequently run as root, where permission bits are
# ignored and a chmod-000-directory test would pass vacuously without ever
# exercising the tee-failure branch). ENOTDIR fails on macOS bash 3.2 and on
# Ubuntu regardless of uid.
#
# Cases:
#   a. scanner fails (nonzero exit, tee succeeds) -> exit == scanner's status,
#      the scanner-failure diagnostic is printed.
#   b. scanner succeeds, tee fails (ENOTDIR) -> NON-ZERO exit (the tee's own
#      status) -- this is the regression being pinned: it was 0 before
#      41e77ee33.
#   c. both succeed -> exit 0, and the artifact contains the scanner's output.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/ci/run-scan-with-tee.sh"

if [ ! -x "${helper}" ]; then
	printf 'test-security-scan-tee-status: missing executable script: %s\n' "${helper}" >&2
	exit 1
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fail() {
	printf 'test-security-scan-tee-status: %s\n' "$*" >&2
	exit 1
}

# write_fake_scanner writes a shim at "<dir>/fake-scanner" that only
# understands the fixed invocation `fake-scanner`, prints the given stdout,
# and exits with the given code -- standing in for govulncheck/nancy without
# a real scan.
write_fake_scanner() {
	local dir="$1" stdout="$2" exit_code="$3"
	mkdir -p "${dir}"
	cat >"${dir}/fake-scanner" <<SCRIPT
#!/usr/bin/env bash
printf '%s\n' $(printf '%q' "${stdout}")
exit ${exit_code}
SCRIPT
	chmod +x "${dir}/fake-scanner"
}

# --- Case a: scanner fails (tee succeeds) -> exit == scanner's status, the
# scanner-failure diagnostic is printed, and PIPESTATUS[1] (tee) is never
# mistaken for the reported status.
case_a() {
	local case_dir="${tmp_root}/scanner-fails"
	mkdir -p "${case_dir}/bin"
	write_fake_scanner "${case_dir}/bin" "a real finding" 3
	local artifact="${case_dir}/scan.out"

	local out got_exit
	set +e
	out="$(PATH="${case_dir}/bin:${PATH}" bash "${helper}" fakescan "${artifact}" \
		'fakescan: scanner failed with status %s, see scan.out above\n' \
		-- fake-scanner 2>&1)"
	got_exit=$?
	set -e

	[ "${got_exit}" -eq 3 ] ||
		fail "scanner-fails: exit=${got_exit}, want 3. output:
${out}"

	case "${out}" in
		*"fakescan: scanner failed with status 3, see scan.out above"*) ;;
		*)
			fail "scanner-fails: output missing scanner-failure diagnostic. output:
${out}"
			;;
	esac

	[ -f "${artifact}" ] || fail "scanner-fails: artifact ${artifact} was not written"
	rg --fixed-strings --quiet -- "a real finding" "${artifact}" ||
		fail "scanner-fails: artifact missing scanner output"

	printf 'PASS: scanner-fails\n'
}

# --- Case b (THE REGRESSION BEING PINNED): scanner succeeds, tee fails
# (ENOTDIR -- the artifact path walks through a regular file as though it
# were a directory) -> the helper must exit non-zero. Before 41e77ee33's
# fix, only PIPESTATUS[0] (the scanner's 0) was checked here, so this exact
# case exited 0 -- a green scan with no artifact ever written.
case_b() {
	local case_dir="${tmp_root}/tee-fails-enotdir"
	mkdir -p "${case_dir}/bin"
	write_fake_scanner "${case_dir}/bin" "clean scan, nothing found" 0

	# A regular file used as a directory component: writing to
	# "<regfile>/out" fails with ENOTDIR under any uid, including root, so
	# this is not defeated by a CI container running privileged (unlike a
	# chmod-000 directory, whose mode bits root ignores).
	local blocker="${case_dir}/not-a-directory"
	touch "${blocker}"
	local artifact="${blocker}/scan.out"

	local out got_exit
	set +e
	out="$(PATH="${case_dir}/bin:${PATH}" bash "${helper}" fakescan "${artifact}" \
		'fakescan: scanner failed with status %s, see scan.out above\n' \
		-- fake-scanner 2>&1)"
	got_exit=$?
	set -e

	[ "${got_exit}" -ne 0 ] ||
		fail "tee-fails-enotdir: exit=0, want non-zero (this is the #5813 false-green regression). output:
${out}"

	case "${out}" in
		*"failed to write ${artifact}"*) ;;
		*)
			fail "tee-fails-enotdir: output missing tee-failure diagnostic. output:
${out}"
			;;
	esac

	[ ! -e "${artifact}" ] ||
		fail "tee-fails-enotdir: artifact ${artifact} unexpectedly exists"

	printf 'PASS: tee-fails-enotdir\n'
}

# --- Case c: both the scanner and tee succeed -> exit 0, artifact contains
# the scanner's output.
case_c() {
	local case_dir="${tmp_root}/clean-scan"
	mkdir -p "${case_dir}/bin"
	write_fake_scanner "${case_dir}/bin" "no vulnerabilities found" 0
	local artifact="${case_dir}/scan.out"

	local out got_exit
	set +e
	out="$(PATH="${case_dir}/bin:${PATH}" bash "${helper}" fakescan "${artifact}" \
		'fakescan: scanner failed with status %s, see scan.out above\n' \
		-- fake-scanner 2>&1)"
	got_exit=$?
	set -e

	[ "${got_exit}" -eq 0 ] ||
		fail "clean-scan: exit=${got_exit}, want 0. output:
${out}"

	[ -f "${artifact}" ] || fail "clean-scan: artifact ${artifact} was not written"
	rg --fixed-strings --quiet -- "no vulnerabilities found" "${artifact}" ||
		fail "clean-scan: artifact missing scanner output"

	printf 'PASS: clean-scan\n'
}

case_a
case_b
case_c

printf 'PASS: run-scan-with-tee.sh distinguishes scanner failure, tee failure (ENOTDIR), and a clean scan\n'
