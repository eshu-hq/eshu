// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
)

// osvDeltaWatermarkStaleReason is the warning reason the coordinator logs
// when a replan watermark's last successful poll is older than the
// provisional stale bound. It matches the wording the freshness signals use,
// so one alert query covers both.
const osvDeltaWatermarkStaleReason = "osv_delta_watermark_stale"

// osvDeltaReplanRejectedStaleReason is the warning reason the coordinator
// logs when queued rows the drain intersection dropped sit older than the
// replan stale bound. It matches the wording the freshness signals use,
// so one alert query covers both.
const osvDeltaReplanRejectedStaleReason = "osv_delta_rejected_rows_stale"

// osvDeltaReplanMalformedReason is the warning reason the coordinator logs
// when fresh changed_name facts arrive malformed (bad withdrawn spelling,
// unknown ecosystem, blank field, bad timestamp). It matches the wording
// the freshness signals use, so one alert query covers both.
const osvDeltaReplanMalformedReason = "osv_delta_malformed_changed_facts"

// reportOSVDeltaReplan records the pass on the replan signals and warns
// when the commit watermark's last successful poll is stale. Quiet passes
// stay silent in the logs; the gauges carry them.
func (s Service) reportOSVDeltaReplan(
	ctx context.Context,
	pass *osvDeltaPass,
	observation vulnerability.OSVDeltaReplanObservation,
	survivorIDs []int64,
	observedAt time.Time,
) {
	watermark, err := pass.store.ReadCommitWatermark(ctx, pass.instanceID)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn(
				"workflow coordinator owned target replan watermark unreadable",
				"collector_instance_id", pass.instanceID,
			)
		}
		return
	}
	// Rejected queued rows stay pending in the store: they hold depth and
	// set the oldest age exactly like the rows the drain carried, so a
	// poisoned row cannot sit at a healthy-looking depth of zero.
	depth := len(pass.pending) - len(survivorIDs) + len(pass.rejected)
	oldest := time.Time{}
	for _, change := range pass.pending {
		skip := false
		for _, id := range survivorIDs {
			if id == change.RowID {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if oldest.IsZero() || change.FeedTimestamp.Before(oldest) {
			oldest = change.FeedTimestamp
		}
	}
	oldestRejected := time.Time{}
	for _, rejected := range pass.rejected {
		if rejected.FeedTimestamp.IsZero() {
			continue
		}
		if oldest.IsZero() || rejected.FeedTimestamp.Before(oldest) {
			oldest = rejected.FeedTimestamp
		}
		if oldestRejected.IsZero() || rejected.FeedTimestamp.Before(oldestRejected) {
			oldestRejected = rejected.FeedTimestamp
		}
	}
	now := observedAt.UTC()
	record := OSVDeltaReplanMetricsObservation{
		CollectorInstanceID: pass.instanceID,
		Admitted:            pass.admitted,
		Duplicates:          pass.duplicates,
		ParseFailed:         pass.parseFailed,
		Drained:             len(survivorIDs),
		Overflow:            depth,
		PendingDepth:        depth,
	}
	if !oldest.IsZero() {
		record.OldestPendingAge, record.OldestPendingKnown = now.Sub(oldest), true
	}
	if watermark.CommittedSet {
		record.WatermarkAge, record.WatermarkKnown = now.Sub(watermark.Committed.UTC()), true
	}
	if watermark.LastPollSet {
		record.PollAge, record.PollKnown = now.Sub(watermark.LastPoll.UTC()), true
	}
	if s.Metrics != nil {
		s.Metrics.RecordOSVDeltaReplan(ctx, record)
	}
	if s.Logger == nil {
		return
	}
	if len(survivorIDs) > 0 {
		s.Logger.Info(
			"workflow coordinator committed owned target replan drain",
			"collector_instance_id", pass.instanceID,
			"drained_rows", len(survivorIDs),
			"overflow_rows", depth,
			"scopes_minted", observation.ScopesMinted,
			"watermark_set", observation.WatermarkSet,
		)
	}
	if vulnerability.OSVDeltaWatermarkStale(watermark.LastPoll, watermark.LastPollSet, now) {
		s.Logger.Warn(
			"workflow coordinator owned target replan watermark is stale",
			"collector_instance_id", pass.instanceID,
			"reason", osvDeltaWatermarkStaleReason,
			"last_poll_age_seconds", record.PollAge.Seconds(),
		)
	}
	// A malformed fresh fact never enters the queue, so it never ages:
	// log it every pass it appears, scoped to the instance, while the
	// fact stays malformed. The ParseFailed metric carries the count.
	if pass.freshParseFailed > 0 {
		s.Logger.Warn(
			"workflow coordinator owned target replan dropped malformed changed facts",
			"collector_instance_id", pass.instanceID,
			"reason", osvDeltaReplanMalformedReason,
			"malformed_changed_facts", pass.freshParseFailed,
		)
	}
	// A dropped queued row never drains on a later pass: its ecosystem and
	// fields are fixed at admission, so without this line it pins depth
	// silently. Alarm on the same stale horizon the watermark uses, so one
	// alert query covers both; an unparseable feed time alarms at once
	// because its age can never go below the bound.
	if len(pass.rejected) > 0 &&
		(oldestRejected.IsZero() || now.Sub(oldestRejected.UTC()) > vulnerability.OSVDeltaReplanStaleAfter) {
		rejectedAge := -1.0
		if !oldestRejected.IsZero() {
			rejectedAge = now.Sub(oldestRejected.UTC()).Seconds()
		}
		s.Logger.Warn(
			"workflow coordinator owned target replan dropped queued rows",
			"collector_instance_id", pass.instanceID,
			"reason", osvDeltaReplanRejectedStaleReason,
			"rejected_rows", len(pass.rejected),
			"oldest_rejected_age_seconds", rejectedAge,
		)
	}
}
