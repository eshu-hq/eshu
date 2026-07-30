# cmd/heredoc-budget

`heredoc-budget` is a static lint gate ([#5074](https://github.com/eshu-hq/eshu/issues/5074))
that flags shell heredoc bodies large enough to deadlock under Homebrew bash
>= 5.1 on macOS.

## Why this exists

Bash 5.1+ writes an entire `<<EOF`-style heredoc body to a pipe before forking
the process that reads it. macOS's pipe buffer is 512 bytes, so any heredoc
body strictly between 512 bytes and the pipe buffer's ~64 KB ceiling
deadlocks — the writer blocks on a full pipe with no reader yet spawned to
drain it. The same script runs fine under macOS's stock `/bin/bash` (3.2.57),
which predates the change, so the bug is invisible until something (like
PR #5071/#5050's ci-gates local runner) steers subprocesses to a newer bash.
See `doc.go` for the full background and the safe alternatives
(`$(<file)` + `printf`, plain `printf`, or `cmd < <(printf ...)`).

## What it does

1. Walks `scripts/**/*.sh`, skipping non-`.sh` files.
2. For each file, finds every heredoc opener on every line — `<<DELIM`,
   `<<'DELIM'`, `<<"DELIM"`, and the tab-stripping `<<-DELIM` form — while
   explicitly ignoring `<<<` here-strings (which never carry a multi-line
   body) and any `<<IDENT` written inside a string literal or a full-line `#`
   comment.
3. Sums each heredoc body's byte size (`len(line)+1` per body line) and
   compares it against an effective budget: the raw budget (default 512
   bytes) for a quoted delimiter, or a stricter 75%-of-budget margin (384
   bytes by default) for an unquoted delimiter, since bash expands
   `${var}`/`$(cmd)` substitutions in an unquoted body at runtime — see
   "Unquoted heredocs and the runtime-expansion margin" below (#5085).
4. Compares the current per-file violation counts against a checked-in
   baseline (`scripts/heredoc-budget-baseline.txt`) and fails only on
   regression:
   - a file **not** in the baseline that now has 1+ over-budget heredocs, or
   - a baselined file whose over-budget count **increased**.

   A file's count staying the same or **decreasing** always passes — that is
   burn-down progress, and later slices of #5074 convert the existing
   offenders one by one.

## Unquoted heredocs and the runtime-expansion margin

A heredoc with a bare (unquoted) delimiter — `<<EOF`, not `<<'EOF'` or
`<<"EOF"` — has its body expanded by bash at runtime: `${var}`,
`${arr[*]}`, `$(cmd)`, and arithmetic all get substituted before the body is
written to the reader. That means a heredoc whose literal **source** is
under the 512-byte budget can still cross the real pipe-buffer deadlock
threshold once expanded — the concrete case (#5074 batch 1,
`verify-oci-scorecard-adapter.sh`) was a 496-byte source heredoc whose
`${fact_families[*]}` expansion pushed it over 512 bytes at runtime, invisible
to a scanner that only measures literal source bytes ([#5085](https://github.com/eshu-hq/eshu/issues/5085)).

Since an array's or command's expanded size can't be known statically, the
gate applies a conservative margin instead: an **unquoted** heredoc is
flagged once its source exceeds 384 bytes (75% of the default 512-byte
budget), not 512. A **quoted** delimiter disables all substitution, so its
body never grows past its literal size and keeps the full 512-byte budget. A
violation reported only because of this margin is called out explicitly in
the CLI's failure output (`unquoted; exceeds N-byte runtime-expansion margin
though under the literal M-byte budget`) so it isn't mistaken for an ordinary
over-budget failure.

**This margin narrows the window for the observed expansion shape — it does
NOT close the general case.** It only helps when the literal source is
already reasonably close to budget. Adversarial review found a
counter-example: a 17-byte literal heredoc referencing a 200-element array
via `${arr[*]}` expands to 607 real bytes under actual bash, and the scanner
passes it silently, because 17 bytes of source is nowhere near the 384-byte
margin regardless of what it expands to at runtime. Flagging by construct
(any `$(...)` in an unquoted body, regardless of size) was measured against
this repo's own `scripts/**/*.sh` and would newly flag roughly a third of the
existing baselined files — mostly ordinary, bounded command substitution
(a version string, a timestamp), not the unbounded-array pattern that caused
the original incident — too noisy to ship. The narrower
array-subscript-only form (`${arr[*]}`/`${arr[@]}`) currently matches zero
files in this tree. Neither is implemented; see "Known limitations" below.

## Usage

Run from the `go` module directory:

```bash
# Check the current tree against the baseline (exit 1 on regression).
go run ./cmd/heredoc-budget -baseline ../scripts/heredoc-budget-baseline.txt

# Regenerate the baseline after fixing (or knowingly adding) heredocs.
go run ./cmd/heredoc-budget -baseline ../scripts/heredoc-budget-baseline.txt -update

# Override the byte budget (rarely needed; 512 matches the macOS pipe buffer).
go run ./cmd/heredoc-budget -baseline ../scripts/heredoc-budget-baseline.txt -budget 1024
```

The scan root is always the baseline file's own directory, so pointing
`-baseline` at `scripts/heredoc-budget-baseline.txt` scans all of `scripts/`.

On failure, stderr lists each regressed file with its baselined count and
every current offending heredoc as `path:line body=N bytes`.

## Baseline file

`scripts/heredoc-budget-baseline.txt` is `<relative/path> <count>` per line
(plus a header comment), sorted by path, generated only via `-update` — never
hand-edited. Zero-count entries are omitted, so burning a file's count down to
zero and regenerating drops it from the file.

## Wiring

Registered in `specs/ci-gates.v1.yaml` as the `heredoc-budget` gate
(category `exactness`, tier `pre-pr`, blocking) and mirrored in
`.github/workflows/static-contract-gates.yml`.

## Ownership boundary

This command owns heredoc scanning and baseline comparison only. It does not
rewrite any offending script — that is left to the follow-on slices of #5074
that convert individual files.

## Known limitations

This list is deliberately non-exhaustive — it names every gap found so far,
not a closed set. The scanner is a line-based approximation, not a full shell
lexer, and adversarial review keeps finding more of these than any one pass
catches.

**Handled correctly (fixed, not gaps):** blanks between `<<`/`<<-` and the
delimiter (`cat << EOF`); a `<<IDENT` in a full-line `#` comment; a delimiter
word inside another heredoc's body (does not mis-close it); a `<<IDENT`
inside a single- or double-quoted string, or inside an ANSI-C `$'...'` string
even across an escaped `\'`; more than one heredoc opener on a line
(`cmd <<A <<B`, both measured, #5079); a heredoc opener nested inside
`$(...)` command substitution, including when that substitution sits inside
an outer double-quoted string that has not closed yet; a double-quoted
string spanning multiple physical lines (quote/substitution context now
persists across lines, not just within one line); backslash-escaping at
the bare unquoted level, including the extremely common `'\''` idiom for
embedding a literal `'` inside a single-quoted string — found missing during
this same review, when it desynced the quote stack on a real script in this
repo (`verify-remote-e2e-remediation-benchmark.sh`) and silently swallowed a
real over-budget heredoc dozens of lines later; and an unquoted,
word-starting `#` that trails real code on the same line (`echo x # <<EOF`)
is now recognized as a genuine bash comment ending the line. **This was a
genuine fail-open, not merely a false positive:** only a FULL-line comment
was previously recognized, so the `<<EOF` fragment after a trailing `#`
phantom-opened the scanner exactly like the full-line case, and dropped a
real over-budget heredoc elsewhere in the file as an unterminated opener —
0 heredocs detected, exit 0, while the real deadlock risk shipped unflagged.
Verified against real bash both for the comment case itself and for the
constructs that must stay literal under the same start-of-word check
(`echo foo#bar`, `${x#pat}`, `$#`, `#` inside single or double quotes).

The word-start check is tracked as explicit state across the scan (not
re-derived from the raw byte before `#`), because a raw-byte lookback cannot
tell a real separator apart from one already consumed as half of a wider
unit. A P1 regression shipped exactly that way: a backslash-escaped blank
(`x\ #<<EOF`) leaves that blank byte sitting right before `#` even though the
escape branch already consumed it, so the lookback wrongly saw a "real"
separator bash itself does not -- misreading a genuine heredoc opener as a
trailing comment (0 detected, exit 0). The same explicit-state fix also lets
word-start recognize an unquoted statement-separator operator (`;`, `|`,
`&`), not just a blank -- `true;#<<EOF` and `true|#<<EOF` are genuine bash
comments too, and the old raw-byte check missed both with the identical
fail-open shape. Both verified against real `/bin/bash`.

**Still open (real, adversarially-found gaps):**

- A heredoc opener split immediately after `<<`/`<<-` by a
  backslash-newline line continuation (`cat <<\` then a newline, then the
  delimiter on the next physical line) is valid bash and opens a real
  heredoc this line-based scanner never sees. Verified against real
  `/bin/bash`. Pre-existing, not introduced or fixed by the word-start work
  above; considered low real-world likelihood. Not fixed here because
  splicing backslash-newline continuations before lexing would have to
  interact correctly with the persisted cross-line quote stack (literal
  inside single quotes, a real continuation inside double quotes or at the
  base level) without the full old-vs-new re-proof this file's other
  quote-stack fixes required.
- The unquoted-heredoc runtime-expansion margin (#5085, above) narrows the
  window for a source body already close to budget; it does not catch a
  small literal body that references an unbounded runtime expansion (a large
  array, a large command substitution) — see above for the measurement
  behind not implementing a construct-based rule instead. **#5085's
  runtime-expansion blind spot is NOT closed by this package; it is only
  narrowed.**
- The baseline burn-down comparison keys on a per-file violation **count**,
  not the identity of which heredoc it is. Fixing one violation while
  introducing a different one elsewhere in the same file can leave the count
  unchanged and pass silently. Pre-existing, unrelated to this branch's
  fixes, and intentionally not redesigned here.
- Legacy backtick command substitution (`` `cmd` ``) is not tracked as its
  own lexical scope the way `$(...)` is.
- A numeric-first delimiter (`cat <<123`) is rejected on purpose, to avoid
  mistaking a `$(( x << 2 ))` arithmetic shift for a heredoc — intentional,
  not a bug.

**#5079 is likewise NOT fully closed** by this or any prior slice: it names a
class of line-based-scanner false negatives (quote/substitution-context
desyncs), and the backtick-substitution gap above is a further instance of
that same class, not yet a closed list.

## Tests

```bash
cd go && go test ./cmd/heredoc-budget -count=1
```
