// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordingIAMEscalationWriter captures the edge and retract calls so tests assert
// the exact materialization request the handler issues.
type recordingIAMEscalationWriter struct {
	edgeCalls       int
	edgeRows        []map[string]any
	scopeID         string
	generationID    string
	evidenceSource  string
	retractCalls    int
	retractScopeIDs []string
	retractEvidence string
	edgeErr         error
}

func (w *recordingIAMEscalationWriter) WriteIAMEscalationEdges(
	_ context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string,
) error {
	w.edgeCalls++
	w.edgeRows = append(w.edgeRows, rows...)
	w.scopeID = scopeID
	w.generationID = generationID
	w.evidenceSource = evidenceSource
	return w.edgeErr
}

func (w *recordingIAMEscalationWriter) RetractIAMEscalationEdges(
	_ context.Context, scopeIDs []string, _, evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return nil
}

func iamEscalationIntent() Intent {
	return Intent{
		IntentID:     "intent-iam-esc-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainIAMEscalationMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// iamEscalationFacts builds a scope with one scanned principal, one scanned policy
// target, and one complete iam:CreatePolicyVersion grant — exactly one edge.
func iamEscalationFacts() []facts.Envelope {
	return []facts.Envelope{
		iamNodeEnvelope(iamResourceTypeUser, attackerUserARN),
		iamNodeEnvelope(iamResourceTypePolicy, targetPolicyARN),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:createpolicyversion"}, []string{targetPolicyARN}),
	}
}

func TestIAMEscalationHandlerRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := IAMEscalationMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		Writer:          &recordingIAMEscalationWriter{},
		ReadinessLookup: allKeyspacesReady(),
	}
	intent := iamEscalationIntent()
	intent.Domain = DomainSQLRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestIAMEscalationHandlerRequiresFactLoaderAndWriter(t *testing.T) {
	t.Parallel()

	if _, err := (IAMEscalationMaterializationHandler{
		Writer:          &recordingIAMEscalationWriter{},
		ReadinessLookup: allKeyspacesReady(),
	}).Handle(context.Background(), iamEscalationIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
	if _, err := (IAMEscalationMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: allKeyspacesReady(),
	}).Handle(context.Background(), iamEscalationIntent()); err == nil {
		t.Fatal("expected error when writer is nil")
	}
}

// TestIAMEscalationHandlerGatesOnCloudResourceUID proves the edge domain blocks
// (with a retryable error and zero writes) until the cloud_resource_uid
// canonical-nodes phase is committed.
func TestIAMEscalationHandlerGatesOnCloudResourceUID(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMEscalationWriter{}
	handler := IAMEscalationMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: iamEscalationFacts()},
		Writer:          writer,
		ReadinessLookup: readyExceptKeyspace(GraphProjectionKeyspaceCloudResourceUID),
	}
	_, err := handler.Handle(context.Background(), iamEscalationIntent())
	if err == nil {
		t.Fatal("expected a not-ready error while cloud_resource_uid is uncommitted")
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("not-ready error must be retryable, got %v", err)
	}
	if writer.edgeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no writes allowed before the gate opens: %+v", writer)
	}
}

// TestIAMEscalationHandlerProjectsResolvedEdge proves the post-gate happy path
// writes exactly one CAN_ESCALATE_TO edge with the right scope/evidence and count.
func TestIAMEscalationHandlerProjectsResolvedEdge(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMEscalationWriter{}
	handler := IAMEscalationMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamEscalationFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	result, err := handler.Handle(context.Background(), iamEscalationIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.edgeCalls != 1 || len(writer.edgeRows) != 1 {
		t.Fatalf("escalation edge writes wrong: calls=%d rows=%d", writer.edgeCalls, len(writer.edgeRows))
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.evidenceSource != iamEscalationEvidenceSource {
		t.Fatalf("edge evidence = %q, want %q", writer.evidenceSource, iamEscalationEvidenceSource)
	}
	got := writer.edgeRows[0]["primitives"].([]string)
	if len(got) != 1 || got[0] != "iam_create_policy_version" {
		t.Fatalf("edge primitives = %v, want [iam_create_policy_version]", got)
	}
}

// TestIAMEscalationHandlerQuarantinesMalformedFact proves the iam_escalation
// handler records a per-fact input_invalid quarantine (the metric + structured
// log via recordQuarantinedFacts, surfaced on Result.SubSignals) rather than
// silently skipping it, while the batch's valid escalation edge still projects.
// iam_escalation was the one migrated domain that collected result.Quarantined
// but never recorded it — a silent skip the redesign forbids.
func TestIAMEscalationHandlerQuarantinesMalformedFact(t *testing.T) {
	t.Parallel()

	// A malformed aws_resource fact (account_id absent) shares the batch with the
	// complete, valid escalation fixture. The malformed fact is quarantined; the
	// valid principal/policy/grant still resolves exactly one edge.
	malformed := awsResourceEnvelope(map[string]any{
		"region":        iamEscRegion,
		"resource_type": iamResourceTypeUser,
		"resource_id":   "arn:aws:iam::111122223333:user/poison",
	})
	envelopes := append([]facts.Envelope{malformed}, iamEscalationFacts()...)

	writer := &recordingIAMEscalationWriter{}
	handler := IAMEscalationMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: envelopes},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), iamEscalationIntent())
	if err != nil {
		t.Fatalf("Handle returned error %v; a malformed fact must be quarantined per-fact, not fail the intent", err)
	}

	// The malformed fact is recorded as one input_invalid quarantine on the
	// per-intent signal (also on the counter + structured error log).
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the malformed fact must be recorded, not silently skipped", got)
	}

	// The batch's valid escalation edge must still materialize.
	if writer.edgeCalls != 1 || len(writer.edgeRows) != 1 {
		t.Fatalf("escalation edge writes wrong: calls=%d rows=%d, want 1 and 1 (valid edge projects despite the quarantined fact)", writer.edgeCalls, len(writer.edgeRows))
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

// TestIAMEscalationHandlerIdempotentReprojection proves re-running the same
// generation issues the same edge and retracts the prior generation first.
func TestIAMEscalationHandlerIdempotentReprojection(t *testing.T) {
	t.Parallel()

	newHandler := func(writer *recordingIAMEscalationWriter) IAMEscalationMaterializationHandler {
		return IAMEscalationMaterializationHandler{
			FactLoader:           &stubFactLoader{envelopes: iamEscalationFacts()},
			Writer:               writer,
			ReadinessLookup:      allKeyspacesReady(),
			PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
		}
	}

	first := &recordingIAMEscalationWriter{}
	if _, err := newHandler(first).Handle(context.Background(), iamEscalationIntent()); err != nil {
		t.Fatalf("first projection error: %v", err)
	}
	second := &recordingIAMEscalationWriter{}
	if _, err := newHandler(second).Handle(context.Background(), iamEscalationIntent()); err != nil {
		t.Fatalf("second projection error: %v", err)
	}
	if first.edgeRows[0]["target_uid"] != second.edgeRows[0]["target_uid"] {
		t.Fatal("reprojection must produce the same edge identity (idempotent MERGE)")
	}
	if second.retractCalls != 1 {
		t.Fatalf("reprojection with a prior generation must retract first, got %d", second.retractCalls)
	}
	if second.retractEvidence != iamEscalationEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", second.retractEvidence, iamEscalationEvidenceSource)
	}
}

// TestIAMEscalationHandlerSkipsFirstGenerationRetract proves the first generation
// does not retract (no prior edges) but still writes the edge.
func TestIAMEscalationHandlerSkipsFirstGenerationRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMEscalationWriter{}
	handler := IAMEscalationMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamEscalationFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if _, err := handler.Handle(context.Background(), iamEscalationIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("first generation must not retract, got %d", writer.retractCalls)
	}
	if writer.edgeCalls != 1 {
		t.Fatalf("first generation must still write the edge, got %d", writer.edgeCalls)
	}
}

// TestIAMEscalationHandlerWildcardTargetIsGracefulNoEdge proves a wildcard-resource
// grant produces no edge but still succeeds (graceful degradation), and the
// skipped_ambiguous counter records it.
func TestIAMEscalationHandlerWildcardTargetIsGracefulNoEdge(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	envs := []facts.Envelope{
		iamNodeEnvelope(iamResourceTypeUser, attackerUserARN),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"iam:createpolicyversion"}, []string{"*"}),
	}
	writer := &recordingIAMEscalationWriter{}
	handler := IAMEscalationMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: envs},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
		Instruments:          instruments,
	}
	result, err := handler.Handle(context.Background(), iamEscalationIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.edgeCalls != 0 {
		t.Fatalf("wildcard target must not write an edge, got %d", writer.edgeCalls)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if !metricHasAttrs(rm, "eshu_dp_iam_escalation_skipped_total", map[string]string{"skip_reason": iamEscalationSkipAmbiguous}) {
		t.Fatal("expected eshu_dp_iam_escalation_skipped_total{skip_reason=skipped_ambiguous}")
	}
}
