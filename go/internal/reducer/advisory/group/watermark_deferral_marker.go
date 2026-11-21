// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"context"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// deferralMarkerState classifies the first-deferral marker a prior verdict
// left in the intent's failure details (#11089). The classification keeps
// the fallback cases apart: only an absent, unparsable, or future-dated
// marker on a row that already waited is a loss an operator must see.
type deferralMarkerState int

const (
	// deferralMarkerUsable is a same-cycle marker the stale clock anchors on.
	deferralMarkerUsable deferralMarkerState = iota
	// deferralMarkerAbsent is failure details carrying no marker at all.
	deferralMarkerAbsent
	// deferralMarkerUnparsable is a marker whose shape or timestamps do not parse.
	deferralMarkerUnparsable
	// deferralMarkerFutureDated is a same-cycle marker dated after now.
	deferralMarkerFutureDated
	// deferralMarkerPriorCycle is a parsable marker from an earlier repair
	// cycle: a reopen restarts the clock by design, so it is not a loss.
	deferralMarkerPriorCycle
)

// classifyDeferralMarker parses the deferral marker in failure details for
// the intent's current repair cycle. It returns the first-deferral instant
// only for a usable marker.
func classifyDeferralMarker(details string, cycleStartedAt, now time.Time) (time.Time, deferralMarkerState) {
	idx := strings.Index(details, winnersWatermarkDeferralMarkerPrefix)
	if idx < 0 {
		return time.Time{}, deferralMarkerAbsent
	}
	rest := strings.TrimSpace(details[idx+len(winnersWatermarkDeferralMarkerPrefix):])
	fields := strings.Fields(rest)
	if len(fields) != 2 || !strings.HasPrefix(fields[1], "cycle-started-at=") {
		return time.Time{}, deferralMarkerUnparsable
	}
	first, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return time.Time{}, deferralMarkerUnparsable
	}
	cycle, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(fields[1], "cycle-started-at="))
	if err != nil {
		return time.Time{}, deferralMarkerUnparsable
	}
	if !cycle.UTC().Equal(cycleStartedAt.UTC()) {
		return time.Time{}, deferralMarkerPriorCycle
	}
	if first.After(now) {
		return time.Time{}, deferralMarkerFutureDated
	}
	return first.UTC(), deferralMarkerUsable
}

// isWatermarkWaitClass reports whether a prior failure class is one of the
// two winners-watermark wait verdicts — the only verdicts that write a
// deferral marker, so the only ones whose absent marker is a loss.
func isWatermarkWaitClass(failureClass string) bool {
	return failureClass == WinnersWatermarkNotReadyFailureClass ||
		failureClass == WinnersWatermarkStaleFailureClass
}

// resolveDeferralStart returns the moment the stale clock measures from:
// the cycle's first deferral when the intent carries a usable same-cycle
// marker, otherwise now (this evaluation is the first deferral). The
// returned loss reason names why a marker a prior wait verdict should
// have left was unusable (#11089); it is empty for a genuine first
// deferral, a non-wait prior verdict, a healthy marker, and the
// prior-cycle marker a reopen restarts from by design. The start is
// exactly what the pre-#11089 fallback returned for every input, so the
// signal changes no verdict.
func resolveDeferralStart(intent reducercontract.Intent, now time.Time) (start time.Time, lossReason string) {
	if intent.Failure == nil {
		return now, ""
	}
	first, state := classifyDeferralMarker(intent.Failure.Details, intent.CycleStartedAt, now)
	if state == deferralMarkerUsable {
		return first, ""
	}
	if !isWatermarkWaitClass(intent.Failure.FailureClass) {
		return now, ""
	}
	switch state {
	case deferralMarkerAbsent:
		return now, telemetry.AdvisoryGroupWatermarkMarkerLossMissing
	case deferralMarkerUnparsable:
		return now, telemetry.AdvisoryGroupWatermarkMarkerLossUnparsable
	case deferralMarkerFutureDated:
		return now, telemetry.AdvisoryGroupWatermarkMarkerLossFutureDated
	default: // deferralMarkerPriorCycle
		return now, ""
	}
}

// recordWatermarkMarkerLoss emits the #11089 marker-loss count for one
// deferral verdict whose prior wait verdict left no usable marker.
func (h Handler) recordWatermarkMarkerLoss(ctx context.Context, intent reducercontract.Intent, reason string) {
	if reason == "" || intent.Failure == nil {
		return
	}
	h.Instruments.RecordAdvisoryGroupWatermarkMarkerLoss(
		ctx,
		string(reducercontract.DomainAdvisoryGroupProjection),
		intent.Failure.FailureClass,
		reason,
	)
}
