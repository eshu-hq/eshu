// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type recordingEC2BlockDeviceKMSPostureNodeWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
}

func (w *recordingEC2BlockDeviceKMSPostureNodeWriter) WriteEC2BlockDeviceKMSPostureNodes(
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

func (w *recordingEC2BlockDeviceKMSPostureNodeWriter) RetractEC2BlockDeviceKMSPostureNodes(
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

func ec2BlockDeviceKMSPostureIntent() Intent {
	return Intent{
		IntentID:     "intent-ec2-block-device-kms-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2BlockDeviceKMSPostureMaterialization,
		EntityKeys:   []string{"ec2_block_device_kms_posture_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func ec2BlockDeviceKMSBothReady() map[string]bool {
	return map[string]bool{
		"aws_resource_materialization:scope-1":      true,
		"ec2_instance_node_materialization:scope-1": true,
	}
}

func ec2BlockDeviceKMSPostureFixture() []facts.Envelope {
	const account = "111122223333"
	const region = "us-east-1"
	const keyARN = "arn:aws:kms:us-east-1:111122223333:key/customer"
	volumeARN := "arn:aws:ec2:us-east-1:111122223333:volume/vol-a"
	return []facts.Envelope{
		ec2BlockKMSVolumeEnvelope(account, region, "vol-a", true, keyARN, attachedTo("i-aaa", "vol-a")),
		ec2BlockKMSKeyEnvelope(account, region, keyARN, "CUSTOMER"),
		ec2BlockKMSRelationship("vol-a", volumeARN, keyARN),
		ec2BlockKMSPostureEnvelope("fact-i-aaa", account, region, "i-aaa", "vol-a"),
	}
}

func TestEC2BlockDeviceKMSPostureMaterializationGatesUntilBothPhasesCommit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		readyKeys map[string]bool
		wantOpen  bool
	}{
		{name: "neither phase", readyKeys: map[string]bool{}, wantOpen: false},
		{name: "only aws resource phase", readyKeys: map[string]bool{
			"aws_resource_materialization:scope-1": true,
		}, wantOpen: false},
		{name: "only ec2 instance node phase", readyKeys: map[string]bool{
			"ec2_instance_node_materialization:scope-1": true,
		}, wantOpen: false},
		{name: "both phases", readyKeys: ec2BlockDeviceKMSBothReady(), wantOpen: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			writer := &recordingEC2BlockDeviceKMSPostureNodeWriter{}
			handler := EC2BlockDeviceKMSPostureMaterializationHandler{
				FactLoader:           &stubFactLoader{envelopes: ec2BlockDeviceKMSPostureFixture()},
				NodeWriter:           writer,
				ReadinessLookup:      ec2UsesProfileDualKeyLookup(tc.readyKeys),
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
			}

			_, err := handler.Handle(context.Background(), ec2BlockDeviceKMSPostureIntent())
			if tc.wantOpen {
				if err != nil {
					t.Fatalf("Handle() error = %v, want nil when both phases are ready", err)
				}
				if writer.writeCalls != 1 {
					t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
				}
				return
			}
			if err == nil {
				t.Fatal("expected retryable error while a readiness phase is missing")
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

func TestEC2BlockDeviceKMSPostureMaterializationProjectsNodeProperties(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2BlockDeviceKMSPostureNodeWriter{}
	handler := EC2BlockDeviceKMSPostureMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2BlockDeviceKMSPostureFixture()},
		NodeWriter:           writer,
		ReadinessLookup:      ec2UsesProfileDualKeyLookup(ec2BlockDeviceKMSBothReady()),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), ec2BlockDeviceKMSPostureIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 (prior generation exists)", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written rows = %d, want 1", len(writer.writtenRows))
	}
	if got, want := writer.writtenRows[0]["state"], "encrypted"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if writer.writeEvidence != ec2BlockDeviceKMSPostureEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, ec2BlockDeviceKMSPostureEvidenceSource)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestEC2BlockDeviceKMSPostureMaterializationRetractsStalePropertiesWhenGenerationHasNoRows(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2BlockDeviceKMSPostureNodeWriter{}
	handler := EC2BlockDeviceKMSPostureMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: nil},
		NodeWriter:           writer,
		ReadinessLookup:      ec2UsesProfileDualKeyLookup(ec2BlockDeviceKMSBothReady()),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), ec2BlockDeviceKMSPostureIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for empty generation", result.CanonicalWrites)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 to remove stale prior properties", writer.retractCalls)
	}
}
