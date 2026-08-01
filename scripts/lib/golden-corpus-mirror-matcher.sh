#!/usr/bin/env bash
# Needle matcher for scripts/test-verify-golden-corpus-gate.sh. Sourced by that
# mirror and by nothing else; it calls the mirror's fail(). It lives here rather
# than inline so the mirror stays under the repo's 500-line file rule, and it is
# named golden-corpus-*.sh so the trigger lists that already select the mirror on
# a golden-corpus lib edit select it too, with no new path entry to keep in sync.
#
# ----------------------------------------------------------------------------
# The exactly-one-non-comment-home invariant.
#
# Every positive assertion in the mirror resolves its needle to EXACTLY ONE
# non-comment line of the text it searches, judged with THAT target's own
# comment syntax. Four review rounds found the same defect from four angles:
#
#   1. the matcher was not comment-aware at all, so a `#` comment naming a
#      helper, a sourced lib or a gate flag satisfied the assertion while the
#      real line was gone (15 of 68 assertions were vulnerable);
#   2. the matcher became comment-aware but hardcoded `#`, and `.sql` comments
#      with `--`, so the DDL-fixture assertion still passed with the real DDL
#      commented out — reproduced: the mirror printed `pass` and returned 0;
#   3. a needle had TWO real homes (an emission target and a log message naming
#      the same file), so renaming the emission left the assertion green;
#   4. the comment rule was a LINE-prefix test, so a needle mentioned in a
#      TRAILING comment on an otherwise-code line still counted as a home, and
#      `.sql` block comments (`/* … */`) were not comments at all — both
#      reproduced against this file's own assertions.
#
# One matcher closes all four, including for assertions not yet written. An
# unregistered file extension is a HARD FAILURE, not an implicit "this language
# has no comments" — that assumption is precisely defect (2). Zero homes fails
# "missing"; two or more fails "ambiguous", forcing a narrower needle or a
# scoped region instead of a search any surviving mention satisfies. The count
# runs on every call, so a needle that GROWS a second home fails immediately
# instead of waiting for a fifth reviewer.
#
# The `if rg ...; then fail` guards in the mirror assert ABSENCE and are outside
# the invariant: their satisfying state is zero homes, and a comment carrying a
# banned string makes them false-FAIL, which is the safe direction.
# ----------------------------------------------------------------------------
comment_prefix=""
comment_block=0
comment_anywhere=0
# comment_syntax_registered is the same class of emptiness sentinel
# count_homes/resolve_unique_line already carry for build_home_pattern's own
# `fail` (#5837 P2-1): set_comment_prefix's `*` arm calls `fail`, which prints
# a diagnostic and `exit 1`s, relying on that `exit` to unwind whatever
# subshell set_comment_prefix happens to be running in. That is reliable for
# every call shape this file's callers use TODAY -- require_in/require_in_region
# call it bare (no subshell at all) and resolve_unique_line's own single-`$()`
# wrapping is exactly the depth a bare `exit` always unwinds correctly -- but it
# is reliable only because no caller of require_in/require_in_region/
# resolve_unique_line currently nests a SECOND, unchecked `$()` around that
# single wrap. count_homes/build_home_pattern's #5837 P2-1 sentinel exists
# precisely because that second layer of nesting DOES already exist for THAT
# pair (require_in_text's `homes="$(count_homes ...)"` wrapping count_homes's
# own `pattern="$(build_home_pattern ...)"`), and a bare `exit` inside the
# inner call is swallowed at that depth unless the outer function explicitly
# checks its own assignment's exit status -- reproduced with a minimal shim:
# `p="$(inner)"` inside `mid()`, itself captured bare as `homes="$(mid)"`,
# continues past inner's `exit 1` with p empty. Nothing today builds that same
# second layer around require_in/require_in_region/resolve_unique_line's OWN
# invocation, so set_comment_prefix's `exit` is not presently swallowed in
# practice -- but relying on "no caller happens to nest it that way yet" is
# exactly the same latent posture build_home_pattern had before #5837 P2-1,
# and an emptiness check on comment_prefix could not close it even if a caller
# did add that nesting: comment_prefix is legitimately `""` for the registered
# `.json` case, so "comment_prefix is empty" cannot distinguish "registered, no
# comment syntax" from "never registered". This flag closes the gap at the
# SAME sentinel-after-call depth count_homes/resolve_unique_line already use
# for build_home_pattern, so a future caller that adds that second layer around
# require_in/require_in_region/resolve_unique_line fails loudly here instead of
# silently reusing whatever comment_prefix/comment_block/comment_anywhere the
# PREVIOUS call left behind. Each case arm below sets this flag explicitly, and
# every caller (require_in, require_in_region, resolve_unique_line) asserts it
# immediately after calling set_comment_prefix.
comment_syntax_registered=0
set_comment_prefix() {
	comment_block=0
	comment_anywhere=0
	comment_syntax_registered=0
	case "$1" in
		*.sh|*.bash|*.yml|*.yaml) comment_prefix='#'; comment_syntax_registered=1 ;;
		# SQL's `--` opens a comment wherever it appears on the line, including
		# mid-word (`1--`); unlike shell's `#`, it has no predecessor
		# requirement, so comment_anywhere drops that constraint for it.
		*.sql) comment_prefix='--'; comment_block=1; comment_anywhere=1; comment_syntax_registered=1 ;;
		# RFC 8259 JSON has no comment syntax, so every line is a real home.
		*.json) comment_prefix=''; comment_syntax_registered=1 ;;
		*) fail "no comment syntax registered for $(basename "$1"); teach set_comment_prefix before asserting against it" ;;
	esac
}

# strip_block_comments deletes `/* … */` spans from stdin, carrying the open
# state across lines, so a block-commented target line stops being a home. Only
# the `.sql` targets enable it. A `/*` or `*/` inside a string literal is treated
# as a real delimiter here — approximation, not a lexer.
strip_block_comments() {
	awk '
	{
		if (b) { if (match($0, /\*\//)) { $0 = substr($0, RSTART + 2); b = 0 } else { $0 = "" } }
		while (match($0, /\/\*/)) {
			pre = substr($0, 1, RSTART - 1)
			rest = substr($0, RSTART + 2)
			if (match(rest, /\*\//)) { $0 = pre substr(rest, RSTART + 2) } else { $0 = pre; b = 1 }
		}
		print
	}'
}

# build_home_pattern turns a needle into the PCRE2 pattern that decides
# whether a line counts as a non-comment "home" for it, per the comment syntax
# the caller registered with set_comment_prefix. Both count_homes (below) and
# resolve_unique_line share this one pattern builder so the two can never
# diverge on what counts as a home — a needle that count_homes considers
# ambiguous must resolve_unique_line consider identically ambiguous, not pass
# one check and fail the other.
#
# The needle is PCRE2-quoted (\Q..\E) so its metacharacters stay literal. That
# holds only while the needle carries no literal \E of its own, which would close
# the quote and hand the remainder to the regex engine (`\Ecode_line.*` matched
# `code_line ANYTHING_HERE`), so such a needle is rejected outright by `fail`
# here. That reject alone is not sufficient: this function is always reached
# through `pattern="$(build_home_pattern ...)"`, and every real call site
# (count_homes, resolve_unique_line) makes that assignment INSIDE ITS OWN
# command substitution (`homes="$(count_homes ...)"`,
# `line="$(resolve_unique_line ...)"`). Under that double nesting bash
# suppresses errexit for the failed inner assignment, so the caller used to
# carry on with pattern="" and hand `rg` an empty PCRE2 pattern -- which
# matches every line, silently reinterpreting the rejected needle as
# match-everything instead of aborting (#5837 P2-1). count_homes and
# resolve_unique_line each carry their own `[[ -n "${pattern}" ]] || exit 1`
# sentinel immediately after this call for exactly that reason: rejected here
# does not mean aborted there without it. The sentinel exits bare, with no
# second message of its own -- the one diagnostic this needle gets was
# already printed by the `fail` two lines up (its subshell's stderr is
# inherited by the caller, not swallowed), so a rejected needle prints
# exactly ONE message and aborts, the same as before this function was
# extracted to its own lib file, not the \E-reject message followed by a
# misleading "ambiguous: N non-comment lines carry it" second verdict.
#
# What the comment rule DOES handle: a marker that opens the line, a marker
# after leading whitespace, a TRAILING marker on an otherwise-code line, a
# marker inside a quoted run that opens AND closes before the needle, a marker
# that is not word-initial for languages where that matters (`${#arr}`, `$#`,
# `a--b` in shell), `.sql`'s rule that `--` opens a comment WHEREVER it
# appears including mid-word (no predecessor requirement — SQL has no
# `${#arr}`-style construct that needs one), and — for `.sql` — block
# comments spanning any number of lines.
#
# What it does NOT handle: a marker inside a quoted run that is still OPEN where
# the needle sits (scored as a comment, so the assertion fails "missing" — the
# safe direction), and heredoc bodies and other multi-line quoting. It is a
# word-boundary (or, for `.sql`, anywhere-on-line) heuristic over single lines,
# not a lexer. A heredoc body line that happens to carry the needle text is
# therefore scored as a REAL (non-comment) home, same as genuine code — this is
# what makes resolve_unique_line's exactly-one-home check catch a heredoc-body
# decoy: the decoy adds a second home rather than silently passing as a comment.
build_home_pattern() {
	local needle="$1" open
	[[ "${needle}" != *'\E'* ]] ||
		fail "needle carries a literal \\E, which would close its \\Q..\\E quote and let the remainder act as a regex: ${needle}"
	if [[ -n "${comment_prefix}" ]]; then
		if (( comment_anywhere )); then
			# SQL: `--` always opens a comment, even mid-word (`1--`), so no
			# predecessor constraint applies. Reproduced without this branch:
			# `SELECT 1-- DROP TABLE ...` scored the DROP as a live
			# (non-comment) home, so an assertion requiring that DDL passed
			# while the statement was entirely inside a real SQL comment.
			open="\Q${comment_prefix}\E"
		else
			open="(?<![^\s;|&(])\Q${comment_prefix}\E"
		fi
		printf '%s' "^(?!${open})(?:'[^']*'|\"[^\"]*\"|(?!${open}).)*?\Q${needle}\E"
	else
		printf '%s' "\Q${needle}\E"
	fi
}

# count_homes prints how many lines of stdin carry the needle on a NON-comment
# line, per the pattern build_home_pattern resolves for it.
#
# The emptiness sentinel below closes #5837 P2-1: build_home_pattern's own
# `fail` runs inside THIS assignment's command substitution, and every real
# caller of count_homes reaches it through a SECOND, enclosing command
# substitution (`homes="$(count_homes ...)"` in require_in_text). Under that
# double nesting, bash suppresses errexit for the failed inner assignment
# (reproduced with a minimal shim: `p="$(inner)"` inside `mid()`, itself
# captured as `outer="$(mid)"`, continues past inner's `exit 1`), so this
# function used to carry on with pattern="" after a needle-\E rejection and
# hand an EMPTY PCRE2 pattern to `rg`, which matches every line -- the
# `fail` above still printed its message and returned a nonzero exit from
# ITS OWN subshell, but this function's flow continued regardless, turning a
# clean single-message reject into a second, misleading "ambiguous" verdict
# instead of aborting. build_home_pattern never returns empty on a genuine
# success (its shortest output is the 4-character `\Q\E`), so an empty
# pattern here is unambiguous proof the assignment above already failed.
count_homes() {
	local needle="$1" pattern
	pattern="$(build_home_pattern "${needle}")"
	# build_home_pattern's own `fail` already printed the one diagnostic this
	# needle gets (its subshell's stderr is inherited, not captured); only
	# propagate the failure here, with no second message, or a rejected needle
	# prints its real reason FOLLOWED BY a misleading "ambiguous" verdict
	# instead of aborting on the first, correct one.
	[[ -n "${pattern}" ]] || exit 1
	if (( comment_block )); then
		strip_block_comments | rg --pcre2 --count -- "${pattern}" || true
	else
		rg --pcre2 --count -- "${pattern}" || true
	fi
}

# require_in_text is the single enforcement point: text on stdin,
# comment_prefix already set by the caller.
require_in_text() {
	local label="$1" origin="$2" needle="$3" homes
	# The explicit `|| exit 1` matters, not just style: count_homes's own
	# `|| true` guards make ITS exit status always 0 EXCEPT when its
	# build_home_pattern sentinel fires (a rejected needle, e.g. one carrying
	# a literal \E). A genuinely absent needle and a rejected needle both
	# print empty stdout here, so `${homes:-0}` alone cannot tell them apart
	# -- only count_homes's own exit status can. Relying on bare `set -e` to
	# react to that nonzero status is NOT enough: this assignment runs inside
	# whatever subshell depth the caller is at (any require_in/require caller
	# wrapped in so much as one `(...)` group already qualifies), and at
	# depth >= 1 bash suppresses errexit for a failed nested command-
	# substitution assignment (the same #5837 P2-1 class, one layer up) --
	# reproduced: without this guard, a rejected needle called through one
	# extra layer of subshell fell through to the "missing" branch below
	# instead of stopping on count_homes's already-printed rejection reason.
	# `||`/`&&` check $? explicitly, so they work at any depth.
	homes="$(count_homes "${needle}")" || exit 1
	case "${homes:-0}" in
		0) fail "missing ${label} (no non-comment line of ${origin} carries it): ${needle}" ;;
		1) return 0 ;;
	esac
	fail "ambiguous ${label}: ${homes} non-comment lines of ${origin} carry it; narrow the needle or scope the region until exactly one home remains: ${needle}"
}

# require_in: the invariant against a whole file.
require_in() {
	local label="$1" file="$2" needle="$3" origin
	set_comment_prefix "${file}"
	[[ "${comment_syntax_registered}" -eq 1 ]] || exit 1
	origin="$(basename "${file}")"
	require_in_text "${label}" "${origin}" "${needle}" <"${file}"
}

# require_in_region: the invariant against a sed address range of a file, for a
# needle that legitimately recurs across blocks — the snapshot flag is passed to
# BOTH gate invocations, and the ci-gates lib triggers appear under both gate
# ids. Narrowing the REGION instead of the needle keeps the assertion one home
# wide AND pins it to the specific block that must carry it. A range whose start
# pattern matches more than once yields more than one home and fails loudly
# rather than silently widening.
require_in_region() {
	local label="$1" file="$2" range="$3" needle="$4" region
	set_comment_prefix "${file}"
	[[ "${comment_syntax_registered}" -eq 1 ]] || exit 1
	region="$(sed -n "${range}p" "${file}")"
	[[ -n "${region}" ]] || fail "empty region ${range} of $(basename "${file}") for ${label}"
	require_in_text "${label}" "$(basename "${file}") ${range}" "${needle}" \
		< <(printf '%s\n' "${region}")
}

# resolve_unique_line: the SAME exactly-one-non-comment-home invariant
# require_in enforces, for a caller that additionally needs the resolved
# line NUMBER — bracket PLACEMENT (source-line ordering), not just presence.
# It shares build_home_pattern with count_homes, so a decoy that would make
# count_homes report "ambiguous" (a heredoc-body line, a quoted string, a
# second real occurrence) makes this die with the identical diagnostic
# instead of silently binding to whichever line rg listed first.
#
# Process substitution, not a `<<<` here-string or a `... | cut` pipeline: a
# `rg ... | cut` idiom under `set -o pipefail` propagates rg's exit 1 (zero
# matches) straight through `set -e` and aborts the whole script BEFORE this
# function's own "missing"/"ambiguous" diagnostic ever runs (#5837 P2-2, the
# silent-abort case — reproduced with trailing whitespace defeating a
# `--line-regexp` lookup). The `|| true` on the rg call below is what keeps a
# zero-match result inside this function's own control flow instead of
# escaping through pipefail.
resolve_unique_line() {
	local label="$1" file="$2" needle="$3" pattern home_line
	local -a home_lines=()
	set_comment_prefix "${file}"
	[[ "${comment_syntax_registered}" -eq 1 ]] || exit 1
	pattern="$(build_home_pattern "${needle}")"
	# See count_homes's sentinel comment above: this assignment sits inside the
	# SAME double-command-substitution shape every real caller uses
	# (`x="$(resolve_unique_line ...)"`), so errexit is suppressed here too
	# without this explicit check, and the one diagnostic a rejected needle
	# gets was already printed by build_home_pattern's own `fail` -- no second
	# message here, just propagate the failure.
	[[ -n "${pattern}" ]] || exit 1
	while IFS=: read -r home_line _; do
		home_lines+=("${home_line}")
	done < <(
		if (( comment_block )); then
			strip_block_comments <"${file}" | rg --pcre2 -n -- "${pattern}" || true
		else
			rg --pcre2 -n -- "${pattern}" "${file}" || true
		fi
	)
	case "${#home_lines[@]}" in
		0) fail "missing ${label} (no non-comment line of $(basename "${file}") carries it): ${needle}" ;;
		1) printf '%s\n' "${home_lines[0]}"; return 0 ;;
	esac
	fail "ambiguous ${label}: ${#home_lines[@]} non-comment lines of $(basename "${file}") carry it (lines ${home_lines[*]}); narrow the needle or scope the region until exactly one home remains: ${needle}"
}

# require_matches: the invariant for the two assertions whose needle is a
# multi-line PCRE2 shape rather than a fixed string (contiguity checks). Same
# rule, counted in matches: the pattern must carry its own non-comment anchor
# and must match exactly once.
require_matches() {
	local label="$1" file="$2" pattern="$3" matches
	matches="$(rg --pcre2 --multiline --count-matches -- "${pattern}" "${file}" || true)"
	[[ "${matches:-0}" -eq 1 ]] ||
		fail "${label}: want exactly one non-comment match in $(basename "${file}"), found ${matches:-0}"
}
