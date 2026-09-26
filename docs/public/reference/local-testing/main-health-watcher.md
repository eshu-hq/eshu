# Main Health Watcher

`.github/workflows/main-health.yml` tells the team when `main` is red. Before it
existed, a required workflow could fail on `main` after two green pull requests
merged and nobody was told: a guard-test red stayed open for 4h44m and was found
by PR authors, and the scheduled ruleset verifier failed on every run with no
owner (#7111).

## What it does

After every required workflow completes on `main`, and every 6 hours as a
backstop, the watcher judges the newest commit on `main` (never the commit of
whichever workflow woke it up) and publishes a `main-health` commit status:

| Verdict | Condition | Issue | Status |
| --- | --- | --- | --- |
| red | a required workflow's latest run on the tip failed, or the latest scheduled `Required Gates` ruleset verification failed | open or update the single `main is red @<sha>` issue | `failure` |
| green | every required workflow that ran on the tip succeeded, none is running, and the aggregator's source workflow has a verdict | close the open issue | `success` |
| pending | a required workflow is still running, or the source workflow has not registered yet | unchanged | `pending` |
| unknown | a required run was cancelled and never re-run | unchanged | `error` |

The issue carries the label `main-health`, is pinned on a best-effort basis, and
lists each failing workflow with its run, job, failed step, and one verdict line
taken from the job log. A `verify-live-ruleset` failure appears in the same
issue. A newer red commit retitles and rewrites the one open issue; it never
opens a second one, and any duplicate found is closed. A newer green commit
closes it.

The watcher exits 0 whenever the evaluation finished, so a red `main` does not
add a red row for the watcher itself.

## Where the required workflows come from

Nothing is listed in the script. The required workflows are the workflows
`.github/workflows/required-gates.yml` aggregates (its `workflow_run` trigger)
plus the `source_workflow` of the registry's aggregating status check in
`specs/ci-gates.v1.yaml`. The registry drift check already verifies that
aggregator list against the blocking gates. `workflow_run` cannot take a
computed list, so the watcher workflow repeats the names; the mirror below
fails when that list differs from the aggregator's plus `Required Gates`.

## Prove it locally

```bash
bash scripts/test-main-health.sh
```

The mirror runs the script against a fake `gh` serving recorded JSON for red,
green, flapping (rerun attempt), newer-commit-supersedes-older-red, pending,
cancelled, path-filtered, duplicate-issue, and ruleset-verifier shapes, and
asserts which writes were and were not attempted. It is registered as the
`main-health-watcher` gate.

## Check it against real GitHub

The mirror cannot prove GitHub's behavior: the shape of the live API payloads,
whether the workflow token may pin an issue, or that `workflow_run` fires. After
merge, run `Main Health` from the Actions tab (`Run workflow`; `dry_run` is on by
default). The log prints the decision line
`main-health: sha=... state=... action=...` and each `DRY-RUN would:` write
without making one. Then run it once with `dry_run` off on a red `main` (or after
a deliberately red commit) to see the issue and status appear.
