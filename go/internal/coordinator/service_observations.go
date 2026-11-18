// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// nvdFeedTransportInvalidReason is the closed reason the coordinator logs when
// an instance's nvd_feed_transport value is neither on nor off
// (eshu-hq/eshu#10716). It is a log value only, never a metric label.
const nvdFeedTransportInvalidReason = "invalid_value"

func (s Service) recordReconcile(ctx context.Context, observation ReconcileObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordReconcile(ctx, observation)
}

func (s Service) recordReap(ctx context.Context, observation ReapObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordReap(ctx, observation)
}

func (s Service) recordStaleRunExpiry(ctx context.Context, observation StaleRunExpiryObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordStaleRunExpiry(ctx, observation)
}

func (s Service) recordRunReconciliation(ctx context.Context, observation RunReconciliationObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordRunReconciliation(ctx, observation)
}

func (s Service) recordAWSFreshnessReap(ctx context.Context, observation FreshnessReapObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordAWSFreshnessReap(ctx, observation)
}

func (s Service) recordGCPFreshnessReap(ctx context.Context, observation FreshnessReapObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordGCPFreshnessReap(ctx, observation)
}

func (s Service) recordIncidentFreshnessReap(ctx context.Context, observation FreshnessReapObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordIncidentFreshnessReap(ctx, observation)
}

func (s Service) recordScannerDerivedPage(ctx context.Context, observation ScannerDerivedPageObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordScannerDerivedPage(ctx, observation)
}

func (s Service) recordEPSSDerived(ctx context.Context, observation vulnerability.IntelligenceDerivationObservation) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordEPSSDerivedPage(ctx, EPSSDerivedObservation{
		PlannedTargets: observation.Targets,
		Skipped:        observation.Skipped,
	})
}

// recordOwnedDerivedPage records the owned-package derivation of one plan
// (eshu-hq/eshu#9287 criterion 4): the read's selected targets per ecosystem
// plus the owned-package skip evidence of the same plan. Counts only, never
// package names.
func (s Service) recordOwnedDerivedPage(
	ctx context.Context,
	instance workflow.CollectorInstance,
	page workflow.OwnedPackageDependencyTargetPage,
	observation vulnerability.IntelligenceDerivationObservation,
) {
	if s.Metrics == nil {
		return
	}
	ecosystems, selected := page.SelectedByEcosystem()
	counts := make(map[string]int, len(ecosystems))
	for i, ecosystem := range ecosystems {
		counts[ecosystem] = selected[i]
	}
	s.Metrics.RecordOwnedDerivedPage(ctx, OwnedDerivedObservation{
		CollectorInstanceID: instance.InstanceID,
		Selected:            counts,
		Skipped:             observation.Skipped,
	})
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}

// vulnerabilityNVDWindowCursor is the one rolling-cursor read for the NVD
// window derivation, and it reports the row it read (#10716). A disabled
// derivation reads nothing; one with no reader fails loudly, never from zero.
// Both planners (the in-tree instance and the component activation) pass
// through it for every NVD derivation, so the invalid-setting warning rides
// here too.
func (s Service) vulnerabilityNVDWindowCursor(
	ctx context.Context,
	derivation vulnerability.NVDWindowDerivation,
	instanceID string,
	observedAt time.Time,
) (vulnerability.NVDWindowCursor, error) {
	s.reportNVDFeedTransportSetting(ctx, instanceID, derivation)
	if !derivation.Enabled {
		return vulnerability.NVDWindowCursor{}, nil
	}
	if s.NVDWindowCursorReader == nil {
		return vulnerability.NVDWindowCursor{}, fmt.Errorf("nvd window cursor reader is required for rolling NVD windows")
	}
	cursor, err := s.NVDWindowCursorReader.ReadNVDWindowCursor(ctx, instanceID)
	if err != nil {
		return vulnerability.NVDWindowCursor{}, fmt.Errorf("read nvd window cursor: %w", err)
	}
	s.reportNVDWindowCursor(ctx, instanceID, cursor, observedAt)
	return cursor, nil
}

// reportNVDFeedTransportSetting warns that an instance's nvd_feed_transport
// value is neither on nor off (eshu-hq/eshu#10716). It changes nothing the
// coordinator plans: the kill switch is decided per claim by the extension
// host, which reads the same bad value as off, so planning continues and the
// feed transport simply stays off until the operator fixes the value. The
// coordinator used to fail the tick here, which stopped a healthy API-only
// stream (and, at startup, the coordinator itself) over a typo in a setting
// that can only add the feed.
//
// The line repeats every tick while the value stays wrong, like the host's
// per-claim line, so a stale typo does not scroll out of sight. It carries the
// instance id and the closed reason only, never the configured value: an
// operator can put anything in that field.
func (s Service) reportNVDFeedTransportSetting(
	ctx context.Context,
	instanceID string,
	derivation vulnerability.NVDWindowDerivation,
) {
	if !derivation.FeedTransportInvalid || s.Logger == nil {
		return
	}
	s.Logger.WarnContext(ctx, "workflow coordinator read an invalid nvd_feed_transport value and kept the feed transport off",
		"collector_instance_id", instanceID,
		workflow.NVDFeedTransportKey, string(workflow.NVDFeedTransportOff),
		"reason", nvdFeedTransportInvalidReason,
	)
}

// reportNVDWindowCursor logs the coordinator's view of one instance's
// rolling NVD cursor after the plan's read (eshu-hq/eshu#10716). It is the
// consumer of the widened cursor reader: cursor_lag_seconds is the stored
// head minus the cursor, cursor_updated_age_seconds is how long ago the
// cursor row last moved. Both operands are server-stamped in the commit
// transaction, so the lag does not depend on a collector process being
// alive to emit a gauge: a dead collector shows as a growing row age here.
// A stream that never committed logs only that it is unset. A head of zero
// means the satellite stamped none, so no lag is invented, and a zero update
// time omits the age. Both figures floor at zero. Counts and timestamps
// only, never a scope id.
func (s Service) reportNVDWindowCursor(
	ctx context.Context,
	instanceID string,
	cursor vulnerability.NVDWindowCursor,
	observedAt time.Time,
) {
	if s.Logger == nil {
		return
	}
	attrs := []any{"collector_instance_id", instanceID, "cursor_set", cursor.Set}
	if cursor.Set {
		attrs = append(attrs, "cursor_unix_micro", cursor.CursorUnixMicro, "head_unix_micro", cursor.HeadUnixMicro)
		if cursor.HeadUnixMicro > 0 {
			attrs = append(attrs, "cursor_lag_seconds", flooredSeconds(float64(cursor.HeadUnixMicro-cursor.CursorUnixMicro)/1e6))
		}
		if !cursor.UpdatedAt.IsZero() {
			attrs = append(attrs, "cursor_updated_age_seconds", flooredSeconds(observedAt.Sub(cursor.UpdatedAt).Seconds()))
		}
	}
	s.Logger.InfoContext(ctx, "workflow coordinator read NVD window cursor", attrs...)
}

// flooredSeconds clamps a lag or age to zero: a stored head behind the
// cursor, or an update time ahead of the coordinator clock, reads as zero
// rather than a negative figure.
func flooredSeconds(seconds float64) float64 {
	if seconds < 0 {
		return 0
	}
	return seconds
}
