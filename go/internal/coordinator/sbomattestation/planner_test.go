// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomattestation

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerPreservesSBOMAttestationPlanningContract(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
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

	run, items, err := (WorkPlanner{}).PlanSBOMAttestationWork(t.Context(), PlanRequest{
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
	if got, want := items[0].FairnessKey, "sbom_attestation:sbom-attestation:sbom"; got != want {
		t.Fatalf("FairnessKey = %q, want %q", got, want)
	}
}
