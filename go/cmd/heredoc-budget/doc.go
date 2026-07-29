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
// To close this blind spot without a full static-expansion estimate (which
// is generally impossible — an array's runtime size is unknowable from
// source), the scanner compares an UNQUOTED heredoc against a stricter
// effective threshold: budget minus a 25% margin (384 bytes for the default
// 512-byte budget). This is a conservative, documented policy choice, not a
// re-derived OS constant, and it only ever tightens (never loosens) what an
// unquoted heredoc must clear.
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
// The scanner is a line-based approximation, not a full shell lexer. It
// handles blanks between `<<`/`<<-` and the delimiter (`cat << EOF`), ignores
// a `<<IDENT` written in a full-line `#` comment, does not mis-close a
// heredoc on a delimiter word appearing inside another body, tracks
// single/double-quote state so a `<<IDENT` inside a string literal (e.g.
// `echo "a <<X b"`) does not phantom-open the scanner, and measures every
// heredoc opener on a line (`cmd <<A <<B`), not just the first (#5079). Two
// edge cases remain, neither present in the scanned tree today: a
// numeric-first delimiter (`cat <<123`, rejected to avoid mistaking a
// `$(( x << 2 ))` shift for a heredoc — intentional, not a bug) and a
// `<<IDENT` in an inline comment after a command (`echo x # <<EOF`, a false
// positive; only a full-line comment is recognized).
package main
