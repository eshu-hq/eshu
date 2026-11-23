# Reducer Waiting Key Bound

A reducer item that defers on the restricted corpus fence parks a wait row in
`reducer_waiting_items`. The row's `waits_for_key` names the scopes the item
waits on, and it sits in two btree indexes (the primary key and the wake
index). A btree entry may not exceed 2,704 bytes after compression, and the
set of blocking scopes is unbounded: a bootstrap can leave every incomplete
git scope blocking.

Since #11256 the key is bounded by construction. It names the scope, the
generation and a sorted prefix of the blocking set: at most 16 scopes and at
most 1,024 bytes of rendered key. The wait stays exact. The activation wake is
a hint, the retried handler re-checks the whole restricted fence, and once 16
or fewer blockers remain all of them are named, so the last activation always
wakes the item. The read set is not part of the key.

A size guard covers every wait kind: `work_item_id`, `scope_id` and
`generation_id` have no length bound. A wait row whose `work_item_id`, kind
and key exceed 2,048 uncompressed bytes is not written. The queue row keeps the
timer path the retry already set (the 10-minute backstop for a parked kind), so
the item runs again.

## Counters

| Metric | Labels | Meaning |
| --- | --- | --- |
| `eshu_dp_reducer_waiting_key_truncated_total` | `domain` | A narrowed wait was parked naming fewer blocking scopes than the verdict held. The wait is still exact. A sustained rate means bootstrap-sized verdicts, not a fault. |
| `eshu_dp_reducer_waiting_key_oversize_total` | `kind` | A wait was not parked because its row would not fit the btree indexes. The item keeps its timer path. Expect zero: a non-zero value means an unbounded `work_item_id`, `scope_id` or `generation_id`, or a narrowed set where no scope ID fits the key. |

Neither counter carries a scope or item id. Those are on the log.

## Logs

| Message | Level | Fields |
| --- | --- | --- |
| `reducer waiting row does not fit the waiting table's btree indexes; the item keeps its timer path` (`event_name=reducer_waiting.key_oversize`) | error | `work_item_id`, `waits_for_kind`, `row_bytes` (0 when a narrowed set had no ID that fits), `budget_bytes`, `blocking_scopes` |

## What to do

- Truncated count rising during a bootstrap or resync: nothing. Waits reopen as
  the named scopes activate and the handler names the next prefix.
- Oversize count above zero: find the item in the log. Its queue row is
  `retrying` at the backstop, so it still runs; the fix is the unbounded
  identifier, not the wait.
