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
2. For each file, finds every heredoc opener — `<<DELIM`, `<<'DELIM'`,
   `<<"DELIM"`, and the tab-stripping `<<-DELIM` form — while explicitly
   ignoring `<<<` here-strings (which never carry a multi-line body).
3. Sums each heredoc body's byte size (`len(line)+1` per body line) and
   compares it against a budget (default 512 bytes).
4. Compares the current per-file violation counts against a checked-in
   baseline (`scripts/heredoc-budget-baseline.txt`) and fails only on
   regression:
   - a file **not** in the baseline that now has 1+ over-budget heredocs, or
   - a baselined file whose over-budget count **increased**.

   A file's count staying the same or **decreasing** always passes — that is
   burn-down progress, and later slices of #5074 convert the existing
   offenders one by one.

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

## Tests

```bash
cd go && go test ./cmd/heredoc-budget -count=1
```
