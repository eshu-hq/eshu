// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package multicloud

import (
	"strings"
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

// TestManagementStatusForValueComparisonInconclusive keeps the multi-cloud
// classifier's management-status vocabulary in step with the single-cloud one.
//
// The two paths are duplicated classifiers over one shared FindingKind set, so
// a kind added to cloudruntime is silently unhandled here until its arm is
// added -- and the fallthrough is an empty string, not a compile error. An
// empty status routes the finding to the generic review_evidence action instead
// of expand_collector_coverage_or_permissions, which is the wrong instruction
// for a resource whose evidence could not be read (#5837).
func TestManagementStatusForValueComparisonInconclusive(t *testing.T) {
	t.Parallel()

	got := managementStatusForFinding(cloudruntime.FindingKindValueComparisonInconclusive)
	if got != cloudruntime.ManagementStatusUnknown {
		t.Fatalf("managementStatusForFinding(value_comparison_inconclusive) = %q, want %q",
			got, cloudruntime.ManagementStatusUnknown)
	}
}

// TestMultiCloudInconclusiveNamesUncomparableAttributes closes the gap between
// what the multi-cloud route's OpenAPI contract promises and what it emits.
//
// Both routes document that value_comparison_inconclusive "names the unreadable
// attributes in missing_evidence". The AWS builder derives those names from
// ClassifyValueComparison; this builder only forwarded Row.MissingEvidence,
// which the loader populates for EXISTENCE gaps, so the multi-cloud response
// fell back to a bare "collector_coverage" and never named an attribute. The
// finding then told an operator to expand coverage without saying of what
// (#5837).
func TestMultiCloudInconclusiveNamesUncomparableAttributes(t *testing.T) {
	t.Parallel()

	const uid = "aws:123456789012:us-east-1:ec2:instance/i-0multicloud"
	// ami readable on the cloud side, absent on the declared side: covered,
	// uncomparable, and therefore inconclusive.
	rows := []Row{{
		Provider:         "aws",
		CloudResourceUID: uid,
		ResourceType:     "aws_instance",
		ScopeID:          "aws:123456789012:us-east-1:ec2",
		Cloud: &cloudruntime.ResourceRow{
			ARN: uid, ResourceType: "aws_ec2_instance",
			Attributes: map[string]string{"ami": "ami-observed"},
		},
		State:  &cloudruntime.ResourceRow{ARN: uid, ResourceType: "aws_instance"},
		Config: &cloudruntime.ResourceRow{ARN: uid, ResourceType: "aws_instance"},
	}}

	candidates := BuildCandidates(rows, "aws:123456789012:us-east-1:ec2")
	if len(candidates) != 1 {
		t.Fatalf("BuildCandidates() = %d candidates, want 1", len(candidates))
	}

	var gaps []string
	for _, a := range candidates[0].Evidence {
		if a.Key == "missing_evidence" {
			gaps = append(gaps, a.Value)
		}
	}
	want := "comparable_attribute:ami"
	found := false
	for _, g := range gaps {
		if g == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing_evidence = %#v, want it to contain %q", gaps, want)
	}
}

// TestMultiCloudGapAtomsOnlyForInconclusive is the negative half of
// TestMultiCloudInconclusiveNamesUncomparableAttributes, and it exists because
// deleting the kind guard in appendManagementEvidence passed every suite in the
// repo.
//
// Firing comparable_attribute atoms for other kinds is not cosmetic. Non-empty
// missing_evidence values OVERRIDE the status fallback in
// multiCloudRuntimeMissingEvidence, so an unmanaged_cloud_resource would answer
// a VALUE question ("ami could not be read") where the operator asked an
// EXISTENCE one ("no terraform config declares this"), and the real answer
// would be gone (#5837).
func TestMultiCloudGapAtomsOnlyForInconclusive(t *testing.T) {
	t.Parallel()

	const uid = "aws:123456789012:us-east-1:ec2:instance/i-0negative"
	const lambdaUID = "aws:123456789012:us-east-1:lambda:function/gapatoms"
	scope := "aws:123456789012:us-east-1:ec2"
	cloudRow := func() *cloudruntime.ResourceRow {
		return &cloudruntime.ResourceRow{
			ARN: uid, ResourceType: "aws_ec2_instance",
			Attributes: map[string]string{"ami": "ami-observed"},
		}
	}

	cases := []struct {
		name string
		row  Row
	}{
		{
			// No config layer: an EXISTENCE verdict. The ami is just as
			// uncomparable here, which is exactly why the guard has to hold.
			name: "unmanaged_cloud_resource",
			row: Row{
				Provider: "aws", CloudResourceUID: uid, ResourceType: "aws_instance", ScopeID: scope,
				Cloud: cloudRow(),
				State: &cloudruntime.ResourceRow{ARN: uid, ResourceType: "aws_instance"},
			},
		},
		{
			// A REAL drift that ALSO has an uncomparable attribute -- the only
			// other shape that discriminates. version compares and differs (so
			// the kind is image_version_drift) while image_uri is absent on the
			// declared side (so Uncomparable is non-empty and an unguarded
			// emitter has something to emit).
			//
			// A plain single-attribute drift does NOT discriminate: when every
			// covered attribute compares, Uncomparable is empty and the loop
			// emits nothing either way. Worth recording, because that was tried
			// first and passed under the mutation it was written to catch.
			name: "image_version_drift_with_an_uncomparable_attribute",
			row: Row{
				Provider: "aws", CloudResourceUID: lambdaUID, ResourceType: "aws_lambda_function", ScopeID: scope,
				Cloud: &cloudruntime.ResourceRow{
					ARN: lambdaUID, ResourceType: "lambda.function",
					Attributes: map[string]string{"image_uri": "acct.dkr.ecr/app:v2", "version": "9"},
				},
				State: &cloudruntime.ResourceRow{
					ARN: lambdaUID, ResourceType: "aws_lambda_function",
					Attributes: map[string]string{"version": "7"},
				},
				Config: &cloudruntime.ResourceRow{ARN: lambdaUID, ResourceType: "aws_lambda_function"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidates := BuildCandidates([]Row{tc.row}, scope)
			if len(candidates) != 1 {
				t.Fatalf("BuildCandidates() = %d candidates, want 1", len(candidates))
			}
			for _, a := range candidates[0].Evidence {
				// Pin the ID shape too, not only the value prefix.
				if strings.HasPrefix(a.Value, "comparable_attribute:") ||
					strings.Contains(a.ID, "/uncomparable/") {
					t.Fatalf("kind %s emitted %q; comparable_attribute atoms belong to "+
						"value_comparison_inconclusive alone, and a non-empty missing_evidence "+
						"here would override the existence fallback", tc.name, a.Value)
				}
			}
		})
	}
}
