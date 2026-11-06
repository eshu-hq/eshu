// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"reflect"
	"sort"
	"testing"
)

// frozenSupplyChainReadAdmission pins the exact method set behind the
// versioned #9669 D6 admission contract for the SupplyChain family reads
// the supply-chain endpoints admit through the family reader ports. A
// renamed, added, removed, or re-signed method fails here until that list
// is deliberately updated; bump SupplyChainReadAdmissionContractVersion
// alongside (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape, keeping the eshu-api
// extraction boundary from drifting silently.
//
// D-wave consumer: D6 (Status + SupplyChain reads, per the arbiter D-wave
// ruling on #9669). The suppressions mutation row stays out (Never-satellite
// survivor via SuppressionAdmission, already pinned); shared graph/content,
// root readiness, and packet-responder ports stay out per the pin comment.
var frozenSupplyChainReadAdmission = []string{
	"ContainerImageIdentityInventory func(context.Context, chain.ContainerImageIdentityAggregateFilter, chain.ContainerImageIdentityInventoryDimension, int, int) ([]chain.ContainerImageIdentityInventoryRow, error)",
	"CountContainerImageIdentities func(context.Context, chain.ContainerImageIdentityAggregateFilter) (chain.ContainerImageIdentityAggregateCount, error)",
	"CountSBOMAttestationAttachments func(context.Context, chain.SBOMAttestationAttachmentAggregateFilter) (chain.SBOMAttestationAttachmentAggregateCount, error)",
	"CountSecurityAlertReconciliations func(context.Context, chain.SecurityAlertReconciliationAggregateFilter) (chain.SecurityAlertReconciliationAggregateCount, error)",
	"CountSupplyChainImpactFindings func(context.Context, impact.AggregateFilter) (impact.AggregateCount, error)",
	"CurrentAuthorizedCloudResourceUIDs func(context.Context, []string, bool, []string, []string) ([]string, error)",
	"CurrentAuthorizedCloudResourcesByDigest func(context.Context, []string, bool, []string, []string) ([]chain.CloudResourceRuntimeDigestMatch, error)",
	"CurrentAuthorizedKubernetesRuntimeWorkloads func(context.Context, []chain.KubernetesRuntimeCandidate, bool, []string, []string) ([]chain.KubernetesRuntimeWorkloadMatch, error)",
	"ExplainSupplyChainImpact func(context.Context, impact.ExplanationFilter) (impact.ExplanationRow, error)",
	"ListAdvisoryCatalog func(context.Context, advisory.CatalogFilter) (advisory.CatalogPage, error)",
	"ListAdvisoryEvidence func(context.Context, advisory.EvidenceFilter) ([]advisory.EvidenceRow, error)",
	"ListContainerImageIdentities func(context.Context, chain.ContainerImageIdentityFilter) ([]chain.ContainerImageIdentityRow, error)",
	"ListSBOMAttestationAttachments func(context.Context, chain.SBOMAttestationAttachmentFilter) (chain.SBOMAttestationAttachmentPage, error)",
	"ListSecurityAlertReconciliations func(context.Context, chain.SecurityAlertReconciliationFilter) ([]chain.SecurityAlertReconciliationRow, error)",
	"ListSupplyChainImpactFindings func(context.Context, impact.FindingFilter) ([]impact.FindingRow, error)",
	"ReadSupplyChainImpactReadiness func(context.Context, impact.ReadinessQuery) (impact.ReadinessSnapshot, error)",
	"SBOMAttestationAttachmentInventory func(context.Context, chain.SBOMAttestationAttachmentAggregateFilter, chain.SBOMAttestationAttachmentInventoryDimension, int, int) ([]chain.SBOMAttestationAttachmentInventoryRow, error)",
	"SecurityAlertCollectorConfigurations func(context.Context) ([]string, error)",
	"SecurityAlertReconciliationInventory func(context.Context, chain.SecurityAlertReconciliationAggregateFilter, chain.SecurityAlertReconciliationInventoryDimension, int, int) ([]chain.SecurityAlertReconciliationInventoryRow, error)",
	"SecurityAlertReconciliationQueueStates func(context.Context, []string) ([]chain.SecurityAlertReconciliationQueueState, error)",
	"SecurityAlertTargetStates func(context.Context, []string, []string) ([]chain.SecurityAlertTargetState, error)",
	"SupplyChainImpactGroupedInventory func(context.Context, impact.AggregateFilter, impact.GroupedInventoryDimension, int, int) (impact.GroupedInventoryPage, error)",
	"SupplyChainImpactInventory func(context.Context, impact.AggregateFilter, impact.InventoryDimension, int, int) ([]impact.InventoryRow, error)",
}

// supplyChainReadAdmissionMethodSet reflects the live
// SupplyChainReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func supplyChainReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SupplyChainReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSupplyChainReadAdmissionContractIsVersioned pins the #9669 D6
// admission-contract version for the SupplyChain family reads (the
// supply-chain endpoints admit through the family reader ports). The
// version value plus the frozen method set below are the enforcement: a
// breaking change to the admission shape fails this test until the
// version bumps in the same diff.
func TestSupplyChainReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SupplyChainReadAdmissionContractVersion != "v0" {
		t.Errorf("SupplyChainReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SupplyChainReadAdmissionContractVersion, "v0")
	}
	got := supplyChainReadAdmissionMethodSet(t)
	if len(got) != len(frozenSupplyChainReadAdmission) {
		t.Fatalf("SupplyChainReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSupplyChainReadAdmission, len(frozenSupplyChainReadAdmission), SupplyChainReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSupplyChainReadAdmission[i] {
			t.Errorf("SupplyChainReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSupplyChainReadAdmission[i], SupplyChainReadAdmissionContractVersion)
		}
	}
}
