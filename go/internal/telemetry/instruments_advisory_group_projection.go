// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// AdvisoryGroupProjectionOutcome* values are the closed outcome set of
// eshu_dp_advisory_group_projection_groups_total (#10411). Projected means
// the group's AdvisoryGroup node and its qualifying AFFECTED_BY edges were
// reconciled into the graph; Deleted means no winner qualified, so the
// reconcile removed the node and its edges; Failed means the reconcile
// errored and the intent retries or dead-letters.
const (
	// AdvisoryGroupProjectionOutcomeProjected is a group reconciled with at
	// least one qualifying winner edge.
	AdvisoryGroupProjectionOutcomeProjected = "projected"
	// AdvisoryGroupProjectionOutcomeDeleted is a group whose qualifying
	// winner set is empty, so its node and edges were retracted.
	AdvisoryGroupProjectionOutcomeDeleted = "deleted"
	// AdvisoryGroupProjectionOutcomeFailed is a group whose reconcile
	// returned an error.
	AdvisoryGroupProjectionOutcomeFailed = "failed"
	// AdvisoryGroupProjectionOutcomeEdgelessWithWinners is a group whose
	// qualifying winners exist but resolve to no edge (every winner's
	// repository is absent from the graph), so the anchor was deleted even
	// though qualifying winner rows were read. A steady rate here points at
	// repository-materialization gaps, not advisory churn.
	AdvisoryGroupProjectionOutcomeEdgelessWithWinners = "edgeless_with_winners"
)

var (
	// advisoryGroupProjectionReconcileBuckets spans a single-group Cypher
	// reconcile: sub-10ms reads up to a 30 s pathological write.
	advisoryGroupProjectionReconcileBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	// advisoryGroupProjectionLagBuckets spans enqueue-to-completion lag,
	// which includes the winners maintainer cadence (30 s) and queue wait.
	advisoryGroupProjectionLagBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600}
)

// registerAdvisoryGroupProjectionInstruments registers the advisory-group
// graph projection signals of #10411 on inst. Every label is domain or a
// closed outcome: no label carries a group, CVE, advisory, repository, or
// package id.
//
//   - eshu_dp_advisory_group_projection_groups_total{domain,outcome}: groups
//     reconciled, by outcome (projected, deleted, failed).
//   - eshu_dp_advisory_group_projection_edges_upserted_total{domain}:
//     AFFECTED_BY edges the reconcile created.
//   - eshu_dp_advisory_group_projection_edges_retracted_total{domain}:
//     AFFECTED_BY edges the reconcile deleted.
//   - eshu_dp_advisory_group_projection_nodes_deleted_total{domain}:
//     AdvisoryGroup nodes the reconcile deleted.
//   - eshu_dp_advisory_group_projection_member_union_oversized_total{domain}:
//     groups whose member union exceeded 32 ids, the signal that alias
//     grouping produced an unusually wide group.
//   - eshu_dp_advisory_group_projection_enqueue_failures_total{domain}:
//     supply_chain_impact passes whose post-commit group-projection enqueue
//     failed. The pass still acks (the graph is a cache over the winners
//     read model), so this counter is how an operator sees that
//     convergence now waits on the next touch or backfill.
//   - eshu_dp_advisory_group_projection_reconcile_duration_seconds{domain}:
//     wall time of one group's graph reconcile.
//   - eshu_dp_advisory_group_projection_lag_seconds{domain}: enqueue to
//     completion seconds per projection intent, including the winners
//     watermark wait. A rising tail means the graph trails the winners
//     read model.
func registerAdvisoryGroupProjectionInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.AdvisoryGroupProjectionGroups, err = meter.Int64Counter(
		"eshu_dp_advisory_group_projection_groups_total",
		metric.WithDescription("Advisory groups the advisory_group_projection reducer reconciled into the graph, by outcome (projected, deleted, failed). Deleted means no canonical winner qualified, so the AdvisoryGroup node and its AFFECTED_BY edges were retracted."),
	); err != nil {
		return fmt.Errorf("register AdvisoryGroupProjectionGroups counter: %w", err)
	}
	if inst.AdvisoryGroupProjectionEdgesUpserted, err = meter.Int64Counter(
		"eshu_dp_advisory_group_projection_edges_upserted_total",
		metric.WithDescription("Repository-[AFFECTED_BY]->AdvisoryGroup edges the advisory-group projection created."),
	); err != nil {
		return fmt.Errorf("register AdvisoryGroupProjectionEdgesUpserted counter: %w", err)
	}
	if inst.AdvisoryGroupProjectionEdgesRetracted, err = meter.Int64Counter(
		"eshu_dp_advisory_group_projection_edges_retracted_total",
		metric.WithDescription("Repository-[AFFECTED_BY]->AdvisoryGroup edges the advisory-group projection deleted because their winner no longer qualifies."),
	); err != nil {
		return fmt.Errorf("register AdvisoryGroupProjectionEdgesRetracted counter: %w", err)
	}
	if inst.AdvisoryGroupProjectionNodesDeleted, err = meter.Int64Counter(
		"eshu_dp_advisory_group_projection_nodes_deleted_total",
		metric.WithDescription("AdvisoryGroup anchor nodes the advisory-group projection deleted because no canonical winner qualified for the group."),
	); err != nil {
		return fmt.Errorf("register AdvisoryGroupProjectionNodesDeleted counter: %w", err)
	}
	if inst.AdvisoryGroupProjectionMemberUnionOversized, err = meter.Int64Counter(
		"eshu_dp_advisory_group_projection_member_union_oversized_total",
		metric.WithDescription("Advisory groups whose member union (winner upstream members plus each winner's own CVE and advisory ids) exceeded 32 ids. A steady rate means alias grouping is producing unusually wide groups worth checking against the allow/deny lists."),
	); err != nil {
		return fmt.Errorf("register AdvisoryGroupProjectionMemberUnionOversized counter: %w", err)
	}
	if inst.AdvisoryGroupProjectionEnqueueFailures, err = meter.Int64Counter(
		"eshu_dp_advisory_group_projection_enqueue_failures_total",
		metric.WithDescription("supply_chain_impact passes whose post-commit advisory-group projection enqueue failed. The pass still acknowledges because the graph is a cache over the winners read model; a non-zero rate means some groups converge only on their next touch or the backfill."),
	); err != nil {
		return fmt.Errorf("register AdvisoryGroupProjectionEnqueueFailures counter: %w", err)
	}
	if inst.AdvisoryGroupProjectionReconcileDuration, err = meter.Float64Histogram(
		"eshu_dp_advisory_group_projection_reconcile_duration_seconds",
		metric.WithDescription("Wall time of one advisory group's graph reconcile (winner read plus AdvisoryGroup node and AFFECTED_BY edge write)."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(advisoryGroupProjectionReconcileBuckets...),
	); err != nil {
		return fmt.Errorf("register AdvisoryGroupProjectionReconcileDuration histogram: %w", err)
	}
	if inst.AdvisoryGroupProjectionLag, err = meter.Float64Histogram(
		"eshu_dp_advisory_group_projection_lag_seconds",
		metric.WithDescription("Seconds from advisory-group projection intent enqueue to completion, including the wait for the supply-chain impact winners watermark to pass the enqueue time. It bounds how long the graph trails the winners read model for a touched group."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(advisoryGroupProjectionLagBuckets...),
	); err != nil {
		return fmt.Errorf("register AdvisoryGroupProjectionLag histogram: %w", err)
	}
	return nil
}

// RecordAdvisoryGroupReconciled counts one reconciled advisory group and
// the graph mutations its reconcile produced. Outcome is one of the
// AdvisoryGroupProjectionOutcome* constants. A nil Instruments or an
// unregistered counter records nothing, matching sibling handlers.
func (inst *Instruments) RecordAdvisoryGroupReconciled(
	ctx context.Context,
	domain string,
	outcome string,
	edgesCreated int,
	edgesDeleted int,
	nodesDeleted int,
) {
	if inst == nil {
		return
	}
	if inst.AdvisoryGroupProjectionGroups != nil {
		inst.AdvisoryGroupProjectionGroups.Add(ctx, 1, metric.WithAttributes(
			AttrDomain(domain), AttrOutcome(outcome),
		))
	}
	if edgesCreated > 0 && inst.AdvisoryGroupProjectionEdgesUpserted != nil {
		inst.AdvisoryGroupProjectionEdgesUpserted.Add(ctx, int64(edgesCreated), metric.WithAttributes(AttrDomain(domain)))
	}
	if edgesDeleted > 0 && inst.AdvisoryGroupProjectionEdgesRetracted != nil {
		inst.AdvisoryGroupProjectionEdgesRetracted.Add(ctx, int64(edgesDeleted), metric.WithAttributes(AttrDomain(domain)))
	}
	if nodesDeleted > 0 && inst.AdvisoryGroupProjectionNodesDeleted != nil {
		inst.AdvisoryGroupProjectionNodesDeleted.Add(ctx, int64(nodesDeleted), metric.WithAttributes(AttrDomain(domain)))
	}
}

// RecordAdvisoryGroupMemberUnionOversized counts one group whose member
// union exceeded the oversized threshold.
func (inst *Instruments) RecordAdvisoryGroupMemberUnionOversized(ctx context.Context, domain string) {
	if inst == nil || inst.AdvisoryGroupProjectionMemberUnionOversized == nil {
		return
	}
	inst.AdvisoryGroupProjectionMemberUnionOversized.Add(ctx, 1, metric.WithAttributes(AttrDomain(domain)))
}

// RecordAdvisoryGroupEnqueueFailure counts one failed advisory-group
// projection enqueue from a supply_chain_impact pass. The domain label
// carries the emitting pass's domain so the counter attributes to the
// writer that owed the enqueue.
func (inst *Instruments) RecordAdvisoryGroupEnqueueFailure(ctx context.Context, domain string) {
	if inst == nil || inst.AdvisoryGroupProjectionEnqueueFailures == nil {
		return
	}
	inst.AdvisoryGroupProjectionEnqueueFailures.Add(ctx, 1, metric.WithAttributes(AttrDomain(domain)))
}

// RecordAdvisoryGroupReconcileDuration records one group's reconcile wall
// time in seconds.
func (inst *Instruments) RecordAdvisoryGroupReconcileDuration(ctx context.Context, domain string, seconds float64) {
	if inst == nil || inst.AdvisoryGroupProjectionReconcileDuration == nil {
		return
	}
	inst.AdvisoryGroupProjectionReconcileDuration.Record(ctx, seconds, metric.WithAttributes(AttrDomain(domain)))
}

// RecordAdvisoryGroupLag records one projection intent's enqueue-to-
// completion lag in seconds. Negative samples (clock skew between the
// queue row and this process) are dropped rather than recorded.
func (inst *Instruments) RecordAdvisoryGroupLag(ctx context.Context, domain string, seconds float64) {
	if inst == nil || inst.AdvisoryGroupProjectionLag == nil || seconds < 0 {
		return
	}
	inst.AdvisoryGroupProjectionLag.Record(ctx, seconds, metric.WithAttributes(AttrDomain(domain)))
}
