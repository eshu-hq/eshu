// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testCoverageAccount = "111122223333"
	testCoverageRegion  = "us-east-1"
)

type stubObservabilityCoverageCorrelationFactLoader struct {
	scopeFacts []facts.Envelope
	kindCalls  [][]string
}

func (s *stubObservabilityCoverageCorrelationFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubObservabilityCoverageCorrelationFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

type recordingObservabilityCoverageCorrelationWriter struct {
	write ObservabilityCoverageCorrelationWrite
	calls int
}

func (w *recordingObservabilityCoverageCorrelationWriter) WriteObservabilityCoverageCorrelations(
	_ context.Context,
	write ObservabilityCoverageCorrelationWrite,
) (ObservabilityCoverageCorrelationWriteResult, error) {
	w.calls++
	w.write = write
	return ObservabilityCoverageCorrelationWriteResult{
		FactsWritten: len(write.Decisions),
	}, nil
}

// awsResourceFact builds an aws_resource fact envelope for one resource, the
// monitored-target substrate the coverage index resolves against.
func awsResourceFact(factID, resourceType, resourceID, arn, name string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactID:      factID,
		FactKind:    facts.AWSResourceFactKind,
		IsTombstone: tombstone,
		Payload: map[string]any{
			"account_id":          testCoverageAccount,
			"region":              testCoverageRegion,
			"resource_type":       resourceType,
			"resource_id":         resourceID,
			"arn":                 arn,
			"name":                name,
			"correlation_anchors": []string{arn, resourceID},
		},
	}
}

// alarmObservesMetricFact builds a cloudwatch_alarm_observes_metric relationship
// fact. The dimensions slice mirrors the scanner's dimensionSummary shape: a
// []any of {name, value} maps. System dimension values are unredacted resource
// ids; customer-tag dimensions arrive redacted.
func alarmObservesMetricFact(factID, alarmARN, metricID string, dimensions []map[string]any) facts.Envelope {
	dims := make([]any, 0, len(dimensions))
	for _, dim := range dimensions {
		dims = append(dims, dim)
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AWSRelationshipFactKind,
		Payload: map[string]any{
			"account_id":         testCoverageAccount,
			"region":             testCoverageRegion,
			"relationship_type":  "cloudwatch_alarm_observes_metric",
			"source_resource_id": alarmARN,
			"source_arn":         alarmARN,
			"target_resource_id": metricID,
			"target_type":        "aws_cloudwatch_metric",
			"attributes": map[string]any{
				"namespace":   "AWS/EC2",
				"metric_name": "CPUUtilization",
				"dimensions":  dims,
			},
		},
	}
}

// xraySamplingMatchesServiceFact builds an xray_sampling_rule_matches_service
// relationship fact carrying a service_name anchor (no CloudResource uid).
func xraySamplingMatchesServiceFact(factID, ruleARN, serviceName string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AWSRelationshipFactKind,
		Payload: map[string]any{
			"account_id":         testCoverageAccount,
			"region":             testCoverageRegion,
			"relationship_type":  "xray_sampling_rule_matches_service",
			"source_resource_id": ruleARN,
			"source_arn":         ruleARN,
			"target_resource_id": "xray-service:" + serviceName,
			"target_type":        "aws_xray_service_correlation",
			"attributes": map[string]any{
				"service_name": serviceName,
			},
		},
	}
}

func observabilityCoverageDecisions(
	decisions []ObservabilityCoverageCorrelationDecision,
) map[string]ObservabilityCoverageCorrelationDecision {
	out := make(map[string]ObservabilityCoverageCorrelationDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.CoverageSignal+"|"+decision.ObservabilityObjectRef+"|"+decision.TargetUID+decision.TargetServiceRef] = decision
	}
	return out
}

func assertCoverageOutcome(
	t *testing.T,
	decision ObservabilityCoverageCorrelationDecision,
	wantOutcome ObservabilityCoverageCorrelationOutcome,
	wantStatus string,
) {
	t.Helper()
	if decision.Outcome != wantOutcome {
		t.Fatalf("outcome = %q, want %q; reason=%s", decision.Outcome, wantOutcome, decision.Reason)
	}
	if decision.CoverageStatus != wantStatus {
		t.Fatalf("coverage_status = %q, want %q; reason=%s", decision.CoverageStatus, wantStatus, decision.Reason)
	}
}
