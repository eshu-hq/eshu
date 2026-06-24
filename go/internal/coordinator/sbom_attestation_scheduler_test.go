// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestSBOMAttestationWorkPlannerPlansOneWorkItemPerTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "sbom-attestation",
		CollectorKind:  scope.CollectorSBOMAttestation,
		Mode:           workflow.CollectorModeScheduled,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[{"scope_id":"sbom://configured/example","source_type":"configured_source","artifact_kind":"sbom","document_format":"cyclonedx","document_url":"https://sbom.example.com/sbom.json"}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := SBOMAttestationWorkPlanner{}.PlanSBOMAttestationWork(context.Background(), SBOMAttestationPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260515T120000Z",
	})
	if err != nil {
		t.Fatalf("PlanSBOMAttestationWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorSBOMAttestation); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.CollectorKind, scope.CollectorSBOMAttestation; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := item.ScopeID, "sbom://configured/example"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if !strings.HasPrefix(item.GenerationID, "sbom_attestation:") {
		t.Fatalf("GenerationID = %q, want sbom_attestation prefix", item.GenerationID)
	}
	if item.FairnessKey != "sbom_attestation:sbom-attestation:sbom" {
		t.Fatalf("FairnessKey = %q", item.FairnessKey)
	}
}

func TestSBOMAttestationWorkPlannerRejectsDuplicateScopes(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "sbom-attestation",
		CollectorKind:  scope.CollectorSBOMAttestation,
		Mode:           workflow.CollectorModeScheduled,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[{"scope_id":"sbom://configured/example","source_type":"configured_source","artifact_kind":"sbom","document_format":"cyclonedx","document_url":"https://sbom.example.com/one.json"},{"scope_id":"sbom://configured/example","source_type":"configured_source","artifact_kind":"sbom","document_format":"spdx","document_url":"https://sbom.example.com/two.json"}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, _, err := SBOMAttestationWorkPlanner{}.PlanSBOMAttestationWork(context.Background(), SBOMAttestationPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260515T120000Z",
	})
	if err == nil {
		t.Fatal("PlanSBOMAttestationWork() error = nil, want duplicate scope rejection")
	}
	if got, want := err.Error(), `duplicate SBOM attestation target scope_id "sbom://configured/example"`; !strings.Contains(got, want) {
		t.Fatalf("PlanSBOMAttestationWork() error = %q, want substring %q", got, want)
	}
}
