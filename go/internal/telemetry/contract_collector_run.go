package telemetry

// Metric names and span name for per-collector claimed-service run telemetry.
// Extracted as a sibling file so the frozen contract.go does not grow,
// mirroring contract_collector_stage.go and contract_bootstrap_ingestion.go.
//
// Every collector runs under the shared claimed-service worker harness
// (go/internal/collector/claimed_service.go). Before this contract an operator
// could see per-stage cost (via #3678 per-stage metrics) but not which
// collector family was the long pole in a full-corpus or multi-collector run.
// These instruments expose per-collector duration and volume at the single
// dispatch chokepoint (ClaimedService.processClaimed) so one instrumentation
// point covers all ~40 collector families.
//
// Instruments driven by this contract:
//
//   - eshu_dp_workflow_claim_run_duration_seconds (collector_kind, source_system,
//     outcome) — per-collector wall time from claim heartbeat to complete/fail.
//     Labeled by outcome (success, unchanged, fail_retryable, fail_terminal,
//     released) so an operator can separate fast-released no-ops from long
//     successful runs without joining to claim state tables.
//
//   - eshu_dp_workflow_claim_facts_emitted_total (collector_kind, source_system)
//     — per-collector fact count recorded from CollectedGeneration.FactCount
//     after a successful commit. Zero means a collector ran but emitted nothing;
//     a spike per collector matches #3678's per-stage content_entity counter.
//
// Join query: sum by (collector_kind) of rate(eshu_dp_workflow_claim_run_duration_seconds_sum)
// / sum by (collector_kind) of rate(eshu_dp_workflow_claim_run_duration_seconds_count)
// gives mean run duration per collector. Pairing with
// eshu_dp_workflow_claim_facts_emitted_total gives facts-per-second per
// collector. Both share the collector_kind label with #3678's
// eshu_dp_bootstrap_pipeline_phase_seconds so the layers join cleanly.

// CollectorRunOutcome* are the bounded outcome label values for
// eshu_dp_workflow_claim_run_duration_seconds. Producers MUST use exactly
// these constants; the metric must stay low-cardinality.
const (
	// CollectorRunOutcomeSuccess is the outcome when a claimed work item was
	// collected, committed, and its claim completed normally.
	CollectorRunOutcomeSuccess = "success"
	// CollectorRunOutcomeUnchanged is the outcome when a claimed source
	// reported no new facts (Unchanged == true) and the claim was completed
	// without a commit.
	CollectorRunOutcomeUnchanged = "unchanged"
	// CollectorRunOutcomeReleased is the outcome when a claimed source
	// returned ok == false (work item not yet ready) and the claim was
	// released back to the queue.
	CollectorRunOutcomeReleased = "released"
	// CollectorRunOutcomeFailRetryable is the outcome when the work item was
	// re-queued with FailClaimRetryable (transient error, within attempt budget).
	CollectorRunOutcomeFailRetryable = "fail_retryable"
	// CollectorRunOutcomeFailTerminal is the outcome when the work item was
	// permanently failed with FailClaimTerminal (terminal error or attempt budget
	// exhausted).
	CollectorRunOutcomeFailTerminal = "fail_terminal"
)

func init() {
	// The outcome label uses the existing MetricDimensionOutcome ("outcome") key
	// already in the registry — no new dimension key is needed.
	// SpanCollectorClaimedRun is registered below.
	spanNames = append(spanNames, SpanCollectorClaimedRun)
}

// SpanCollectorClaimedRun wraps one claimed-service processing cycle
// (ClaimedService.processClaimed) so a trace can attribute latency and outcome
// to the specific collector family that held the claim.
const SpanCollectorClaimedRun = "collector.claimed_run"
