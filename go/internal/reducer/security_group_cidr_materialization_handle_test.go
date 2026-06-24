// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSecurityGroupCidrMaterializationHandleWritesNodes(t *testing.T) {
	t.Parallel()

	writer := &recordingSecurityGroupEndpointNodeWriter{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "0.0.0.0/0")),
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv6", "2001:db8::/32")),
		securityGroupRuleEnvelope(sgRulePayload("prefix_list", "pl-123")),
	}}

	handler := SecurityGroupCidrMaterializationHandler{
		FactLoader: loader,
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupCidrMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.cidrCalls != 1 {
		t.Fatalf("cidrCalls = %d, want 1", writer.cidrCalls)
	}
	if len(writer.cidrRows) != 2 {
		t.Fatalf("len(cidrRows) = %d, want 2", len(writer.cidrRows))
	}
	if writer.prefixCalls != 1 {
		t.Fatalf("prefixCalls = %d, want 1", writer.prefixCalls)
	}
	if len(writer.prefixRows) != 1 {
		t.Fatalf("len(prefixRows) = %d, want 1", len(writer.prefixRows))
	}
	if result.CanonicalWrites != 3 {
		t.Fatalf("CanonicalWrites = %d, want 3 (2 cidr + 1 prefix)", result.CanonicalWrites)
	}
	for _, source := range writer.evidenceSources {
		if source != securityGroupEndpointEvidenceSource {
			t.Fatalf("evidenceSource = %q, want %q", source, securityGroupEndpointEvidenceSource)
		}
	}
}

func TestSecurityGroupCidrMaterializationHandleNoFactsIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingSecurityGroupEndpointNodeWriter{}
	handler := SecurityGroupCidrMaterializationHandler{
		FactLoader: &stubFactLoader{},
		NodeWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupCidrMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.cidrCalls != 0 || writer.prefixCalls != 0 {
		t.Fatalf("cidrCalls=%d prefixCalls=%d, want 0/0 (no facts must not write)", writer.cidrCalls, writer.prefixCalls)
	}
}

func TestSecurityGroupCidrMaterializationHandleIsIdempotentAcrossReprojection(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{envelopes: []facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.1.2.3/8")),
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.4.5.6/8")),
	}}
	handler := SecurityGroupCidrMaterializationHandler{
		FactLoader: loader,
		NodeWriter: &recordingSecurityGroupEndpointNodeWriter{},
	}

	intent := Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupCidrMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}

	first, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("first Handle returned error: %v", err)
	}
	second, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("second Handle returned error: %v", err)
	}
	// Both rules collapse to the same 10.0.0.0/8 node; the deterministic uid means
	// re-projection produces an identical single-node batch (idempotent MERGE).
	if first.CanonicalWrites != 1 {
		t.Fatalf("first CanonicalWrites = %d, want 1 (both rules canonicalize to one node)", first.CanonicalWrites)
	}
	if second.CanonicalWrites != first.CanonicalWrites {
		t.Fatalf("re-projection CanonicalWrites = %d, want stable %d", second.CanonicalWrites, first.CanonicalWrites)
	}
}

func TestSecurityGroupCidrMaterializationHandlePublishesCanonicalNodesCommittedPhase(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.0.0.0/8")),
	}}
	handler := SecurityGroupCidrMaterializationHandler{
		FactLoader:     loader,
		NodeWriter:     &recordingSecurityGroupEndpointNodeWriter{},
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupCidrMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (the edge slice gates on this readiness phase)", len(publisher.calls))
	}
	rows := publisher.calls[0]
	if len(rows) != 1 {
		t.Fatalf("published rows = %d, want 1", len(rows))
	}
	if got, want := rows[0].Key.Keyspace, GraphProjectionKeyspaceSecurityGroupEndpointUID; got != want {
		t.Fatalf("keyspace = %q, want %q", got, want)
	}
	if got, want := rows[0].Phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("phase = %q, want %q", got, want)
	}
}

func TestSecurityGroupCidrMaterializationHandlePublishesPhaseOnEmptyGeneration(t *testing.T) {
	t.Parallel()

	// A generation with zero CIDR/prefix endpoints must still publish the phase,
	// otherwise the later edge slice blocks forever on the readiness gate.
	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingSecurityGroupEndpointNodeWriter{}
	handler := SecurityGroupCidrMaterializationHandler{
		FactLoader:     &stubFactLoader{},
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupCidrMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if writer.cidrCalls != 0 || writer.prefixCalls != 0 {
		t.Fatalf("cidrCalls=%d prefixCalls=%d, want 0/0", writer.cidrCalls, writer.prefixCalls)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("publisher.calls = %d, want 1 (empty generation must still unblock the edge slice)", len(publisher.calls))
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, GraphProjectionKeyspaceSecurityGroupEndpointUID; got != want {
		t.Fatalf("keyspace = %q, want %q", got, want)
	}
}

func TestSecurityGroupCidrMaterializationHandleDoesNotPublishPhaseOnWriteFailure(t *testing.T) {
	t.Parallel()

	// Publishing the readiness gate after a failed node write would let the edge
	// slice resolve edges against nodes that never committed.
	publisher := &recordingGraphProjectionPhasePublisher{}
	writer := &recordingSecurityGroupEndpointNodeWriter{err: errors.New("graph backend unavailable")}
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		securityGroupRuleEnvelope(sgRulePayload("cidr_ipv4", "10.0.0.0/8")),
	}}
	handler := SecurityGroupCidrMaterializationHandler{
		FactLoader:     loader,
		NodeWriter:     writer,
		PhasePublisher: publisher,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSecurityGroupCidrMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}); err == nil {
		t.Fatal("expected error when node write fails")
	}

	if len(publisher.calls) != 0 {
		t.Fatalf("publisher.calls = %d, want 0 (no readiness gate after a failed write)", len(publisher.calls))
	}
}
