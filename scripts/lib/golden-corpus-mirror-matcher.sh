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
set_comment_prefix() {
	comment_block=0
	case "$1" in
		*.sh|*.bash|*.yml|*.yaml) comment_prefix='#' ;;
		*.sql) comment_prefix='--'; comment_block=1 ;;
		# RFC 8259 JSON has no comment syntax, so every line is a real home.
		*.json) comment_prefix='' ;;
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

# count_homes prints how many lines of stdin carry the needle on a NON-comment
# line, per the comment syntax the caller registered with set_comment_prefix.
#
# The needle is PCRE2-quoted (\Q..\E) so its metacharacters stay literal. That
# holds only while the needle carries no literal \E of its own, which would close
# the quote and hand the remainder to the regex engine (`\Ecode_line.*` matched
# `code_line ANYTHING_HERE`), so such a needle is rejected outright rather than
# silently reinterpreted.
#
# What the comment rule DOES handle: a marker that opens the line, a marker
# after leading whitespace, a TRAILING marker on an otherwise-code line, a
# marker inside a quoted run that opens AND closes before the needle, a marker
# that is not word-initial (`${#arr}`, `$#`, `a--b`), and — for `.sql` — block
# comments spanning any number of lines.
#
# What it does NOT handle: a marker inside a quoted run that is still OPEN where
# the needle sits (scored as a comment, so the assertion fails "missing" — the
# safe direction), heredoc bodies and other multi-line quoting, and SQL's rule
# that `--` opens a comment even mid-word. It is a word-boundary heuristic over
# single lines, not a lexer.
count_homes() {
	local needle="$1" open pattern
	[[ "${needle}" != *'\E'* ]] ||
		fail "needle carries a literal \\E, which would close its \\Q..\\E quote and let the remainder act as a regex: ${needle}"
	if [[ -n "${comment_prefix}" ]]; then
		open="(?<![^\s;|&(])\Q${comment_prefix}\E"
		pattern="^(?!${open})(?:'[^']*'|\"[^\"]*\"|(?!${open}).)*?\Q${needle}\E"
	else
		pattern="\Q${needle}\E"
	fi
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
	homes="$(count_homes "${needle}")"
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
	region="$(sed -n "${range}p" "${file}")"
	[[ -n "${region}" ]] || fail "empty region ${range} of $(basename "${file}") for ${label}"
	require_in_text "${label}" "$(basename "${file}") ${range}" "${needle}" \
		< <(printf '%s\n' "${region}")
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
