// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchdecaytelemetry

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/searchdecay"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

// Observer records search decay scoring decisions through telemetry instruments.
type Observer struct {
	instruments *telemetry.Instruments
}

// NewObserver returns a search decay observer backed by telemetry instruments.
func NewObserver(instruments *telemetry.Instruments) Observer {
	return Observer{instruments: instruments}
}

// ObserveDecay records one bounded decay scoring decision.
func (observer Observer) ObserveDecay(ctx context.Context, observation searchdecay.Observation) {
	if observer.instruments == nil || observer.instruments.SearchDecayPolicyApplications == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	observer.instruments.SearchDecayPolicyApplications.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrPolicyID(observation.PolicyID),
		telemetry.AttrEvidenceClass(string(observation.EvidenceClass)),
		telemetry.AttrOutcome(string(observation.Outcome)),
	))
}
