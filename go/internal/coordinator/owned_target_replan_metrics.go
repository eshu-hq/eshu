// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// OSV delta replan operator signals (#9777 step 2). Five step-6 signals,
// four registered in-tree and one satellite-owned:
//
//   - eshu_dp_osv_delta_records_total{outcome}: every tuple the pass
//     admitted, deduplicated, refused to parse, drained, or left waiting.
//   - eshu_dp_osv_delta_watermark_age_seconds: now minus the committed feed
//     timestamp, per collector instance. Absent until the first drain
//     commits.
//   - eshu_dp_osv_delta_poll_age_seconds: now minus the last successful
//     poll, per collector instance. The osv_delta_watermark_stale alarm
//     reads this age; absent until the first successful pass.
//   - eshu_dp_owned_target_replan_depth /
//     eshu_dp_owned_target_replan_oldest_age_seconds: the pending queue
//     depth and its oldest row age, per collector instance.
//   - Fetch bytes emit from the satellite delta poller at enablement, never
//     in-tree: this package records nothing for them. See the enablement
//     note in docs/internal/evidence/9777-osv-delta-replan-drain.md.
//
// Nothing emits in-tree while the per-instance osv_delta_replan flag is off:
// the instruments register at startup, and the record method returns early
// with no pass to report.
// OSVDeltaReplanMetricsObservation is what one replan pass reports for one
// collector instance: the closed outcome counts plus the queue and
// watermark ages. Ages unknown this pass read zero with their Known flag
// false, and the recorder drops those gauge series.
type OSVDeltaReplanMetricsObservation struct {
	CollectorInstanceID string
	Admitted            int
	Duplicates          int
	ParseFailed         int
	Drained             int
	Overflow            int
	PendingDepth        int
	OldestPendingAge    time.Duration
	OldestPendingKnown  bool
	WatermarkAge        time.Duration
	WatermarkKnown      bool
	PollAge             time.Duration
	PollKnown           bool
}

// osvDeltaReplanInstruments serves the #9777 step-2 replan signals. The last
// ages per collector instance sit behind a mutex because the reconcile loop
// writes them and the metrics reader's callback reads them from another
// goroutine; the map is bounded by the number of
// vulnerability-intelligence instances.
type osvDeltaReplanInstruments struct {
	recordsTotal   metric.Int64Counter
	watermarkAge   metric.Float64ObservableGauge
	pollAge        metric.Float64ObservableGauge
	replanDepth    metric.Float64ObservableGauge
	replanOldest   metric.Float64ObservableGauge
	mu             sync.Mutex
	watermarkBy    map[string]time.Duration
	pollBy         map[string]time.Duration
	depthBy        map[string]int
	oldestBy       map[string]time.Duration
	oldestKnown    map[string]bool
	watermarkKnown map[string]bool
	pollKnown      map[string]bool
}

// newOSVDeltaReplanInstruments registers the replan counter and the four
// gauges. The full literal names are deliberate:
// verify-telemetry-coverage.sh only recognizes a metric whose constructor's
// first argument is a literal.
func newOSVDeltaReplanInstruments(meter metric.Meter) (*osvDeltaReplanInstruments, error) {
	instruments := &osvDeltaReplanInstruments{
		watermarkBy:    make(map[string]time.Duration),
		pollBy:         make(map[string]time.Duration),
		depthBy:        make(map[string]int),
		oldestBy:       make(map[string]time.Duration),
		oldestKnown:    make(map[string]bool),
		watermarkKnown: make(map[string]bool),
		pollKnown:      make(map[string]bool),
	}
	// The full literal names are deliberate:
	// verify-telemetry-coverage.sh only recognizes a metric whose
	// constructor's first argument is a literal.
	records, err := meter.Int64Counter(
		"eshu_dp_osv_delta_records_total",
		metric.WithDescription("Owned-target replan queue records per pass, by collector instance and closed outcome (#9777 step 2)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register osv delta replan records counter: %w", err)
	}
	instruments.recordsTotal = records
	watermarkAge, err := meter.Float64ObservableGauge(
		"eshu_dp_osv_delta_watermark_age_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Age of the committed owned-target replan feed timestamp per collector instance (#9777 step 2)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register osv delta replan watermark age gauge: %w", err)
	}
	instruments.watermarkAge = watermarkAge
	pollAge, err := meter.Float64ObservableGauge(
		"eshu_dp_osv_delta_poll_age_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Age of the last successful owned-target replan poll per collector instance (#9777 step 2)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register osv delta replan poll age gauge: %w", err)
	}
	instruments.pollAge = pollAge
	depth, err := meter.Float64ObservableGauge(
		"eshu_dp_owned_target_replan_depth",
		metric.WithDescription("Pending owned-target replan queue depth per collector instance (#9777 step 2)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register owned target replan depth gauge: %w", err)
	}
	instruments.replanDepth = depth
	oldest, err := meter.Float64ObservableGauge(
		"eshu_dp_owned_target_replan_oldest_age_seconds",
		metric.WithUnit("s"),
		metric.WithDescription("Age of the oldest pending owned-target replan row per collector instance (#9777 step 2)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register owned target replan oldest age gauge: %w", err)
	}
	instruments.replanOldest = oldest
	if _, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		for instanceID, entry := range instruments.snapshot() {
			attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionCollectorInstanceID, instanceID))
			if entry.known.watermark {
				observer.ObserveFloat64(watermarkAge, entry.value.watermark.Seconds(), attrs)
			}
			if entry.known.poll {
				observer.ObserveFloat64(pollAge, entry.value.poll.Seconds(), attrs)
			}
			observer.ObserveFloat64(depth, float64(entry.value.depth), attrs)
			if entry.known.oldest {
				observer.ObserveFloat64(oldest, entry.value.oldest.Seconds(), attrs)
			}
		}
		return nil
	}, watermarkAge, pollAge, depth, oldest); err != nil {
		return nil, fmt.Errorf("register osv delta replan gauge callback: %w", err)
	}
	return instruments, nil
}

type osvDeltaReplanSnapshot struct {
	watermark, poll, oldest time.Duration
	depth                   int
}

type osvDeltaReplanKnown struct {
	watermark, poll, oldest bool
}

func (i *osvDeltaReplanInstruments) snapshot() map[string]struct {
	value osvDeltaReplanSnapshot
	known osvDeltaReplanKnown
} {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make(map[string]struct {
		value osvDeltaReplanSnapshot
		known osvDeltaReplanKnown
	}, len(i.depthBy))
	for instanceID := range i.depthBy {
		out[instanceID] = struct {
			value osvDeltaReplanSnapshot
			known osvDeltaReplanKnown
		}{
			value: osvDeltaReplanSnapshot{
				watermark: i.watermarkBy[instanceID],
				poll:      i.pollBy[instanceID],
				oldest:    i.oldestBy[instanceID],
				depth:     i.depthBy[instanceID],
			},
			known: osvDeltaReplanKnown{
				watermark: i.watermarkKnown[instanceID],
				poll:      i.pollKnown[instanceID],
				oldest:    i.oldestKnown[instanceID],
			},
		}
	}
	return out
}

// RecordOSVDeltaReplan counts the pass's closed outcomes and stores the
// instance's queue and watermark ages for the gauges. Outcomes are the
// closed vulnerability.OSVDeltaOutcome* set, so the label stays
// low-cardinality. A nil recorder or nil instruments is a no-op.
func (m *otelMetrics) RecordOSVDeltaReplan(ctx context.Context, observation OSVDeltaReplanMetricsObservation) {
	if m == nil || m.osvDeltaReplan == nil {
		return
	}
	outcomes := map[string]int{
		vulnerability.OSVDeltaOutcomeAdmitted:         observation.Admitted,
		vulnerability.OSVDeltaOutcomeDuplicate:        observation.Duplicates,
		vulnerability.OSVDeltaOutcomeParseFailed:      observation.ParseFailed,
		vulnerability.OSVDeltaOutcomeDrained:          observation.Drained,
		vulnerability.OSVDeltaOutcomeOverflowRetained: observation.Overflow,
	}
	for outcome, count := range outcomes {
		if count <= 0 {
			continue
		}
		m.osvDeltaReplan.recordsTotal.Add(ctx, int64(count), metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionCollectorInstanceID, observation.CollectorInstanceID),
			attribute.String(telemetry.MetricDimensionOutcome, outcome),
		))
	}
	m.osvDeltaReplan.mu.Lock()
	defer m.osvDeltaReplan.mu.Unlock()
	m.osvDeltaReplan.depthBy[observation.CollectorInstanceID] = observation.PendingDepth
	m.osvDeltaReplan.oldestBy[observation.CollectorInstanceID] = observation.OldestPendingAge
	m.osvDeltaReplan.oldestKnown[observation.CollectorInstanceID] = observation.OldestPendingKnown
	m.osvDeltaReplan.watermarkBy[observation.CollectorInstanceID] = observation.WatermarkAge
	m.osvDeltaReplan.watermarkKnown[observation.CollectorInstanceID] = observation.WatermarkKnown
	m.osvDeltaReplan.pollBy[observation.CollectorInstanceID] = observation.PollAge
	m.osvDeltaReplan.pollKnown[observation.CollectorInstanceID] = observation.PollKnown
}
