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
| `eshu_dp_owned_target_oldest_slot_age_seconds` | `collector_instance_id` | Age of the least recently planned virtual slot at the latest read: bucket distance times the reconcile interval. Reported only once all 1,024 slots have a progress row and the read is not in clock fallback; before that the first rotation is still warming up and the gauge is absent. At the 1x defaults (P = 1,024, one slot per window, 30 minute interval) that is about 21.3 days after enablement. |
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

The age counts *planned* buckets: it moves when a slot is put into a run, not
when the run finishes. A work item that fails after admission leaves its slot
advanced. Read it beside the work-item failure signals, not instead of them.

A healthy instance shows `oldest` and `advanced` climbing together with the
age flat near one rotation. `lost_race` climbing with `advanced` flat means
fresh picks keep finding the bucket already recorded, so another coordinator is
doing the work. `clock_fallback` means the progress
flag is on but not in effect; shrink T per target limit or raise the target
limit. The age rising past `max_staleness` with `advanced` flat means
the coordinator stopped planning; check the coordinator, not the table.
