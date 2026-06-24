// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type recordingEC2InternetExposureNodeWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
}

func (w *recordingEC2InternetExposureNodeWriter) WriteEC2InternetExposureNodes(
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

func (w *recordingEC2InternetExposureNodeWriter) RetractEC2InternetExposureNodes(
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

func ec2InternetExposureIntent() Intent {
	return Intent{
		IntentID:     "intent-ec2-internet-exposure-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainEC2InternetExposureMaterialization,
		EntityKeys:   []string{"ec2_instance_node_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func ec2InternetExposureFixture() []facts.Envelope {
	return []facts.Envelope{
		ec2ExposurePostureEnvelope("fact-posture-1", "i-123", map[string]any{"public_ip_associated": true}),
		ec2ExposureRelationshipEnvelope("fact-eni-instance", "ec2_network_interface_attached_to_resource", "eni-1", "i-123", "aws_ec2_instance"),
		ec2ExposureRelationshipEnvelope("fact-eni-sg", "ec2_network_interface_uses_security_group", "eni-1", "sg-1", "aws_ec2_security_group"),
		ec2ExposureSecurityGroupRuleEnvelope("fact-sg-rule", "sg-1", "ingress", true),
	}
}

func TestEC2InternetExposureMaterializationGatesOnEC2CanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2InternetExposureNodeWriter{}
	handler := EC2InternetExposureMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: ec2InternetExposureFixture()},
		NodeWriter:      writer,
		ReadinessLookup: readyLookup(false, false),
	}

	_, err := handler.Handle(context.Background(), ec2InternetExposureIntent())
	if err == nil {
		t.Fatal("expected a retryable error while EC2 canonical nodes phase is not ready")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable so the intent re-enters the queue, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before EC2 nodes commit: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

func TestEC2InternetExposureMaterializationProjectsNodeProperties(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2InternetExposureNodeWriter{}
	handler := EC2InternetExposureMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2InternetExposureFixture()},
		NodeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), ec2InternetExposureIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written rows = %d, want 1", len(writer.writtenRows))
	}
	row := writer.writtenRows[0]
	if got, want := row["state"], "exposed"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["source_fact_id"], "fact-posture-1"; got != want {
		t.Fatalf("source_fact_id = %v, want %v", got, want)
	}
	if writer.writeEvidence != ec2InternetExposureEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, ec2InternetExposureEvidenceSource)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestEC2InternetExposureMaterializationRetractsStalePropertiesWhenGenerationHasNoRows(t *testing.T) {
	t.Parallel()

	writer := &recordingEC2InternetExposureNodeWriter{}
	handler := EC2InternetExposureMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: nil},
		NodeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), ec2InternetExposureIntent())
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
