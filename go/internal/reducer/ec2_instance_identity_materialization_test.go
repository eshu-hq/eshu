// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type recordingEC2InstanceIdentityNodeWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
}

func (w *recordingEC2InstanceIdentityNodeWriter) WriteEC2InstanceIdentityNodes(
	_ context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	w.writeCalls++
	w.writtenRows = append(w.writtenRows, rows...)
	w.writeScopeID = scopeID
	w.writeGenerationID = generationID
	w.writeEvidence = evidenceSource
	return nil
}

func (w *recordingEC2InstanceIdentityNodeWriter) RetractEC2InstanceIdentityNodes(
	_ context.Context,
	scopeIDs []string,
	_ string,
	evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return nil
}

func ec2InstanceIdentityIntent() Intent {
	return Intent{
		IntentID:     "intent-ec2-identity-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InstanceIdentityMaterialization,
		// Mirrors buildEC2InstanceNodeMaterializationReducerIntent's own entity
		// key exactly: this domain's readiness gate resolves the phase the EC2
		// instance node domain published, not the generic aws_resource one.
		EntityKeys:  []string{"ec2_instance_node_materialization:scope-1"},
		EnqueuedAt:  time.Now(),
		AvailableAt: time.Now(),
	}
}

func ec2InstanceIdentityFixture() []facts.Envelope {
	return []facts.Envelope{
		ec2InstanceIdentityEnvelope(
			"i-0000000000000000a",
			"arn:aws:ec2:us-east-1:123456789012:instance/i-0000000000000000a",
			"ami-0000000000000000a",
		),
	}
}

func TestEC2InstanceIdentityMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := EC2InstanceIdentityMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		NodeWriter:      &recordingEC2InstanceIdentityNodeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	intent := ec2InstanceIdentityIntent()
	intent.Domain = DomainS3LogsToMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestEC2InstanceIdentityMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := EC2InstanceIdentityMaterializationHandler{
		NodeWriter:      &recordingEC2InstanceIdentityNodeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), ec2InstanceIdentityIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestEC2InstanceIdentityMaterializationRequiresNodeWriter(t *testing.T) {
	t.Parallel()

	handler := EC2InstanceIdentityMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), ec2InstanceIdentityIntent()); err == nil {
		t.Fatal("expected error when node writer is nil")
	}
}

func TestEC2InstanceIdentityMaterializationGatesOnEC2InstanceNodePhase(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2InstanceIdentityNodeWriter{}
	handler := EC2InstanceIdentityMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: ec2InstanceIdentityFixture()},
		NodeWriter:      writer,
		ReadinessLookup: readyLookup(false, false),
	}

	_, err := handler.Handle(context.Background(), ec2InstanceIdentityIntent())
	if err == nil {
		t.Fatal("expected a retryable error while the EC2 instance node phase is not ready")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before readiness: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

func TestEC2InstanceIdentityMaterializationProjectsAMIID(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2InstanceIdentityNodeWriter{}
	handler := EC2InstanceIdentityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2InstanceIdentityFixture()},
		NodeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), ec2InstanceIdentityIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written identity rows = %d, want 1", len(writer.writtenRows))
	}
	if got := writer.writtenRows[0]["ami_id"]; got != "ami-0000000000000000a" {
		t.Fatalf("ami_id = %v, want ami-0000000000000000a", got)
	}
	if writer.writeScopeID != "scope-1" || writer.writeGenerationID != "gen-1" {
		t.Fatalf("write scope/generation = %q/%q, want scope-1/gen-1", writer.writeScopeID, writer.writeGenerationID)
	}
	if writer.writeEvidence != ec2InstanceIdentityEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, ec2InstanceIdentityEvidenceSource)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 (prior generation exists)", writer.retractCalls)
	}
	if writer.retractEvidence != ec2InstanceIdentityEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, ec2InstanceIdentityEvidenceSource)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestEC2InstanceIdentityMaterializationFirstGenerationSkipsRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2InstanceIdentityNodeWriter{}
	handler := EC2InstanceIdentityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2InstanceIdentityFixture()},
		NodeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), ec2InstanceIdentityIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 for first generation", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
}

// TestEC2InstanceIdentityMaterializationDoesNotDisturbPostureNode is the
// #5448 CRUX-1 cross-domain proof at the handler level: running BOTH
// EC2InstanceNodeMaterializationHandler (posture, node-owning) and
// EC2InstanceIdentityMaterializationHandler (identity, augment-only) against
// facts for the SAME instance produces node writes to the SAME uid, and each
// handler's write call carries only its OWN domain's properties — proving
// neither handler's row payload ever names a property the other handler
// writes. Combined with
// TestEC2InstanceIdentityWriterDisjointFromEC2InstancePostureWriter (Cypher
// SET-clause disjointness, go/internal/storage/cypher package) and this
// package's cloudResourceNodeRow exclusion
// (TestExtractCloudResourceNodeRowsExcludesEC2Instance in
// aws_resource_materialization_test.go), this closes the loop from raw facts
// to the exact uid and exact property set each domain contributes.
func TestEC2InstanceIdentityMaterializationDoesNotDisturbPostureNode(t *testing.T) {
	t.Parallel()

	instanceID := "i-0000000000000000a"
	arn := "arn:aws:ec2:us-east-1:123456789012:instance/" + instanceID

	// Posture domain (node-owning): same instance, unrelated posture facts.
	postureWriter := &recordingEC2InstanceNodeWriter{}
	postureHandler := EC2InstanceNodeMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			ec2PostureEnvelopeForIdentityTest(instanceID, arn),
		}},
		NodeWriter: postureWriter,
	}
	postureIntent := Intent{
		IntentID:     "intent-ec2-posture-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InstanceNodeMaterialization,
		EntityKeys:   []string{"ec2_instance_node_materialization:scope-1"},
	}
	if _, err := postureHandler.Handle(context.Background(), postureIntent); err != nil {
		t.Fatalf("posture Handle returned error: %v", err)
	}

	// Identity domain (augment-only): the #5448 aws_resource identity fact for
	// the exact same instance.
	identityWriter := &recordingEC2InstanceIdentityNodeWriter{}
	identityHandler := EC2InstanceIdentityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2InstanceIdentityFixture()},
		NodeWriter:           identityWriter,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if _, err := identityHandler.Handle(context.Background(), ec2InstanceIdentityIntent()); err != nil {
		t.Fatalf("identity Handle returned error: %v", err)
	}

	if len(postureWriter.rows) != 1 || len(identityWriter.writtenRows) != 1 {
		t.Fatalf("expected one written row per domain, got posture=%d identity=%d",
			len(postureWriter.rows), len(identityWriter.writtenRows))
	}
	wantUID := cloudResourceUID(testEC2IdentityAccount, testEC2IdentityRegion, "aws_ec2_instance", instanceID)
	if got := postureWriter.rows[0]["uid"]; got != wantUID {
		t.Fatalf("posture row uid = %v, want %v (both domains must target the SAME node)", got, wantUID)
	}
	if got := identityWriter.writtenRows[0]["uid"]; got != wantUID {
		t.Fatalf("identity row uid = %v, want %v (both domains must target the SAME node)", got, wantUID)
	}

	// The identity domain's write NEVER carries a base/posture property name —
	// only its own disjoint contribution.
	for _, forbidden := range []string{"arn", "resource_id", "resource_type", "name", "state", "imds_v2_required", "instance_profile_arn"} {
		if _, exists := identityWriter.writtenRows[0][forbidden]; exists {
			t.Fatalf("identity write carried posture-owned property %q — dual-writer clobber risk", forbidden)
		}
	}
	// The posture domain's write NEVER carries the identity domain's property.
	if _, exists := postureWriter.rows[0]["ami_id"]; exists {
		t.Fatal("posture write carried the identity-owned ami_id property — dual-writer clobber risk")
	}

	// Retracting the identity domain's contribution (evidence_source-scoped)
	// never touches the posture domain's write call at all: the posture
	// handler performs no generation-scoped retract of its own (confirmed by
	// EC2InstanceNodeWriter carrying no Retract method), so there is nothing
	// for the identity retract to race against, and the identity retract's
	// own REMOVE clause is proven disjoint from the posture SET clause by
	// TestEC2InstanceIdentityWriterDisjointFromEC2InstancePostureWriter.
	if identityWriter.retractCalls != 0 {
		t.Fatalf("identity retractCalls = %d, want 0 for the first generation", identityWriter.retractCalls)
	}
}

// ec2PostureEnvelopeForIdentityTest builds a minimal ec2_instance_posture fact
// for the cross-domain proof above, using the SAME account/region/instance id
// as ec2InstanceIdentityEnvelope so both domains resolve to the identical
// cloud_resource_uid.
func ec2PostureEnvelopeForIdentityTest(instanceID, arn string) facts.Envelope {
	return facts.Envelope{
		FactID:   "fact-posture-" + instanceID,
		FactKind: facts.EC2InstancePostureFactKind,
		Payload: map[string]any{
			"account_id":       testEC2IdentityAccount,
			"region":           testEC2IdentityRegion,
			"instance_id":      instanceID,
			"arn":              arn,
			"state":            "running",
			"imds_v2_required": true,
		},
	}
}
