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

## Superseded-generation projector rows

A projector row whose scope generation is `superseded` is never replayed
(#7130). A newer ingestion of the same scope already replaced that generation,
and projecting the replayed row would re-project the retired generation's graph
and content over the published one. Broad selectors skip
these rows. A request naming them in `work_item_ids` gets a `422` whose
`refused_work_items` entries carry `work_item_id`, `generation_id`,
`failure_class: projector_replay_generation_superseded`, and a `reason`.

`force=true` does not apply to this refusal. It runs after the request-level
`failure_class` check and before the explicit-id unsafe-class check, so an
unforced request with an unsafe `failure_class` gets that class's `422` first.
Drop the named ids and retry. The governance audit reason code is
`replay_refused_superseded_generation`. Reducer rows on a superseded generation
are not affected.

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
- **`force=true` skips the unsafe-class check.** The named rows replay as
  before, unless one is a superseded-generation projector row.
- **The check honors `scope_id` and `stage`.** An id outside the requested scope
  or stage is not a candidate and does not cause a refusal, because the check
  applies the same selectors as the replay.
- **A `failure_class` selector skips the check.** An unsafe class is refused (or
  forced) before the check runs, so a class that reaches it is safe and cannot
  match an unsafe row. Ids in another class are simply not replayed.
- **The check runs before the idempotency claim.** A retry of a key whose rows
  have since re-dead-lettered into a refused class gets a `422`, not the stored
  outcome of the earlier request.

A refusal writes an `admin_recovery_action` governance audit event with reason
code `replay_refused_unsafe_work_items` (`replay_refused_unsafe_class` for the
`failure_class` selector).
