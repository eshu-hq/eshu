# Go reachability operator plan

Out-of-rotation one-shot for Go reachability scopes (eshu-hq/eshu#10002,
arbiter ruling B.3): `eshu-workflow-coordinator plan-go-reachability-scope`
plans exactly one repository through the real `reachability.Plan` and the real
open-target admission, under a distinct `go-modules-operator-<request-id>` plan
key and the `operator_recovery` trigger. It exists for the case where a
repository's rotation slot has not arrived yet and waiting is not acceptable;
the steady-state rotation stays armed and remains the default path.

## When not to fire

- **Do not fire when a scheduled item for the scope is open.** In the same
  hour as firing, probe `workflow_work_items` for the fixture scope
  (`go-reachability://<repository source_key>`): if an open item exists, stand
  down and let rotation deliver. Firing anyway is harmless but purposeless —
  admission returns `already_open` and creates nothing.
- **Do not fire to "test the tool"** against an evidence scope. The pre-fire
  check (fixture still eligible: active generation plus `go.mod` rows) must pass.
- Never fire against prod, never fire for more than the one named scope, and
  never re-fire with a new request id as a retry mechanism for `auth_denied`,
  OOM, or partial-generation failures — those escalate per the step-5 bounds,
  they do not retry.

## Flags

All of `--instance-id`, `--repository-id`, `--request-id`, `--requested-by`,
and `--reason` are required; a missing flag usage-errors before any connection
opens. `--dry-run` evaluates eligibility and the fence decision, prints the
would-plan verdict, and writes nothing.

| Flag | Meaning |
| --- | --- |
| `--instance-id` | Durable collector-instance id. Must resolve to exactly one instance, enabled and claims-enabled with `derive_from_go_modules` on and a valid interval. |
| `--repository-id` | Repository `source_key` (for example `repository:r_efdfb126`). The commit scanned is always the active generation's observed commit. |
| `--request-id` | Operator-chosen idempotency token (for example `10032-fixture-20261123`). Must pass the plan-key charset; timestamp-only values are refused because a retry would mint a second identity. |
| `--requested-by`, `--reason` | Audit fields. They land in the structured log line only, never in `requested_scope_set`. |

## Outcomes and exit codes

| Outcome | Exit | Meaning |
| --- | --- | --- |
| `planned` | 0 | One run row and one work item admitted. |
| `already_open` | 0 | Nothing created: the target is already open (scheduled or prior operator run), or this request id already fired and the existing run is reported. When a hosted tenant boundary is configured, a grant-denied scope also lands here as `target_already_open`; check the boundary grants before reading the reason literally. |
| `dry_run` | 0 | Eligibility and fence decision only; zero writes. |
| `not_eligible` | 1 | The repository would never enter the rotation page. `reason` names it: `repository_not_found`, `no_active_generation`, `no_go_module_or_evidence`, or the planner's skip (`unsupported_remote`, `missing_commit`, …). |
| invocation / infrastructure failure | 2 | Refusal (bad flags, instance mismatch, derivation off, fence error) or a store failure. The outcome word is still logged. |

A re-fire under the same `--request-id` is an exact no-op: the existing run id
probes planned first, so neither the fence nor admission runs twice. A genuinely
new attempt uses a new request id, and the run record explains why.

## QA firing sequence

1. **Dry run first.** Capture the JSON outcome document and paste it into the run record.
2. **Apply a reviewed in-cluster Job** using the workflow-coordinator image at
   the then-current QA pin and the coordinator's own settings, RBAC, and DB role
   (control route). Record the manifest, job name, image digest, and flags in
   the run record **before** applying. Fire on QA only; this lane ships no
   manifest for any other cluster.
3. **Read back the audit line.** Every attempt logs
   `workflow coordinator planned go reachability scope for operator request`
   with `plan_key`, `run_id`, `scope_id`, short `commit`, `outcome`,
   `trigger_kind="operator_recovery"`, `requested_by`, `reason`, `dry_run`.
   Capture `EXPLAIN (ANALYZE, BUFFERS)` of the single-repository read for the
   firing record: no local plan proof exists for it, only the CI live-reading
   equivalence test.
4. **Prove the pair.** The operator audit line plus the next scheduled
   `planned derived scopes` line for context, and an indexed
   `workflow_work_items` probe showing the fixture scope row under the operator
   run id; then terminal claim state, then the full evidence set.

## Slot-day behavior

Open means the run status is not `complete` or `failed`. When the rotation slot
arrives after a triggered run:

| Triggered run state at slot plan | Slot-day behavior for the fixture |
| --- | --- |
| Open (pending/claimed/in-progress) | Fixture item **not** admitted (target already open); the rest of the page plans normally; the skip is visible as `target_already_planned`. No duplicate, no collision. |
| Complete | Fixture item admitted again under the scheduled key: a second generation id and a second scan of the same commit. If it completes, its generation supersedes the operator generation. |
| Failed (terminal: `auth_denied`, OOM budget-exhausted, dead-letter) | Failed runs do not count as open, so the fixture is admitted on slot day normally. A triggered failure never wedges the fixture. |

Known residual: in the Complete row, the slot-day re-scan carries the standing
`v0.1.0` partial-generation TrustedDeletion limitation — a partial slot-day
scan could degrade good operator-generation evidence. Capture the completeness
and readback evidence as soon as the triggered generation completes, and record
the slot-page outcome when it lands.

## Related pages

- [Service Runtimes](../deployment/service-runtimes.md)
- `go/internal/coordinator/README.md`
- `go/internal/coordinator/reachability/README.md`
- `go/cmd/workflow-coordinator/README.md`
