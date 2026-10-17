# dirgate

## Purpose

Keeps a Go package directory from growing past 40 non-test `.go` files, and
keeps a file out of a directory whose sibling subpackage it belongs in
([#6054](https://github.com/eshu-hq/eshu/issues/6054)). The cap is a ratchet
derived from git on every run. There is no committed ledger, digest, or
generated mirror to merge.

## Ownership boundary

This tool owns the cap and the sibling-subpackage naming rule. It does not own
the 500-line file cap (`tools/golangci-lint-filelength`), file-versus-directory
stutter (`scripts/verify-filename-stutter.sh`), or glued directory names
(`go/cmd/naming-glue-gate`). It reads git objects only and never touches the
working tree.

## The rule

Per directory `D`, with `b`/`h` the count at the base and head, `B0` the baseline
row, `W` the growth waived in all history reachable from the base, and `w` the
growth waived in `base..head`:

```text
delta_needed = max(0, h - max(40, b))
abs_needed   = max(0, h - max(40, B0) - W)
needed       = max(delta_needed, abs_needed)      RED when w != needed
```

Moving files out, swapping files, and renaming inside a directory need no
action. To grow a directory, put one trailer per directory in the message of the
commit that adds the files:

```text
Dirgate-Growth: <dir> +<N> #<issue> <reason>
```

`<dir>` is relative to `go/`. Too little, too much, and an unused trailer are all
RED; a trailer cannot authorize later growth. `abs_needed` catches growth that
landed concurrently and was never waived (two PRs each +1 on a 39-file directory):
the second push run goes RED and a one-commit repair PR carries the trailer. The
gate prints the exact line to add and the commit that carries an invalid trailer.

## Baselines

`baseline-cap.tsv` and `baseline-naming.tsv` record what existed when the gate
landed. They are write-once: present at the base means any diff is RED with no
trailer escape; creating them is GREEN (bootstrap). Generate them with
`go run ./cmd/dirgate -repo .. -base-ref origin/main baseline` at landing and after
a rebase, never after they are on main. A naming collision is RED on every event
unless it has a justified `//nolint:dirgate` marker or a baseline row.

## Residual slack

The invariant is not airtight. `W` sums every commit reachable from the base and
nothing subtracts from it, so a trailer that reaches main counts for good. Each
stray `+N` lets exactly `N` units of future unwaived growth pass the absolute
check in that directory. Cases: growth waived then reverted on main (the revert's
push run exits 0 and `W` keeps the `N`); a `+N` valid at its last-checked base but
merged after a concurrent shrink; and UI-edited or `PR_TITLE` squash messages or
admin pushes. The push run goes RED for an unused trailer only while the trailer is
in the range being evaluated; once merged, no revert or shrink PR lowers `W`. See
the docs page for the full list. The `dirgate` CI job runs on every pull request,
including docs-only ones, and the registry marks it required for every change.

## Modes

| Mode | Head tree | Base | Used by |
| --- | --- | --- | --- |
| `ci` | `HEAD` (merge ref) | `HEAD^1` | every CI event, the `dirgate` job |
| `pre-push` | `merge-tree origin/main HEAD` | `origin/main` | `make pre-push`, the registry row |
| `pre-commit` | the index | merge base | the `go-dir-gate` hook, changed directories only |
| `baseline` | the tree at `-base-ref` | none | landing, after a rebase |

Every event is enforcing, including `push`, `schedule`, `workflow_dispatch` and an
empty event name. Pre-commit is the one exception: growth and trailer findings are
notes there because the message does not exist yet. A shallow checkout is
unshallowed with one blobless fetch; there is no commit-count limit and a failed
fetch exits 2. An unresolvable base exits 2. Pre-commit and pre-push need
`origin/main` or `ESHU_DIRGATE_BASE`. A trailer on a commit reverted inside the
PR is an unused trailer and is RED; amend or rebase the branch.
`scripts/verify-dirgate.sh` is a thin wrapper (`--ci`, `--pre-push`,
`--pre-commit`, `--all`).

## Exported surface

None. `run(args, stdout, stderr, getenv) int` is the testable entry point.
`evaluate` applies the rule to two `snapshot` values, the baselines, the history
total `W`, and a list of `trailer` values with no git, so the arithmetic is
unit-testable.

## Telemetry

None. This is a developer gate. Output is the operator signal: per RED
directory, the base and head counts and the exact trailer to add; per invalid
trailer, the commit that carries it; a NOTE per stale `//nolint:dirgate` marker.

## Tests

`go test ./cmd/dirgate/ -count=1` drives the real `run` against scratch git
repositories: the seeded RED cases, the GREEN cases, a shallow-checkout case, and
the mode and base-resolution cases.
