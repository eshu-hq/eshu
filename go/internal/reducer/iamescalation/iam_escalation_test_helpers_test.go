// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamescalation

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// Local copies of the reducer-root test helpers this family's tests used before
// the move (issue #6061). Go test files cannot share unexported symbols across a
// package boundary, so each helper the moved tests still need is duplicated here
// verbatim rather than exported from the root for test-only use.

func awsResourceEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload:  payload,
	}
}

// resourceEnvelope is a small helper for join-index tests. account+region are
// part of the uid identity (the cross-account/region trust boundary).
func resourceEnvelope(accountID, region, resourceType, resourceID, arn string, anchors ...string) facts.Envelope {
	anchorVals := make([]any, 0, len(anchors))
	for _, a := range anchors {
		anchorVals = append(anchorVals, a)
	}
	return awsResourceEnvelope(map[string]any{
		"account_id":          accountID,
		"region":              region,
		"resource_type":       resourceType,
		"resource_id":         resourceID,
		"arn":                 arn,
		"correlation_anchors": anchorVals,
	})
}

// stubFactLoader replays a fixed envelope batch and counts loads.
type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}

// allKeyspacesReady reports the requested phase ready for every keyspace, so a
// test can exercise the post-gate path.
func allKeyspacesReady() gpphase.ReadinessLookup {
	return func(_ gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		return true, true
	}
}

// readyExceptKeyspace reports every keyspace ready except the named one, which
// is reported not-found, so a test can prove the multi-keyspace gate blocks
// until ALL of them commit.
func readyExceptKeyspace(withheld gpphase.Keyspace) gpphase.ReadinessLookup {
	return func(key gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		if key.Keyspace == withheld {
			return false, false
		}
		return true, true
	}
}

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
