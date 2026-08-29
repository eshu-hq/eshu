#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced helper; `fail` is parent-owned.
# The one rule the Ifá pin counters use to decide that a line EXECUTES NOTHING,
# so a needle sitting on it is data rather than live code.
#
# WHY THIS EXISTS (#6194). The counters skip comments and heredoc bodies, which
# closed every ordinary way to disable a line. One way was left: park the line
# as an argument to a null command.
#
#	:  'trap ifa_det_cleanup EXIT'
#
# `scripts/test-verify-ifa-determinism.sh` exited 0 with the exit trap never
# installed. Same false green as the here-doc case #6173 closed.
#
# WHAT THIS IS NOT. It is NOT "a quoted string is not code", and that is the
# whole design decision here. That rule was the obvious one and it is WRONG --
# measured, not argued. Instrumenting all three code-portion counters and
# running the four mirrors gives 456 matched lines; 182 of them (40%) have every
# occurrence of their needle sitting entirely inside one quoted word, and they
# are load-bearing:
#
#	source "${repo_root}/scripts/lib/ifa_determinism_common.sh"
#	die "graph-determinism matrix FAILED: digests diverged across worker counts"
#
# A needle inside quotes is ordinary code. What makes the #6194 shape dead is
# not the quoting, it is the COMMAND: `:` discards its arguments. So the rule
# below is about the command word, not about quotes, and it is closed rather
# than open-ended -- bash has exactly three builtins that do not execute their
# arguments, and this file names all three. That is deliberately unlike the
# textual comment model #6173 spent nine rounds extending one character class at
# a time without converging: there is no next character class here, because
# there is no fourth null command.
#
# EVERY DOUBT RESOLVES TO "LIVE". A line is called dead only when nothing on it
# can expand, redirect, or start another command. Any `$`, backtick, redirection,
# separator, subshell, or backslash OUTSIDE quotes and BEFORE a comment and the
# answer is "live", which is the status quo. So this rule can only ever
# REMOVE matches, never add them, and a wrong answer costs a false RED (a human
# minute) rather than a false GREEN (a dead gate) -- the same direction every
# other choice in these counters elects.
#
# WHERE a metacharacter sits decides whether it means anything, which is why the
# scan below tracks both kinds of quote and comments instead of searching the
# raw line.
# The first cut of this rule did search the raw line, and it left the #6194 false
# green fully open through the shape a human actually types:
#
#	:  'trap ifa_det_cleanup EXIT' # disabled; see #6194
#
# Nothing there executes -- `:` discards its argument and the rest is a comment --
# but the `;` in that comment read as a separator, the line was called live, the
# counters then stripped the same comment and kept the needle, and the pin passed
# over a trap that is never installed. `(`, `)`, `$`, `>`, `&` and `|` are just as
# natural in a comment (`# (see #6194)`, `# costs $5`, `# foo > bar`), and a needle
# with a metacharacter in it -- `docker compose down -v || true` -- carries the
# same problem inside the quotes. Single quotes are absolute in bash: no
# expansion, no escapes, no splitting, so nothing inside them can act. Double
# quotes are absolute for everything EXCEPT `$` and a backtick -- a backslash
# there quotes whatever follows, so it cannot make the line act either, but it
# does hide a `"` from the scan and has to be stepped over. The scan below
# tracks them on exactly those terms: `: "${VAR:=default}"` keeps
# reading live, while `:  "trap ifa_det_cleanup EXIT; parked"` reads dead, which
# is what closed the last version of the parking trick. Reading a `#` inside
# double quotes as a comment was the mirror-image bug in that same version --
# `: "see #6194" && trap ifa_det_cleanup EXIT` installs the trap, but the scan
# stopped at the `#` and called the line dead, so the pin on that trap counted
# nothing. A `#` only starts a comment outside both kinds of quote.
#
# WHY `: "${VAR:=default}"` SURVIVES, and why that mattered. Every null-command
# line in the counted corpus is that idiom -- `: "${GATE_DRAIN_TIMEOUT:=3m}"`,
# `: "${DETERMINISM_COMPOSE_PROJECT:=eshu-ifa-determinism-$$}"` -- and four of
# them are pinned. Those lines DO have an effect: the `:=` expansion assigns.
# They carry a `$`, so the doubt rule above keeps them live, and the whole-corpus
# differential over 456 matched lines confirms it: zero counts change.
#
# WHAT IT DOES NOT CATCH, stated plainly rather than left for the next reader:
#   - An assignment to an unused variable, `parked='trap ifa_det_cleanup EXIT'`.
#     Assignments are NOT covered and must not be: `worker_counts=(1 2 4)` and
#     `digests[${n}]=` are both real pins, and an assignment is load-bearing by
#     default. Excluding them is a decision, not an oversight.
#   - `printf '%s' 'trap ifa_det_cleanup EXIT' >/dev/null`. The redirection makes
#     the line live by the doubt rule, and `printf` is not a null command.
#   - `: "${x-trap ifa_det_cleanup EXIT}"`. It carries a `$`, so it reads live.
#     Narrowing that needs the expansion parser this file exists to avoid.
#   - A null command whose argument is continued from the PREVIOUS line. The
#     counters read line by line, so the continued line is judged on its own.
# Each of those is the false-GREEN direction and none of them is a shape an
# ordinary edit produces; they are named here so the next auditor starts from a
# measured list instead of building one.

# ifa_is_dead_command_line returns 0 when ${1} -- a line with its leading
# whitespace already stripped -- runs a null command over literal arguments and
# therefore executes nothing at all. It returns 1 for everything else, including
# everything it is unsure about.
ifa_is_dead_command_line() {
	local line="$1" rest ch prev="" i=0 n state=bare
	case "${line}" in
	# A bare null command carries no needle worth counting either way.
	: | true | false) return 0 ;;
	# `:`, `true` and `false` are the only bash builtins that never execute,
	# expand into, or otherwise act on their argument words. There is no fourth,
	# so this list is closed and not the beginning of a character-class ladder.
	:[[:space:]]* | true[[:space:]]* | false[[:space:]]*) ;;
	*) return 1 ;;
	esac
	rest="${line#*[[:space:]]}"
	n=${#rest}
	# One left-to-right pass over the arguments, carrying just enough state to
	# know whether a character means anything: which quoted span it is inside,
	# and whether a comment has started. Anything richer would be the expansion
	# parser this file exists to avoid.
	while [[ "${i}" -lt "${n}" ]]; do
		ch="${rest:i:1}"
		case "${state}" in
		# Inside single quotes bash interprets nothing at all, so a
		# metacharacter there is text and cannot make the line act. Only the
		# closing quote means anything.
		single) [[ "${ch}" == "'" ]] && state=bare ;;
		# Inside double quotes bash expands `$` and a backtick and nothing
		# else: `#`, `;`, `|`, `(` and a single quote are all literal there, so
		# a `#` inside double quotes is NOT a comment and a metacharacter
		# inside them is NOT a command separator. A backslash quotes the next
		# character, an escaped `"` included, so it cannot make the line act
		# and it must not be read as the end of the span either -- skip past
		# what it quotes. A backslash as the LAST character leaves the span
		# open, and the unclosed-quote check below then refuses to judge.
		double)
			case "${ch}" in
			'\') i=$((i + 1)) ;;
			'"') state=bare ;;
			'$' | '`') return 1 ;;
			esac
			;;
		*)
			case "${ch}" in
			"'") state=single ;;
			'"') state=double ;;
			# A `#` that starts a word starts a comment: everything after it is
			# not executed, so it cannot make the line act either.
			'#')
				if [[ -z "${prev}" || "${prev}" == [[:space:]] ]]; then
					return 0
				fi
				;;
			# Anything that can expand (`$`, backtick), redirect, escape, or
			# begin another command means the line is not provably inert. Live is
			# the safe answer. A backslash anywhere goes the same way: it may
			# quote the next character or continue the line onto the next one,
			# which this function never sees.
			'$' | '`' | '<' | '>' | '|' | ';' | '&' | '(' | ')' | '\') return 1 ;;
			esac
			;;
		esac
		prev="${ch}"
		i=$((i + 1))
	done
	# An unclosed quote is a line this pass cannot read. Refuse to judge it.
	[[ "${state}" == bare ]] || return 1
	return 0
}

# ifa_code_portion sets IFA_CODE_PORTION to the part of ${1} that bash would
# execute: everything before a `#` that starts a comment. It sets a variable
# rather than printing one because the counters call it once per line of every
# file they scan, and a command substitution there is a fork per line.
#
# It exists for the same reason as the scan above, and shares its blind spot if
# it does not: bash starts a comment at a `#` that begins a word OUTSIDE quotes,
# so `: "see #6194" && trap ifa_det_cleanup EXIT` is a line with no comment on
# it at all. The plain `${line%%[[:space:]\;\|\&\(\)\<\>\`]#*}` this replaces
# cut there anyway, dropped the `&&` and everything after it, and a pin on that
# trap counted zero -- the classifier called the line live and the counter then
# threw the live half away. Both halves of that had to move together.
#
# The two cheap paths first: a line with no `#` has no comment, and a line with
# a `#` but no quote cannot be hiding one, so only a line with both is scanned
# character by character -- 1,187 of the 76,081 code lines under `scripts/` and
# `.github/` (1.6%), measured. The rest pay one parameter expansion, as before.
# Over ten passes of verify-ifa-fault-injection.sh (3,900 line reads) the whole
# helper costs 104ms against the bare expansion's 61ms, same match count, and
# the four mirrors run in 37s/53s/1s/1s either way.
ifa_code_portion() {
	local line="$1" ch prev="" i=0 n state=bare
	case "${line}" in
	*'#'*) ;;
	*) IFA_CODE_PORTION="${line}"; return 0 ;;
	esac
	case "${line}" in
	*[\'\"]*) ;;
	*) IFA_CODE_PORTION="${line%%[[:space:]\;\|\&\(\)\<\>\`]#*}"; return 0 ;;
	esac
	n=${#line}
	while [[ "${i}" -lt "${n}" ]]; do
		ch="${line:i:1}"
		case "${state}" in
		# Single quotes are absolute: only the closing quote acts.
		single) [[ "${ch}" == "'" ]] && state=bare ;;
		# Inside double quotes a backslash still quotes the next character, so
		# `\"` does not end the span. Nothing else there starts a comment.
		double)
			case "${ch}" in
			'\') i=$((i + 1)) ;;
			'"') state=bare ;;
			esac
			;;
		*)
			case "${ch}" in
			'\') i=$((i + 1)) ;;
			"'") state=single ;;
			'"') state=double ;;
			# A comment starts at a `#` that begins a word: after whitespace,
			# after one of the metacharacters that end a command, or at the
			# start of the line. Anything else is `${#arr[@]}` or `${var#pfx}`,
			# and cutting there made every line using them unpinnable.
			'#')
				if [[ -z "${prev}" ]]; then
					IFA_CODE_PORTION=""
					return 0
				fi
				if [[ "${prev}" == [[:space:]\;\|\&\(\)\<\>\`] ]]; then
					IFA_CODE_PORTION="${line:0:i-1}"
					return 0
				fi
				;;
			esac
			;;
		esac
		prev="${ch}"
		i=$((i + 1))
	done
	IFA_CODE_PORTION="${line}"
	return 0
}

# THE PROBE CORPUS THAT PINS THE RULE ABOVE, and why it lives here rather than
# in the mirrors. Each mirror runs every one of its `require*` pin helpers
# against synthetic files and asserts a DIRECTION: a needle on a line that
# executes nothing must not be counted, a needle on a line that acts must be.
# The corpus started as a copy inside each mirror, and a copy pins one mirror --
# so `true` and `false` went unprobed in all three copies at once, and nine of
# the ten doubt-class characters had no probe in any of them. One corpus beside
# the rule it pins is the only shape where adding a case covers every caller.
#
# Two lists, one per direction, because the assertion differs: the dead ones
# must be REJECTED by a pin helper, the live ones must be ACCEPTED.

# IFA_DEAD_COMMAND_DEAD_PROBES names the probes whose needle is not live code.
# A helper that counts a needle here is not binding code, so a call site
# rewritten as `:  'the call'` would keep its pin green (#6194).
IFA_DEAD_COMMAND_DEAD_PROBES=(
	null_command_only
	null_command_comment_tail
	null_command_quoted_meta
	null_command_dquoted_meta
	null_command_dquoted_escape
	null_command_escaped_quote_comment
	null_command_true
	null_command_false
)

# IFA_DEAD_COMMAND_LIVE_PROBES names the probes whose needle IS live code and
# must stay counted. Each of the first ten carries exactly ONE member of the
# doubt class outside quotes, so dropping that one character from the class
# reddens exactly one probe. Before them the class had a single control (`$`,
# through the `: "${VAR:=default}"` idiom), and dropping any of the other nine
# made a line like `:  'needle'; rm -rf /` read dead with no gate noticing.
# `(` and `)` are the two that cannot be written as runnable bash on a null
# command line -- bash reaches them only after `$`, `<`, `>` or another `(`,
# and the scan returns on whichever comes first -- so those two probes are
# scanner input rather than a runnable line, which is all any counter does
# with a probe file. `$` is probed unquoted here because the pre-existing
# `: "${VAR:=x}"` control already covers `$` inside double quotes, and the two
# are separate branches of the scan.
#
# The last four are the quoted spans themselves, which the counters and the
# rule have to agree about. A `#` inside either kind of quote is text, so those
# two lines still run their `&&` tail and both needles must survive the
# counters' comment strip as well. A backtick inside double quotes runs a
# command substitution, so that line genuinely acts. The last one carries an
# ESCAPED double quote before its `#`: read as the end of the span, the `#`
# after it looks like a comment and the live `&&` tail is thrown away, so that
# single probe pins the backslash skip in the rule and in the code portion at
# once. Its inert sibling `:  "<needle> \$parked"` is in the DEAD list -- `\$`
# is literal text, so that line really does execute nothing.
IFA_DEAD_COMMAND_LIVE_PROBES=(
	null_command_live_meta_dollar
	null_command_live_meta_backtick
	null_command_live_meta_lt
	null_command_live_meta_gt
	null_command_live_meta_pipe
	null_command_live_meta_semicolon
	null_command_live_meta_amp
	null_command_live_meta_open_paren
	null_command_live_meta_close_paren
	null_command_live_meta_backslash
	null_command_live_squoted_hash
	null_command_live_dquoted_hash
	null_command_live_dquoted_backtick
	null_command_live_dquoted_escaped_quote
)

# ifa_write_dead_command_probes writes both lists into directory ${1}, with
# ${2} as the needle each probe carries. The needle is the caller's, so the
# same corpus works for mirrors that use different probe needles.
ifa_write_dead_command_probes() {
	local dir="$1" needle="$2" sq="'" dq='"'
	# Dead: `:` discards its arguments, so none of these lines installs
	# anything -- bare, with the trailing comment a human actually types, with
	# a metacharacter inside single and inside double quotes, and under the
	# other two members of the closed set.
	printf '#!/usr/bin/env bash\n:  %s%s%s\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_only.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s # disabled; see (#6194)\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_comment_tail.sh"
	printf '#!/usr/bin/env bash\n:  %s%s; parked || true%s\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_quoted_meta.sh"
	printf '#!/usr/bin/env bash\n:  %s%s; parked || true%s\n' "${dq}" "${needle}" "${dq}" >"${dir}/null_command_dquoted_meta.sh"
	printf '#!/usr/bin/env bash\n:  %s%s \\$parked%s\n' "${dq}" "${needle}" "${dq}" >"${dir}/null_command_dquoted_escape.sh"
	# An ESCAPED double quote outside any span opens nothing, so the `#` after
	# it really does start a comment and the needle behind it is not code. Read
	# the `\"` as the start of a quoted span instead and the comment looks
	# quoted, which is the false-GREEN direction: a needle in a plain comment
	# would count. This is the only probe covering that half of the skip.
	printf '#!/usr/bin/env bash\n:  parked %s%s # %s\n' "\\" "${dq}" "${needle}" >"${dir}/null_command_escaped_quote_comment.sh"
	printf '#!/usr/bin/env bash\ntrue  %s%s%s\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_true.sh"
	printf '#!/usr/bin/env bash\nfalse  %s%s%s\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_false.sh"
	# Live: one doubt-class character each, all outside the quotes.
	printf '#!/usr/bin/env bash\n:  %s%s%s $HOME\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_dollar.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s `true`\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_backtick.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s </dev/null\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_lt.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s >/dev/null\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_gt.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s | :\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_pipe.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s; :\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_semicolon.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s && :\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_amp.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s ( parked\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_open_paren.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s ) parked\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_close_paren.sh"
	printf '#!/usr/bin/env bash\n:  %s%s%s \\\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_meta_backslash.sh"
	# A `#` inside double quotes is TEXT, not a comment, so this line runs its
	# `&&` tail. The needle sits before the `#` so the counters' own trailing
	# comment strip leaves it in the code portion either way, and the only
	# thing being asked is whether the line reads live.
	printf '#!/usr/bin/env bash\n:  %s%s #6194%s && :\n' "${dq}" "${needle}" "${dq}" >"${dir}/null_command_live_dquoted_hash.sh"
	printf '#!/usr/bin/env bash\n:  %s%s #6194%s && :\n' "${sq}" "${needle}" "${sq}" >"${dir}/null_command_live_squoted_hash.sh"
	printf '#!/usr/bin/env bash\n:  %s%s `true`%s\n' "${dq}" "${needle}" "${dq}" >"${dir}/null_command_live_dquoted_backtick.sh"
	printf '#!/usr/bin/env bash\n:  %s%s \\%s #6194%s && :\n' "${dq}" "${needle}" "${dq}" "${dq}" >"${dir}/null_command_live_dquoted_escaped_quote.sh"
}

# ifa_assert_live_command_probes runs one pin helper (${2}) against every LIVE
# probe in ${3} and fails unless the helper counts that probe's line. ${1} is
# the mirror's own probe runner -- the function that repoints the helper's
# target variables at a probe file -- because each mirror has its own and the
# assertion is otherwise identical in all three. ${4} is the base needle, used
# only to confirm the probe was written. Any further arguments are the helper's
# discovered call shape (the expected count, for *_count helpers). `fail` is
# parent-owned, as everywhere else in this file.
#
# The needle asserted is the probe's WHOLE LINE, read back from the file. A
# whole-line helper (`require_line`, and the exact-line counter behind it)
# rightly rejects a needle that is only part of a line, so passing the base
# needle would have failed those helpers for a reason that has nothing to do
# with the rule under test. The pre-existing `: "${VAR:=x}"` control is the
# whole line for the same reason.
ifa_assert_live_command_probes() {
	local runner="$1" fn="$2" dir="$3" needle="$4" probe probe_needle
	shift 4
	for probe in "${IFA_DEAD_COMMAND_LIVE_PROBES[@]}"; do
		rg -qF -- "${needle}" "${dir}/${probe}.sh" \
			|| fail "probe ${probe}.sh was not written or lacks the needle; the acceptance check below would then pass or fail for the wrong reason"
		# Line 1 is the shebang, line 2 is the probe. Read it back rather than
		# rebuilding it here, so the corpus above stays the single definition.
		probe_needle="$(sed -n '2p' "${dir}/${probe}.sh")"
		[[ -n "${probe_needle}" ]] \
			|| fail "probe ${probe}.sh has no second line to assert on; the acceptance check below would pass on an empty needle"
		"${runner}" "${fn}" "${dir}/${probe}.sh" "${probe_needle}" "$@" \
			|| fail "${fn}() rejected a needle on the ${probe//_/ } line, which DOES act -- the doubt rule has lost a metacharacter, so a live line now reads dead and every pin on it counts one less"
	done
}
