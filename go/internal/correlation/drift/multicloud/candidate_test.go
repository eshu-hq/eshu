// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package multicloud

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
)

const (
	gcpInstance      = "//compute.googleapis.com/projects/proj/zones/z/instances/orphan"
	azureStorage     = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/acct"
	gcpUnmanagedInst = "//compute.googleapis.com/projects/proj/zones/z/instances/unmanaged"
)

func resourceRow(arn, scope string) *cloudruntime.ResourceRow {
	return &cloudruntime.ResourceRow{ARN: arn, ScopeID: scope}
}

func TestBuildCandidatesGCPOrphanedAndAzureUnmanaged(t *testing.T) {
	t.Parallel()

	rows := []Row{
		{
			Provider:    cloudinventory.ProviderGCP,
			RawIdentity: gcpInstance,
			ScopeID:     "gcp:proj:z",
			Cloud:       resourceRow(gcpInstance, "gcp:proj:z"),
		},
		{
			Provider:    cloudinventory.ProviderAzure,
			RawIdentity: azureStorage,
			ScopeID:     "azure:sub:rg",
			Cloud:       resourceRow(azureStorage, "azure:sub:rg"),
			State:       resourceRow(azureStorage, "state:azure"),
		},
	}

	candidates := BuildCandidates(rows, "multi")
	if got, want := len(candidates), 2; got != want {
		t.Fatalf("BuildCandidates() = %d candidates, want %d", got, want)
	}

	// Candidates are uid-sorted; assert each finding by its provider, not order.
	byProvider := map[string]model.Candidate{}
	for _, c := range candidates {
		byProvider[ProviderFromCandidate(c)] = c
	}
	gcp, ok := byProvider[cloudinventory.ProviderGCP]
	if !ok {
		t.Fatalf("missing GCP candidate")
	}
	if got := FindingKindFromCandidate(gcp); got != string(cloudruntime.FindingKindOrphanedCloudResource) {
		t.Fatalf("gcp finding = %q, want orphaned", got)
	}
	if got := ManagementStatusFromCandidate(gcp); got != cloudruntime.ManagementStatusCloudOnly {
		t.Fatalf("gcp status = %q, want cloud_only", got)
	}
	azure, ok := byProvider[cloudinventory.ProviderAzure]
	if !ok {
		t.Fatalf("missing Azure candidate")
	}
	if got := FindingKindFromCandidate(azure); got != string(cloudruntime.FindingKindUnmanagedCloudResource) {
		t.Fatalf("azure finding = %q, want unmanaged", got)
	}
	if got := ManagementStatusFromCandidate(azure); got != cloudruntime.ManagementStatusTerraformStateOnly {
		t.Fatalf("azure status = %q, want terraform_state_only", got)
	}

	// CorrelationKey must be the canonical uid, not the raw identity.
	wantGCPUID := cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderGCP, gcpInstance).CloudResourceUID
	if gcp.CorrelationKey != wantGCPUID {
		t.Fatalf("gcp CorrelationKey = %q, want canonical uid %q", gcp.CorrelationKey, wantGCPUID)
	}
}

func TestBuildCandidatesAmbiguousAndUnknownOverrides(t *testing.T) {
	t.Parallel()

	rows := []Row{
		{
			Provider:         cloudinventory.ProviderGCP,
			RawIdentity:      gcpInstance,
			ScopeID:          "gcp:proj:z",
			Cloud:            resourceRow(gcpInstance, "gcp:proj:z"),
			State:            resourceRow(gcpInstance, "state:gcp"),
			Config:           resourceRow(gcpInstance, "config:gcp"),
			FindingKind:      cloudruntime.FindingKindAmbiguousCloudResource,
			ManagementStatus: cloudruntime.ManagementStatusAmbiguous,
			WarningFlags:     []string{"ambiguous_ownership"},
		},
		{
			Provider:         cloudinventory.ProviderAzure,
			RawIdentity:      azureStorage,
			ScopeID:          "azure:sub:rg",
			Cloud:            resourceRow(azureStorage, "azure:sub:rg"),
			State:            resourceRow(azureStorage, "state:azure"),
			FindingKind:      cloudruntime.FindingKindUnknownCloudResource,
			ManagementStatus: cloudruntime.ManagementStatusUnknown,
			MissingEvidence:  []string{"collector_coverage"},
		},
	}

	candidates := BuildCandidates(rows, "multi")
	if got, want := len(candidates), 2; got != want {
		t.Fatalf("BuildCandidates() = %d candidates, want %d", got, want)
	}
	byProvider := map[string]model.Candidate{}
	for _, c := range candidates {
		byProvider[ProviderFromCandidate(c)] = c
	}
	if got := FindingKindFromCandidate(byProvider[cloudinventory.ProviderGCP]); got != string(cloudruntime.FindingKindAmbiguousCloudResource) {
		t.Fatalf("gcp finding = %q, want ambiguous override even with config present", got)
	}
	if got := FindingKindFromCandidate(byProvider[cloudinventory.ProviderAzure]); got != string(cloudruntime.FindingKindUnknownCloudResource) {
		t.Fatalf("azure finding = %q, want unknown coverage gap", got)
	}
	// Ambiguous warning is upgraded to the conflict evidence type.
	if !hasEvidence(byProvider[cloudinventory.ProviderGCP], EvidenceTypeAmbiguousManagement) {
		t.Fatalf("ambiguous candidate missing %q evidence", EvidenceTypeAmbiguousManagement)
	}
	// Unknown missing-evidence is upgraded to the coverage-gap evidence type.
	if !hasEvidence(byProvider[cloudinventory.ProviderAzure], EvidenceTypeCoverageGap) {
		t.Fatalf("unknown candidate missing %q evidence", EvidenceTypeCoverageGap)
	}
}

func TestBuildCandidatesSkipsUnresolvedAndConverged(t *testing.T) {
	t.Parallel()

	rows := []Row{
		{
			// Malformed GCP identity (no // prefix) is unresolved, not fabricated.
			Provider:    cloudinventory.ProviderGCP,
			RawIdentity: "compute.googleapis.com/projects/p/instances/bad",
			Cloud:       resourceRow("compute.googleapis.com/projects/p/instances/bad", "gcp:p"),
		},
		{
			// Cloud+state+config converge: no runtime drift to admit.
			Provider:    cloudinventory.ProviderGCP,
			RawIdentity: gcpUnmanagedInst,
			Cloud:       resourceRow(gcpUnmanagedInst, "gcp:proj:z"),
			State:       resourceRow(gcpUnmanagedInst, "state:gcp"),
			Config:      resourceRow(gcpUnmanagedInst, "config:gcp"),
		},
		{
			// Unsupported provider is not keyable.
			Provider:    "oracle",
			RawIdentity: "ocid1.instance.oc1..abc",
			Cloud:       resourceRow("ocid1.instance.oc1..abc", "oracle"),
		},
	}

	if candidates := BuildCandidates(rows, "multi"); len(candidates) != 0 {
		t.Fatalf("BuildCandidates() = %d candidates, want 0 (unresolved/converged/unsupported skipped)", len(candidates))
	}
}

func TestBuildCandidatesDoesNotOverwriteDeclaredConfigEvidence(t *testing.T) {
	t.Parallel()

	// An unmanaged row carries state but no config: the absence of config
	// evidence is what makes it unmanaged. The builder must not synthesize a
	// config atom, which would falsely promote the resource to managed.
	rows := []Row{{
		Provider:    cloudinventory.ProviderGCP,
		RawIdentity: gcpUnmanagedInst,
		ScopeID:     "gcp:proj:z",
		Cloud:       resourceRow(gcpUnmanagedInst, "gcp:proj:z"),
		State:       resourceRow(gcpUnmanagedInst, "state:gcp"),
	}}

	candidates := BuildCandidates(rows, "multi")
	if len(candidates) != 1 {
		t.Fatalf("BuildCandidates() = %d, want 1", len(candidates))
	}
	if hasEvidence(candidates[0], EvidenceTypeConfigResource) {
		t.Fatalf("unmanaged candidate must not carry fabricated config evidence")
	}
	if got := FindingKindFromCandidate(candidates[0]); got != string(cloudruntime.FindingKindUnmanagedCloudResource) {
		t.Fatalf("finding = %q, want unmanaged", got)
	}
}

func TestBuildCandidatesEvaluateAdmitsThroughSharedPack(t *testing.T) {
	t.Parallel()

	rows := []Row{{
		Provider:    cloudinventory.ProviderGCP,
		RawIdentity: gcpInstance,
		ScopeID:     "gcp:proj:z",
		Cloud:       resourceRow(gcpInstance, "gcp:proj:z"),
	}}
	candidates := BuildCandidates(rows, "multi")
	evaluation, err := engine.Evaluate(rules.MultiCloudRuntimeDriftRulePack(), candidates)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	admitted := 0
	for _, result := range evaluation.Results {
		if result.Candidate.State == model.CandidateStateAdmitted {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("admitted = %d, want 1 through shared multi-cloud pack", admitted)
	}
}

func hasEvidence(candidate model.Candidate, evidenceType string) bool {
	for _, a := range candidate.Evidence {
		if a.EvidenceType == evidenceType {
			return true
		}
	}
	return false
}
