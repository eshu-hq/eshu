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
// heredocs across 56 files already exceed the budget, and rewriting all of
// them is out of scope for this slice. Instead, the command compares the
// current scan against a checked-in baseline
// (scripts/heredoc-budget-baseline.txt) and fails only on regression — a
// brand-new file with an over-budget heredoc, or an existing baselined
// file's over-budget count going up. A file's count staying the same or
// going down (the expected burn-down direction) always passes.
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
package main
