#!/usr/bin/env bash
# Seeded RED/GREEN for the #9621 generator-input drift subcheck.
#
# Extracted to keep scripts/test-verify-ci-gates-registry.sh under the
# repository's 500-line file cap. It is sourced, not executed, and uses the
# caller's `fail`, `script`, and `repo_root`.
#
# The subcheck fails `verify-ci-gates-registry.sh --drift` when a
# generator_inputs class of a regenerate-and-diff gate is matched by no
# trigger of that gate. The RED half below shapes #9588: the readiness
# generator's go inlet trigger removed, so a PR changing only a live test's
# marker no longer selects live-readiness-registration-drift. The GREEN half
# is the existing live `validate --drift` step in the parent script — this
# file restores the fragment before asserting, so that step proves the clean
# tree on the restored bytes, and a checksum pins the restore.
check_ci_gate_generator_input_coverage() {
	local fragment backup seeded_output status checksum_before checksum_after
	fragment="${repo_root}/specs/ci-gates.d/gates-03.yaml"
	backup=""
	seeded_output=""
	backup="$(mktemp)" || fail "could not create backup tempfile"
	seeded_output="$(mktemp)" || fail "could not create output tempfile"
	cp "${fragment}" "${backup}" || fail "could not back up ${fragment}"
	checksum_before=""
	checksum_before="$(shasum -a 256 "${fragment}" | cut -d' ' -f1)"

	# Delete only the FIRST occurrence of the shared line: the trigger sits
	# above generator_inputs in this fragment, and the inputs entry carries
	# identical text. A global delete would remove the declared input too
	# and prove nothing about trigger coverage.
	awk '!dropped && $0 == "      - \"go/**/*.go\"" {dropped=1; next} {print}' \
		"${fragment}" >"${backup}.seeded" || fail "could not seed ${fragment}"
	mv "${backup}.seeded" "${fragment}" || fail "could not install seeded ${fragment}"

	if bash "${script}" --drift >"${seeded_output}" 2>&1; then
		status=0
	else
		status=$?
	fi
	# Restore before asserting: a `fail` below exits the whole run, and the
	# tree must never be left seeded.
	cp "${backup}" "${fragment}" || fail "could not restore ${fragment}"
	checksum_after=""
	checksum_after="$(shasum -a 256 "${fragment}" | cut -d' ' -f1)"

	[[ "${status}" -ne 0 ]] ||
		fail "seeded generator-input gap passed ${script} --drift (exit 0); want the #9588 shape to fail"
	rg --fixed-strings 'generator input "go/**/*_test.go"' "${seeded_output}" >/dev/null ||
		fail "seeded run names no uncovered go/**/*_test.go class — see ${seeded_output}"
	[[ "${checksum_before}" == "${checksum_after}" ]] ||
		fail "fragment restore mismatch on ${fragment}"
	rm -f "${backup}" "${seeded_output}"
	printf 'PASS: generator_inputs drift subcheck fails on the seeded #9588-shaped gap\n'
}
