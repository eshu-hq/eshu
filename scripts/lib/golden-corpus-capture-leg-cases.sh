#!/usr/bin/env bash
# Seeded RED/GREEN cases for the differential job's "Require recordings from
# every capture leg" step in .github/workflows/golden-corpus-gate.yml (#6965
# phase 5). Sourced by golden-corpus-mirror-workflow-paths.sh, after
# golden-corpus-mirror-matcher.sh (defines require_in) and after ${workflow}
# is set.
#
# #6965 phase 5 made "Compare backend recordings" advisory (continue-on-error).
# Compare used to fail closed when one side recorded nothing, and statement
# coverage fails only when every capture dir is empty, so the capture-leg step
# is now the only blocking check that a one-sided capture loss cannot pass.
# These cases run the step's own run: text, extracted from the workflow, so an
# edit that breaks it (a flipped comparison, a glob that never matches, a
# dropped leg) fails here instead of shipping green.

capture_leg_step="Require recordings from every capture leg"

# The step's run: block, de-indented, with the runner.temp expression rewritten
# to ${RUNNER_TEMP} so it runs locally. Empty output means the step or its
# run: block is gone.
extract_capture_leg_run() {
	awk -v step="      - name: ${capture_leg_step}" '
		$0 == step { in_step = 1; next }
		in_step && /^      - name: / { exit }
		in_step && $0 == "        run: |" { in_run = 1; next }
		in_run && /^          / { sub(/^          /, ""); print; next }
		in_run && /^[[:space:]]*$/ { print ""; next }
		in_run { exit }
	' "${workflow}" | sed 's/\${{ runner\.temp }}/${RUNNER_TEMP}/g'
}

capture_leg_script="$(extract_capture_leg_run)"
[[ -n "${capture_leg_script}" ]] ||
	fail "golden-corpus-gate.yml: no run: block found for step '${capture_leg_step}'"
[[ "${capture_leg_script}" != *'${{'* ]] ||
	fail "golden-corpus-gate.yml: '${capture_leg_step}' uses an expression these cases cannot rewrite"

capture_leg_root="$(mktemp -d -t golden-corpus-capture-leg.XXXXXX)"
capture_leg_legs=(pair1/nornicdb pair1/neo4j pair2/nornicdb pair2/neo4j)

# seed_capture_legs writes one non-empty recording into every leg.
seed_capture_legs() {
	rm -rf "${capture_leg_root}/diff-capture"
	local leg
	for leg in "${capture_leg_legs[@]}"; do
		mkdir -p "${capture_leg_root}/diff-capture/${leg}"
		printf '{"record":{"backend":"x"}}\n' >"${capture_leg_root}/diff-capture/${leg}/capture.jsonl"
	done
}

# run_capture_leg_step runs the extracted step the way the runner does
# (bash -e with pipefail) and returns its exit status.
run_capture_leg_step() {
	RUNNER_TEMP="${capture_leg_root}" bash -eo pipefail -c "${capture_leg_script}" >/dev/null 2>&1
}

expect_capture_leg() {
	local label="$1" want="$2" got=0
	run_capture_leg_step || got=$?
	if [[ "${want}" == "pass" && "${got}" -ne 0 ]] || [[ "${want}" == "fail" && "${got}" -eq 0 ]]; then
		rm -rf "${capture_leg_root}"
		fail "capture-leg step (${label}): want ${want}, got exit ${got}"
	fi
}

# GREEN: every leg recorded.
seed_capture_legs
expect_capture_leg "all four legs recorded" pass

# RED, one leg at a time, so a step that checks only some legs fails a case.
for capture_leg_case in "${capture_leg_legs[@]}"; do
	seed_capture_legs
	: >"${capture_leg_root}/diff-capture/${capture_leg_case}/capture.jsonl"
	expect_capture_leg "${capture_leg_case} holds only an empty file" fail

	seed_capture_legs
	rm -f "${capture_leg_root}/diff-capture/${capture_leg_case}/capture.jsonl"
	expect_capture_leg "${capture_leg_case} holds no recording" fail

	seed_capture_legs
	rm -rf "${capture_leg_root:?}/diff-capture/${capture_leg_case}"
	expect_capture_leg "${capture_leg_case} directory missing" fail
done

# A file that is not *.jsonl is not a recording (capture.LoadDir globs *.jsonl).
seed_capture_legs
rm -f "${capture_leg_root}/diff-capture/pair2/neo4j/capture.jsonl"
printf 'x\n' >"${capture_leg_root}/diff-capture/pair2/neo4j/capture.json"
expect_capture_leg "pair2/neo4j holds only a non-jsonl file" fail

rm -rf "${capture_leg_root}"

# The advisory wiring the capture step exists to backstop: Compare carries the
# id the later steps read, and the upload also runs on an advisory failure.
require_in "golden-corpus-gate.yml Compare step id (#6965)" "${workflow}" "id: compare"
require_in "golden-corpus-gate.yml capture upload on advisory failure (#6965)" "${workflow}" \
	"if: failure() || steps.compare.outcome == 'failure'"

capture_leg_cases_completed=1
