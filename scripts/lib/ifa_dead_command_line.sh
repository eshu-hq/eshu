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
# separator, subshell, or backslash OUTSIDE single quotes and BEFORE a comment
# and the answer is "live", which is the status quo. So this rule can only ever
# REMOVE matches, never add them, and a wrong answer costs a false RED (a human
# minute) rather than a false GREEN (a dead gate) -- the same direction every
# other choice in these counters elects.
#
# WHERE a metacharacter sits decides whether it means anything, which is why the
# scan below tracks single quotes and comments instead of searching the raw line.
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
# quotes are NOT absolute (`$` and backtick still expand), so they are deliberately
# not treated as literal, and `: "${VAR:=default}"` keeps reading live.
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
	local line="$1" rest ch prev="" i=0 n quoted=0
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
	# know whether a character means anything: which single-quoted span it is in,
	# and whether a comment has started. Anything richer would be the expansion
	# parser this file exists to avoid.
	while [[ "${i}" -lt "${n}" ]]; do
		ch="${rest:i:1}"
		if [[ "${quoted}" -eq 1 ]]; then
			[[ "${ch}" == "'" ]] && quoted=0
		else
			case "${ch}" in
			# Inside single quotes bash interprets nothing at all, so a
			# metacharacter there is text and cannot make the line act.
			"'") quoted=1 ;;
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
		fi
		prev="${ch}"
		i=$((i + 1))
	done
	# An unclosed quote is a line this pass cannot read. Refuse to judge it.
	[[ "${quoted}" -eq 0 ]] || return 1
	return 0
}
