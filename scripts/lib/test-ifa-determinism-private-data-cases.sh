#!/usr/bin/env bash
# shellcheck disable=SC2154
# Private-data scan for scripts/test-verify-ifa-determinism.sh, extracted so
# that mirror stays under the repository's 500-line cap -- the same split, for
# the same reason, as the teeth and pin-behaviour case modules beside it. The
# sibling fault mirror moved its own assert_no_private_data into
# test-ifa-fault-injection-assertions.sh on exactly this reasoning.
#
# `fail`, `script`, `repo_root` and the *_lib variables come from the parent.
# The mirror's own path is passed in rather than read from ${BASH_SOURCE[0]},
# which inside this file names this file, not the mirror.
#
# It must run AFTER every case module has been sourced: the *_lib derivation is
# `compgen -v`, so it covers exactly the libraries that are bound at call time.
run_ifa_determinism_private_data_cases() {
	local mirror="$1" private_lib_var private_target lib_cap_checked private_pattern
	# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
	#
	# DERIVED, not hand-typed. The previous form named five targets out of the
	# fourteen *_lib variables this mirror declares, and missed every one this
	# branch adds -- codeowners, fixtures, family-cases, registry-lockstep-cases,
	# pins and registry-family. A hand-typed scan does not grow when the tree does,
	# which is the same defect the sibling fault mirror carried until it was
	# derived; this is that fix applied here.
	#
	# The pattern brackets one character per alternative so it does not match its
	# own definition now that the scan covers this file too. A bracketed
	# single-character class matches exactly the text the bare character does, so
	# detection is unchanged -- verified alternative by alternative.
	private_pattern='gh[p]_|github_pa[t]_|glpa[t]-|AKI[A]|ASI[A]|xo[x][baprs]-|arn:aw[s]:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/[U]sers/|/[h]ome/[a-z]'
	local -a private_targets=("${script}" "${mirror}")
	lib_cap_checked=0 # floors the *_lib loop itself -- see _ifa_det_assert_lib_cap_floor's own comment
	while IFS= read -r private_lib_var; do
		private_targets+=("${!private_lib_var}")
		_ifa_det_assert_lib_under_500 "${private_lib_var}" "${!private_lib_var}"; lib_cap_checked=$((lib_cap_checked + 1))
	done < <(compgen -v | rg '_lib$' | sort)
	_ifa_det_assert_lib_cap_floor "${lib_cap_checked}"
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
	for private_target in "${private_targets[@]}"; do
		[[ -f "${private_target}" ]] \
			|| fail "private-data scan target ${private_target} does not exist -- a scan that skips a missing file proves nothing"
		if rg --pcre2 --quiet -- "${private_pattern}" "${private_target}"; then
			fail "$(basename "${private_target}") looks like it contains private data"
		fi
	done
	printf 'private-data scan: %s file(s) scanned\n' "${#private_targets[@]}"
}
