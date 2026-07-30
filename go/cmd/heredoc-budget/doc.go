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
// Still open (real, adversarially-found gaps):
//
//   - A heredoc opener split immediately after `<<`/`<<-` by a
//     backslash-newline line continuation (`cat <<\` then a newline, then
//     the delimiter on the next physical line) is valid bash -- the
//     continuation is spliced away before tokenizing, so the delimiter
//     still binds to the `<<` -- and opens a real heredoc this scanner
//     never sees, since it lexes one physical line at a time and the `<<`
//     has no delimiter text on its own line. Verified against real
//     /bin/bash: a script whose first line is `cat <<\` (trailing
//     backslash-newline) followed by `EOF` on the next line and a
//     600-byte body prints the body and exits 0, while ScanContent on the
//     same source reports zero heredocs. This is PRE-EXISTING, not
//     introduced or fixed by the word-start work above, and considered low
//     real-world likelihood. Closing it would mean splicing
//     backslash-newline continuations before lexing, which is unsafe to do
//     blindly: `\<newline>` is literal (kept, not spliced) inside a
//     single-quoted string but IS a continuation inside double quotes or
//     at the base level, and the splice would have to interact correctly
//     with the persisted cross-line quote stack -- the same fragile
//     mechanism this file's other quote-stack fixes required a full
//     old-vs-new re-proof against real scripts/**/*.sh for. That proof was
//     not done for this gap, so it is documented rather than fixed here.
//   - The unquoted-heredoc runtime-expansion margin (#5085, see above) only
//     narrows the window for a source body already close to budget; it does
//     not catch a small literal body that references an unbounded runtime
//     expansion (a large array via `${arr[*]}`, a large command
//     substitution). This is a fundamental limit of any literal-byte-count
//     heuristic, not a bug to fix in this pass. #5085's runtime-expansion
//     blind spot is NOT closed by this package; it is only narrowed.
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
//     is intentional design, not a limitation.
//
// #5079 (quote/substitution-context desyncs) is likewise NOT fully closed by
// this or any prior slice: it names a class of line-based-scanner false
// negatives, and the "Still open" gaps above (backtick substitution in
// particular) are further instances of that same class, not yet found ones.
package main
