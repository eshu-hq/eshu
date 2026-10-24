<!-- docs-catalog
title: Collector cutover rollback
description: Runs the producer-authority rollback for a collector cutover in the safe order, checks that no two producers hold authority, and compares read truth before and after.
type: operate
audience: operator
entrypoint: false
landing: false
-->

# Collector cutover rollback

Use this page when a collector that moved from the Eshu repository to an
external component must hand authority back to the in-tree producer, and when
you rehearse that handoff before the cutover. It covers one collector instance
set at a time: Jira (`jira`), PagerDuty (`pagerduty`), or the documentation
collector (`documentation`).

Rollback is a producer-authority transition, not a deployment reversal. The
[collector extraction policy](../reference/collector-extraction-policy.md#cutover-and-rollback)
requires that only one producer holds authority for a source scope and
generation at any moment. Putting the old Deployment back while the external
producer still holds a claim or a live grant breaks that rule.

`scripts/collector-cutover-rollback.sh` runs the six steps in order from
existing primitives. It adds no new core command. It refuses to move past a
step when the real state (pods, claim rows, grants) does not satisfy the step
before it.

## Before you start

Do these before the cutover, not during an incident. A rollback you have not
rehearsed is a guess.

- Plan to capture the truth baseline immediately before step 1, not on the day
  of the original cutover. Step 6 requires exact equality, and an old baseline
  differs wherever the source changed since. The command and procedure are on the
  [truth checks page](collector-cutover-truth-checks.md#compare-against-a-baseline-captured-just-before-step-1).
- Record the identifiers the script needs: the collector kind, the external
  and in-tree collector instance IDs, the component ID and version, the
  component registry home the workloads use, the granted core fact kinds, and
  the Kubernetes names of the extension worker, the workflow coordinator, and
  the in-tree collector Deployments. Name every instance on both sides.
  `--external-instance` must list each activation the component runs (see
  `eshu component list`). `--in-tree-instance` must list every in-tree instance
  of the collector kind. The script treats any other instance of the kind as an
  external producer when it checks claims.
- Prepare the restore values: a Helm values overlay that re-enables the
  in-tree collector and its collector instance claims, and keeps
  `componentExtensionCollector.enabled` off. With that setting off the chart
  deletes the extension worker Deployment, and the script counts a missing
  worker Deployment as stopped. Commit the overlay with the cutover change so
  the rollback is reviewed ahead of time. In a GitOps environment, prepare the
  same change as a commit you can merge on demand.
- Set the database connection through the standard libpq environment variables
  or a password file. The script never takes a credential on its command line
  and never prints one.
- Check that `kubectl`, `helm` (for the Helm restore), `psql`, and `eshu` are on
  `PATH`. To run `eshu` inside a pod that mounts the component home, point
  `ROLLBACK_ESHU_BIN` at a wrapper script that runs `kubectl exec`. The same
  pattern works for `ROLLBACK_KUBECTL_BIN`, `ROLLBACK_PSQL_BIN`, and
  `ROLLBACK_HELM_BIN`.
- Know the timeout knobs: `ROLLBACK_KUBECTL_REQUEST_TIMEOUT` (default `30s`,
  `0` disables) bounds each `kubectl` request, `PGCONNECT_TIMEOUT` (default
  `10` seconds) bounds each `psql` connection, and `ROLLBACK_CLOCK_SKEW_SECONDS`
  (default `30`) moves the replay window start back from the database clock,
  because the coordinator stamps work items with its own clock.

## The six steps and the primitive behind each

| Step | What happens | Existing primitive | Gate to continue |
| --- | --- | --- | --- |
| 1. Stop new claims | Remove the external activations, stop the extension worker, restart the coordinator. | `eshu component disable`, `eshu component list`, `kubectl scale`, `kubectl rollout restart` | The component has no enabled activation, no worker pod remains and the worker Deployment is at 0 replicas or absent, and the coordinator rollout finished. |
| 2. Drain leases | Wait until no external producer holds work. | The coordinator reaper (`ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL`) expires stale leases; the script only observes. | 0 claimed work items and 0 active claims for every instance of the kind outside the in-tree set. |
| 3. Revoke the grant | Revoke every producer grant for the external component. | `eshu component revoke-grant` | Every grant lists `revoked=true`. |
| 4. Restore the producer | Re-enable the in-tree collector and make the coordinator plan for it. | `helm upgrade --reuse-values` or a GitOps change, `kubectl scale`, coordinator restart | Steps 1 to 3 still hold when this step starts, and no claim overlap exists yet. |
| 5. Replay | Let the in-tree producer plan and finish work for its scopes. | The coordinator planning pass | No pending, claimed, or retryable item; at least one completed; none failed terminally. |
| 6. Verify | Check there is no dual authority and that fact truth equals the baseline. | Read-only SQL, then the read-truth queries on the [truth checks page](collector-cutover-truth-checks.md) | No dual authority and no difference from the baseline. |

Three details explain the shape of step 1:

- The coordinator and the extension worker read the component registry when
  they start. `eshu component disable` alone changes nothing for a running
  coordinator or worker. That is why step 1 also scales the worker to zero and
  restarts the coordinator.
- The script does not shorten or rewrite leases. A claim ends when its owner
  finishes it or when the coordinator reaper expires it after the lease TTL
  (`ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL`). Step 2 waits for that. Forcing
  a lease with raw SQL would bypass the fencing token that protects against a
  late write, so the script does not do it.
- The script does not trust the instance IDs you typed. `eshu component disable`
  reports "not enabled" for an instance that is already disabled and for an ID
  that does not exist. After the disables, step 1 reads `eshu component list` and
  refuses, before it scales or restarts anything, if the component still has an
  enabled activation. The drain and authority checks count the whole collector
  kind outside the in-tree set, so a mistyped `--external-instance` cannot hide a
  claim held by the real one.

## Run it

Print the plan first. `plan` prints the six steps with the exact commands and
runs nothing.

```bash
scripts/collector-cutover-rollback.sh plan \
  --collector-kind <collector-kind> \
  --external-instance <external-instance-id> \
  --in-tree-instance <in-tree-instance-id> \
  --component-id <component-id> --component-version <component-version> \
  --component-home <component-home> \
  --grant-kind <granted-fact-kind> --grant-kind <another-granted-fact-kind> \
  --namespace <namespace> \
  --worker-deployment <extension-worker-deployment> \
  --coordinator-deployment <workflow-coordinator-deployment> \
  --in-tree-deployment <in-tree-collector-deployment> \
  --helm-release <release> --helm-chart <chart> \
  --restore-values <rollback-values-file>
```

Run it with the same options plus `--yes` and the baseline:

```bash
scripts/collector-cutover-rollback.sh run --yes \
  --baseline baseline-<collector-kind>.txt \
  <the same options as plan>
```

Each step prints its commands, a `RESULT` line, and an `EVIDENCE` line. Paste
every `EVIDENCE` line into the cutover record. A run ends with `ROLLBACK COMPLETE`.

Useful controls:

- `--through-step N` stops after step N. Use it in a GitOps environment: run
  `--through-step 3`, merge and sync the restore change, then resume with
  `--from-step 4 --restore-applied-by-gitops`. Replace the Helm options with
  `--restore-applied-by-gitops` for that resume. Applying the restore change
  before step 3 completes lets two producers claim at once.
- `--from-step N` resumes at step N and re-checks every earlier step against the
  live state first, so a resume cannot skip a gate. Step 5 also needs `--since`.
- `--drain-timeout`, `--step-timeout`, `--replay-timeout`: whole seconds (600, 180, 1800).
- `--keys-only` compares fact kind, count, and stable keys but not payloads.
- `--report-drift` makes a step 6 mismatch also print which stable keys were
  added, removed, or changed. It never accepts a difference; the exit code stays
  6. See the [truth checks page](collector-cutover-truth-checks.md#when-step-6-exits-6).

A partial run is safe to repeat, because every step re-checks the live state
before it acts. Rerun the same command from step 1, or resume at the step that
failed with `--from-step N` (after a step 5 timeout, add the `--since` value that
step 4 printed). A rerun does the following:

- An instance that is not enabled is reported and skipped. The registry check
  then decides whether the component is clear.
- A revoked grant is revoked again without error, and `helm upgrade` or a scale
  to the same state changes nothing.
- A worker Deployment that the restore overlay already deleted counts as
  stopped, so a rerun does not fail on `kubectl scale`. Step 1 still checks the
  registry and the coordinator rollout, and the pod selector check still has to
  pass. That check catches a mistyped Deployment name when the worker pods carry
  the chart's default label.
- The coordinator restarts again, which is harmless: claims live in Postgres.

### Exit codes

| Exit | Meaning | What to do |
| --- | --- | --- |
| 0 | Done. | Record the evidence. |
| 2 | Usage error or missing `--yes` or baseline. | Fix the options. Nothing ran. |
| 3 | A step failed. | Read the `FAILED` line. Nothing after the failed command ran. Fix the cause and rerun. |
| 4 | Refused: a precondition is false. | Read the `REFUSED` line. It names the activation, claim, pod, Deployment, or grant that blocks the step. Do not work around it. |
| 5 | Timeout in step 2 or 5. | See the drain notes below, or the replay notes on the truth checks page. |
| 6 | Fact truth differs from the baseline. | See the comparison notes on the truth checks page. |
| 7 | Dual authority detected. | Stop. Both producers held authority for one scope. Treat it as an incident. The gate before the restore also raises it, which catches an early GitOps restore. |

### If the drain does not finish

Step 2 times out when claimed work remains after the limit. Do not continue and
do not restore the in-tree producer. Check, in order:

1. The coordinator is running. The reaper runs inside it.
2. `ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL` and the claim lease TTL are not
   longer than your drain timeout.
3. No extension worker pod for the external instance is still heartbeating.
   Step 1 waits for the pods that match `--worker-selector` to disappear and the
   Deployment to report 0 replicas. If your chart labels the worker pods
   differently, set that selector.

Then rerun. The script repeats step 1's checks and resumes the wait.

## Check authority, replay, and read truth

The script checks for dual authority and compares fact truth by itself. Three
things live on the companion page
[Collector cutover truth checks](collector-cutover-truth-checks.md): how to
confirm that no two producers hold authority, what "the last committed
generation" means per fact family and how to widen the replay window, and the
read-truth queries to compare before and after.

## What this runbook does not cover

- It does not remove the in-tree implementation. Removal waits for the cutover
  gate to pass, per the extraction policy.
- It does not roll forward. To move authority to the external producer again,
  follow the cutover procedure from its first step; a rollback leaves the
  extension worker stopped (scaled to zero, or deleted by the chart when the
  restore overlay keeps `componentExtensionCollector.enabled` off) and the
  grants revoked on purpose.
- Pending work items the external instance planned before step 1 stay in the
  work table and are never claimed, because nothing runs that instance. They do
  not affect read truth.
- Live rehearsal evidence belongs in the cutover record. The script was tested
  against fakes and a throwaway Postgres, not a deployed environment.

## Related

- [Collector cutover truth checks](collector-cutover-truth-checks.md)
- [Collector Repository Migration Policy](../reference/collector-extraction-policy.md)
- [Component producer grants](../reference/component-producer-grants.md)
- [Component package manager](../reference/component-package-manager.md)
- [Hosted extension operator policy](hosted-extension-policy.md)
- [HTTP API reference](../reference/http-api.md)
