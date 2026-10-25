# Pre-Merge Check

`scripts/dev/pre-enqueue-check.sh` is a read-only check that fails closed.
The coordinator runs it immediately before asking the arbiter to approve a
merge. It was written for the merge-queue enqueue step. `main` has had no merge
queue since 2026-10-16, so the `merge-queue` arm reads an absent queue and
passes with `queue empty`. This page is named for the script's real job,
verifying a PR before merge, while the script keeps its file name and its
queue arm until the owner decides to drop or restore them (#8368). It re-derives
the facts an arbiter used to collect by hand, so the arbiter reviews one report
instead of re-running each query.

```bash
GH_TOKEN="$(gh auth token --user <account>)" \
  scripts/dev/pre-enqueue-check.sh <pr-number> <expected-head-sha>
```

`<expected-head-sha>` is the full 40-character SHA that passed review. The
script uses the ambient `gh` authentication. On a machine where the active
`gh` account cannot read the repository, prefix `GH_TOKEN` as shown above.
`PRE_ENQUEUE_REPO=owner/name` overrides the repository slug from `gh repo view`, and
`PRE_ENQUEUE_REMOTE` overrides the git remote (default `origin`). The script reads
the ruleset-required contexts by running `go -C go run ./cmd/ci-gates contexts`,
so it needs Go on `PATH`. `PRE_ENQUEUE_CI_GATES` replaces that command and
`PRE_ENQUEUE_REGISTRY` the registry path. If `ci-gates` cannot list a ruleset
context, the script exits `2` rather than guess. Pass
`--dispositioned <context>=<issue-or-reason>` before the two positionals to accept
one completed-red check the ruleset does not require (see the `checks` arm).

## Arms

Each arm prints one `PASS` or `FAIL` line. Any `FAIL` exits `1`, and a usage
error exits `2`.

| Arm | Passes when |
| --- | --- |
| `pr-state` | The PR is `OPEN`, not a draft, and `headRefOid` equals the expected SHA. |
| `merge-main` | `git merge-tree --write-tree origin/main <head>` is clean after fetching `main` and the head. The arm also prints a base-drift note: how many commits `main` is past the branch's merge base, and which files both `main` and the PR changed since that base. A clean merge can still need a regenerated artifact, such as `ci-gates.md`, rebuilt, so the arbiter reads this note. It fails only when there is no merge base. |
| `merge-queue` | The head merges cleanly with the merge-queue tip: the `headCommit` of the last queued entry that is not this PR. A queue entry's `headCommit` is the GitHub-built merge-group commit, not the PR's own head, and it contains `main` plus every PR up to that entry. For example, #7311's entry `26a514a55` has parent `main` `98394122f`, while that PR's head was `f9992c17f`. Checking against the tip therefore covers the whole queue, so a collision that a check against `origin/main` alone misses still fails here. The arm prints the file overlap with each queued PR. An empty queue, or no queue at all as on `main` today, passes with `queue empty`. |
| `checks` | The merge rule holds on the head's check runs, read with `gh api repos/<owner>/<repo>/commits/<sha>/check-runs` (REST, paginated). Every context that `ci-gates contexts --ruleset-only` lists (today `go-core-complete` and `go-race-complete`) must be `success`, as a check run or, failing that, a commit status; the set comes from `specs/ci-gates.v1.yaml`, never from a list in the script. A check the ruleset does not require, the `required-gates-complete` aggregate included, never blocks while it is pending or absent: the arm prints a note. A completed red one (`failure`, `cancelled`, `timed_out`, `action_required`, or a `failure`/`error` status) fails the arm unless the caller passes `--dispositioned <context>=<issue-or-reason>`. The flag repeats, and each use is printed in the arm output as `applied`, `unused (not red)`, or `ignored (ruleset-required)`: a ruleset-required context can never be dispositioned. A run is judged by its `status` and `conclusion`, never by its name. `success`, `skipped`, and `neutral` pass; any other conclusion is unclassified and fails. The endpoint's default `filter=latest` returns the latest run per name within each check suite; a re-triggered workflow on the same SHA adds a second suite, and every suite's rows count except for the manifest contexts, where the newest suite (highest suite id) wins and a superseded suite is named in a note. Rows are never collapsed by name, because one job name can be defined by many workflows (`queue-selection / select` is 13 distinct checks). A name with rows in more than one suite prints a `multi-suite:` note; when such a name is failing (or pending, which is only a note), the line adds that an older run on the same SHA is still counted, that GitHub defines no winner across suites, and that the caller should wait for or cancel the orphan (`gh run list --commit <sha>`). A read that returns zero rows fails with `no check rows reported`; it is an error, not `pending`. A `gh api` call that errors fails with `gh api check-runs read failed (exit N)`. A row with no check suite is never treated as superseded. `mergeStateStatus` must be `CLEAN`. A PR that is already in a merge queue is the one exception (`main` has none today): GitHub reports `mergeStateStatus` `UNKNOWN` for it, so the row is labeled `IN-GROUP` and a queued PR accepts `UNKNOWN` or `CLEAN`; any other merge state fails the row and is named in it (rulings a9-135.2 and a9-139.2, from PR #8000's read). |
| `threads` | GraphQL `reviewThreads` has zero unresolved threads. A truncated page fails closed. |
| `body` | The body contains either a bare `#N` issue reference (including `Issue: #N`) or `Closes`/`Fixes`/`Resolves #N`, and no AI attribution as the `no-ai-attribution` gate defines it. The report labels closing keywords so the caller can confirm each issue is meant to close; it labels a bare reference as non-closing and does not infer issue closure. When both forms occur, it lists both categories separately. That definition, a shared pattern in `scripts/lib/ai-attribution-pattern.sh`, matches an AI-tool co-author trailer, a "generated with" line that names an AI tool, the robot-emoji footer, or the vendor noreply address. Prose that only describes attribution, and a human co-author trailer, both pass. The same arm fails a body that carries an environment or organization identifier, as the `no-private-identifiers` gate defines it (the shared pattern in `scripts/lib/private-identifier-pattern.sh`); the report names the body line numbers and never repeats the matched text. |

The `checks` arm uses `gh api`, not `gh pr checks --json`, because `gh` 2.46.0
has no `--json` flag on `gh pr checks`. The REST read needs no newer `gh`.

The script performs one read. It does not replace the two-consecutive-stable-reads
rule for CI completion. Run it only after the caller's watcher has seen the full
check set stable twice.

The script writes nothing to GitHub and never touches a branch, the index, or
the worktree. `git fetch origin main` advances the remote-tracking ref
`origin/main`. The head and queue-tip commits are fetched by SHA, which stores
objects only, and `git merge-tree --write-tree` writes loose objects only.

## Self-test

```bash
bash scripts/dev/test-pre-enqueue-check.sh
```

The self-test is hermetic. A fake `gh` on `PATH`
(`scripts/lib/test-pre-enqueue-check-fake-gh.sh`) serves JSON for each case and
rejects `gh pr checks --json` as `gh` 2.46.0 does. A fake `ci-gates`
(`scripts/lib/test-pre-enqueue-check-fake-ci-gates.sh`) serves the per-case
ruleset and manifest context lists, and one case runs the real
`go run ./cmd/ci-gates` against `specs/ci-gates.v1.yaml`. The checks-arm cases live in
`scripts/lib/test-pre-enqueue-check-checks-cases.sh`. A local bare repository stands in for `origin`, so the merge-tree arms run real
merges over real conflicts. One all-green case must exit `0`. Each arm has a
seeded RED that must exit `1` with a single `FAIL` line naming that arm. The
registry row is `pre-enqueue-check` in `specs/ci-gates.v1.yaml`, and CI runs the
self-test in the `Verify ci-gate registry test mirror` job.
