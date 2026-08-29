#!/usr/bin/env bash
# shellcheck disable=SC2154
# Private-data scan for scripts/test-verify-ifa-determinism.sh, extracted so
# that mirror stays under the repository's 500-line cap -- the same split, for
# the same reason, as the teeth and pin-behaviour case modules beside it. The
# sibling fault mirror moved its own assert_no_private_data into
# test-ifa-fault-injection-assertions.sh on exactly this reasoning.
#
# `fail`, `script`, `repo_root`, `lib` and the *_lib variables come from the
# parent.
# The mirror's own path is passed in rather than read from ${BASH_SOURCE[0]},
# which inside this file names this file, not the mirror.
#
# It must run AFTER every case module has been sourced: the *_lib derivation is
# `compgen -v`, so it covers exactly the libraries that are bound at call time.
run_ifa_determinism_private_data_cases() {
	local mirror="$1" private_lib_var private_target lib_cap_checked private_pattern private_rc
	# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
	#
	# DERIVED, not hand-typed. The previous form named five targets out of the
	# fourteen *_lib variables this mirror declares, and missed every one this
	# branch adds -- codeowners, fixtures, family-cases, registry-lockstep-cases,
	# pins and registry-family. A hand-typed scan does not grow when the tree does,
	# which is the same defect the sibling fault mirror carried until it was
	# derived; this is that fix applied here.
	#
	# The pattern and its positive control live in ifa_private_data_pattern.sh,
	# which asserts one planted token per alternative still matches before it
	# hands the pattern over. It used to be a literal here and an identical
	# literal in the fault mirror's scan, verified "alternative by alternative"
	# by hand in a comment -- and nothing measured it, so replacing it with
	# anything unmatchable left this gate at exit 0 still printing its file count
	# (#6161). The helper assigns into private_pattern rather than printing,
	# because a `fail` inside a command substitution exits only the subshell.
	ifa_private_data_pattern private_pattern
	# ${lib} is NAMED, not derived. The shared ifa_determinism_common.sh is bound
	# to `lib`, which does not end in _lib, so the compgen derivation below cannot
	# see it -- this mirror scanned every library it declares EXCEPT the one all
	# three Ifá gates source. The sibling fault mirror covered it only by the
	# accident of spelling its own variable det_lib: renaming that variable, with
	# no behaviour change at all, carried a planted AWS key past all four mirrors
	# while they still printed their file counts (#6161). A named target breaks
	# loudly under `set -u` when it is renamed; a derived one just goes quiet.
	local -a private_targets=("${script}" "${mirror}" "${lib}")
	lib_cap_checked=0 # floors the *_lib loop itself -- see _ifa_det_assert_lib_cap_floor's own comment
	while IFS= read -r private_lib_var; do
		private_targets+=("${!private_lib_var}")
		_ifa_det_assert_lib_under_500 "${private_lib_var}" "${!private_lib_var}"; lib_cap_checked=$((lib_cap_checked + 1))
	done < <(compgen -v | rg '_lib$' | sort)
	_ifa_det_assert_lib_cap_floor "${lib_cap_checked}"
	# The floor that call applies is `-ge 18`, and its NUMBER was pinned by
	# nothing: lowering it to `-ge 1` left every gate at exit 0 (#6195), so the
	# one guard on the 500-line cap's own coverage could be neutered without
	# being deleted. Same defect as the scan floor two blocks below, one file
	# over. EXACTLY ONE, counted in the helpers lib rather than here, so this
	# line cannot satisfy itself; raising the floor is meant to cost both edits.
	[[ "$(_ifa_det_count_code_matches '"${checked}" -ge 18' "${require_helpers_lib}")" -eq 1 ]] \
		|| fail "the 500-line-cap coverage floor is no longer exactly 18 -- a lowered floor still passes on a collapsed *_lib derivation"
	# The registry, its rows and its hand-derived pins are not bound to *_lib vars,
	# so the derivation cannot see them. Globbed, so a seventh row is covered the
	# day it lands.
	for private_target in "${repo_root}"/scripts/lib/ifa_family_registry.sh \
		"${repo_root}"/scripts/lib/ifa_family_registry/rows/*.sh \
		"${repo_root}"/scripts/lib/ifa_family_registry_pins/*.sh; do
		[[ -e "${private_target}" ]] && private_targets+=("${private_target}")
	done
	# The glob block above is otherwise bound only by the floor, and only by a
	# 3-file margin -- three more *_lib declarations and deleting it would stop
	# reddening, which is the exact silent revert the sibling mirror was fixed
	# for. Pin it directly so the margin stops mattering.
	#
	# EXACTLY TWO: the glob block itself, and this pin's own line. An at-least-one
	# form is useless here -- a pin whose needle lives in the same file is always
	# satisfied by itself, which is how the previous version stayed green with the
	# whole glob block deleted. Counting is what makes an in-file pin able to fail.
	[[ "$(_ifa_det_count_code_matches 'ifa_family_registry_pins/*.sh' "${BASH_SOURCE[0]}")" -eq 2 ]] \
		|| fail "the determinism private-data scan no longer globs the registry rows and pins (expected the glob block plus this pin line)"
	# Floor against a collapsed derivation: if the *_lib expression stops matching,
	# the loop silently scans almost nothing and passes. Hand-written, below the
	# current count, never derived from the expression it guards.
	[[ "${#private_targets[@]}" -ge 20 ]] \
		|| fail "private-data scan covers only ${#private_targets[@]} file(s); the derivation has collapsed"
	# ...and the floor itself needs a pin, or it is the one guard here that can be
	# deleted in silence: every gate still passed with the two lines above removed
	# (#6195). EXACTLY TWO -- the floor line plus this pin's own line -- because an
	# in-file pin is always satisfied by itself, the defect #6173 had to fix twice.
	# The message deliberately does NOT quote the needle: a fail string carrying it
	# would count as a third match and make the pin unsatisfiable.
	[[ "$(_ifa_det_count_code_matches '"${#private_targets[@]}" -ge 20' "${BASH_SOURCE[0]}")" -eq 2 ]] \
		|| fail "the private-data scan floor was removed or altered (expected the floor line plus this pin's own line)"
	# The positive control is a guard, so it gets what every other guard here
	# gets: a pin, or it can be deleted in silence and the pattern goes back to
	# being handed over unchecked. EXACTLY ONE of each in the shared module --
	# the control loop's own assertion, and the hand-written sample count that
	# catches a deleted sample leaving one alternative unguarded.
	[[ "$(_ifa_det_count_code_matches '[[ "${rc}" -eq 0 ]] || {' "${private_data_pattern_lib}")" -eq 1 ]] \
		|| fail "the private-data pattern's positive control no longer asserts that each planted sample matches -- the pattern would be handed over unchecked"
	[[ "$(_ifa_det_count_code_matches '"${#samples[@]}" -eq 14' "${private_data_pattern_lib}")" -eq 1 ]] \
		|| fail "the private-data pattern's sample set is no longer counted at 14 -- add the sample for the new alternative and bump both numbers together"
	for private_target in "${private_targets[@]}"; do
		[[ -f "${private_target}" ]] \
			|| fail "private-data scan target ${private_target} does not exist -- a scan that skips a missing file proves nothing"
		# rc is CAPTURED, not tested through `if`. rg exits 2 on a pattern it
		# cannot compile, `if` reads that as "no match", and `set -e` does not
		# apply inside an `if` condition -- so one uncompilable pattern made every
		# file read as clean and this gate passed (#6161).
		private_rc=0
		rg --pcre2 --quiet -- "${private_pattern}" "${private_target}" || private_rc=$?
		[[ "${private_rc}" -ne 0 ]] \
			|| fail "$(basename "${private_target}") looks like it contains private data"
		[[ "${private_rc}" -eq 1 ]] \
			|| fail "the private-data scan could not run over $(basename "${private_target}") (rg exit ${private_rc}); a scanner that cannot run must never read as clean"
	done
	printf 'private-data scan: %s file(s) scanned\n' "${#private_targets[@]}"
}
