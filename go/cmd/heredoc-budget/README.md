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

The scanner is a line-based approximation, not a full shell lexer. It handles
blanks before the delimiter (`cat << EOF`), ignores a `<<IDENT` written in a
full-line `#` comment, never mis-closes on a delimiter word inside another
heredoc body, tracks single/double-quote state so a `<<IDENT` inside a string
literal (e.g. `echo "a <<X b"`) does not phantom-open the scanner, and
measures every heredoc opener on a line (`cmd <<A <<B`), not just the first
(#5079 — both fixed). Two edge cases remain, neither present in the tree
today: a numeric-first delimiter (`cat <<123`, rejected so a `$(( x << 2 ))`
shift is not mistaken for a heredoc — intentional, not a bug), and a
`<<IDENT` in an inline comment after a command (`echo x # <<EOF`, a false
positive; only a full-line comment is recognized).

## Tests

```bash
cd go && go test ./cmd/heredoc-budget -count=1
```
