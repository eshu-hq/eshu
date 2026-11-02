// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

// SpecReading is one interpretation the harness makes of a sentence in
// REMOVAL_CONTENTION_SPEC.md. Every report carries the full list, so the
// arbiter can accept or reject each reading at the live-run review.
type SpecReading struct {
	ID string `json:"id"`
	// Spec names the section and the line on the harness branch.
	Spec string `json:"spec"`
	// Quote is the spec text the reading interprets.
	Quote   string `json:"quote"`
	Reading string `json:"reading"`
}

// SpecReadings is the list of readings this harness version applies. The
// README "Spec readings" table mirrors it; TestSpecReadingsMatchREADME
// keeps the two in step.
func SpecReadings() []SpecReading {
	return []SpecReading{
		{
			ID: "workers", Spec: "§5 step 1, line 145",
			Quote:   "ClaimNextEligible from W claimers (W = 1x, then 2x worker_counts)",
			Reading: "W = worker_counts x worker_multiple; each worker is one claim loop with its own owner id holding one claim at a time, the production shape of one claim loop per collector Deployment.",
		},
		{
			ID: "quota_held", Spec: "§5 step 5, line 153; §6 no overshoot, line 188",
			Quote:   "at least one synthetic scope is held at its budget ... grants per budget bucket never exceed quota_budget",
			Reading: "The quota kind gets an at-budget scope with budget+extra items and a below-budget scope with budget-1 items. no_overshoot passes when no (kind, scope, hour) bucket is above budget and at least one bucket reached it. Items still held above budget at the end are reported as quota_held_items and excluded from drain_to_zero; a new hour bucket may drain them legitimately.",
		},
		{
			ID: "pin", Spec: "§5 step 7, lines 158-160",
			Quote:   "some are pinned and then outlive the commit deadline; some are pinned after reclaim (stale)",
			Reading: "A seeded share pins before completing; a short pin whose deadline passes while the lease lives must still complete; half of the abandoned claims are pinned past their lease and the reaper must wait for the pin; stale pins rotate into the step-9 writes and must be fence-rejected.",
		},
		{
			ID: "replay", Spec: "§5 step 8, line 161",
			Quote:   "a seeded share of terminal calls is replayed once after an injected timeout",
			Reading: "Terminal means CompleteClaim and FailClaimTerminal. The first call gets a 1 ms deadline, then one replay on a fresh deadline must ack. Release and FailClaimRetryable are not replayed: after another worker re-claims the item, rejection is correct and belongs to step 9.",
		},
		{
			ID: "stale_writers", Spec: "§5 step 9, line 163",
			Quote:   "a seeded share of workers keeps writing with an old fencing token after their claim was reaped and re-claimed",
			Reading: "An abandoned claim's worker writes once (heartbeat, complete, fail_terminal, or pin) after the client has seen the item re-granted at a higher token. Success aborts; any code other than the fence rejection fails the criterion.",
		},
		{
			ID: "reclaim", Spec: "§6 reclaim, line 181",
			Quote:   "every expired lease is reclaimed within lease_ttl + reap_interval + one reap pass",
			Reading: "Measured per abandoned claim as finished_at of the expired claim row minus the later of lease_expires_at and the commit pin, against reap_interval + the longest measured reap pass + 1 s + clock skew (lease_ttl is already inside lease_expires_at). The 5 s requeue delay applies after the reap and is not in the bound.",
		},
		{
			ID: "starvation", Spec: "§6 starvation bound, line 186",
			Quote:   "no eligible item waits longer than starvation_bound_seconds; no scope goes unclaimed while below budget",
			Reading: "The longest ClaimNextEligible call (issued or idle) must stay under the bound, and every backlog scope must get a grant inside the window. Enqueue-to-first-claim wait over a 100,000-item backlog is minutes by construction and is not judged.",
		},
		{
			ID: "drain", Spec: "§6 drain to zero, line 195",
			Quote:   "stop issuing work, let reap and replay finish, confirm zero nonterminal synthetic items and zero live synthetic claims",
			Reading: "After the window the chaos mix stops; workers keep claiming and completing the remaining backlog until every non-quota-held item is terminal, no claim is live, and every armed stale writer has fired.",
		},
		{
			ID: "planner_statistics", Spec: "§2 backlog_items, line 84",
			Quote:   "peak nonterminal workflow_work_items depth from production history",
			Reading: "Production tables carry planner statistics, a freshly seeded one does not. After seeding the harness waits read-only (bounded, default 5m) for autovacuum to analyze workflow_work_items; stats_ready=false records a window run on cold statistics.",
		},
		{
			ID: "lock_waiters", Spec: "§2 pg_headroom, line 89",
			Quote:   "satellite database CPU, connection count, and replication-lag limits",
			Reading: "The live ceiling on lock waiters counts core's database only (ESHU_CONTENTION_PG_LOCK_DATABASE=eshu). A local in-tree advisory round queues up to 24 sessions on pg_advisory_xact_lock by design, so the local ceiling sits above the advisory goroutines plus W.",
		},
	}
}
