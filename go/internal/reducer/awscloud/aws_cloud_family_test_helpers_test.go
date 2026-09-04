// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// Local copies of the reducer-root test helpers this family's tests used
// before the move (issue #6061). Go test files cannot share unexported
// symbols across a package boundary, so each helper the moved tests still
// need is duplicated here verbatim rather than exported from the root for
// test-only use.

func awsResourceEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload:  payload,
	}
}

func awsRelationshipEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSRelationshipFactKind,
		Payload:  payload,
	}
}

// stubFactLoader returns a fixed set of envelopes regardless of the
// requested scope/generation/fact kinds, recording how many times it was
// called.
type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}

// readyLookup returns a gpphase.ReadinessLookup that reports the same
// (ready, found) pair for every key, so a test can force the readiness gate
// open or closed without a real Postgres-backed lookup.
func readyLookup(ready, found bool) gpphase.ReadinessLookup {
	return func(_ gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		return ready, found
	}
}

// metricHasAttrs reports whether the collected metrics contain an int64 sum
// data point for metricName carrying every attribute in attrs, with a
// positive value.
func metricHasAttrs(rm metricdata.ResourceMetrics, metricName string, attrs map[string]string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				matches := true
				for key, want := range attrs {
					got, ok := point.Attributes.Value(attribute.Key(key))
					if !ok || got.AsString() != want {
						matches = false
						break
					}
				}
				if matches && point.Value > 0 {
					return true
				}
			}
		}
	}
	return false
}
