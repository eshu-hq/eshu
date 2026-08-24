#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced helper; `fail` and `script` are parent-owned.
# Shared pin helpers for the smaller Ifá gate mirrors --
# test-verify-ifa-dead-letter-matrix.sh and test-verify-ifa-replay-drive.sh.
#
# WHY THIS EXISTS (#6161). Both of those mirrors pinned their gate with a bare
# `rg --fixed-strings` over the WHOLE target file, so a COMMENT satisfied an
# assertion. Commenting out `set -euo pipefail` in verify-ifa-replay-drive.sh
# left the mirror green, because the phrase also appears in that script's header
# prose. Deleting the drive-populated guard outright left it green too. The
# determinism and fault-injection mirrors were converted away from that form
# already; these two were missed, and carried 58 code-binding assertions on it.
#
# The matcher below is a DELIBERATE COPY of the semantics in
# test-ifa-fault-injection-assertions.sh's _ifa_count_code_matches, which took
# several review rounds to settle. Do not "improve" it here in isolation: the
# lesson from #6173 is that extending a textual model of bash comment syntax one
# character class at a time does not converge, because that model re-implements
# the shell's lexer in parameter expansion. If it must change, change it there
# and here together, and re-run the behavioural probe at the bottom of this file.
#
# WHAT IT DOES NOT DO, stated plainly:
#   - It does not parse bash. It tracks unquoted heredoc delimiters and cuts the
#     line at a `#` that follows whitespace or one of `; | & ( ) < > \``.
#   - A `#` inside a QUOTED STRING therefore truncates the line early, so a
#     needle sitting after it reads as ABSENT and the pin REDS. That direction is
#     chosen: a false RED is a human minute, a false GREEN is a dead gate.
#   - It counts LINES, not occurrences. Two needles on one line count 1, which
#     again can only over-report absence.
#   - A quoted heredoc delimiter on a line that also carries other code is
#     tracked by delimiter only, with no attempt to model quoting.

# IF YOU ARE ADDING A NEW Ifá MIRROR, READ THIS.
#
# SOURCE THIS FILE and use the `require` it defines. Do NOT hand-roll a local
#     require() { rg --fixed-strings --quiet -- "$2" "${script}" || fail ...; }
# in your new mirror. That form is satisfied by a COMMENT, and it is how
# test-verify-ifa-dead-letter-matrix.sh and test-verify-ifa-replay-drive.sh
# between them accumulated 58 assertions that could not fail (#6161). Then call
# ifa_mirror_assert_pins_bind_code at the very end of your mirror, and pin that
# call with an exact count the way the two mirrors here do.
#
# IF YOU AUDIT THESE PINS, TWO THINGS COST US A ROUND EACH. Both are ways of
# getting a confident answer that is not an answer, which is the same shape as
# the defect the pins themselves have.
#
#   1. ENUMERATE BY OPERATOR, NOT BY NAME. To find the pins a deletion can
#      survive you want every `-ge 1` check whose needle has more than one code
#      occurrence. The reliable way is to instrument the counters and log what
#      they actually evaluated; deriving it from the source by regex measures
#      what the source looks like instead. When the #6161 census did that, it
#      still excluded one check on the grounds that its NEIGHBOURS were all
#      exact-count -- and that one was `-ge 1`, over a three-occurrence needle,
#      guarding the private-data file list. One assumption in an otherwise
#      measured census, and the assumption is where the miss was.
#
#   2. SEED WITHOUT BREAKING SYNTAX. To test whether a pin can fail, change the
#      line rather than delete it: replace the call with `true`, or alter a
#      character inside an identifier. DELETING the only line inside an `if`
#      block, or mangling a trailing `then`, makes the mirror red on `bash -n`
#      -- which looks exactly like the pin doing its job and proves nothing
#      about the pin. That is the mirror image of a seed that silently no-ops
#      and reads as a clean pass. Assert both directions: that the edit landed
#      (the needle's code-match count dropped by exactly one) and that the file
#      still parses.
#
# EVERY CENSUS FIGURE CARRIES THE COMMIT IT WAS MEASURED AT. Method, so it can
# be re-run rather than re-argued: wrap all three code-portion counters
# (ifa_mirror_count_code_matches here, _ifa_count_code_matches and
# _ifa_det_count_code_matches in the two large mirrors) to log count / caller
# file / caller line / target / needle; run all four mirrors; drop the
# meta-gate's synthetic probe targets; classify each evaluation weak or exact by
# reading the comparison on the caller's own line -- by OPERATOR, per point 1
# above, never by helper name.
#
#   commit                            weak `-ge 1` pins over >=2 code occurrences
#   71e0da200b (commit  2 of 19)      68 pins / 186 occurrences
#   339afbc2f0 (commit 11 of 19)      42 pins / 119 occurrences
#   92afe8a388 (commit 19 of 19)      17 pins /  55 occurrences
#   93cc866fd6 (this comment's parent) 14 pins /  49 occurrences
#
# "42 pins / 119 occurrences" was written up as this audit's OPENING count. It
# reproduces exactly -- at commit ELEVEN, on a tree already carrying ten commits
# of repairs. The opening figure is 68 / 186. The distinction is not pedantry:
# the "20 repaired plus 6 cleared" tally was reconciled against 42, so it read as
# a complete accounting of a population that was never 42. Earlier than
# 71e0da200b there is no comparable number at all -- the two smaller mirrors had
# no code-portion counter, so every one of their assertions was satisfiable by a
# comment and "weak" describes all of them.
#
# FOURTEEN REMAIN at the figure above, listed rather than summarised so the next
# auditor starts from a measured list instead of building one. Four carry a
# written clearance beside them (cell_start in the cells and SQL-cells libs,
# `wall=%ss`, and `graph-dump -digest`); all four are reporting-only or bound by
# a separate guard. The other TEN have no stated disposition and have not been
# seeded -- they are candidates, not findings:
#
#   ifa_fault_injection_documentation_ack_barrier.sh
#     ifa_documentation_census_ack_holder (4), ifa_documentation_census_ack_waiter (3),
#     ifa_documentation_signal_ack_producers (2), ifa_documentation_join_ack_producers (2),
#     "1|true" (2)
#   ifa_fault_injection_driver.sh          ifa_documentation_stop_ack_producers (2)
#   ifa_fault_injection_documentation_cells.sh   ifa_documentation_claim_snapshot (2)
#   ifa_fault_injection_deployable_unit_cells.sh
#     ifa_deployable_unit_live_assert_empty_before_maintenance (3)
#   verify-ifa-determinism.sh
#     done < <(ifa_family_registry_names) (2),
#     "lower N, retry, or otherwise normalize this away" (2)
#
# A SECOND LIMIT, and the one most likely to bite: discovery is
# `compgen -A function | rg '^require'`, so the probe executes only helpers whose
# NAME starts with `require`. An identically weak helper named `pin_foo` is
# invisible to it -- measured, not assumed: adding `pin_foo()` with the old
# whole-file body plus a call site left the mirror green, while the same body
# named `require_foo()` went red. Name new pin helpers `require*`. The narrower
# rule is deliberate for the same reason as the paragraph below: matching every
# plausible helper name would put the guard back in the business of guessing.
#
# A KNOWN AND DELIBERATELY UNGUARDED LIMIT: nothing in this repository forces
# you to do any of that. The probe below makes a weak helper red WITHIN a mirror
# that runs it, but a brand-new mirror that sources nothing and calls nothing is
# invisible to every gate in the tree. That hole is left open on purpose. Closing
# it means detecting pin-helper DEFINITIONS textually across files, and #6173
# already recorded that exact approach being defeated in review by `require_x ()`,
# by `function require_x`, by a leading tab, and by a digit in the name -- a guard
# that looks like it covers the family while missing members is worse than a
# documented limit, because it invites the trust it has not earned. So the
# guarantee here is honest and narrow: it binds mirrors that opt in, and this
# paragraph is the only thing binding the ones that have not yet been written.

# ifa_mirror_count_code_matches counts lines of ${2} whose CODE portion contains
# ${1}. Lines that are wholly comments, and lines inside a heredoc body, are not
# code and are skipped.
ifa_mirror_count_code_matches() {
	local needle="$1" file="$2" n=0 line stripped code heredoc=""
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
		if [[ -n "${heredoc}" ]]; then
			[[ "${stripped}" == "${heredoc}" ]] && heredoc=""
			continue
		fi
		if [[ "${line}" =~ \<\<-?[[:space:]]*[\'\"]?([A-Za-z_][A-Za-z0-9_]*)[\'\"]?[[:space:]]*$ ]]; then
			heredoc="${BASH_REMATCH[1]}"
		fi
		[[ "${stripped}" == "#"* ]] && continue
		code="${line%%[[:space:]\;\|\&\(\)\<\>\`]#*}"
		[[ "${code}" == *"${needle}"* ]] && n=$((n + 1))
	done < "${file}"
	printf '%s\n' "${n}"
}

# require is the DEFAULT pin, and it binds LIVE CODE. That default is the whole
# point: the previous default was the weak whole-file match, so every assertion
# anyone added inherited the defect. Getting the weak behaviour now costs a
# deliberately-named helper plus an allowlist entry below, which is a decision a
# reviewer can see instead of an accident nobody can.
require() {
	local label="$1" needle="$2"
	[[ "$(ifa_mirror_count_code_matches "${needle}" "${script}")" -ge 1 ]] \
		|| fail "missing ${label}, or it survives only inside a comment or heredoc: ${needle}"
}

# require_count pins a needle that must appear an EXACT number of times. Reach
# for it when the same line legitimately appears more than once and EVERY
# occurrence is load-bearing, because `require` is satisfied by any one survivor:
# verify-ifa-dead-letter-matrix.sh carries the identical `down -v` teardown twice
# -- once in the exit trap, once per cell inside the loop -- and deleting the
# per-cell one left the mirror green while cells N=1/2/4 ran against a
# contaminated stack, so the cross-worker dead-letter-set comparison compared
# accumulated state (#6161). No single-line needle can tell two identical lines
# apart, so the count is what binds them. Copied from the shape settled on the
# fault-injection side (require_documentation_barrier_count).
require_count() {
	local label="$1" needle="$2" want="$3" got
	got="$(ifa_mirror_count_code_matches "${needle}" "${script}")"
	[[ "${got}" == "${want}" ]] \
		|| fail "${label}: expected ${want} code occurrence(s), found ${got} -- an identical sibling line keeps a -ge 1 pin green when one of the two is deleted: ${needle}"
}

# IFA_MIRROR_PROSE_HELPERS lists helpers exempt from the behavioural probe
# because they deliberately bind DOCUMENTATION rather than behaviour. It is
# empty on purpose: neither mirror currently has a prose pin. Before adding a
# name, measure code-portion matches = 0 for every one of that helper's needles
# -- a pin aimed at the WRONG FILE also measures zero, and that is how a live
# flag came to be filed as prose while its parser went unpinned.
: "${IFA_MIRROR_PROSE_HELPERS:=}"

# ifa_mirror_assert_pins_bind_code is the META-GATE, and it is what keeps this
# fix from being forgotten. It is BEHAVIOURAL, not textual: bash enumerates its
# own functions with `compgen -A function`, and each pin helper is EXECUTED
# against synthetic targets where the needle appears only in a comment, then only
# inside a heredoc, then as real code. A helper that accepts either negative is
# not binding code, whatever it is implemented with -- so a new helper added
# tomorrow is covered the moment it is defined AND NAMED `require*`, and a
# forgotten conversion is a RED rather than a review finding. The naming half of
# that is a real condition, not a formality: see the second limit above.
# Earlier textual versions of this idea were
# defeated by `require_x ()`, `function require_x`, and by bodies that merely
# MENTIONED the matcher; none of that survives being run.
#
# Call it LAST, after every helper is defined, since discovery is by scope.
ifa_mirror_assert_pins_bind_code() {
	local probe_dir needle fn probe checked=0
	needle='__ifa_mirror_pin_probe__'
	probe_dir="$(mktemp -d -t ifa-mirror-pin-probe.XXXXXX)"
	printf '#!/usr/bin/env bash\n# %s\n:\n' "${needle}" >"${probe_dir}/comment_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF%s\n%s\nIFAEOF\n:\n' "'" "'" "${needle}" >"${probe_dir}/heredoc_only.sh"
	printf '#!/usr/bin/env bash\n%s\n' "${needle}" >"${probe_dir}/real_code.sh"
	local -a extra=()
	while IFS= read -r fn; do
		case " ${IFA_MIRROR_PROSE_HELPERS} " in *" ${fn} "*) continue ;; esac
		checked=$((checked + 1))
		# Arity is discovered by TRIAL, not declared: require_count takes an
		# expected count as a third argument, and probing it with two arguments
		# fails on real code, which the loop below would then misread as "this
		# helper binds code". Find the call shape that passes on live code first,
		# then run the negatives with that same shape.
		extra=()
		if ! _ifa_mirror_pin_probe_run "${fn}" "${probe_dir}/real_code.sh" "${needle}"; then
			extra=(1)
			_ifa_mirror_pin_probe_run "${fn}" "${probe_dir}/real_code.sh" "${needle}" "${extra[@]}" \
				|| fail "${fn}() rejected a needle that IS live code under both call shapes -- the probe cannot tell binding from broken"
		fi
		for probe in comment_only heredoc_only; do
			if _ifa_mirror_pin_probe_run "${fn}" "${probe_dir}/${probe}.sh" "${needle}" "${extra[@]}"; then
				fail "${fn}() accepted a needle that appears only in a ${probe%%_*} -- it is not binding code, so a commented-out or deleted call site would satisfy every pin that uses it"
			fi
		done
	done < <(compgen -A function | rg '^require' | sort)
	rm -rf "${probe_dir}"
	# Both mirrors that source this file execute exactly two pin helpers, so the
	# floor is 2, not 1. At -ge 1 the message claimed a property it did not have:
	# renaming require() to pin() -- which is precisely how discovery collapses,
	# since the loop below matches on the NAME -- halved the set, left the check
	# reporting "1 helper(s) executed", and passed. That let the pre-#6161
	# whole-file matcher back in with the mirror green. Siblings floor at 20 and
	# 5 for the same reason: a floor below the real count only catches total
	# collapse, and total collapse is the one case that is obvious anyway.
	[[ "${checked}" -ge 2 ]] \
		|| fail "pin-helper behaviour check exercised ${checked} helper(s), expected at least 2; discovery has collapsed or a pin helper was renamed out of it"
	printf 'pin-helper behaviour check: %s helper(s) executed against comment and heredoc probes\n' "${checked}"
}

# _ifa_mirror_pin_probe_run executes one pin helper against one synthetic target.
# Every ${script}/*_lib-shaped variable is repointed at that file inside a
# subshell, so whichever target the helper reads, it reads the probe. fail() is
# redefined to exit the subshell rather than abort the whole mirror.
_ifa_mirror_pin_probe_run() {
	local fn="$1" target="$2"
	shift 2
	(
		fail() { exit 7; }
		local probe_var=""
		while IFS= read -r probe_var; do
			printf -v "${probe_var}" '%s' "${target}"
		done < <(compgen -v | rg '_lib$|^lib$|^script$')
		"${fn}" "pin-probe" "$@"
	) >/dev/null 2>&1
}
