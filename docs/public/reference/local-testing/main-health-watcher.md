# Main Health Watcher

`.github/workflows/main-health.yml` tells the team when `main` is red. Before it
existed, a required workflow could fail on `main` after two green pull requests
merged and nobody was told: a guard-test red stayed open for 4h44m and was found
by PR authors, and the scheduled ruleset verifier failed on every run with no
owner (#7111).

## What it does

After a required workflow completes on `main`, and every 6 hours as a
backstop, the watcher judges the newest commit on `main` (never the commit of
whichever workflow woke it up) and publishes a `main-health` commit status:

| Verdict | Condition | Issue | Status |
| --- | --- | --- | --- |
| red | a required workflow's latest run on the tip failed in a blocking job, or the latest scheduled `Required Gates` ruleset verification failed | open or update the single `main is red @<sha>` issue; comment on it when the set of blocking reds changes | `failure` |
| green | every required workflow that ran on the tip succeeded or failed only in advisory jobs, none is running, and the aggregator's source workflow has a verdict | close the open issue | `success`, naming any advisory failure |
| pending | a required workflow is still running, or the source workflow has not registered yet | unchanged | `pending` |
| unknown | a required run was cancelled and never re-run, or a run listing came back truncated | unchanged | `error` |

"Latest run" is judged per triggering event, because one workflow can carry
more than one verdict on a commit. Security Scan's `push` run scans the tree,
and its `workflow_run` run (fired when `Publish Image and Helm Chart`
completes) scans the published image; a failed image scan makes `main` red
even though the push run passed. A red run of one event is never hidden by a
newer green run of another event. The runs that count are `push`, `schedule`,
and `workflow_run` runs on the tip. Pull-request and merge-queue runs never
count. A fully skipped run has no verdict, so it never hides an earlier failed
run of the same event.

A `workflow_run` run is attributed to the commit that was `main`'s HEAD when
it was created, whatever fired it: GitHub reports `head_branch: main` and
that HEAD as its `head_sha` even when a pull-request or merge-queue run of
Publish fired it. Security Scan's image scan runs only for push and dispatch
origins (`security-scan.yml`), so those other runs are skipped and never
decide. A late Publish for an older commit can scan `:main` and land on the
current tip; the tip's own scan corrects it.

## Blocking and advisory failures

Only blocking failures make `main` red. For each failed run the watcher reads
its failed jobs and looks each one up in `specs/ci-gates.v1.yaml`: a gate
claims a job when its `ci.workflow` is the run's workflow file and its
`ci.job` equals the job name, lists it in `ci.check_names`, or is the matrix
prefix of a `job (...)` name. A failed run is advisory only when every failed
job is claimed, and every gate that claims it says `blocking: false`.
Anything else is blocking: a job no gate claims, or a failed run whose jobs
cannot be read. The rule fails closed so that a gap in the registry cannot
hide a red.

Advisory failures never change red or green. They are listed in an
"Advisory failures" section of the open issue and named in the `main-health`
status. Today that covers the Trivy image scan (`trivy-image`, which is
post-publish evidence and cannot block a merge), so a chronic image-scan
failure alone leaves `main` green with an advisory note.

The scheduled ruleset verification (`verify-live-ruleset` in
`required-gates.yml`) is not a registry gate, so the same fail-closed rule
counts it as blocking. It checks that the live ruleset still requires the
checks the registry declares; when it fails, merge protection itself has
drifted.

The issue body records the current set of blocking reds in a hidden marker.
When a later evaluation finds a different set, with a new red added or a red
cleared while others remain, the watcher posts a comment naming what changed
alongside the body edit. An issue edit alone notifies nobody. An unchanged
set, or a change only among advisory failures, posts no comment. Only names
the policy can produce are read back from the marker.

The scheduled ruleset verification is not tied to one commit. Its latest
result stays in force until the next scheduled `Required Gates` run, so if
that run failed, the first evaluation after the watcher lands opens an issue
even when every workflow on the tip is green. That is intended (#7111): the
ruleset has drifted and someone needs to know.

The issue carries the label `main-health` and a hidden body marker, is pinned
on a best-effort basis, and
lists each failing workflow with its run, job, failed step, and one verdict line
taken from the job log. A `verify-live-ruleset` failure appears in the same
issue. A newer red commit retitles and rewrites the one open issue; it never
opens a second one, and any duplicate found is closed. A newer green commit
closes it. The watcher only manages issues that carry its body marker. An
unrelated issue that someone labels `main-health` is never edited or closed.

The watcher exits 0 whenever the evaluation finished, so a red `main` does not
add a red row for the watcher itself.

## Cost, concurrency and trust

Each evaluation makes a fixed number of API calls, whatever the history
length. It reads the commit and makes one ruleset probe that asks for a single
run. It lists the tip's runs once per event, filtered by the server
(`head_sha`, `branch=main`, `event`), and lists `workflow_run` runs once per
required workflow file that declares that trigger. It then reads the open
issues and the tip's statuses. If a listing returns fewer runs than its
`total_count`, the verdict can still be red but is never green.

The concurrency group is set on the job, not the workflow. A workflow-level
group holds every run, including one whose only job the guard then skips,
and GitHub keeps one pending entry per group. A skipped run could therefore
evict a queued live evaluation. A job the guard skips never queues, so only
real evaluations share the group. Live evaluations never cancel each other in
progress. A newer pending live evaluation replaces an older pending one, and
both judge the current tip of `main` when they start. A dry-run dispatch uses
a separate group, so it cannot evict or wait behind a live run.

`Required Gates` is not a trigger. Every one of its publisher runs, including
those for pull requests and the merge queue, reports `head_branch: main`; about
18 a minute were measured. Its scheduled ruleset verification (cron
`17 */6 * * *`) is read on every evaluation, and the watcher's own schedule
(`47 */6 * * *`) runs 30 minutes after it.

The job guard is an allow-list. Only the schedule, a manual dispatch, or a
completed `push`, `schedule`, or `workflow_run` run of `main` in this
repository wakes the watcher. The mirror evaluates the guard against fork,
pull-request, `pull_request_target`, `issue_comment`, and merge-queue contexts.

Every GitHub call in the script goes through three read helpers or the one
`write` helper, and `write` returns before calling `gh` in dry-run mode. The
mirror fails if a `gh` call appears anywhere else, and it runs dry-run on the
open, update, duplicate-close, close, and status paths.

## Where the required workflows come from

Nothing is listed in the script. The required workflows are the workflows
`.github/workflows/required-gates.yml` aggregates (its `workflow_run` trigger)
plus the `source_workflow` of the registry's aggregating status check in
`specs/ci-gates.v1.yaml`. The registry drift check already verifies that
aggregator list against the blocking gates. `workflow_run` cannot take a
computed list, so the watcher workflow repeats the names. The mirror below
fails when that list differs from the list the script judges
(`scripts/ci/main-health.sh --print-required`), or when it names the
aggregator itself.

## Prove it locally

```bash
bash scripts/test-main-health.sh
```

The mirror runs the script against a fake `gh` that applies the Actions API
query filters. The fake serves recorded JSON for red, green, flapping (rerun
attempt), newer-commit-supersedes-older-red, pending, cancelled,
path-filtered, duplicate-issue, truncated-listing, and ruleset-verifier
shapes. It also serves Security Scan's image-scan shape recorded from `main`
at `022c9bd255` (advisory alone, and next to a blocking red), a red that
beats a newer green of another event, unregistered and unreadable failed
jobs, and changed, unchanged, cleared, and forged red-set markers. The mirror
asserts which writes were and were not attempted
and how many calls one evaluation makes. It is registered as the
`main-health-watcher` gate.

## Check it against real GitHub

The mirror cannot prove GitHub's behavior: the shape of the live API payloads,
whether the workflow token may pin an issue, or that `workflow_run` fires. After
merge, run `Main Health` from the Actions tab (`Run workflow`; `dry_run` is on by
default). The log prints the decision line
`main-health: sha=... state=... action=...` and each `DRY-RUN would:` write
without making one. Then run it once with `dry_run` off on a red `main` (or after
a deliberately red commit) to see the issue and status appear.
