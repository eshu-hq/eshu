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
# separator, subshell, or line continuation and the answer is "live", which is
# the status quo. So this rule can only ever REMOVE matches, never add them, and
# a wrong answer costs a false RED (a human minute) rather than a false GREEN (a
# dead gate) -- the same direction every other choice in these counters elects.
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
	local line="$1" rest
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
	case "${rest}" in
	# Anything that can expand (`$`, backtick), redirect, or begin another
	# command means the line is not provably inert. Live is the safe answer.
	*['$`<>|;&()']*) return 1 ;;
	# A trailing backslash continues the command onto the next line, which this
	# function never sees. Refuse to judge it.
	*\\) return 1 ;;
	esac
	return 0
}
