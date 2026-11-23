// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
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
	"ExplainSupplyChainImpact func(context.Context, packet.ExplanationFilter) (impact.ExplanationRow, error)",
	"ListAdvisoryCatalog func(context.Context, advisory.CatalogFilter) (advisory.CatalogPage, error)",
	"ListAdvisoryEvidence func(context.Context, advisory.EvidenceFilter) ([]advisory.EvidenceRow, error)",
	"ListContainerImageIdentities func(context.Context, chain.ContainerImageIdentityFilter) ([]chain.ContainerImageIdentityRow, error)",
	"ListSBOMAttestationAttachments func(context.Context, chain.SBOMAttestationAttachmentFilter) (chain.SBOMAttestationAttachmentPage, error)",
	"ListSecurityAlertReconciliations func(context.Context, chain.SecurityAlertReconciliationFilter) ([]chain.SecurityAlertReconciliationRow, error)",
	"ListSupplyChainImpactFindings func(context.Context, impact.FindingFilter) ([]impact.FindingRow, error)",
	"ReadSupplyChainImpactReadiness func(context.Context, impact.ReadinessQuery) (packet.ReadinessSnapshot, error)",
	"SBOMAttestationAttachmentInventory func(context.Context, chain.SBOMAttestationAttachmentAggregateFilter, chain.SBOMAttestationAttachmentInventoryDimension, int, int) ([]chain.SBOMAttestationAttachmentInventoryRow, error)",
	"SecurityAlertCollectorConfigurations func(context.Context) ([]string, error)",
	"SecurityAlertReconciliationInventory func(context.Context, chain.SecurityAlertReconciliationAggregateFilter, chain.SecurityAlertReconciliationInventoryDimension, int, int) ([]chain.SecurityAlertReconciliationInventoryRow, error)",
	"SecurityAlertReconciliationQueueStates func(context.Context, []string) ([]chain.SecurityAlertReconciliationQueueState, error)",
	"SecurityAlertTargetStates func(context.Context, []string, []string) ([]chain.SecurityAlertTargetState, error)",
	"SupplyChainImpactGroupedInventory func(context.Context, impact.AggregateFilter, impact.GroupedInventoryDimension, int, int) (impact.GroupedInventoryPage, error)",
	"SupplyChainImpactInventory func(context.Context, impact.AggregateFilter, impact.InventoryDimension, int, int) (impact.AggregateInventoryPage, error)",
}

// supplyChainReadAdmissionMethodSet reflects the live
// SupplyChainReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func supplyChainReadAdmissionMethodSet(t *testing.T, iface any) []string {
	t.Helper()
	rt := reflect.TypeOf(iface).Elem()
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

	if SupplyChainReadAdmissionContractVersion != "v1" {
		t.Errorf("SupplyChainReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SupplyChainReadAdmissionContractVersion, "v1")
	}
	got := supplyChainReadAdmissionMethodSet(t, (*SupplyChainReadAdmission)(nil))
	if len(got) != len(frozenSupplyChainReadAdmission) {
		t.Fatalf("SupplyChainReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSupplyChainReadAdmission, len(frozenSupplyChainReadAdmission), SupplyChainReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSupplyChainReadAdmission[i] {
			t.Errorf("SupplyChainReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSupplyChainReadAdmission[i], SupplyChainReadAdmissionContractVersion)
		}
	}
}

// supplyChainReadAdmissionWitness proves SupplyChainReadAdmission is
// satisfiable with the minimal shape: the 23 family reads and nothing
// wider — no suppression mutation, no shared graph/content port, no
// packet-writer surface.
type supplyChainReadAdmissionWitness struct{}

func (supplyChainReadAdmissionWitness) ContainerImageIdentityInventory(context.Context, ContainerImageIdentityAggregateFilter, ContainerImageIdentityInventoryDimension, int, int) ([]ContainerImageIdentityInventoryRow, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) CountContainerImageIdentities(context.Context, ContainerImageIdentityAggregateFilter) (ContainerImageIdentityAggregateCount, error) {
	return ContainerImageIdentityAggregateCount{}, nil
}

func (supplyChainReadAdmissionWitness) CountSBOMAttestationAttachments(context.Context, SBOMAttestationAttachmentAggregateFilter) (SBOMAttestationAttachmentAggregateCount, error) {
	return SBOMAttestationAttachmentAggregateCount{}, nil
}

func (supplyChainReadAdmissionWitness) CountSecurityAlertReconciliations(context.Context, SecurityAlertReconciliationAggregateFilter) (SecurityAlertReconciliationAggregateCount, error) {
	return SecurityAlertReconciliationAggregateCount{}, nil
}

func (supplyChainReadAdmissionWitness) CountSupplyChainImpactFindings(context.Context, impact.AggregateFilter) (impact.AggregateCount, error) {
	return impact.AggregateCount{}, nil
}

func (supplyChainReadAdmissionWitness) CurrentAuthorizedCloudResourceUIDs(context.Context, []string, bool, []string, []string) ([]string, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) CurrentAuthorizedCloudResourcesByDigest(context.Context, []string, bool, []string, []string) ([]CloudResourceRuntimeDigestMatch, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) CurrentAuthorizedKubernetesRuntimeWorkloads(context.Context, []KubernetesRuntimeCandidate, bool, []string, []string) ([]KubernetesRuntimeWorkloadMatch, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) ExplainSupplyChainImpact(context.Context, packet.ExplanationFilter) (impact.ExplanationRow, error) {
	return impact.ExplanationRow{}, nil
}

func (supplyChainReadAdmissionWitness) ListAdvisoryCatalog(context.Context, advisory.CatalogFilter) (advisory.CatalogPage, error) {
	return advisory.CatalogPage{}, nil
}

func (supplyChainReadAdmissionWitness) ListAdvisoryEvidence(context.Context, advisory.EvidenceFilter) ([]advisory.EvidenceRow, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) ListContainerImageIdentities(context.Context, ContainerImageIdentityFilter) ([]ContainerImageIdentityRow, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) ListSBOMAttestationAttachments(context.Context, SBOMAttestationAttachmentFilter) (SBOMAttestationAttachmentPage, error) {
	return SBOMAttestationAttachmentPage{}, nil
}

func (supplyChainReadAdmissionWitness) ListSecurityAlertReconciliations(context.Context, SecurityAlertReconciliationFilter) ([]SecurityAlertReconciliationRow, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) ListSupplyChainImpactFindings(context.Context, impact.FindingFilter) ([]impact.FindingRow, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) ReadSupplyChainImpactReadiness(context.Context, impact.ReadinessQuery) (packet.ReadinessSnapshot, error) {
	return packet.ReadinessSnapshot{}, nil
}

func (supplyChainReadAdmissionWitness) SBOMAttestationAttachmentInventory(context.Context, SBOMAttestationAttachmentAggregateFilter, SBOMAttestationAttachmentInventoryDimension, int, int) ([]SBOMAttestationAttachmentInventoryRow, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) SecurityAlertCollectorConfigurations(context.Context) ([]string, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) SecurityAlertReconciliationInventory(context.Context, SecurityAlertReconciliationAggregateFilter, SecurityAlertReconciliationInventoryDimension, int, int) ([]SecurityAlertReconciliationInventoryRow, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) SecurityAlertReconciliationQueueStates(context.Context, []string) ([]SecurityAlertReconciliationQueueState, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) SecurityAlertTargetStates(context.Context, []string, []string) ([]SecurityAlertTargetState, error) {
	return nil, nil
}

func (supplyChainReadAdmissionWitness) SupplyChainImpactGroupedInventory(context.Context, impact.AggregateFilter, impact.GroupedInventoryDimension, int, int) (impact.GroupedInventoryPage, error) {
	return impact.GroupedInventoryPage{}, nil
}

func (supplyChainReadAdmissionWitness) SupplyChainImpactInventory(context.Context, impact.AggregateFilter, impact.InventoryDimension, int, int) (impact.AggregateInventoryPage, error) {
	return impact.AggregateInventoryPage{}, nil
}

var _ SupplyChainReadAdmission = supplyChainReadAdmissionWitness{}

// TestSupplyChainReadAdmissionWireShapesAreSealed proves the seal on the
// pin: exactly the 23 embedded narrow family reads, with no suppression
// mutation, no shared graph/content port, and no snapshot/ledger surface
// riding along. The frozen method-set contract test pins the exact
// names; this test pins the exclusion the arbiter ruling demands.
func TestSupplyChainReadAdmissionWireShapesAreSealed(t *testing.T) {
	t.Parallel()

	rt := reflect.TypeOf((*SupplyChainReadAdmission)(nil)).Elem()
	if rt.NumMethod() != 23 {
		t.Fatalf("SupplyChainReadAdmission has %d methods, want exactly 23: the seal forbids any wider surface", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		for _, banned := range []string{"Suppress", "Mutat", "Append", "Write", "Delete", "Create", "Update", "Graph", "Snapshot", "Ledger", "Tenant", "Exec", "Revert", "Enable", "Disable", "Migrate", "Backfill"} {
			if strings.Contains(name, banned) {
				t.Errorf("SupplyChainReadAdmission.%s contains %q: suppression, mutation, and shared-port legs stay out of this pin", name, banned)
			}
		}
	}
}

// seededSupplyChainDrift is a deliberately widened copy of the pin: the
// 23 frozen reads plus a suppression write the read legs never issue. It
// never ships; it proves the frozen-set comparison below can see a
// one-method widening.
type seededSupplyChainDrift interface {
	SupplyChainReadAdmission
	WriteSuppressionLedger(context.Context) error
}

// TestSupplyChainReadAdmissionSeededDriftIsDetected is the RED half of
// the seeded-drift pair: the same reflect comparison the contract test
// runs must report a mismatch on the planted widening. It passes on a
// clean tree precisely because the guard fires on drift — a guard that
// cannot see the seeded method is no guard.
func TestSupplyChainReadAdmissionSeededDriftIsDetected(t *testing.T) {
	t.Parallel()

	got := supplyChainReadAdmissionMethodSet(t, (*seededSupplyChainDrift)(nil))
	if reflect.DeepEqual(got, frozenSupplyChainReadAdmission) {
		t.Fatal("seeded widening (WriteSuppressionLedger) matches the frozen set: the comparison cannot see drift")
	}
	if len(got) != len(frozenSupplyChainReadAdmission)+1 {
		t.Fatalf("seeded drift method set len = %d, want frozen len + 1 (%d): the seed is malformed", len(got), len(frozenSupplyChainReadAdmission)+1)
	}
	for _, want := range frozenSupplyChainReadAdmission {
		found := false
		for _, have := range got {
			if have == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("seeded drift dropped frozen entry %q: the seed must only widen, never narrow", want)
		}
	}
}
