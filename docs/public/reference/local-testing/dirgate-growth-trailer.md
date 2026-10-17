# Dirgate-Growth Trailer

The directory gate (`go/cmd/dirgate`, issue #6054) caps a Go package directory
at 40 non-test `.go` files. The cap is a ratchet derived from git on every run,
plus two small write-once baselines for what already existed. A directory may
not grow past `max(40, its count at the base)` unless the commit that adds the
files carries a trailer. The gate also catches growth that landed concurrently
and was never waived.

## The rule

For each directory `D`:

```text
b  = count of non-test .go files at the base      h = count at the head tree
B0 = baseline-cap.tsv row for D (0 if none)
W  = growth waived by valid trailers in every commit reachable from the base
w  = growth waived by valid trailers in the commits of base..head

delta_needed = max(0, h - max(40, b))
abs_needed   = max(0, h - max(40, B0) - W)
needed       = max(delta_needed, abs_needed)
```

`D` is RED when `w` differs from `needed`. Too little, too much, and an unused
trailer are all RED. Moving files out, swapping files at the same count, and
renaming inside a directory are GREEN with no action. A move that creates a new
directory over 40 files needs a trailer.

`delta_needed` guards each merge. `abs_needed` catches the case no per-merge
check can see: two PRs each add one file to a 39-file directory, both pass their
own run, and both land. The directory is then at 41 with no waiver. The push run
of the second merge goes RED (`40 -> 41`), and every later PR is RED too, naming
the overshoot as "already on base and never waived".

## The trailer

Put one line for each directory in the commit message:

```text
Dirgate-Growth: <dir> +<N> #<issue> <reason>
```

- `<dir>` is relative to `go/`, for example `internal/storage/postgres`.
- `<N>` is the growth the directory needs, at least 1.
- `#<issue>` names the issue that owns the split. `owner/repo#N` also works.
- `<reason>` says why the files cannot go in a subpackage now.

A trailer cannot authorize later growth. Write it in the commit message, not the
PR body, because local gates cannot read the PR body. The gate prints the exact
line to add, and the commit that carries an invalid trailer.

### Repairing concurrent growth

When the gate names growth "already on base and never waived", either move the
extra files out (GREEN by shrink), or land a one-commit PR that carries the
trailer:

```text
Dirgate-Growth: <dir> +<N> #<issue> concurrent merges of #A and #B
```

`N` equals the unwaived overshoot, so the trailer is valid, not unused. After it
merges, every surface is GREEN. No history rewrite is needed.

## Events

| Where | Head | Base | Result |
| --- | --- | --- | --- |
| `pull_request` (CI) | `HEAD`, which must be the merge commit | `HEAD^1` | enforced; a non-merge `HEAD` exits 2 |
| `merge_group` (CI) | `HEAD` | `HEAD^1` | enforced |
| `push`, `schedule` | `HEAD` | `HEAD^1` | enforced |
| `workflow_dispatch`, or no event name | `HEAD` | `HEAD^1` | enforced |
| `make pre-push` | `origin/main` merged with `HEAD` | `origin/main` | enforced |
| pre-commit hook | the index | merge base | naming and baseline findings enforced; growth and trailer findings are notes |

CI uses `HEAD^1`, never the merge base. A PR that branched before main moved
files out of a directory would otherwise read as a shrink and hide its own
growth. If the merge of `origin/main` into `HEAD` conflicts, the pre-push run
falls back to the merge base, says so, and the branch needs a rebase.

Every enforcing event reads the waivers in the whole history reachable from the
base, so a shallow checkout is unshallowed first with one
`git fetch --no-tags --filter=blob:none --unshallow origin`. Only commits and
trees arrive. There is no commit-count limit. If that fetch fails, the run exits
2 and never passes.

Pre-commit cannot see the commit message that does not exist yet, so a missing
trailer is a note there. Pre-push and CI enforce it. Pre-commit and pre-push
need `origin/main` to resolve, or another base ref passed with `-base-ref` (the
wrapper script reads the matching environment variables named in its header).
With neither, the run exits 2.

### Squash merges

The repository squash setting is `COMMIT_MESSAGES`. A squash commit then keeps
the PR commit messages as body lines, and the gate scans every line, so a
trailer on a non-tip commit survives. If the owner switches the setting to
`PR_TITLE`, the push run goes RED with `w = 0`, which is true: main grew with no
waiver. A one-commit PR with the retroactive trailer repairs it.

## Reverted growth

A trailer that no longer matches real growth is RED as an unused trailer. If a
commit that carried a trailer is reverted inside the same PR, the trailer stays
in that commit's message and the gate is RED. Pre-push reports it before the
push. Amend or rebase the branch so the trailer and the growth go away together.
Do not add a second trailer to offset it.

### Stacked pull requests

When a stacked PR's parent squash-merges, the child's range still holds the
parent's trailer commit. The trailer is unused against the new base and the gate
is RED. Run `git rebase --onto origin/main <old-parent-tip>` so the range holds
only the child's own commits.

## Residual slack

The invariant is **not airtight**. It is correct with or without a merge queue or
strict status checks only up to this slack. `W` sums the waivers in every commit
reachable from the base, and nothing subtracts from it. A trailer that reaches
`main` therefore counts for good, whatever happens to the growth it waived. Each
stray `+N` lets exactly `N` units of future unwaived growth pass the absolute
check in that directory.

- **Growth waived and later reverted on `main`.** The revert's push run exits 0,
  and `W` keeps the `N`. A seeded test pins this.
- **A `+N` valid at the base it was last checked against, merged after a
  concurrent shrink** with strict checks off. The trailer is now larger than the
  growth it covers.
- **A squash message edited in the UI, a `PR_TITLE` squash, or an admin push.**
  Any of these can put a trailer on `main` that no pull request run checked.

For the last two cases the push run goes RED for an unused trailer only while the
trailer is in the range being evaluated. Once that commit is in the base, the
trailer counts in `W` for good, and no revert or shrink PR lowers it. A revert or
shrink repairs the directory's size, not `W`. Do not add a second trailer to
offset a stray one.

The `dirgate` job runs on every pull request, docs-only ones included, and the
registry marks it required for every change (`TestDirgateIsRequiredOnEveryPullRequest`
pins that), so an ordinary pull request cannot add a stray trailer unchecked. On
the pull request that introduces the job, the trusted publisher still reads the
default branch registry, so the job is advisory there.

## Baselines

`go/cmd/dirgate/baseline-cap.tsv` lists every directory over 40 files when the
baseline was cut (`dir<TAB>count`). `go/cmd/dirgate/baseline-naming.tsv` lists
every file that collided with a sibling subpackage (`dir<TAB>file`). Both are
write-once:

- If a file exists at the base, any diff to it at the head is RED: modified,
  emptied, a row removed, or the file deleted. No trailer waives it.
- Creating a file that is absent at the base is GREEN. That is the bootstrap.
- Rows are never removed. A row that no longer matches the tree is harmless,
  because the delta rule still guards every merge.

Generate both from a tree with `dirgate baseline`. The output is deterministic.

```bash
cd go && go run ./cmd/dirgate -repo .. -base-ref origin/main baseline
```

Run it once at landing and again after any rebase, so the real tree is GREEN with
`W = 0`. If main moves the count of an over-40 directory between the baseline and
the merge, the pre-push merge-tree check goes RED: rebase and regenerate the
baselines. After the baselines are on main, never regenerate them.

## Naming rule

A file `bar_x.go` in a directory with a sibling subpackage `bar/` collides with
it. The collision is RED on every event unless the file has
`//nolint:dirgate // <reason>` on its package line, or the file has a row in
`baseline-naming.tsv`. The marker is the only per-file escape and does not waive
the cap. The gate prints a note for each marker that is not a live naming escape.

## Commands

```bash
bash scripts/verify-dirgate.sh --pre-push   # what make pre-push runs
bash scripts/verify-dirgate.sh --ci         # the merge ref against HEAD^1
cd go && go test ./cmd/dirgate/ -count=1    # the seeded RED and GREEN cases
```
