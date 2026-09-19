// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"log/slog"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

func (t *iamCanPerformTally) recordSkip(reason string) {
	switch reason {
	case iamCanPerformSkipUncatalogued:
		t.skippedUncatalogued++
	case iamCanPerformSkipAmbiguous:
		t.skippedAmbiguous++
	case iamCanPerformSkipUnresolved:
		t.skippedUnresolved++
	case iamCanPerformSkipDeny:
		t.skippedDeny++
	case iamCanPerformSkipConditioned:
		t.skippedConditioned++
		t.conditionedProvenanceOnly++
	case iamCanPerformSkipNotActionResource:
		t.skippedNotActionResource++
	case iamCanPerformSkipSelfLoop:
		t.skippedSelfLoop++
	case iamCanPerformSkipPermissionBoundary:
		t.skippedPermissionBoundary++
	}
}

// iamCanPerformTiming groups stage durations and the resolution tally so the
// completion log identifies fact-load, extraction, retract, and graph-write time,
// plus why catalog-action evaluations lost edges.
type iamCanPerformTiming struct {
	intent                  reducercontract.Intent
	resourceCount           int
	permissionCount         int
	permissionBoundaryCount int
	resourcePolicyCount     int
	edgeCount               int
	tally                   iamCanPerformTally
	skipRetract             bool
	loadDuration            time.Duration
	extractDuration         time.Duration
	retractDuration         time.Duration
	writeDuration           time.Duration
	totalDuration           time.Duration
}

func logIAMCanPerformCompleted(ctx context.Context, timing iamCanPerformTiming) {
	slog.InfoContext(
		ctx, "iam can_perform materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("iam_permission_fact_count", timing.permissionCount),
		slog.Int("permission_boundary_fact_count", timing.permissionBoundaryCount),
		slog.Int("resource_policy_permission_fact_count", timing.resourcePolicyCount),
		slog.Int("can_perform_edge_count", timing.edgeCount),
		slog.Int("skipped_uncatalogued_action", timing.tally.skippedUncatalogued),
		slog.Int("skipped_ambiguous", timing.tally.skippedAmbiguous),
		slog.Int("skipped_unresolved", timing.tally.skippedUnresolved),
		slog.Int("skipped_deny", timing.tally.skippedDeny),
		slog.Int("skipped_conditioned", timing.tally.skippedConditioned),
		slog.Int("conditioned_provenance_only", timing.tally.conditionedProvenanceOnly),
		slog.Int("skipped_not_action_resource", timing.tally.skippedNotActionResource),
		slog.Int("skipped_self_loop", timing.tally.skippedSelfLoop),
		slog.Int("skipped_permission_boundary", timing.tally.skippedPermissionBoundary),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
