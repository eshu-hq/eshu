# Pre-Push Merge Step

`make pre-push` tests the branch head, but GitHub's merge ref and the merge
queue test the head merged with `origin/main`, and main moves between the two.
A head can compile on its own and still fail on the main it lands on: #7053
vetted clean at its head and failed CI because main had moved a package one of
its tests imported. The merge step (`scripts/lib/pre-push-merge.sh`) runs that
check locally before a push or queue cycle is spent.

## What it runs

1. `git merge-tree --write-tree <base> HEAD`, where the base is `origin/main`
   unless a stacked branch overrides it (see the header of
   `scripts/dev/pre-push.sh`). The committed HEAD is what is
   merged; uncommitted edits are reported and are not part of the merged tree.
2. A conflict fails the run and lists the conflicting paths. A merge git cannot
   compute also fails the run; on a shallow clone that is usually a missing
   merge base, and the message suggests `git fetch --deepen=200 origin main`.
3. The merged tree is checked out into
   `$(git rev-parse --absolute-git-dir)/eshu-pre-push-merge/tree`, a stable
   directory under the worktree's own git dir. It never appears in
   `git status`, `git worktree remove` deletes it, and later runs update it in
   place.
4. `go vet ./...` runs in the merged tree's `go/` module.

The run summary prints the merge tree id as
`merge tree: <tree> (HEAD <sha> + <base> <sha>)`.

## When it skips

The step prints why it skipped instead of passing silently:

- HEAD already contains the base (the usual state right after a rebase). The
  merged tree is HEAD's own tree, which the changed-package build and vet and
  the whole-module `go-vet` registry gate already cover.
- The merged tree's Go inputs, meaning `go/` plus every local `replace`
  directory in `go/go.mod`, are identical to the base's. The merge adds nothing
  main's own CI has not already vetted.

## Why `go vet` and a stable directory

`go vet` type-checks every package and its tests, so it reports every compile
error `go build` would, except link-only errors, and test-file breakage too.
Measured on this repository (#7111):

| Operation | Seconds |
| --- | --- |
| `git merge-tree --write-tree` | 0.04–0.05 |
| First checkout of the merged tree | 8.0 |
| Incremental update after 5 main commits | 0.3–2.6 |
| `go vet ./...`, first run in a worktree | 67.1 |
| `go vet ./...`, warm, after main moved 5 commits | 20.1–24.2 |
| `go build ./...`, warm (links every command; not used) | 63.1–138.0 |

Go's vet cache is keyed on the source directory. Vetting the same packages in
a fresh temporary directory took 19.4s against 2.1s in a reused one, so the
tree lives in one directory per worktree rather than a new temp directory
per run.

## Review receipts

The merge step does not change `ci-gates review-attest`. A review receipt
binds the reviewed diff, not the merge:

- Recommended: capture and verify with
  `--base "$(git merge-base origin/main HEAD)"`. The receipt then binds the
  merge base and the diff the review covered, and it still verifies when main
  moves while `make pre-push` runs.
- Known trap: `--base origin/main` binds main's moving tip. `make pre-push`
  fetches main first, so `review-attest verify` fails with
  `base_commit changed` whenever main moved between capture and push. On a
  busy day that is almost every push, and each failure demands a full review
  of an unchanged diff.

The merge the floor vetted is recorded in the pre-push summary line,
`merge tree: <tree> (HEAD <sha> + origin/main <sha>)`: the floor tested the
merge of that HEAD with that origin/main commit, and nothing later. The merge
queue re-tests the exact landing merge and is the authority for it.

## What it does not catch

It catches a merge that no longer compiles or vets. It does not catch a
combination that still compiles but behaves differently, for example main
wrapping a value in a new type that a branch type-asserts on. The merge
queue's CI run on the exact merge remains the authority for behavior.
