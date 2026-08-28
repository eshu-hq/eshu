#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced test helper reads parent-owned paths.
# The pin-helper META-GATE for scripts/test-verify-ifa-fault-injection.sh: the
# synthetic probe files, the prose-helper allowlist, and
# assert_pin_helpers_bind_code itself. The parent verifier owns strict mode,
# fail(), and all target path variables; the pin helpers this gate executes are
# defined in test-ifa-fault-injection-assertions.sh, so this module MUST be
# sourced after that one.
#
# Split out of that file under #6261. It sat at exactly 499 lines against the
# `< 500` cap for every commit of #6161's review cycle, and the ceiling produced
# two P1s of the same class the file exists to prevent -- a probe `printf`
# appended after a trailing `#` became a comment, so the file it was supposed to
# write never existed and the assertion that expects a failing helper passed
# unconditionally; and a truncation inlined to win back a line then got mirrored
# into its sibling. The probe writers below are one statement per line again
# because of that, and both halves now have headroom rather than none.
# assert_pin_helpers_bind_code is a META-GATE: it asserts that every pin helper
# in either mirror routes through a code-portion matcher, unless it is on the
# hand-written prose allowlist below.
#
# It exists because the alternative -- converting helpers from a hand-enumerated
# list -- missed pins in two consecutive review rounds, and nothing in
# the tree noticed either time. A list that must be kept complete by hand is the
# same defect class these mirrors exist to catch, one level up. This gate makes
# a forgotten helper a RED instead of a finding.
#
# PROSE_PIN_HELPERS is deliberately hand-written and deliberately short. Adding a
# name here is a claim that the helper binds documentation, not behaviour, and
# every current entry was verified by measuring code-portion matches = 0 for all
# of its needles. If you add one, do that measurement first -- a pin aimed at the
# wrong FILE also measures zero, and that is how --no-compose came to be
# misfiled as prose while its parser went unpinned.
# _ifa_pin_probe_run executes one pin helper against one synthetic target file.
# Every *_lib-shaped variable (and ${script}) is repointed at that file inside a
# subshell, so whichever target the helper happens to read, it reads the probe.
# fail() is redefined to exit rather than abort the whole mirror.
_ifa_pin_probe_run() {
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
# Helpers that deliberately bind PROSE, and are therefore exempt from the
# behavioural probe below. Every entry is a claim that the pin's subject is
# documentation, not behaviour. Verify by measuring code-portion matches = 0 for
# all of its needles BEFORE adding one -- a pin aimed at the wrong FILE also
# measures zero, which is how --no-compose came to be filed as prose while its
# parser went unpinned.
#
# require_delivery_cells_multiline is here for a different reason: it pins a
# whole multi-line invocation with `rg -U`, braces included, so commenting any
# inner line breaks the match. It is comment-immune by construction, but it
# cannot pass a single-line probe.
# require_catalog binds prose in docs/internal/ifa-fault-cell-catalog.md, the
# numbered cell list that moved out of the gate script when it hit its 500-line
# cap (#6212). Both of its needles were measured against the precondition above
# before it was added here: each matches the catalog exactly once, each matches
# the gate script zero times, and the catalog has zero code-portion lines because
# it is entirely markdown. The wrong-FILE trap the comment warns about is what
# the first of those three measurements rules out -- a pin aimed at nothing also
# reports zero code matches.
IFA_PIN_PROSE_HELPERS="require require_framing require_delivery_cells_multiline require_catalog"
assert_pin_helpers_bind_code() {
	# BEHAVIOURAL, not textual. Earlier versions of this gate discovered helpers
	# with a regex and judged them by substring-matching their bodies. Both halves
	# were defeated in review: `require_x ()`, `function require_x`, a leading tab
	# and a digit in the name all escaped discovery, and a body that merely
	# MENTIONED the matcher in a comment -- or called it and discarded the result --
	# passed the body check. That is the same "a comment satisfies a pin" defect
	# this whole mechanism exists to end, rebuilt one level up inside the gate
	# meant to end it.
	#
	# So nothing here reads code. Bash enumerates its own functions, and each
	# helper is EXECUTED against a synthetic target where its needle appears only
	# in a comment, then only inside a heredoc. A helper that passes either probe
	# is not binding code, whatever it is implemented with. Spelling, formatting,
	# comments about the matcher, and ignoring the matcher's return value all stop
	# mattering, because none of them survives being run.
	local probe_dir needle live_needle fn rc checked=0
	needle='__ifa_pin_probe_needle__'
	# The LIVE control, asserted in the opposite direction below.
	live_needle=': "${__ifa_pin_probe_live__:=x}"'
	probe_dir="$(mktemp -d -t ifa-pin-probe.XXXXXX)"
	printf '#!/usr/bin/env bash\n# %s\n:\n' "${needle}" >"${probe_dir}/comment_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF%s\n%s\nIFAEOF\n:\n' "'" "'" "${needle}" >"${probe_dir}/heredoc_only.sh"
	# Unpacked one statement per line (#6261). These five writers were packed
	# onto one line to stay under the 500-line cap, and that is how a sixth
	# printf came to be appended after a trailing `#`: it read as a comment, the
	# probe file was never written, the loop below probed a missing path, every
	# helper failed for THAT reason, and the assertion passed unconditionally
	# while printing a six-class coverage message it had not earned.
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF%s >/dev/null\n%s\nIFAEOF\n:\n' "'" "'" "${needle}" >"${probe_dir}/heredoc_redirect_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF >/dev/null\n%s\nIFAEOF\n:\n' '\' "${needle}" >"${probe_dir}/heredoc_bslash_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF  # parked\n%s\nIFAEOF\n:\n' '' "${needle}" >"${probe_dir}/heredoc_comment_tail_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF-1\n%s\nIFAEOF-1\n:\n' '' "${needle}" >"${probe_dir}/heredoc_hyphen_delim_only.sh"
	# The needle parked as an argument to a null command (#6194). `:` discards
	# its arguments, so `:  'trap ifa_det_cleanup EXIT'` installs nothing and a
	# pin satisfied by that line is bound to a line that does not run.
	printf '#!/usr/bin/env bash\n:  %s%s%s\n' "'" "${needle}" "'" >"${probe_dir}/null_command_only.sh"
	# The same line with the trailing comment a human actually types: the `;` in
	# it read as a separator, so the line stayed classified live, the comment
	# strip then removed the comment and kept the needle, and the pin passed
	# over a trap that is never installed.
	printf '#!/usr/bin/env bash\n:  %s%s%s # disabled; see (#6194)\n' "'" "${needle}" "'" >"${probe_dir}/null_command_comment_tail.sh"
	# ...and a metacharacter INSIDE the quotes, which cannot act: single quotes
	# are absolute in bash.
	printf '#!/usr/bin/env bash\n:  %s%s; parked || true%s\n' "'" "${needle}" "'" >"${probe_dir}/null_command_quoted_meta.sh"
	# `true` and `false` discard their arguments exactly as `:` does.
	printf '#!/usr/bin/env bash\ntrue  %s%s%s\n' "'" "${needle}" "'" >"${probe_dir}/null_command_true.sh"
	printf '#!/usr/bin/env bash\nfalse  %s%s%s\n' "'" "${needle}" "'" >"${probe_dir}/null_command_false.sh"
	# The one POSITIVE: `: "${VAR:=default}"` ASSIGNS, so it is live code and must
	# still be counted. The `$` doubt rule is the only thing keeping it counted.
	# The needle here is the whole line, so a whole-line helper accepts it too.
	printf '#!/usr/bin/env bash\n%s\n' "${live_needle}" >"${probe_dir}/null_command_live_expansion.sh"
	printf '#!/usr/bin/env bash\n%s\n' "${needle}" >"${probe_dir}/real_code.sh"

	while IFS= read -r fn; do
		case " ${IFA_PIN_PROSE_HELPERS} " in *" ${fn} "*) continue ;; esac
		checked=$((checked + 1))
		# Arity is discovered by trial, not declared: require_generic_cells_count
		# takes an expected COUNT as a third argument, and a 2-arg probe would fail
		# on real code and be misread as "binding". Probe with the shape that
		# actually passes on live code, then use that same shape for the negatives.
		local -a extra=()
		if ! _ifa_pin_probe_run "${fn}" "${probe_dir}/real_code.sh" "${needle}"; then
			extra=(1)
			_ifa_pin_probe_run "${fn}" "${probe_dir}/real_code.sh" "${needle}" "${extra[@]}" \
				|| fail "${fn}() rejected a needle that IS live code under both call shapes -- the probe cannot distinguish binding from broken, so its comment result proves nothing"
		fi
		local probe
		for probe in comment_only heredoc_only heredoc_redirect_only heredoc_bslash_only heredoc_comment_tail_only heredoc_hyphen_delim_only null_command_only null_command_comment_tail null_command_quoted_meta null_command_true null_command_false; do
			# Every *_lib-shaped variable is repointed at the probe file, so whichever
			# target this helper happens to read, it reads the probe.
			rc=0; [[ -s "${probe_dir}/${probe}.sh" ]] && rg -qF -- "${needle}" "${probe_dir}/${probe}.sh" || fail "probe ${probe}.sh was not written or lacks the needle; the negative below then fails for the wrong reason and this assertion passes unconditionally"
			_ifa_pin_probe_run "${fn}" "${probe_dir}/${probe}.sh" "${needle}" "${extra[@]}" || rc=$?
			[[ "${rc}" -ne 0 ]] \
				|| fail "${fn}() accepted a needle that appears only in a ${probe//_/ } -- it is not binding code, so a commented-out or dead call site would satisfy every pin that uses it"
		done
		# The one positive: a null command that DOES have an effect must still be
		# counted, or this rule stops removing only dead lines and starts hiding
		# live ones -- a false RED for whoever hits it, and a silently weaker pin
		# for everyone else.
		rg -qF -- "${live_needle}" "${probe_dir}/null_command_live_expansion.sh" \
			|| fail "probe null_command_live_expansion.sh lacks its needle; the acceptance check next to it would then pass or fail for the wrong reason"
		_ifa_pin_probe_run "${fn}" "${probe_dir}/null_command_live_expansion.sh" "${live_needle}" "${extra[@]}" \
			|| fail "${fn}() rejected ${live_needle}, which ASSIGNS and is therefore live code -- the doubt rule that keeps a \$-bearing null command counted has been lost, so every pin on that idiom now counts one less"
	done < <(compgen -A function | rg '^require' | sort)

	rm -rf "${probe_dir}"
	[[ "${checked}" -ge 20 ]] \
		|| fail "pin-helper behaviour check exercised only ${checked} helper(s); discovery has collapsed and this gate is checking nothing"
	printf 'pin-helper behaviour check: %s helper(s) executed against comment, heredoc, trailing-redirection, backslash-delimiter, comment-tail and hyphen-delimiter heredoc, bare, comment-tailed, quoted-metacharacter, true and false null-command probes, and the live null-command control\n' "${checked}"
}
