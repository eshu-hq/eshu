// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildObservabilityCoverageDecisionsExactAlarmCoverage proves an alarm
// whose system dimension (InstanceId) resolves to a scanned EC2 CloudResource is
// classified exact/covered, not provenance-only.
func TestBuildObservabilityCoverageDecisionsExactAlarmCoverage(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high"
	const instanceID = "i-0abc123"
	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "cpu-high", false),
		awsResourceFact("ec2-res", "aws_ec2_instance", instanceID, "arn:aws:ec2:us-east-1:111122223333:instance/"+instanceID, "web-1", false),
		alarmObservesMetricFact("alarm-rel", alarmARN, "AWS/EC2/CPUUtilization", []map[string]any{
			{"name": "InstanceId", "value": instanceID},
		}),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	byKey := observabilityCoverageDecisions(decisions)
	want := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_ec2_instance", instanceID)
	decision := byKey["alarm|"+alarmARN+"|"+want]
	assertCoverageOutcome(t, decision, ObservabilityCoverageExact, "covered")
	if decision.ProvenanceOnly {
		t.Fatal("exact coverage ProvenanceOnly = true, want false")
	}
	if decision.ResolutionMode != "bare_id" {
		t.Fatalf("resolution_mode = %q, want bare_id", decision.ResolutionMode)
	}
	if decision.TargetUID != want {
		t.Fatalf("target_uid = %q, want %q", decision.TargetUID, want)
	}
}

// TestBuildObservabilityCoverageDecisionsResolutionModeARN proves an alarm whose
// dimension value is the target's ARN records resolution_mode=arn, not a
// hardcoded bare_id, so the durable fact preserves which join path matched.
func TestBuildObservabilityCoverageDecisionsResolutionModeARN(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:by-arn"
	const instanceID = "i-0byarn"
	const instanceARN = "arn:aws:ec2:us-east-1:111122223333:instance/" + instanceID
	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "by-arn", false),
		awsResourceFact("ec2-res", "aws_ec2_instance", instanceID, instanceARN, "web-1", false),
		alarmObservesMetricFact("alarm-rel", alarmARN, "AWS/EC2/CPUUtilization", []map[string]any{
			{"name": "InstanceId", "value": instanceARN},
		}),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	want := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_ec2_instance", instanceID)
	decision := observabilityCoverageDecisions(decisions)["alarm|"+alarmARN+"|"+want]
	assertCoverageOutcome(t, decision, ObservabilityCoverageExact, "covered")
	if decision.ResolutionMode != "arn" {
		t.Fatalf("resolution_mode = %q, want arn", decision.ResolutionMode)
	}
}

// TestBuildObservabilityCoverageDecisionsResolutionModeCorrelationAnchor proves
// an alarm dimension that resolves only through a published correlation anchor
// records resolution_mode=correlation_anchor.
func TestBuildObservabilityCoverageDecisionsResolutionModeCorrelationAnchor(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:by-anchor"
	const instanceID = "i-0byanchor"
	const anchor = "logical://checkout/primary"
	target := awsResourceFact("ec2-res", "aws_ec2_instance", instanceID, "arn:ec2-anchor", "web-1", false)
	target.Payload["correlation_anchors"] = []string{anchor}
	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "by-anchor", false),
		target,
		alarmObservesMetricFact("alarm-rel", alarmARN, "AWS/EC2/CPUUtilization", []map[string]any{
			{"name": "InstanceId", "value": anchor},
		}),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	want := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_ec2_instance", instanceID)
	decision := observabilityCoverageDecisions(decisions)["alarm|"+alarmARN+"|"+want]
	assertCoverageOutcome(t, decision, ObservabilityCoverageExact, "covered")
	if decision.ResolutionMode != "correlation_anchor" {
		t.Fatalf("resolution_mode = %q, want correlation_anchor", decision.ResolutionMode)
	}
}

// TestBuildObservabilityCoverageDecisionsTombstonedObjectNeverCovers proves a
// tombstoned observability object (a deleted alarm) does not prove coverage for
// an otherwise-active target: tombstones must not overstate current coverage.
func TestBuildObservabilityCoverageDecisionsTombstonedObjectNeverCovers(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:deleted"
	const instanceID = "i-0live"
	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "deleted", true),
		awsResourceFact("ec2-res", "aws_ec2_instance", instanceID, "arn:ec2-live", "web-1", false),
		alarmObservesMetricFact("alarm-rel", alarmARN, "AWS/EC2/CPUUtilization", []map[string]any{
			{"name": "InstanceId", "value": instanceID},
		}),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	targetUID := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_ec2_instance", instanceID)
	for _, decision := range decisions {
		if decision.ObservabilityObjectRef == alarmARN {
			t.Fatalf("tombstoned alarm produced a coverage decision: %+v", decision)
		}
		if decision.TargetUID == targetUID && decision.Outcome == ObservabilityCoverageExact {
			t.Fatalf("tombstoned alarm classified target as exact coverage: %+v", decision)
		}
	}
}

// TestBuildObservabilityCoverageDecisionsGapForUncoveredResource proves a
// monitored resource class (here EC2) that has a covered peer but a second
// instance with no resolving alarm yields a gap finding keyed on the target.
func TestBuildObservabilityCoverageDecisionsGapForUncoveredResource(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high"
	const coveredID = "i-0covered"
	const uncoveredID = "i-0uncovered"
	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "cpu-high", false),
		awsResourceFact("ec2-covered", "aws_ec2_instance", coveredID, "arn:covered", "web-1", false),
		awsResourceFact("ec2-uncovered", "aws_ec2_instance", uncoveredID, "arn:uncovered", "web-2", false),
		alarmObservesMetricFact("alarm-rel", alarmARN, "AWS/EC2/CPUUtilization", []map[string]any{
			{"name": "InstanceId", "value": coveredID},
		}),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	uncoveredUID := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_ec2_instance", uncoveredID)
	var gap *ObservabilityCoverageCorrelationDecision
	for i := range decisions {
		if decisions[i].TargetUID == uncoveredUID && decisions[i].Outcome == ObservabilityCoverageUnresolved {
			gap = &decisions[i]
		}
	}
	if gap == nil {
		t.Fatalf("expected gap finding for uncovered EC2 instance; decisions=%+v", decisions)
	}
	if gap.CoverageStatus != "gap" {
		t.Fatalf("gap coverage_status = %q, want gap", gap.CoverageStatus)
	}
	if !gap.ProvenanceOnly {
		t.Fatal("gap ProvenanceOnly = false, want true")
	}
	if gap.ObservabilityObjectRef != "" {
		t.Fatalf("gap ObservabilityObjectRef = %q, want empty", gap.ObservabilityObjectRef)
	}
}

// TestBuildObservabilityCoverageDecisionsAmbiguousWhenDimensionNonUnique proves
// a dimension value matching two CloudResource uids stays ambiguous with both
// candidates recorded and no covered edge.
func TestBuildObservabilityCoverageDecisionsAmbiguousWhenDimensionNonUnique(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:shared"
	const sharedID = "shared-name"
	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "shared", false),
		awsResourceFact("rds-a", "aws_rds_db_instance", sharedID, "arn:rds-a", "db-a", false),
		awsResourceFact("ecache-b", "aws_elasticache_cluster", sharedID, "arn:ecache-b", "cache-b", false),
		alarmObservesMetricFact("alarm-rel", alarmARN, "AWS/RDS/CPU", []map[string]any{
			{"name": "DBInstanceIdentifier", "value": sharedID},
		}),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	var ambiguous *ObservabilityCoverageCorrelationDecision
	for i := range decisions {
		if decisions[i].ObservabilityObjectRef == alarmARN && decisions[i].Outcome == ObservabilityCoverageAmbiguous {
			ambiguous = &decisions[i]
		}
	}
	if ambiguous == nil {
		t.Fatalf("expected ambiguous decision; decisions=%+v", decisions)
	}
	assertCoverageOutcome(t, *ambiguous, ObservabilityCoverageAmbiguous, "ambiguous")
	if !ambiguous.ProvenanceOnly {
		t.Fatal("ambiguous ProvenanceOnly = false, want true")
	}
	if len(ambiguous.CandidateTargetUIDs) != 2 {
		t.Fatalf("candidate_target_uids = %v, want 2 candidates", ambiguous.CandidateTargetUIDs)
	}
	if ambiguous.TargetUID != "" {
		t.Fatalf("ambiguous TargetUID = %q, want empty (no single pick)", ambiguous.TargetUID)
	}
}

// TestBuildObservabilityCoverageDecisionsStaleWhenTargetTombstoned proves an
// alarm observing only a tombstoned resource is classified stale (drift signal).
func TestBuildObservabilityCoverageDecisionsStaleWhenTargetTombstoned(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:lingering"
	const instanceID = "i-0deleted"
	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "lingering", false),
		awsResourceFact("ec2-dead", "aws_ec2_instance", instanceID, "arn:dead", "gone", true),
		alarmObservesMetricFact("alarm-rel", alarmARN, "AWS/EC2/CPUUtilization", []map[string]any{
			{"name": "InstanceId", "value": instanceID},
		}),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	var stale *ObservabilityCoverageCorrelationDecision
	for i := range decisions {
		if decisions[i].ObservabilityObjectRef == alarmARN && decisions[i].Outcome == ObservabilityCoverageStale {
			stale = &decisions[i]
		}
	}
	if stale == nil {
		t.Fatalf("expected stale decision; decisions=%+v", decisions)
	}
	assertCoverageOutcome(t, *stale, ObservabilityCoverageStale, "stale")
	if !stale.ProvenanceOnly {
		t.Fatal("stale ProvenanceOnly = false, want true")
	}
}

// TestBuildObservabilityCoverageDecisionsRejectsMetricNameOnlyAlarm proves an
// alarm with no resolvable resource dimension (e.g. AWS/Billing) is rejected and
// suppressed, never promoted to covered truth.
func TestBuildObservabilityCoverageDecisionsRejectsMetricNameOnlyAlarm(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:billing"
	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "billing", false),
		alarmObservesMetricFact("alarm-rel", alarmARN, "AWS/Billing/EstimatedCharges", nil),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	var rejected *ObservabilityCoverageCorrelationDecision
	for i := range decisions {
		if decisions[i].ObservabilityObjectRef == alarmARN && decisions[i].Outcome == ObservabilityCoverageRejected {
			rejected = &decisions[i]
		}
	}
	if rejected == nil {
		t.Fatalf("expected rejected decision; decisions=%+v", decisions)
	}
	assertCoverageOutcome(t, *rejected, ObservabilityCoverageRejected, "rejected")
	if !rejected.ProvenanceOnly {
		t.Fatal("rejected ProvenanceOnly = false, want true")
	}
}

// TestBuildObservabilityCoverageDecisionsDerivedXRayService proves an X-Ray
// sampling rule that matches a service by name stays derived/provenance-only and
// keyed on the service ref (no CloudResource uid anchor exists yet).
func TestBuildObservabilityCoverageDecisionsDerivedXRayService(t *testing.T) {
	t.Parallel()

	const ruleARN = "arn:aws:xray:us-east-1:111122223333:sampling-rule/checkout"
	decisions, _, err := BuildObservabilityCoverageDecisions([]facts.Envelope{
		awsResourceFact("rule-res", "aws_xray_sampling_rule", ruleARN, ruleARN, "checkout", false),
		xraySamplingMatchesServiceFact("rule-rel", ruleARN, "checkout"),
	})
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}

	var derived *ObservabilityCoverageCorrelationDecision
	for i := range decisions {
		if decisions[i].CoverageSignal == "trace_sampling" && decisions[i].Outcome == ObservabilityCoverageDerived {
			derived = &decisions[i]
		}
	}
	if derived == nil {
		t.Fatalf("expected derived trace_sampling decision; decisions=%+v", decisions)
	}
	assertCoverageOutcome(t, *derived, ObservabilityCoverageDerived, "covered")
	if !derived.ProvenanceOnly {
		t.Fatal("derived X-Ray ProvenanceOnly = false, want true")
	}
	if derived.TargetServiceRef != "checkout" {
		t.Fatalf("target_service_ref = %q, want checkout", derived.TargetServiceRef)
	}
}

// TestBuildObservabilityCoverageDecisionsEmpty proves empty input yields no
// decisions and never panics.
func TestBuildObservabilityCoverageDecisionsEmpty(t *testing.T) {
	t.Parallel()

	if decisions, _, err := BuildObservabilityCoverageDecisions(nil); len(decisions) != 0 {
		if err != nil {
			t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
		}
		t.Fatalf("decisions = %v, want empty", decisions)
	}
}

// TestBuildObservabilityCoverageDecisionsDeterministicOrder proves repeated runs
// over the same facts produce a stable ordering for idempotent batch writes.
func TestBuildObservabilityCoverageDecisionsDeterministicOrder(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		awsResourceFact("alarm-res", "aws_cloudwatch_alarm", "arn:alarm", "arn:alarm", "a", false),
		awsResourceFact("ec2", "aws_ec2_instance", "i-1", "arn:ec2", "web", false),
		alarmObservesMetricFact("alarm-rel", "arn:alarm", "AWS/EC2/CPUUtilization", []map[string]any{
			{"name": "InstanceId", "value": "i-1"},
		}),
	}
	first, _, err := BuildObservabilityCoverageDecisions(envelopes)
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}
	second, _, err := BuildObservabilityCoverageDecisions(envelopes)
	if err != nil {
		t.Fatalf("BuildObservabilityCoverageDecisions() error = %v, want nil", err)
	}
	if len(first) != len(second) {
		t.Fatalf("decision counts differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ObservabilityObjectRef != second[i].ObservabilityObjectRef ||
			first[i].TargetUID != second[i].TargetUID ||
			first[i].Outcome != second[i].Outcome {
			t.Fatalf("non-deterministic ordering at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
}

// TestObservabilityCoverageCorrelationHandlerLoadsFactsAndWrites proves the
// handler loads the bounded fact kinds, classifies, and writes durable facts.
func TestObservabilityCoverageCorrelationHandlerLoadsFactsAndWrites(t *testing.T) {
	t.Parallel()

	const alarmARN = "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high"
	const instanceID = "i-0abc123"
	loader := &stubObservabilityCoverageCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			awsResourceFact("alarm-res", "aws_cloudwatch_alarm", alarmARN, alarmARN, "cpu-high", false),
			awsResourceFact("ec2-res", "aws_ec2_instance", instanceID, "arn:ec2", "web-1", false),
			alarmObservesMetricFact("alarm-rel", alarmARN, "AWS/EC2/CPUUtilization", []map[string]any{
				{"name": "InstanceId", "value": instanceID},
			}),
		},
	}
	writer := &recordingObservabilityCoverageCorrelationWriter{}
	handler := ObservabilityCoverageCorrelationHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-observability",
		ScopeID:      "aws://111122223333/us-east-1",
		GenerationID: "generation-observability",
		Domain:       DomainObservabilityCoverageCorrelation,
		SourceSystem: "aws_cloud",
		Cause:        "observability facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteObservabilityCoverageCorrelations() calls = %d, want 1", writer.calls)
	}
	if got, want := loader.kindCalls[0], observabilityCoverageCorrelationFactKinds(); !slices.Equal(got, want) {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if got, want := result.Domain, DomainObservabilityCoverageCorrelation; got != want {
		t.Fatalf("result.Domain = %q, want %q", got, want)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("result.CanonicalWrites = 0, want > 0")
	}
	if len(writer.write.Decisions) == 0 {
		t.Fatal("decisions = 0, want > 0")
	}
}

// TestObservabilityCoverageCorrelationHandlerRejectsWrongDomain proves the
// handler guards its domain, mirroring the #390/#805 dispatch guard.
func TestObservabilityCoverageCorrelationHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := ObservabilityCoverageCorrelationHandler{
		FactLoader: &stubObservabilityCoverageCorrelationFactLoader{},
		Writer:     &recordingObservabilityCoverageCorrelationWriter{},
	}
	if _, err := handler.Handle(context.Background(), Intent{Domain: DomainServiceCatalogCorrelation}); err == nil {
		t.Fatal("Handle() error = nil, want domain mismatch error")
	}
}

// TestPostgresObservabilityCoverageCorrelationWriterPersistsReducerFacts proves
// the writer stores one provenance-only reducer fact per decision through the
// shared canonical insert path with a stable, retry-idempotent identity.
func TestPostgresObservabilityCoverageCorrelationWriterPersistsReducerFacts(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresObservabilityCoverageCorrelationWriter{DB: db}

	write := ObservabilityCoverageCorrelationWrite{
		IntentID:     "intent-observability",
		ScopeID:      "aws://111122223333/us-east-1",
		GenerationID: "generation-observability",
		SourceSystem: "aws_cloud",
		Cause:        "observability facts observed",
		Decisions: []ObservabilityCoverageCorrelationDecision{
			{
				Provider:               "aws",
				CoverageSignal:         "alarm",
				ObservabilityObjectRef: "arn:alarm",
				TargetUID:              "uid-ec2",
				Outcome:                ObservabilityCoverageExact,
				Reason:                 "alarm dimension resolves to scanned resource",
				CoverageStatus:         "covered",
				ProvenanceOnly:         false,
				ResolutionMode:         "bare_id",
				EvidenceFactIDs:        []string{"alarm-rel", "ec2-res"},
			},
		},
	}
	result, err := writer.WriteObservabilityCoverageCorrelations(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteObservabilityCoverageCorrelations() error = %v, want nil", err)
	}
	if result.FactsWritten != 1 {
		t.Fatalf("FactsWritten = %d, want 1", result.FactsWritten)
	}
	if got, want := db.execs[0].args[3], observabilityCoverageCorrelationFactKind; got != want {
		t.Fatalf("fact_kind = %v, want %v", got, want)
	}
	payload := unmarshalObservabilityCoveragePayload(t, db.execs[0].args[14])
	if got, want := payload["coverage_signal"], "alarm"; got != want {
		t.Fatalf("coverage_signal = %#v, want %q", got, want)
	}
	if got, want := payload["outcome"], string(ObservabilityCoverageExact); got != want {
		t.Fatalf("outcome = %#v, want %q", got, want)
	}
	if got, want := payload["coverage_status"], "covered"; got != want {
		t.Fatalf("coverage_status = %#v, want %q", got, want)
	}

	// Idempotency: a second write of the same decision reuses the same fact_id.
	db2 := &fakeWorkloadIdentityExecer{}
	writer2 := PostgresObservabilityCoverageCorrelationWriter{DB: db2}
	if _, err := writer2.WriteObservabilityCoverageCorrelations(context.Background(), write); err != nil {
		t.Fatalf("second WriteObservabilityCoverageCorrelations() error = %v", err)
	}
	if db.execs[0].args[0] != db2.execs[0].args[0] {
		t.Fatalf("fact_id not stable across writes: %v vs %v", db.execs[0].args[0], db2.execs[0].args[0])
	}
}

func unmarshalObservabilityCoveragePayload(t *testing.T, raw any) map[string]any {
	t.Helper()
	bytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload): %v", err)
	}
	return payload
}
