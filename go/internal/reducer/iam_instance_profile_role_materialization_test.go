// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type recordingIAMInstanceProfileRoleEdgeWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
}

func (w *recordingIAMInstanceProfileRoleEdgeWriter) WriteIAMInstanceProfileRoleEdges(
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

func (w *recordingIAMInstanceProfileRoleEdgeWriter) RetractIAMInstanceProfileRoleEdges(
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

func iamInstanceProfileRoleIntent() Intent {
	return Intent{
		IntentID:     "intent-profile-role-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainIAMInstanceProfileRoleMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func iamInstanceProfileRoleFixture() []facts.Envelope {
	const acct = "123456789012"
	roleARN := "arn:aws:iam::" + acct + ":role/app"
	return []facts.Envelope{
		iamInstanceProfileResourceEnvelope(acct, "app-profile", roleARN),
		iamRoleEnvelope(acct, roleARN),
	}
}

func TestIAMInstanceProfileRoleMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMInstanceProfileRoleEdgeWriter{}
	handler := IAMInstanceProfileRoleMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: iamInstanceProfileRoleFixture()},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false),
	}

	_, err := handler.Handle(context.Background(), iamInstanceProfileRoleIntent())
	if err == nil {
		t.Fatal("expected retryable error while canonical CloudResource nodes are not ready")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before nodes commit: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

func TestIAMInstanceProfileRoleMaterializationProjectsEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMInstanceProfileRoleEdgeWriter{}
	handler := IAMInstanceProfileRoleMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamInstanceProfileRoleFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), iamInstanceProfileRoleIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if writer.writeEvidence != iamInstanceProfileRoleEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, iamInstanceProfileRoleEvidenceSource)
	}
	if writer.writeScopeID != "scope-1" || writer.writeGenerationID != "gen-1" {
		t.Fatalf("write identity = (%q,%q), want (scope-1,gen-1)", writer.writeScopeID, writer.writeGenerationID)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 for prior generation", writer.retractCalls)
	}
}

func TestIAMInstanceProfileRoleMaterializationNoRolesRetractsStaleEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMInstanceProfileRoleEdgeWriter{}
	handler := IAMInstanceProfileRoleMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			iamInstanceProfileResourceEnvelope("123456789012", "app-profile"),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), iamInstanceProfileRoleIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for no-role profile", result.CanonicalWrites)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for no-role profile", writer.writeCalls)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 to clear stale prior-generation edges", writer.retractCalls)
	}
}

func TestImplementedDefaultDomainDefinitionsIncludesIAMInstanceProfileRoleWhenWired(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMInstanceProfileRoleEdgeWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		FactLoader:                       &stubFactLoader{},
		IAMInstanceProfileRoleEdgeWriter: writer,
		ReadinessLookup:                  readyLookup(true, true),
	})

	for _, def := range definitions {
		if def.Domain != DomainIAMInstanceProfileRoleMaterialization {
			continue
		}
		handler, ok := def.Handler.(IAMInstanceProfileRoleMaterializationHandler)
		if !ok {
			t.Fatalf("handler type = %T, want IAMInstanceProfileRoleMaterializationHandler", def.Handler)
		}
		if handler.FactLoader == nil || handler.EdgeWriter == nil || handler.ReadinessLookup == nil {
			t.Fatal("handler dependencies were not wired")
		}
		return
	}
	t.Fatal("iam_instance_profile_role_materialization not registered after wiring loader+edge writer")
}
