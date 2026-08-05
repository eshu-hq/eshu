// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command heredoc-budget is a static lint gate that flags oversized shell
// heredoc bodies before they can deadlock a developer's `make pre-pr` run.
//
// # Background
//
// Bash 5.1+ (Homebrew's default on macOS, and what PR #5071/#5050 now steers
// local gate subprocesses toward) writes an entire `<<EOF`-style heredoc body
// to a pipe before forking the process that reads it (e.g. `cat`). macOS's
// pipe buffer is 512 bytes. A heredoc body strictly between 512 bytes and the
// pipe buffer's ~64 KB ceiling therefore deadlocks: the writer blocks on a
// full pipe with no reader yet alive to drain it. The same script runs fine
// under macOS's stock `/bin/bash` (3.2.57), which never had bash 5.1's
// heredoc-writer change, so the failure is invisible in some environments and
// a silent hang in others. See #5074 and its prerequisite fix, #5019/#5077
// (the operator-dashboard generator).
//
// Safe alternatives to a large inline heredoc, in order of preference:
//
//   - `$(<file)` to read a template/data file into a variable, paired with
//     `printf '%s'` to emit it — neither construct touches a pipe.
//   - `printf` directly, for a body assembled in-process (a builtin call, so
//     no fork and no pipe).
//   - `cmd < <(printf '%s\n' "$var")` process substitution instead of a
//     `<<<` here-string when feeding a large value to a command's stdin.
//
// # What this command does
//
// heredoc-budget scans `scripts/**/*.sh` for heredoc openers (`<<DELIM`,
// `<<'DELIM'`, `<<"DELIM"`, and the tab-stripping `<<-DELIM` form; `<<<`
// here-strings are explicitly ignored, since they never carry a multi-line
// body). For each heredoc it sums the body's line lengths (plus one byte per
// line for the stripped newline) and compares the total against a byte
// budget (512 by default, matching the macOS pipe-buffer size that triggers
// the deadlock).
//
// This is a burn-down gate, not a hard ban: as of #5074 roughly 120 existing
// heredocs across 56 files already exceeded the budget, and rewriting all of
// them was out of scope for that slice. Instead, the command compares the
// current scan against a checked-in baseline
// (scripts/heredoc-budget-baseline.txt) and fails only on regression — a
// brand-new file with an over-budget heredoc, or an existing baselined
// file's over-budget count going up. A file's count staying the same or
// going down (the expected burn-down direction) always passes.
//
// # Unquoted heredocs and runtime expansion (#5085)
//
// An UNQUOTED heredoc delimiter (`<<DELIM`, as opposed to `<<'DELIM'` or
// `<<"DELIM"`) lets bash perform parameter and command substitution
// (${var}, $(cmd), arithmetic) inside the body at runtime. That means a
// heredoc whose literal SOURCE is under the 512-byte budget can still
// expand past the real macOS pipe-buffer deadlock threshold once the shell
// substitutes its variables — the concrete case (#5074 batch 1,
// verify-oci-scorecard-adapter.sh) was a 496-byte source heredoc whose
// `${fact_families[*]}` expansion crossed 512 bytes at runtime and
// deadlocked, even though the static byte count looked safe. A quoted
// delimiter disables all substitution, so its body never grows past its
// literal size and keeps the full budget.
//
// A full static-expansion estimate is generally impossible — an array's or a
// command substitution's runtime size is unknowable from source — so instead
// the scanner compares an UNQUOTED heredoc against a stricter effective
// threshold: budget minus a 25% margin (384 bytes for the default 512-byte
// budget). This is a conservative, documented policy choice, not a
// re-derived OS constant, and it only ever tightens (never loosens) what an
// unquoted heredoc must clear.
//
// This margin narrows the window for the observed expansion shape (a source
// body already close to budget that a small/medium substitution pushes over)
// — it does NOT close the general case. Adversarial review (#5085 follow-up)
// showed a 17-byte literal source heredoc referencing a 200-element array
// via `${arr[*]}` expands to 607 real bytes under actual bash, and the
// scanner passes it silently: 384 bytes of literal source is nowhere near
// the margin, no matter how much the body expands at runtime. A margin is
// fundamentally a heuristic over literal byte count; it cannot bound an
// expansion whose size depends on data the scanner never sees. Closing that
// gap for real would require either measuring what the runtime value
// actually expands to (this package deliberately never executes bash — see
// "Failure modes" in AGENTS.md) or flagging unquoted heredocs by the
// presence of an unbounded-expansion construct in the body regardless of
// literal size. The latter was measured against this repo's own
// scripts/**/*.sh: a rule that fires on any `$(...)` in an unquoted body
// would newly flag roughly a third of the existing baselined files (most of
// it ordinary, bounded command substitution — a version string, a
// timestamp — not the unbounded-array pattern that caused the original
// incident), which is too noisy to ship; the narrower array-subscript-only
// form (`${arr[*]}`/`${arr[@]}`) currently matches zero files in this tree.
// Neither is implemented; this remains a documented gap.
//
// # Modes
//
//	(default)  scan the tree and compare against the baseline; exit 1 and
//	           print every offending file:line + body size on regression.
//	-update    regenerate the baseline from the current tree and exit 0.
//
// # Flags
//
//	-baseline  path to the baseline file (required in both modes; also
//	           determines the scan root, which is the baseline's directory)
//	-update    regenerate the baseline instead of checking it
//	-budget    byte budget per heredoc body (default 512)
//
// # Known limitations
//
// This list is deliberately non-exhaustive: it names every gap found so far,
// not a closed set. The scanner is a line-based approximation, not a full
// shell lexer, and adversarial review keeps finding more of these than any
// one pass catches.
//
// Handled correctly (fixed, not gaps): blanks between `<<`/`<<-` and the
// delimiter (`cat << EOF`); a `<<IDENT` written in a full-line `#` comment;
// a delimiter word appearing inside another heredoc's body (does not
// mis-close it); a `<<IDENT` inside a single- or double-quoted string
// literal, or inside an ANSI-C `$'...'` string even across an escaped `\'`
// (does not phantom-open); more than one heredoc opener on a line
// (`cmd <<A <<B`, both measured, #5079); a heredoc opener nested inside
// `$(...)` command substitution, including when that substitution itself
// sits inside an outer double-quoted string that has not closed yet; a
// double-quoted string that spans multiple physical lines (with or without
// a trailing backslash-continuation) — quote/substitution context persists
// across lines, not just within one; backslash-escaping at the bare
// unquoted level, including the extremely common close/escaped-quote/reopen
// idiom for embedding a literal single quote inside a single-quoted string
// — found missing during this same review, when it desynced the quote stack
// on a real script in this repo
// (scripts/verify-remote-e2e-remediation-benchmark.sh) and silently
// swallowed a real over-budget heredoc dozens of lines later; and an
// unquoted, word-starting `#` that trails real code on the same line
// (`echo x # <<EOF`) is now recognized as a genuine bash comment that ends
// scanning for the rest of that line. This was a genuine fail-open, not
// merely a false positive: only a FULL-line comment was previously
// recognized, so the `<<EOF` fragment after a trailing `#` phantom-opened
// the scanner exactly like the full-line case, and a real over-budget
// heredoc elsewhere in the file (with no line that is literally its
// delimiter) was silently dropped as an unterminated opener — 0 heredocs
// detected, exit 0, while the actual deadlock risk shipped unflagged.
// Verified against real bash both for the comment case and for the
// constructs that must NOT be mistaken for one on the same start-of-word
// check (`echo foo#bar`, `${x#pat}`, `$#`, `#` inside single or double
// quotes).
//
// The word-start check itself is tracked as EXPLICIT STATE across the scan
// (a `wordStart` boolean in findAllOpeners), not re-derived from the raw
// byte immediately before `#`. A P1 regression shipped from the raw-byte
// version: a backslash-escaped blank (`x\ #<<EOF`) leaves that blank byte
// sitting right before the `#` even though the escape branch already
// consumed it as part of a two-byte unit, so the old lookback wrongly saw a
// "real" separator that bash itself does not, misreading a genuine heredoc
// opener as a trailing comment (0 heredocs detected, exit 0). Explicit state
// also let word-start recognize an unquoted statement-separator operator
// (`;`, `|`, `&`) as a real boundary, not just a blank -- `true;#<<EOF` and
// `true|#<<EOF` are genuine bash comments too, and the old raw-byte check
// missed both, which manifested as the identical fail-open (the phantom
// opener swallowed a real heredoc later in the file). Both were verified
// against real /bin/bash; see the TestScanContent_EscapedWhitespaceBefore*
// and TestScanContent_RealWordBoundaryBeforeHashIsGenuineComment tests in
// scanner_quoting_test.go for the transcripts and regression coverage.
//
// # 2026-07 hardening review: four more P1 fail-opens closed (F1-F4)
//
// A fourth review found four MORE constructs, each independently verified
// against real /bin/bash and the production ScanContent, where the scanner
// reported ZERO heredocs on a file containing a genuine over-budget one --
// the exact live pipe-buffer hang risk this gate exists to catch. All four
// are now fixed at the mechanism level, not by special-casing the four
// reported inputs:
//
//   - F1 (arithmetic): `$((` opens bash arithmetic evaluation, not a command
//     substitution. `<<` inside it is the shift operator, never a heredoc
//     opener, but the scanner previously modeled only the FIRST `(` of
//     `$((` (indistinguishable from plain `$(cmd)`), so `<<` inside an
//     arithmetic expression was read as a real opener whenever its operand
//     was identifier-shaped (`x=$(( flags << shiftamount ))`) rather than
//     the purely-numeric shape the pre-existing mitigation blocked. Fixed by
//     giving `$((` its own frameArith lexical frame (findAllOpeners,
//     scanner_lexer.go), tracked with proper nested-paren depth so a
//     grouping paren inside the expression does not close it early, and
//     suppressing both heredoc-opener detection AND the `#`-comment rule
//     while inside it (arithmetic has no comment syntax in real bash
//     either -- verified). Nested command substitution inside arithmetic,
//     and arithmetic nested inside a command substitution, both still work.
//   - F2 (delimiter character set): parseDelim used to accept only
//     [A-Za-z_][A-Za-z0-9_]* for an unquoted delimiter, an identifier
//     approximation, not real bash's actual word rule. `cat <<E#F` (`#` is
//     ordinary mid-word text in bash, only a WORD-STARTING `#` is a
//     comment) truncated to delimiter "E", which never matched the real
//     closing line "E#F", silently dropping the heredoc and everything
//     after it. Fixed by deriving the accepted character set from
//     isShellWordSeparator (scanner_delim.go) -- the same word-boundary
//     notion F4 below introduces -- instead of an identifier shape. The
//     same rewrite also dropped an identical, previously undocumented
//     restriction on QUOTED delimiters (`<<'E#F'`/`<<"E#F"` were also
//     wrongly rejected), added support for the classic `<<\DELIM`
//     backslash-escaped-delimiter idiom (equivalent to full quoting) found
//     via this pass's mandated adversarial probe for "a delimiter
//     containing backslash", and for a quoted segment concatenated onto an
//     unquoted prefix (`<<FOO'BAR'`, delimiter "FOOBAR") found via the
//     probe for "a delimiter containing quotes" -- see scanner_delim.go and
//     scanner_delim_test.go for each verified transcript. The pre-existing,
//     intentional numeric-first-delimiter rejection (`cat <<123`, see
//     below) is unchanged; frameArith from F1 is now its PRIMARY defense
//     against the arithmetic misread, not this rejection.
//   - F3 (line continuation): a bare trailing backslash at the end of a
//     physical line is a real bash line continuation (`\<newline>` is
//     spliced away before tokenizing) everywhere except inside a
//     single-quoted or ANSI-C `$'...'` string, where backslash has no
//     escape meaning at all. The scanner had no model of this: both the
//     full-line-comment shortcut and findAllOpeners's per-line `wordStart`
//     reset restarted fresh on every PHYSICAL line, so a continuation that
//     fused mid-word onto a line beginning with `#` (which bash does NOT
//     read as a comment there) was misread as one, silently dropping
//     everything after it. Fixed at the line-joining level (ScanContent,
//     scanner.go): findAllOpeners now reports whether a line ends in a
//     splice-eligible dangling backslash, and ScanContent fuses the next
//     physical line directly onto it (repeating for consecutive
//     continuations) before either the comment shortcut or the opener scan
//     ever runs, so both see the same fused text a real bash parser would.
//     As a side effect (not one of the four originally reported
//     constructs), this closed the "heredoc opener split immediately after
//     `<<`/`<<-`" gap this doc previously listed under "Still open" --
//     confirmed by TestScanContent_ContinuationSplitRightAfterHeredocOperatorNowClosed
//     in scanner_continuation_test.go; that gap is REMOVED from the list
//     below.
//   - F4 (word-separator set): findAllOpeners's word-boundary rule (for
//     recognizing a trailing `#` comment) only covered blank/`;`/`|`/`&`.
//     Real bash also treats a bare `(` (subshell open), a bare `)` (e.g. a
//     case-pattern terminator, NOT the `)` that closes `$(...)` -- that one
//     is intentionally excluded, since bash concatenates a substitution's
//     result into the surrounding word), a bare `<`, and a bare `>` as
//     word-separating metacharacters, each verified against real bash (the
//     `(`/`)` cases directly; the `<`/`>` cases via the real-bash SYNTAX
//     ERROR that results when a comment right after one of them swallows a
//     mandatory redirection target, which is only possible if bash's own
//     lexer treats the position right after as a genuine word start).
//     Fixed by adding all four to the shared isShellWordSeparator predicate
//     (scanner_delim.go) that both findAllOpeners's wordStart tracking and
//     parseDelim's unquoted-word scan (F2) now derive from, instead of two
//     independently hand-maintained lists.
//
// See scanner_arith_test.go, scanner_delim_test.go, scanner_continuation_test.go,
// scanner_wordsep_test.go, and scanner_probes_test.go for the full RED/GREEN
// regression coverage, each with its own real-bash-verified transcript.
// scanner_probes_test.go additionally records the mandated post-fix
// adversarial hunt (`$((` nested in `$(`/`$(` nested in `$((`, `<<` inside
// `[[ ]]`, `#` after `!`, `<<` inside an array literal via command
// substitution, and `<<-` with mixed tabs/spaces) -- all of which matched
// already-correct behavior except the two F2 findings folded in above.
//
// # 2026-07 hardening review round 2: two more P1 fail-opens in F2 itself (P1-1, P1-2)
//
// A codex review of PR #5890 -- the very PR that shipped the F2 rewrite above
// -- found two more constructs where F2's broadened word scan still reported
// ZERO heredocs (or, worse, a mismeasured one) on a script with a genuine
// over-budget body. Both live in parseDelim (scanner_delim.go) and both were
// verified against real /bin/bash first:
//
//   - P1-1 (CRLF bare delimiter): on a CRLF-line-ending script, F2's word
//     scan does not stop at '\r' (real bash does not treat '\r' as a word
//     separator either), so a bare opener like "cat <<EOF\r\n" parses to
//     delimiter "EOF\r". closesHeredoc, however, already stripped a trailing
//     '\r' from the candidate closing line (pre-existing CRLF tolerance,
//     predating F2). The two independently-normalised sides never matched,
//     so the heredoc was dropped as unterminated and its body, however
//     oversized, was never measured. Fixed by routing BOTH the parsed
//     delimiter (parseDelim) and the candidate closing line (closesHeredoc,
//     scanner.go) through one shared stripTrailingCR helper
//     (scanner_delim.go), instead of one hand-written TrimSuffix on only one
//     side. See scanner_crlf_test.go for the real-bash transcripts and
//     regression coverage, including the `<<-` and quoted-delimiter variants.
//   - P1-2 (quoted-leading concatenated delimiter): a delimiter word that
//     STARTS with a quote (`<<'DELIM'...`) had its own top-level branch that
//     returned immediately once the quote closed, never continuing to read
//     the rest of the word -- unlike the unquoted-prefix branch's mid-word
//     quote handling (`<<FOO'BAR'`, delimiter "FOOBAR"), which already
//     continues correctly. An opener whose delimiter is an empty quoted pair
//     directly followed by "E" returned delimiter "" instead of real bash's
//     "E", so the scanner waited for a BLANK closing line instead of the real
//     "E" line -- silently folding every intervening line, including any
//     real heredoc's own opener/body/closer text, into ONE phantom "quoted"
//     heredoc measured against the FULL budget instead of the stricter
//     384-byte unquoted-expansion margin (#5085). The severe consequence: a
//     later genuine unquoted heredoc sized in the 385-512 byte margin window
//     could be folded into that phantom body and never independently
//     measured against the margin that actually applies to it, bypassing it
//     entirely. Fixed by deleting the separate leading-quote branch: a
//     leading quote now falls into the SAME general word-scan loop and the
//     SAME quote case that already handles a quote appearing mid-word, so
//     both directions continue reading the rest of the word. See
//     scanner_quoted_prefix_test.go for the real-bash transcripts, the class
//     hunt this pass ran (only-quotes delimiters, mixed quote styles, a
//     quoted segment containing a separator byte), and the severe
//     margin-bypass regression test.
//
// This round's mandated class hunt additionally probed: a delimiter with a
// trailing '\r' mid-word (already correct -- real bash keeps a non-trailing
// '\r' as ordinary word content, and this scanner already did too, since
// stripTrailingCR only strips a truly TRAILING '\r'); `<<` followed
// immediately by end-of-line (already correct -- parseDelim already rejects
// an empty remainder); a delimiter that is only quotes (fixed by the same
// P1-2 change, since it is another instance of the same early-return bug); a
// quoted segment containing '\r' or a separator byte (already correct -- the
// quote case copies the exact bytes between the matching quotes, unaffected
// by isShellWordSeparator or stripTrailingCR); a delimiter with a trailing
// backslash (already correct -- the pre-existing F3 continuation/escape
// handling); CRLF combined with `<<-` (fixed by the same P1-1 change); and
// CRLF combined with a line continuation (already correct in BOTH directions
// -- a trailing backslash is only a real continuation when it is immediately
// followed by '\n', so on a CRLF line the backslash instead escapes the
// '\r' itself, exactly matching real bash's own "delimited by end-of-file"
// unterminated verdict for that construct; see
// TestScanContent_CRLFDanglingBackslashEscapesCRNotContinuation).
//
// One deliberate, documented limitation from this round: stripTrailingCR
// normalises a trailing '\r' identically regardless of whether it arrived as
// a CRLF line-ending artifact or as genuinely-quoted content, and
// closesHeredoc's pre-existing CRLF tolerance means a script with A CRLF
// opener but an LF-only closing line (or vice versa) now closes under this
// scanner even though real bash would call it unterminated. This is an
// intentional, safety-favoring choice (still measure a body rather than miss
// it), not an oversight -- see stripTrailingCR's doc comment in
// scanner_delim.go for the full reasoning.
//
// Still open (real, adversarially-found gaps):
//
//   - The unquoted-heredoc runtime-expansion margin (#5085, see above) only
//     narrows the window for a source body already close to budget; it does
//     not catch a small literal body that references an unbounded runtime
//     expansion (a large array via `${arr[*]}`, a large command
//     substitution). This is a fundamental limit of any literal-byte-count
//     heuristic, not a bug to fix in this pass. #5085's runtime-expansion
//     blind spot is NOT closed by this package; it is only narrowed. This is
//     not a hypothetical count: 40 unquoted heredocs in scripts/ today sit
//     at or under the 384-byte margin threshold and pass silently
//     (verified by walking every *.sh file under scripts/ and counting
//     Heredoc{Unquoted: true, Size<=384}); a 100-byte body containing
//     `${arr[*]}` over a 10 KB array would be one more. No literal-byte
//     threshold closes this — it would require either executing bash (this
//     package deliberately never does) or flagging by construct
//     (`$(...)`/`${arr[*]}` regardless of size), which was measured and
//     rejected as too noisy to ship (see above).
//   - The scan root is always dirname(baseline) (main.go: `scanRoot :=
//     filepath.Dir(*baselinePath)`), i.e. scripts/. 23 *.sh files outside
//     it are never scanned as of this writing: 14 under examples/
//     (collector-extension and supply-chain-demo proof scripts), 7 under
//     tests/ (2 directly, 5 under tests/fixtures/), and 2 under
//     .claude/hooks/ and .codex/hooks/ (agent doc-staleness hooks) —
//     verified with `git ls-files '*.sh' | rg -v '^scripts/'`, which
//     (unlike an un-hidden `rg --files -g '*.sh'`) also finds the two
//     dotdir hooks.
//   - Only *.sh files are scanned at all. Makefile recipes, GitHub Actions
//     `run:` blocks, `bash -c` strings, and heredocs embedded in Go string
//     literals execute as real shell but are invisible to this gate.
//     Confirmed live instances in this repository: a heredoc inside a
//     `run:` step in .github/workflows/generate-bundle-on-demand.yml, and
//     Go string literal heredocs used as test fixtures in this very
//     package (main_test.go, scanner_test.go) — neither is a *.sh file
//     under scripts/, so neither is ever scanned.
//   - A heredoc opener nested inside another heredoc's own UNQUOTED body
//     (`cat <<OUTER` whose body contains `$(cat <<INNER ... INNER)`) is a
//     real, valid bash construct: verified against real bash, which
//     executes it and reads the inner heredoc as its own construct. This
//     scanner has no model of that: the inBody branch of ScanContent only
//     compares each body line against the CURRENT heredoc's own delimiter
//     (closesHeredoc) and otherwise adds it to bodySize as raw content; it
//     never re-invokes findAllOpeners on a body line. The inner heredoc's
//     body and delimiter lines are folded into the outer's Size and
//     reported as one Heredoc under the outer's Line and Unquoted —
//     verified directly against this scanner (ScanContent on the fixture
//     above returns exactly one Heredoc, not two). This does not by itself
//     undercount the combined byte total: the inner's bytes are still
//     summed into the outer's, and the outer's stricter unquoted threshold
//     still applies to the merged total whenever the outer is unquoted.
//     What is lost is attribution — the inner heredoc is never
//     independently reported with its own line number, size, or Unquoted
//     status.
//   - A flagged heredoc's reported Size is its LITERAL SOURCE byte count,
//     never what bash actually delivers to the pipe at runtime. The margin
//     bullet above covers delivered bytes exceeding the literal count; the
//     reverse also happens and is by design not a concern. An unquoted
//     body whose substitutions reference unset or empty variables can
//     deliver FEWER bytes than its source — sometimes dramatically fewer:
//     a 30-byte source body of `${V}${V}${V}${LONG_UNSET_VAR}` (the
//     scanner's own reported Size) with every variable unset delivers 1 byte
//     at runtime (verified against real bash). A heredoc flagged only
//     because of this shape is a false positive, not a missed detection —
//     the gate never fails to flag a heredoc that is actually dangerous
//     because of shrinkage; at worst it occasionally flags one that turns
//     out to be safe.
//   - The baseline burn-down comparison (CheckBaseline in baseline.go) keys
//     on a per-file violation COUNT, not the identity of which heredoc it
//     is. Fixing one baselined violation while introducing a different,
//     unrelated over-budget heredoc elsewhere in the same file can leave the
//     count unchanged and pass with no signal — pre-existing behavior,
//     unchanged by this package's fixes, and intentionally not redesigned
//     here (see AGENTS.md for the owning slice).
//   - Legacy backtick command substitution (backtick-delimited instead of
//     `$(...)`) is not tracked as its own lexical scope the way `$(...)` is;
//     a heredoc opener nested inside backticks that are themselves inside an
//     outer quoted string can still be missed.
//   - A numeric-first delimiter (`cat <<123`) is rejected on purpose, to
//     avoid mistaking a `$(( x << 2 ))` arithmetic shift for a heredoc — this
//     is intentional design, not a limitation. The 2026-07 review's frameArith
//     fix (F1, above) is now the primary defense against that misread; this
//     restriction is kept as secondary, defense-in-depth.
//   - stripTrailingCR (P1-1, above) normalises a trailing '\r' the same way
//     regardless of whether it is a CRLF line-ending artifact or genuinely
//     quoted content, so a script whose heredoc opener and closing line have
//     MISMATCHED line endings (a CRLF opener paired with an LF-only closing
//     line, or vice versa) now closes under this scanner even though real
//     bash would call it unterminated — an intentional, safety-favoring
//     leniency (see stripTrailingCR's doc comment in scanner_delim.go), not
//     an oversight.
//
// #5079 (quote/substitution-context desyncs) is likewise NOT fully closed by
// this or any prior slice: it names a class of line-based-scanner false
// negatives, and the "Still open" gaps above (backtick substitution in
// particular) are further instances of that same class, not yet found ones.
// #5085 (runtime-expansion blind spot) is likewise not closed, per the
// unquoted-margin bullet above. Neither #5085 nor #5079 is closed by the
// 2026-07 hardening review either.
package main
