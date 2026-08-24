#!/usr/bin/env bash
# shellcheck disable=SC2154
# Behavioural pin-helper gate for scripts/test-verify-ifa-determinism.sh,
# extracted so that mirror stays under the repository's 500-line cap. `fail`,
# `repo_root` and the *_lib variables come from the parent.
#
# It must run LAST, after every case module has been sourced: discovery is
# `compgen -A function`, so it sees exactly the helpers that are loaded.
run_ifa_determinism_pin_behaviour_cases() {
	local det_probe_dir det_needle det_fn det_probe det_pin_checked=0
	local -a det_extra=()
	local IFA_DET_PROSE_HELPERS="require"
	det_probe_dir="$(mktemp -d -t ifa-det-pin-probe.XXXXXX)"
	det_needle='__ifa_det_pin_probe__'
	printf '#!/usr/bin/env bash\n# %s\n:\n' "${det_needle}" >"${det_probe_dir}/comment_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF%s\n%s\nIFAEOF\n:\n' "'" "'" "${det_needle}" >"${det_probe_dir}/heredoc_only.sh"
	# Same heredoc, opened with a TRAILING redirection. Heredoc recognition used
	# to be anchored at end-of-line, so `cat <<EOF >/dev/null` never entered
	# heredoc mode and its BODY counted as live code -- a pin could then be
	# satisfied by dead text (#6161).
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF%s >/dev/null\n%s\nIFAEOF\n:\n' "'" "'" "${det_needle}" >"${det_probe_dir}/heredoc_redirect_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF >/dev/null\n%s\nIFAEOF\n:\n' '\' "${det_needle}" >"${det_probe_dir}/heredoc_bslash_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF  # parked\n%s\nIFAEOF\n:\n' '' "${det_needle}" >"${det_probe_dir}/heredoc_comment_tail_only.sh"
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
		for det_probe in comment_only heredoc_only heredoc_redirect_only heredoc_bslash_only heredoc_comment_tail_only; do
			if _ifa_det_pin_probe_run "${det_fn}" "${det_probe_dir}/${det_probe}.sh" "${det_needle}" "${det_extra[@]}"; then
				fail "${det_fn}() accepted a needle that appears only in a ${det_probe%%_*} -- it is not binding code, so a commented-out or dead call site would satisfy every pin that uses it"
			fi
		done
	done < <(compgen -A function | rg '^require' | sort)
	rm -rf "${det_probe_dir}"
	[[ "${det_pin_checked}" -ge 5 ]] \
		|| fail "determinism pin-helper behaviour check exercised only ${det_pin_checked} helper(s); discovery has collapsed"
	printf 'pin-helper behaviour check: %s helper(s) executed against comment, heredoc, trailing-redirection, backslash-delimiter and comment-tail heredoc probes\n' "${det_pin_checked}"

}
