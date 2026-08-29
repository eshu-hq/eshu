#!/usr/bin/env bash
# shellcheck disable=SC2154
# Behavioural pin-helper gate for scripts/test-verify-ifa-determinism.sh,
# extracted so that mirror stays under the repository's 500-line cap. `fail`,
# `repo_root` and the *_lib variables come from the parent.
#
# It must run LAST, after every case module has been sourced: discovery is
# `compgen -A function`, so it sees exactly the helpers that are loaded.
run_ifa_determinism_pin_behaviour_cases() {
	local det_probe_dir det_needle det_live_needle det_fn det_probe det_pin_checked=0
	local -a det_extra=()
	local IFA_DET_PROSE_HELPERS="require"
	det_probe_dir="$(mktemp -d -t ifa-det-pin-probe.XXXXXX)"
	det_needle='__ifa_det_pin_probe__'
	det_live_needle=': "${__ifa_det_pin_probe_live__:=x}"'
	printf '#!/usr/bin/env bash\n# %s\n:\n' "${det_needle}" >"${det_probe_dir}/comment_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF%s\n%s\nIFAEOF\n:\n' "'" "'" "${det_needle}" >"${det_probe_dir}/heredoc_only.sh"
	# Same heredoc, opened with a TRAILING redirection. Heredoc recognition used
	# to be anchored at end-of-line, so `cat <<EOF >/dev/null` never entered
	# heredoc mode and its BODY counted as live code -- a pin could then be
	# satisfied by dead text (#6161).
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF%s >/dev/null\n%s\nIFAEOF\n:\n' "'" "'" "${det_needle}" >"${det_probe_dir}/heredoc_redirect_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF >/dev/null\n%s\nIFAEOF\n:\n' '\' "${det_needle}" >"${det_probe_dir}/heredoc_bslash_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF  # parked\n%s\nIFAEOF\n:\n' '' "${det_needle}" >"${det_probe_dir}/heredoc_comment_tail_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF-1\n%s\nIFAEOF-1\n:\n' '' "${det_needle}" >"${det_probe_dir}/heredoc_hyphen_delim_only.sh"
	# The null-command corpus: the needle parked as an argument to `:`, `true`
	# and `false` -- bare, with the trailing comment a human actually types, and
	# with a metacharacter inside single and inside double quotes -- plus one
	# live line per doubt-class character. It lives beside the rule it pins
	# (ifa_dead_command_line.sh), because a copy per mirror pins one mirror:
	# `require_code "exit trap" "trap ifa_det_cleanup EXIT"` stayed green with
	# the real trap rewritten as `:  'trap ifa_det_cleanup EXIT'` (#6194), and
	# each copy of the corpus was missing a different half of the cases.
	ifa_write_dead_command_probes "${det_probe_dir}" "${det_needle}"
	# The live control, asserted in the other direction below: `: "${VAR:=x}"`
	# assigns, so it must stay counted. Its needle is the whole line, so a
	# whole-line helper accepts it too.
	printf '#!/usr/bin/env bash\n%s\n' "${det_live_needle}" >"${det_probe_dir}/null_command_live_expansion.sh"
	printf '#!/usr/bin/env bash\n%s\n' "${det_needle}" >"${det_probe_dir}/real_code.sh"
	_ifa_det_pin_probe_run() {
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
	det_pin_checked=0
	while IFS= read -r det_fn; do
		case " ${IFA_DET_PROSE_HELPERS} " in *" ${det_fn} "*) continue ;; esac
		det_pin_checked=$((det_pin_checked + 1))
		# Arity is discovered by TRIAL, not declared. The *_count helpers take an
		# expected count as a third argument; probing one of them with two
		# arguments fails on real code, and the negatives below would then read
		# that failure as "this helper binds code" -- a probe that passes because
		# the helper is broken proves nothing. Find the call shape that works on
		# live code first, then run the negatives with that same shape. Copied
		# from the fault-injection mirror, which settled this in review.
		det_extra=()
		if ! _ifa_det_pin_probe_run "${det_fn}" "${det_probe_dir}/real_code.sh" "${det_needle}"; then
			det_extra=(1)
			_ifa_det_pin_probe_run "${det_fn}" "${det_probe_dir}/real_code.sh" "${det_needle}" "${det_extra[@]}" \
				|| fail "${det_fn}() rejected a needle that IS live code under both call shapes -- the probe cannot distinguish binding from broken"
		fi
		for det_probe in comment_only heredoc_only heredoc_redirect_only heredoc_bslash_only heredoc_comment_tail_only heredoc_hyphen_delim_only "${IFA_DEAD_COMMAND_DEAD_PROBES[@]}"; do
			[[ -s "${det_probe_dir}/${det_probe}.sh" ]] && rg -qF -- "${det_needle}" "${det_probe_dir}/${det_probe}.sh" || fail "probe ${det_probe}.sh was not written or lacks the needle; the negative below then fails for the wrong reason and this assertion passes unconditionally"
			if _ifa_det_pin_probe_run "${det_fn}" "${det_probe_dir}/${det_probe}.sh" "${det_needle}" "${det_extra[@]}"; then
				fail "${det_fn}() accepted a needle that appears only in a ${det_probe//_/ } -- it is not binding code, so a commented-out or dead call site would satisfy every pin that uses it"
			fi
		done
		# The one positive: a null command that DOES have an effect must still be
		# counted, or the rule stops removing dead lines and starts hiding live ones.
		rg -qF -- "${det_live_needle}" "${det_probe_dir}/null_command_live_expansion.sh" \
			|| fail "probe null_command_live_expansion.sh lacks its needle; the acceptance check below would then pass or fail for the wrong reason"
		_ifa_det_pin_probe_run "${det_fn}" "${det_probe_dir}/null_command_live_expansion.sh" "${det_live_needle}" "${det_extra[@]}" \
			|| fail "${det_fn}() rejected \`${det_live_needle}\`, which assigns and is live code -- the doubt rule that keeps a \$-bearing null command counted has been lost"
		# ...and one positive per member of the doubt class. `$` was the only one
		# with a control, so dropping any of the other nine from the class turned
		# a live line into a dead one and reddened nothing.
		ifa_assert_live_command_probes _ifa_det_pin_probe_run "${det_fn}" "${det_probe_dir}" "${det_needle}" "${det_extra[@]}"
	done < <(compgen -A function | rg '^require' | sort)
	rm -rf "${det_probe_dir}"
	[[ "${det_pin_checked}" -ge 5 ]] \
		|| fail "determinism pin-helper behaviour check exercised only ${det_pin_checked} helper(s); discovery has collapsed"
	printf 'pin-helper behaviour check: %s helper(s) executed against comment, heredoc, trailing-redirection, backslash-delimiter, comment-tail and hyphen-delimiter heredoc, bare, comment-tailed, quoted-metacharacter, true and false null-command probes, the live null-command control, and one live probe per doubt-class metacharacter\n' "${det_pin_checked}"

}
