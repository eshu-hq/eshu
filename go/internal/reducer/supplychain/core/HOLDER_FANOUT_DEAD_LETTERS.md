# Dead-lettered holders (#11147)

How the supply-chain impact holder fan-out treats a holder whose work item is
dead-lettered. This is a section of [`HOLDER_FANOUT.md`](HOLDER_FANOUT.md),
split out to keep that file under the 500-line Markdown cap.

A holder whose item is a dead letter at its scope's active generation keeps a
stale copy that nothing else re-derives, so the reopen moves it back to
`pending` as a full genuine pass: `attempt_count` 0, lease, retry, and failure
fields cleared, `visible_at` and `reopened_at` set to now, the
container-image-identity authorization columns set the way the admin replay
sets them, and no origin stamp (an existing one is removed). A stamped
attempt-1 pass would run as a CVE-scoped one hop (`fanOutOneHop`), mark the
item succeeded, and hide the unfinished genuine pass. Its `fanout_owed` marker
and `fanout_remaining` list stay, so the reopened pass still fans out what the
dead pass owed. A dead letter whose class is in the admin unsafe-replay set
(`recovery.UnsafeReplayFailureClasses`, the one source the replay endpoint also
reads) stays dead and reports `dead_letter_skipped`; an operator addresses its
cause and replays it. An operator's dead letter or skip of a holder (`POST /api/v0/admin/dead-letter`, `/admin/skip`) is not sticky: its class is not unsafe, so the next change that names the holder reopens it and clears its failure note. There is no attempt cap across reopens: a cap would be a
silent stale copy, and the cost is bounded by the change rate times the attempt
budget. A dead letter at a superseded generation is never touched. Outcomes:
`dead_letter_reopened` and `dead_letter_skipped` carry the prior
`failure_class` on the fan-out counter and write one info line each;
`no_active_generation` names a holder scope with no active generation;
`pending_noop` now means only a queued or already-flagged row. The
`eshu_dp_supply_chain_impact_active_dead_letters{failure_class}` gauge and its
`_owing_fanout` subset show what is still pinned. One residual is open: a running holder that a fan-out flags and that then dead-letters loses the flagged change, because the dead-letter transition clears the flag, until another change reaches it (#11159). A second residual is open: a genuine pass flagged while running that then fails in a non-counting class retries at attempt 1 with the origin stamp, so it runs as a CVE-scoped one hop and hides the genuine pass (#11173).
