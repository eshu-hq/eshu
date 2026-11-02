// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Causes a supply-chain impact pass can end partial (#7154). The set is closed
// because each value is a metric attribute. The first two mirror the truncation
// causes in the reducer; any other value folds into SupplyChainImpactPartialCauseOther.
const (
	// SupplyChainImpactPartialCauseRounds is the active-evidence expansion
	// round cap (the 8-round limit).
	SupplyChainImpactPartialCauseRounds = "active_expansion_rounds"
	// SupplyChainImpactPartialCauseBudget is the per-intent expansion evidence
	// budget (100,000 envelopes by default).
	SupplyChainImpactPartialCauseBudget = "evidence_budget"
	// SupplyChainImpactPartialCauseOther catches a cause this package does not
	// know, so a new reducer cause never adds an unbounded label value.
	SupplyChainImpactPartialCauseOther = "other"
)

// SupplyChainImpactHeadroomWindow is how far back the evidence budget ratio
// looks: the widest pass seen on this process in the last hour.
const SupplyChainImpactHeadroomWindow = time.Hour

const (
	headroomBucketWidth = time.Minute
	headroomBuckets     = int(SupplyChainImpactHeadroomWindow / headroomBucketWidth)
)

var supplyChainImpactPartialCauses = [...]string{
	SupplyChainImpactPartialCauseRounds,
	SupplyChainImpactPartialCauseBudget,
	SupplyChainImpactPartialCauseOther,
}

// SupplyChainImpactHeadroomTracker keeps the two process-local facts behind the
// #9778 evidence headroom gauges: which scopes' last recorded pass ended partial, and
// how close the widest recent pass came to the evidence budget. It
// adds no Postgres query, lock, or transaction to the reducer pass: it is a
// leaf mutex over a small map and a fixed array of 60 buckets.
//
// A scope appears in the map only while its last recorded pass here was
// partial, so memory is bounded by the number of stuck scopes, not by the
// number of scopes the reducer handles.
//
// What process-local costs, exactly (the same limits as SuccessorHoldTracker):
//
//   - a scope that replica A last saw partial and replica B later completed
//     stays partial on A until A next handles that scope (a phantom entry);
//   - a scope two replicas both saw partial is counted once on each, so summing
//     replicas can count it twice;
//   - a restart drops every entry until the scope is handled again;
//   - a scope partial for two causes appears under both, so summing causes
//     counts it twice;
//   - the tracker keeps whichever pass recorded last for a scope, not the pass
//     with the highest fencing token: two admitted passes of one scope can
//     record out of order, and the next pass of that scope corrects it.
//
// So the value is a per-replica view, not a global count and not a bound in
// either direction: phantom entries overstate it and restarts understate it.
// The WARN log "supply chain impact evidence truncated; findings not
// retracted" names the scope and is the per-scope source of truth.
type SupplyChainImpactHeadroomTracker struct {
	mu      sync.Mutex
	partial map[string][]string
	buckets [headroomBuckets]headroomBucket
}

// headroomBucket holds the widest pass ratio seen in one minute. minute is the
// Unix minute the bucket was last written for, zero when never written.
type headroomBucket struct {
	minute int64
	ratio  float64
}

// NewSupplyChainImpactHeadroomTracker returns an empty tracker.
func NewSupplyChainImpactHeadroomTracker() *SupplyChainImpactHeadroomTracker {
	return &SupplyChainImpactHeadroomTracker{partial: make(map[string][]string)}
}

// RecordPass records one admitted supply-chain impact pass. causes is the set
// of truncation causes that made it partial, empty for a complete pass, which
// clears the scope: the last recorded pass wins. chargedEnvelopes, the envelopes
// the pass charged to its evidence budget, over budget feeds the widest-pass
// ratio; a non-positive budget records no ratio. A nil tracker
// records nothing, and a blank scope updates only the ratio.
func (t *SupplyChainImpactHeadroomTracker) RecordPass(
	scopeID string,
	causes []string,
	chargedEnvelopes int,
	budget int,
	now time.Time,
) {
	if t == nil {
		return
	}
	scopeID = strings.TrimSpace(scopeID)
	t.mu.Lock()
	defer t.mu.Unlock()
	if scopeID != "" {
		if len(causes) == 0 {
			delete(t.partial, scopeID)
		} else {
			// Dedupe here, once per pass, so a scrape allocates nothing per
			// scope while holding the mutex.
			folded := make([]string, 0, len(causes))
			for _, cause := range causes {
				cause = foldPartialCause(cause)
				if !slices.Contains(folded, cause) {
					folded = append(folded, cause)
				}
			}
			t.partial[scopeID] = folded
		}
	}
	if budget > 0 {
		t.recordRatio(float64(chargedEnvelopes)/float64(budget), now)
	}
}

func (t *SupplyChainImpactHeadroomTracker) recordRatio(ratio float64, now time.Time) {
	minute := now.Unix() / int64(headroomBucketWidth/time.Second)
	bucket := &t.buckets[minute%int64(headroomBuckets)]
	switch {
	case bucket.minute > minute:
		// An out-of-order older sample for a slot a newer minute owns: drop it.
	case bucket.minute < minute:
		*bucket = headroomBucket{minute: minute, ratio: ratio}
	case ratio > bucket.ratio:
		bucket.ratio = ratio
	}
}

// PartialByCause returns, for every cause in the closed set, how many scopes'
// last recorded pass on this process ended partial for it. Every cause is present,
// zero included, so a dashboard reads "none stuck" as 0 rather than no data.
func (t *SupplyChainImpactHeadroomTracker) PartialByCause() map[string]int64 {
	counts := make(map[string]int64, len(supplyChainImpactPartialCauses))
	if t == nil {
		return counts
	}
	for _, cause := range supplyChainImpactPartialCauses {
		counts[cause] = 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, causes := range t.partial {
		for _, cause := range causes {
			counts[cause]++
		}
	}
	return counts
}

// WidestBudgetRatio returns the widest pass, as envelopes charged to the evidence budget over the
// budget, recorded in the SupplyChainImpactHeadroomWindow ending at now. The
// ratio passes 1 when a pass crossed the budget, because the load that crosses
// the limit is kept. ok is false when no pass fell in the window, so a quiet
// reducer reports no sample instead of a reassuring 0.
func (t *SupplyChainImpactHeadroomTracker) WidestBudgetRatio(now time.Time) (ratio float64, ok bool) {
	if t == nil {
		return 0, false
	}
	nowMinute := now.Unix() / int64(headroomBucketWidth/time.Second)
	oldest := nowMinute - int64(headroomBuckets) + 1
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, bucket := range &t.buckets {
		if bucket.minute == 0 || bucket.minute < oldest || bucket.minute > nowMinute {
			continue
		}
		if !ok || bucket.ratio > ratio {
			ratio = bucket.ratio
		}
		ok = true
	}
	return ratio, ok
}

func foldPartialCause(cause string) string {
	for _, known := range supplyChainImpactPartialCauses {
		if cause == known {
			return cause
		}
	}
	return SupplyChainImpactPartialCauseOther
}

// registerSupplyChainImpactHeadroom registers the #9778 gauges and the tracker
// that feeds them. The callbacks read process-local state only, so a scrape
// adds no query. SupplyChainImpactHeadroom stays nil-safe for stage tests.
func registerSupplyChainImpactHeadroom(meter metric.Meter, inst *Instruments) error {
	inst.SupplyChainImpactHeadroom = NewSupplyChainImpactHeadroomTracker()
	var err error
	inst.SupplyChainImpactPartialScopes, err = meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_partial_scopes",
		metric.WithDescription("Scopes whose last recorded supply-chain impact pass on this process ended partial (it upserted but retracted nothing), by cause "+
			"(active_expansion_rounds, evidence_budget, other) (#9778). A scope that stays partial keeps its stale findings. "+
			"A per-replica view, not a global count: a scope another replica later completed can linger here, a restart empties it, "+
			"a scope partial for two causes counts under both, and passes of one scope that record out of order show the older one until the next pass. "+
			"The WARN line names the exact scopes"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			for cause, count := range inst.SupplyChainImpactHeadroom.PartialByCause() {
				o.Observe(count, metric.WithAttributes(attribute.String("cause", cause)))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register SupplyChainImpactPartialScopes gauge: %w", err)
	}
	inst.SupplyChainImpactEvidenceBudgetRatio, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_evidence_budget_ratio",
		metric.WithDescription("Widest supply-chain impact pass on this process in the last hour, as the envelopes charged to the per-intent evidence budget over that budget (#9778). "+
			"1 means the budget is spent; above 1 a pass crossed it and went partial. No series until a pass has run in the window"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if ratio, ok := inst.SupplyChainImpactHeadroom.WidestBudgetRatio(time.Now()); ok {
				o.Observe(ratio)
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register SupplyChainImpactEvidenceBudgetRatio gauge: %w", err)
	}
	return nil
}
