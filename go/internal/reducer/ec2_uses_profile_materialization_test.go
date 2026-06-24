// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingEC2UsesProfileEdgeWriter captures USES_PROFILE edge writes and retracts
// so tests can assert the exact materialization request.
type recordingEC2UsesProfileEdgeWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
	writeErr          error
	retractErr        error
}

func (w *recordingEC2UsesProfileEdgeWriter) WriteEC2UsesProfileEdges(
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
	return w.writeErr
}

func (w *recordingEC2UsesProfileEdgeWriter) RetractEC2UsesProfileEdges(
	_ context.Context,
	scopeIDs []string,
	_ string,
	evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return w.retractErr
}

func ec2UsesProfileIntent() Intent {
	return Intent{
		IntentID:     "intent-ec2-uses-profile-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2UsesProfileMaterialization,
		EntityKeys:   []string{"ec2_uses_profile_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// ec2UsesProfileDualKeyLookup returns a readiness lookup that only reports the
// canonical-nodes-committed phase ready for the entity keys that are present in
// the readyKeys set. It lets a test prove the dual-key gate stays closed unless
// BOTH the aws_resource node phase AND the ec2_instance node phase are present.
func ec2UsesProfileDualKeyLookup(readyKeys map[string]bool) GraphProjectionReadinessLookup {
	return func(key GraphProjectionPhaseKey, phase GraphProjectionPhase) (bool, bool) {
		if phase != GraphProjectionPhaseCanonicalNodesCommitted {
			return false, false
		}
		if key.Keyspace != GraphProjectionKeyspaceCloudResourceUID {
			return false, false
		}
		ready, found := readyKeys[key.AcceptanceUnitID]
		return ready, found
	}
}

// ec2UsesProfileFixture is one EC2 instance using a scanned IAM instance profile,
// plus a second instance whose profile was NOT scanned (cross-account) and must be
// skipped, not written.
func ec2UsesProfileFixture() []facts.Envelope {
	const acct = "111122223333"
	const region = "us-east-1"
	const profileRegion = "aws-global"
	return []facts.Envelope{
		ec2UsesProfileResourceEnvelope(acct, profileRegion, "app"),
		ec2UsesProfilePostureEnvelope(acct, region, "i-aaa",
			"arn:aws:iam::"+acct+":instance-profile/app"),
		ec2UsesProfilePostureEnvelope(acct, region, "i-bbb",
			"arn:aws:iam::999988887777:instance-profile/external"), // target not scanned
	}
}

func ec2UsesProfileBothReady() map[string]bool {
	return map[string]bool{
		"aws_resource_materialization:scope-1":      true,
		"ec2_instance_node_materialization:scope-1": true,
	}
}

func TestEC2UsesProfileMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := EC2UsesProfileMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      &recordingEC2UsesProfileEdgeWriter{},
		ReadinessLookup: ec2UsesProfileDualKeyLookup(ec2UsesProfileBothReady()),
	}
	intent := ec2UsesProfileIntent()
	intent.Domain = DomainAWSRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestEC2UsesProfileMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := EC2UsesProfileMaterializationHandler{
		EdgeWriter:      &recordingEC2UsesProfileEdgeWriter{},
		ReadinessLookup: ec2UsesProfileDualKeyLookup(ec2UsesProfileBothReady()),
	}
	if _, err := handler.Handle(context.Background(), ec2UsesProfileIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestEC2UsesProfileMaterializationRequiresEdgeWriter(t *testing.T) {
	t.Parallel()

	handler := EC2UsesProfileMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: ec2UsesProfileDualKeyLookup(ec2UsesProfileBothReady()),
	}
	if _, err := handler.Handle(context.Background(), ec2UsesProfileIntent()); err == nil {
		t.Fatal("expected error when edge writer is nil")
	}
}

// TestEC2UsesProfileMaterializationGatesUntilBothPhasesCommit is the load-bearing
// dual-key proof: the handler must stay closed (retryable, no graph writes) when
// EITHER node phase is missing, and only open when BOTH are present.
func TestEC2UsesProfileMaterializationGatesUntilBothPhasesCommit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		readyKeys map[string]bool
		wantOpen  bool
	}{
		{
			name:      "neither phase",
			readyKeys: map[string]bool{},
			wantOpen:  false,
		},
		{
			name: "only aws_resource node phase",
			readyKeys: map[string]bool{
				"aws_resource_materialization:scope-1": true,
			},
			wantOpen: false,
		},
		{
			name: "only ec2 instance node phase",
			readyKeys: map[string]bool{
				"ec2_instance_node_materialization:scope-1": true,
			},
			wantOpen: false,
		},
		{
			name:      "both phases",
			readyKeys: ec2UsesProfileBothReady(),
			wantOpen:  true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			writer := &recordingEC2UsesProfileEdgeWriter{}
			handler := EC2UsesProfileMaterializationHandler{
				FactLoader:           &stubFactLoader{envelopes: ec2UsesProfileFixture()},
				EdgeWriter:           writer,
				ReadinessLookup:      ec2UsesProfileDualKeyLookup(tc.readyKeys),
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
			}

			_, err := handler.Handle(context.Background(), ec2UsesProfileIntent())
			if tc.wantOpen {
				if err != nil {
					t.Fatalf("expected the gate open when both phases committed, got %v", err)
				}
				if writer.writeCalls != 1 {
					t.Fatalf("writeCalls = %d, want 1 once both phases committed", writer.writeCalls)
				}
				return
			}
			if err == nil {
				t.Fatal("expected a retryable error while a node phase is missing")
			}
			if !IsRetryable(err) {
				t.Fatalf("error must be retryable so the intent re-enters the queue, got %v", err)
			}
			if writer.writeCalls != 0 || writer.retractCalls != 0 {
				t.Fatalf("no graph writes allowed before both phases commit: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
			}
		})
	}
}

func TestEC2UsesProfileMaterializationProjectsEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2UsesProfileEdgeWriter{}
	handler := EC2UsesProfileMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2UsesProfileFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      ec2UsesProfileDualKeyLookup(ec2UsesProfileBothReady()),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), ec2UsesProfileIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	// i-aaa -> app (scanned); i-bbb -> external (cross-account) skipped.
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written USES_PROFILE rows = %d, want 1 (external profile unscanned)", len(writer.writtenRows))
	}
	if writer.writeEvidence != ec2UsesProfileEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, ec2UsesProfileEvidenceSource)
	}
	if writer.writeScopeID != "scope-1" {
		t.Fatalf("write scope id = %q, want scope-1", writer.writeScopeID)
	}
	if writer.writeGenerationID != "gen-1" {
		t.Fatalf("write generation id = %q, want gen-1", writer.writeGenerationID)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 (prior generation exists)", writer.retractCalls)
	}
	if writer.retractEvidence != ec2UsesProfileEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, ec2UsesProfileEvidenceSource)
	}
}

func TestEC2UsesProfileMaterializationIdempotentOnReprojection(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2UsesProfileEdgeWriter{}
	handler := EC2UsesProfileMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2UsesProfileFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      ec2UsesProfileDualKeyLookup(ec2UsesProfileBothReady()),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	first, err := handler.Handle(context.Background(), ec2UsesProfileIntent())
	if err != nil {
		t.Fatalf("first Handle error: %v", err)
	}
	second, err := handler.Handle(context.Background(), ec2UsesProfileIntent())
	if err != nil {
		t.Fatalf("second Handle error: %v", err)
	}
	if first.CanonicalWrites != second.CanonicalWrites {
		t.Fatalf("reprojection changed write count: first=%d second=%d", first.CanonicalWrites, second.CanonicalWrites)
	}
	if writer.writeCalls != 2 {
		t.Fatalf("writeCalls = %d, want 2 (one per reprojection)", writer.writeCalls)
	}
}

func TestEC2UsesProfileMaterializationFirstGenerationSkipsRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2UsesProfileEdgeWriter{}
	handler := EC2UsesProfileMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2UsesProfileFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      ec2UsesProfileDualKeyLookup(ec2UsesProfileBothReady()),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), ec2UsesProfileIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 (no prior generation, first attempt)", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
}

func TestEC2UsesProfileMaterializationEmptyGenerationNoWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2UsesProfileEdgeWriter{}
	handler := EC2UsesProfileMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: nil},
		EdgeWriter:           writer,
		ReadinessLookup:      ec2UsesProfileDualKeyLookup(ec2UsesProfileBothReady()),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	result, err := handler.Handle(context.Background(), ec2UsesProfileIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for empty generation", result.CanonicalWrites)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
}
