# Owned Target Slot Progress Signals

The OSV derivation reads the owned package targets in hash slots. By default
the slot comes from the wall clock, so a reconcile bucket the coordinator never
ran leaves its slot unplanned until the rotation comes around again. With
`derive_from_owned_packages.slot_progress: true` the read takes the 1,024/P
least recently planned *virtual* slots instead and records them in
`owned_target_slot_progress` when the work is admitted (#9776). These signals
show whether that rotation is moving.

A virtual slot is the low 24 bits of a target's slot key modulo 1,024. P (the
reader's hash-slot count) divides 1,024 while P is at most 1,024, so a progress
row survives a change of P. Above that bound the read keeps the clock slot and
says so.

## Metrics

| Metric | Labels | Meaning |
| --- | --- | --- |
| `eshu_dp_owned_target_oldest_slot_age_seconds` | `collector_instance_id` | Age of the least recently planned virtual slot at the latest read: the time from the start of the window that planned it to the start of the window being read. Reported only once all 1,024 slots have a progress row and the read is not in clock fallback; before that the first rotation is still warming up and the gauge is absent. At the 1x defaults (P = 1,024, one slot per window, 30 minute interval) that is about 21.3 days after enablement. |
| `eshu_dp_owned_target_slot_selection_total` | `collector_instance_id`, `outcome` | Selection outcomes per planning pass. |

`outcome` is a closed set. A pass counts its selection mode (`oldest`,
`same_bucket_reuse` or `clock_fallback`) and then any of the others that apply:

- `oldest`: the read chose the least recently planned slots.
- `same_bucket_reuse`: the bucket already had recorded slots, so a repeat
  reconcile reproduced the same plan.
- `clock_fallback`: P is above 1,024 (T above 512 times the target limit), so
  the read used the clock slot and advanced nothing.
- `advanced`: the admission transaction moved at least one progress row.
- `lost_race`: a fresh pick (`oldest`) moved no row because another
  coordinator recorded the bucket first. A repeat tick in the same bucket is
  `same_bucket_reuse` and never counts here: the reconcile ticker runs many
  times per bucket, so counting repeats would read as a race on a healthy
  instance.
- `empty_slot`: a fresh pick held no targets and progress advanced anyway, so
  an empty slot cannot pin the head. A repeat of an empty pick is
  `same_bucket_reuse`.
- `truncated`: the read did not cover every picked slot in full: the limit cut
  the window, or it returned more rows than the target limit, so the derivation
  dropped the lookahead row. Only whole slots below the cut advance; a head
  slot that alone exceeds the limit advances and the read span carries
  `owned_package_targets.head_truncated`.

## Logs and spans

- `workflow coordinator advanced owned package slot progress` (INFO) once per
  progress-ordered pass, with `collector_instance_id`, `selection_mode`,
  `virtual_slots`, `slots_requested`, `slots_advanced` and `outcomes`.
- `owned_target_progress_clock_fallback` (WARN reason): the instance is in
  clock fallback; nothing advances until T falls back under the bound.
- `owned_target_slot_age_exceeds_max_staleness` (WARN reason): the oldest slot
  age is longer than `derive_from_owned_packages.max_staleness`.
- The owned-package window log line adds `selection_mode`, `virtual_slots` and
  `oldest_slot_age_seconds`. The `owned_package_targets.read` span carries
  `owned_package_targets.selection_mode`, `.selected_virtual_slots`,
  `.oldest_slot_age_seconds` and `.head_truncated`; the log line has no
  `head_truncated`.

## Reading them

The age counts *planned* windows: it moves when a slot is put into a run, not
when the run finishes. A work item that fails after admission leaves its slot
advanced. Read it beside the work-item failure signals, not instead of them.

A healthy instance shows `oldest` and `advanced` climbing together with the
age flat near one rotation. `lost_race` climbing with `advanced` flat means
fresh picks keep finding the window already recorded, so another coordinator is
doing the work. The one expected exception is a single window right after a
`scan_interval` raise (see below). `clock_fallback` means the progress
flag is on but not in effect; shrink T per target limit or raise the target
limit. The age rising past `max_staleness` with `advanced` flat means
the coordinator stopped planning; check the coordinator, not the table.

## Changing `scan_interval`

Progress is stored as the start time of the window that planned each slot, in
unix seconds, not as a count of scan intervals. An operator can raise or lower
`scan_interval` on an enabled instance without a migration and without
resetting the rotation: the stored rows keep their order whichever interval
wrote them. Disabling the flag and enabling it later resumes oldest-first from
the stored window starts.

After a lower, no row looks older than it is, and the rotation carries on.

After a raise, the new window starts at the clock truncated to the wider
interval, which can fall before windows that already opened at the old width.
A slot planned in such a window looks newer than the new window, so the new
window cannot advance it. Two cases follow:

- A raise by a small factor (30 minutes to 1 hour, say). If the old window
  that opened exactly at the new window start ran, it recorded slots, so the
  first new window reads `same_bucket_reuse`, plans those slots again and
  writes nothing. The next window picks the oldest slots and the rotation
  resumes. If that old window was skipped, the first new window reads `oldest`
  and advances normally.
- A raise to an interval longer than one full rotation at the old interval
  (P times the old interval; at a target limit of 1,500 with P = 64 that is 32
  hours, so a raise to 2 days), applied when the clock is more than one old
  rotation past the new window's start (a 24 hour raise applied ten minutes
  into the day behaves like the small-factor case). Every row looks newer than
  the new window. The
  first new window's compare-and-set moves nothing, its ticks count
  `lost_race`, and the age gauge reads 0. The next window plans the same slots
  again, still at their old starts, and moves them; the rotation then runs
  normally. At the 1x defaults (P = 1,024, a rotation of about 21.3 days) this
  case is out of practical reach.

No target is missed in either case. The cost is one window whose progress is
not recorded and one group planned twice.

To tell this from a second coordinator doing the work: it lasts one window,
starts right after the interval change, shows the gauge at 0 and then
recovering, and `advanced` returns on the next window. A real second
coordinator does not stop after one window and has no interval change behind
it: its gauge stays at its normal value, its repeat ticks count
`same_bucket_reuse`, and `lost_race` recurs in later windows, but only
sporadically, whenever its read overlaps the winner's. A loser that ticks after
the winner has committed reads `same_bucket_reuse` and counts no `lost_race`.
