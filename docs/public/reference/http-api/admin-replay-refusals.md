# Admin Replay Refusals

`POST /api/v0/admin/replay` refuses to replay dead-lettered or failed work in an
unsafe or manual-review failure class unless `force=true` is set. This page is
the contract for that refusal. The rest of the replay workflow is in
[Safe Replay Workflow](status-admin.md#safe-replay-workflow).

## Which classes are refused

`input_invalid`, `unsafe_payload`, and the manual-review dead-letter triage
classes `projection_bug` and `resource_exhausted`. Replaying them unchanged
either fails again or re-triggers the unsafe condition, so fix the cause first,
then replay with `force=true` and a `reason` that says what was fixed.

## Two ways to name a refused class

- **`failure_class` selector.** A request whose `failure_class` is refused gets a
  `422` with `status`, `failure_class`, `reason`, and `detail`.
- **Explicit `work_item_ids`.** A request naming ids whose current
  `failure_class` is refused gets a `422` with `status`, `reason`, `detail`, and
  `refused_work_items`, a list sorted by `work_item_id`. Each entry carries
  `work_item_id`, `failure_class`, and the guidance for that class.

```json
{
  "status": "refused",
  "reason": "the named work items are in an unsafe or manual-review failure class; nothing was replayed",
  "detail": "set force=true to replay these work items after addressing the cause",
  "refused_work_items": [
    {"work_item_id": "wi-a", "failure_class": "projection_bug", "reason": "..."}
  ]
}
```

Broad selectors (`scope_id`, `stage`) still skip refused classes silently and
replay the rest; they do not name specific rows.

## Behavior that does not change

- **Mixed requests are refused whole.** If any named id is in a refused class,
  nothing is replayed, including the safe ids. `refused_work_items` lists only
  the offending ids, so drop them (or fix the cause and set `force=true`) and
  retry.
- **A refusal does not consume the idempotency key.** The check runs before the
  key is claimed, so the same `idempotency_key` can be reused for the corrected
  request.
- **Ids that do not exist, or are not `dead_letter` or `failed`, are not
  refused.** They are simply not replayed. A `200` with `replayed_count: 0`
  therefore means nothing matched, never that matched rows were skipped.
- **`force=true` skips the check.** The named rows replay as before.
- **The check honors `scope_id`, `stage`, and `failure_class`.** An id outside the
  requested scope, stage, or failure class is not a match and does not cause a
  refusal, because the check selects the same rows the replay would.
- **The check runs before the idempotency claim.** A retry of a key whose rows
  have since re-dead-lettered into a refused class gets a `422`, not the stored
  outcome of the earlier request.

A refusal writes an `admin_recovery_action` governance audit event with reason
code `replay_refused_unsafe_work_items` (`replay_refused_unsafe_class` for the
`failure_class` selector).
