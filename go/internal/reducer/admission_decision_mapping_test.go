// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/admissiondecision"
	reducercloudinventory "github.com/eshu-hq/eshu/go/internal/reducer/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// stubCloudInventoryEvidenceLoader is a minimal staying-root fake satisfying
// reducercloudinventory.CloudInventoryEvidenceLoader. The family's own
// functional fake moved with it to reducer/cloudinventory
// (cloud_inventory_admission_test.go); Go test files cannot share unexported
// symbols across a package boundary, so staying tests that exercise the moved
// handler keep this local copy (issue #6061).
type stubCloudInventoryEvidenceLoader struct {
	records []reducercloudinventory.CloudInventoryRecord
	err     error
}

func (s *stubCloudInventoryEvidenceLoader) LoadCloudInventoryEvidence(
	context.Context,
	string,
	string,
) ([]reducercloudinventory.CloudInventoryRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]reducercloudinventory.CloudInventoryRecord(nil), s.records...), nil
}

// stubCloudInventoryAdmissionWriter is a minimal staying-root fake satisfying
// reducercloudinventory.CloudInventoryAdmissionWriter, kept for the same
// package-boundary reason as the loader stub above.
type stubCloudInventoryAdmissionWriter struct {
	writes []reducercloudinventory.CloudInventoryAdmissionWrite
	err    error
}

func (s *stubCloudInventoryAdmissionWriter) WriteCloudInventoryAdmission(
	_ context.Context,
	write reducercloudinventory.CloudInventoryAdmissionWrite,
) (reducercloudinventory.CloudInventoryAdmissionWriteResult, error) {
	if s.err != nil {
		return reducercloudinventory.CloudInventoryAdmissionWriteResult{}, s.err
	}
	s.writes = append(s.writes, write)
	return reducercloudinventory.CloudInventoryAdmissionWriteResult{
		CanonicalWrites: len(write.Resources),
		EvidenceSummary: "stub admission write",
	}, nil
}

// cloudInventoryIntent builds a staying-root admission intent for the moved
// handler. The family's own helper moved with it; this local copy keeps the
// same field values so staying assertions observe identical behavior.
func cloudInventoryIntent() Intent {
	return Intent{
		IntentID:        "intent-cloud-inventory",
		ScopeID:         "gcp:org:eshu:project:prod",
		GenerationID:    "generation-1",
		SourceSystem:    "gcp",
		Domain:          DomainCloudInventoryAdmission,
		Cause:           "cloud inventory facts observed",
		RelatedScopeIDs: []string{"gcp:org:eshu:project:prod"},
	}
}

type recordingAdmissionDecisionWriter struct {
	calls []admissionDecisionWriterCall
}

type admissionDecisionWriterCall struct {
	decisions []admissiondecision.AdmissionDecisionWrite
}

func (w *recordingAdmissionDecisionWriter) WriteAdmissionDecisions(
	_ context.Context,
	decisions []admissiondecision.AdmissionDecisionWrite,
) error {
	w.calls = append(w.calls, admissionDecisionWriterCall{
		decisions: append([]admissiondecision.AdmissionDecisionWrite(nil), decisions...),
	})
	return nil
}

func TestDeployableUnitCorrelationWritesSharedAdmissionDecision(t *testing.T) {
	t.Parallel()

	writer := &recordingAdmissionDecisionWriter{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-edge-api",
				"edge-api",
				[]map[string]any{{
					"repo_id":       "repo-edge-api",
					"language":      "dockerfile",
					"relative_path": "Dockerfile",
					"parsed_file_data": map[string]any{
						"dockerfile_stages": []any{
							map[string]any{"name": "runtime"},
						},
					},
				}},
			),
		},
		ResolvedLoader: &stubDeployableUnitResolvedLoader{
			resolved: []relationships.ResolvedRelationship{{
				SourceRepoID:     "repo-deployments",
				TargetRepoID:     "repo-edge-api",
				RelationshipType: relationships.RelDeploysFrom,
				Confidence:       0.94,
				Details: map[string]any{
					"evidence_kinds": []string{
						string(relationships.EvidenceKindArgoCDAppSource),
					},
				},
			}},
		},
		PhasePublisher:          &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:              &recordingDeployableUnitEdgeWriter{},
		AdmissionDecisionWriter: writer,
		AdmissionDecisionNow:    fixedAdmissionDecisionNow,
	}

	result, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	decisions := onlyAdmissionDecisionBatch(t, writer)
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("shared admission decisions = %d, want %d", got, want)
	}
	decision := decisions[0].Decision
	if decision.Domain != string(DomainDeployableUnitCorrelation) {
		t.Fatalf("Domain = %q, want %q", decision.Domain, DomainDeployableUnitCorrelation)
	}
	if decision.State != admissiondecision.AdmissionStateAdmitted {
		t.Fatalf("State = %q, want %q", decision.State, admissiondecision.AdmissionStateAdmitted)
	}
	if decision.DomainState != "admitted" {
		t.Fatalf("DomainState = %q, want admitted", decision.DomainState)
	}
	if !decision.CanonicalWrite.Eligible || !decision.CanonicalWrite.Written {
		t.Fatalf("CanonicalWrite = %+v, want eligible and written", decision.CanonicalWrite)
	}
	if decision.CanonicalWrite.TargetKind != DomainDeployableUnitEdges {
		t.Fatalf("TargetKind = %q, want %q", decision.CanonicalWrite.TargetKind, DomainDeployableUnitEdges)
	}
	if decision.CandidateID == "" || decision.AnchorID == "" {
		t.Fatalf("CandidateID/AnchorID must be stable, got candidate=%q anchor=%q", decision.CandidateID, decision.AnchorID)
	}
	if len(decisions[0].Evidence) == 0 {
		t.Fatal("shared admission decision evidence = 0, want bounded evidence handles")
	}
}

func TestCloudInventoryAdmissionWritesSharedAdmittedAndNonAdmittedDecisions(t *testing.T) {
	t.Parallel()

	writer := &recordingAdmissionDecisionWriter{}
	canonicalWriter := &stubCloudInventoryAdmissionWriter{}
	handler := reducercloudinventory.CloudInventoryAdmissionHandler{
		EvidenceLoader: &stubCloudInventoryEvidenceLoader{records: []reducercloudinventory.CloudInventoryRecord{
			{
				Provider:     cloudinventory.ProviderGCP,
				FactKind:     "gcp_cloud_resource",
				RawIdentity:  "//compute.googleapis.com/projects/eshu-prod/zones/us-central1-a/instances/api-1",
				ResourceType: "compute.googleapis.com/Instance",
				SourceLayer:  reducercloudinventory.SourceLayerObserved,
			},
			{
				Provider:    cloudinventory.ProviderAzure,
				FactKind:    "azure_cloud_resource",
				RawIdentity: "resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/api",
				SourceLayer: reducercloudinventory.SourceLayerObserved,
			},
			{
				Provider:    "oraclecloud",
				FactKind:    "oci_cloud_resource",
				RawIdentity: "ocid1.instance.oc1..abc",
				SourceLayer: reducercloudinventory.SourceLayerObserved,
			},
			{
				Provider:    cloudinventory.ProviderGCP,
				FactKind:    "gcp_cloud_resource",
				RawIdentity: " ",
				SourceLayer: reducercloudinventory.SourceLayerObserved,
			},
		}},
		Writer:                  canonicalWriter,
		AdmissionDecisionWriter: writer,
		AdmissionDecisionNow:    fixedAdmissionDecisionNow,
	}

	result, err := handler.Handle(context.Background(), cloudInventoryIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1 admitted resource", result.CanonicalWrites)
	}
	decisions := onlyAdmissionDecisionBatch(t, writer)
	if got, want := len(decisions), 4; got != want {
		t.Fatalf("shared admission decisions = %d, want %d", got, want)
	}
	counts := countAdmissionStates(decisions)
	for state, want := range map[admissiondecision.AdmissionState]int{
		admissiondecision.AdmissionStateAdmitted:        1,
		admissiondecision.AdmissionStateAmbiguous:       1,
		admissiondecision.AdmissionStateUnsupported:     1,
		admissiondecision.AdmissionStateMissingEvidence: 1,
	} {
		if got := counts[state]; got != want {
			t.Fatalf("state %q count = %d, want %d", state, got, want)
		}
	}
	for _, write := range decisions {
		if write.Decision.Domain != string(DomainCloudInventoryAdmission) {
			t.Fatalf("Domain = %q, want %q", write.Decision.Domain, DomainCloudInventoryAdmission)
		}
		if write.Decision.CanonicalWrite.Written && write.Decision.State != admissiondecision.AdmissionStateAdmitted {
			t.Fatalf("non-admitted decision wrote canonical truth: %+v", write.Decision)
		}
		if write.Decision.CandidateID == "resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/api" {
			t.Fatalf("CandidateID leaked raw provider identity: %q", write.Decision.CandidateID)
		}
	}
}

func TestPackageSourceCorrelationWritesSharedOwnershipAndConsumptionDecisions(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	admissionWriter := &recordingAdmissionDecisionWriter{}
	correlationWriter := &recordingPackageCorrelationWriter{}
	handler := PackageSourceCorrelationHandler{
		FactLoader: &stubPackageSourceFactLoader{
			scopeFacts: []facts.Envelope{
				packageRegistryPackageFact("pkg:npm://registry.example/team-api", "npm", "team-api", "", observedAt),
				packageRegistryPackageVersionFact(
					"package-version-fact",
					"pkg:npm://registry.example/team-api",
					"pkg:npm://registry.example/team-api@1.2.0",
					"1.2.0",
					observedAt,
					observedAt,
				),
				packageSourceHintFact(
					"pkg:npm://registry.example/team-api",
					"repository",
					"https://github.com/acme/team-api",
					observedAt,
				),
			},
			repositoryFacts: []facts.Envelope{
				packageSourceRepositoryFact(
					"repo-team-api",
					"team-api",
					"https://github.com/acme/team-api.git",
					false,
					observedAt,
				),
			},
			manifestDependencies: []facts.Envelope{
				packageManifestDependencyFact(
					"repo-consumer",
					"consumer",
					"package.json",
					"team-api",
					"npm",
					"^1.2.0",
					observedAt,
				),
			},
		},
		Writer:                  correlationWriter,
		AdmissionDecisionWriter: admissionWriter,
		AdmissionDecisionNow:    fixedAdmissionDecisionNow,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-package-source",
		ScopeID:      "package-registry:npm:team-api",
		GenerationID: "generation-1",
		SourceSystem: "package_registry",
		Domain:       DomainPackageSourceCorrelation,
		Cause:        "package registry source hints observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1 package consumption write", result.CanonicalWrites)
	}
	decisions := onlyAdmissionDecisionBatch(t, admissionWriter)
	if got, want := len(decisions), 3; got != want {
		t.Fatalf("shared admission decisions = %d, want ownership, consumption, and publication", got)
	}
	counts := countAdmissionStates(decisions)
	if got := counts[admissiondecision.AdmissionStateAdmitted]; got != 1 {
		t.Fatalf("admitted decisions = %d, want 1 consumption admission", got)
	}
	if got := counts[admissiondecision.AdmissionStateMissingEvidence]; got != 2 {
		t.Fatalf("missing evidence decisions = %d, want 2 provenance-only package decisions", got)
	}
	for _, write := range decisions {
		if write.Decision.Domain != string(DomainPackageSourceCorrelation) {
			t.Fatalf("Domain = %q, want %q", write.Decision.Domain, DomainPackageSourceCorrelation)
		}
		if write.Decision.DomainState == "manifest_declared" && !write.Decision.CanonicalWrite.Written {
			t.Fatalf("manifest consumption decision did not record canonical write: %+v", write.Decision)
		}
		if write.Decision.DomainState == "derived" && write.Decision.CanonicalWrite.Eligible {
			t.Fatalf("provenance-only ownership decision was graph-eligible: %+v", write.Decision)
		}
	}
}

func onlyAdmissionDecisionBatch(
	t *testing.T,
	writer *recordingAdmissionDecisionWriter,
) []admissiondecision.AdmissionDecisionWrite {
	t.Helper()
	if len(writer.calls) != 1 {
		t.Fatalf("WriteAdmissionDecisions calls = %d, want 1", len(writer.calls))
	}
	return writer.calls[0].decisions
}

func countAdmissionStates(decisions []admissiondecision.AdmissionDecisionWrite) map[admissiondecision.AdmissionState]int {
	counts := make(map[admissiondecision.AdmissionState]int)
	for _, write := range decisions {
		counts[write.Decision.State]++
	}
	return counts
}

func fixedAdmissionDecisionNow() time.Time {
	return time.Date(2026, 6, 17, 3, 0, 0, 0, time.UTC)
}
