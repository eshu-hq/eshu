// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errSupplyWireShapeProbe is the sentinel the failing guarded store below
// answers every read with. A non-nil error (never a panic, never a
// confident empty result) is the fail-closed proof the
// satellite-consumable constructor wires the guarded port through.
var errSupplyWireShapeProbe = errors.New("supply wire-shape probe: store unavailable")

// errSupplyWireShapeStore is a guarded read store that answers every read
// with the probe error. It implements the db.ReadStore surface the
// Postgres readers need, so constructing the admission satisfier over it
// proves the satellite path needs nothing wider than the request-scoped
// ReadStore handle the API wiring holds.
type errSupplyWireShapeStore struct{ err error }

func (s errSupplyWireShapeStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, s.err
}

func (s errSupplyWireShapeStore) QueryRowContext(context.Context, string, ...any) db.Row {
	return errSupplyWireShapeRow(s)
}

func (s errSupplyWireShapeStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, s.err
}

type errSupplyWireShapeRow errSupplyWireShapeStore

func (r errSupplyWireShapeRow) Scan(...any) error { return r.err }

var (
	_ db.ReadStore = errSupplyWireShapeStore{}
	_ db.Row       = errSupplyWireShapeRow{}
)

// TestSupplyChainReadAdmissionWireShapesAreSatelliteConsumable proves the
// wire shape behind the #9669 D6 SupplyChainReadAdmission pin: the
// WithReadStore constructor serves all seventeen family ports over
// nothing wider than the guarded ReadStore the satellite holds. Every
// read fails closed (never panics, never returns a confident empty
// result with a nil error) when the guarded store is unavailable.
//
// Arbiter Decision 2 parity note: the finding, aggregate, and inventory
// reads below carry SuppressionState/IncludeSuppressed and the
// reconciliation reads carry provider-state/reconciliation-status, so
// the proof exercises the exact suppression-state filters the finding
// and reconciliation reads consult inside reader-store SQL — the
// filters travel through the pin-shaped ports, never the suppressions
// mutation port.
func TestSupplyChainReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	admission := NewSupplyChainReadAdmissionWithReadStore(errSupplyWireShapeStore{err: errSupplyWireShapeProbe}, true, nil)
	if admission == nil {
		t.Fatal("NewSupplyChainReadAdmissionWithReadStore() = nil, want a SupplyChainReadAdmission bound to the guarded store")
	}
	if NewSupplyChainReadAdmissionWithReadStore(nil, true, nil) != nil {
		t.Error("NewSupplyChainReadAdmissionWithReadStore(nil, ...) = non-nil, want nil: an unwired store must fail closed")
	}

	ctx := context.Background()
	suppressed := impact.FindingFilter{
		RepositoryID: "repo-wire-shape", SuppressionState: "accepted_risk", IncludeSuppressed: true, Limit: 10,
	}
	active := impact.FindingFilter{
		RepositoryID: "repo-wire-shape", SuppressionState: "active", Limit: 10,
	}
	agg := impact.AggregateFilter{RepositoryID: "repo-wire-shape", SuppressionState: "accepted_risk", IncludeSuppressed: true}
	recon := supplychain.SecurityAlertReconciliationFilter{
		RepositoryID: "repo-wire-shape", ProviderState: "open", ReconciliationStatus: "pending",
	}
	reconAgg := supplychain.SecurityAlertReconciliationAggregateFilter{
		RepositoryID: "repo-wire-shape", ProviderState: "open", ReconciliationStatus: "pending",
	}

	type readCase struct {
		name string
		call func() error
	}
	cases := []readCase{
		{"ListSupplyChainImpactFindings/suppressed", func() error {
			_, err := admission.ListSupplyChainImpactFindings(ctx, suppressed)
			return err
		}},
		{"ListSupplyChainImpactFindings/active", func() error {
			_, err := admission.ListSupplyChainImpactFindings(ctx, active)
			return err
		}},
		{"CountSupplyChainImpactFindings", func() error {
			_, err := admission.CountSupplyChainImpactFindings(ctx, agg)
			return err
		}},
		{"SupplyChainImpactInventory", func() error {
			_, err := admission.SupplyChainImpactInventory(ctx, agg, impact.InventoryByStatus, 10, 0)
			return err
		}},
		{"SupplyChainImpactGroupedInventory", func() error {
			_, err := admission.SupplyChainImpactGroupedInventory(ctx, agg, impact.GroupedInventoryByService, 10, 0)
			return err
		}},
		{"ExplainSupplyChainImpact", func() error {
			_, err := admission.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{RepositoryID: "repo-wire-shape"})
			return err
		}},
		{"ReadSupplyChainImpactReadiness", func() error {
			_, err := admission.ReadSupplyChainImpactReadiness(ctx, impact.ReadinessQuery{RepositoryID: "repo-wire-shape"})
			return err
		}},
		{"ListSBOMAttestationAttachments", func() error {
			_, err := admission.ListSBOMAttestationAttachments(ctx, supplychain.SBOMAttestationAttachmentFilter{RepositoryID: "repo-wire-shape"})
			return err
		}},
		{"CountSBOMAttestationAttachments", func() error {
			_, err := admission.CountSBOMAttestationAttachments(ctx, supplychain.SBOMAttestationAttachmentAggregateFilter{RepositoryID: "repo-wire-shape"})
			return err
		}},
		{"SBOMAttestationAttachmentInventory", func() error {
			_, err := admission.SBOMAttestationAttachmentInventory(ctx, supplychain.SBOMAttestationAttachmentAggregateFilter{RepositoryID: "repo-wire-shape"}, supplychain.SBOMAttestationAttachmentInventoryByAttachmentStatus, 10, 0)
			return err
		}},
		{"ListContainerImageIdentities", func() error {
			_, err := admission.ListContainerImageIdentities(ctx, supplychain.ContainerImageIdentityFilter{RepositoryID: "repo-wire-shape"})
			return err
		}},
		{"CountContainerImageIdentities", func() error {
			_, err := admission.CountContainerImageIdentities(ctx, supplychain.ContainerImageIdentityAggregateFilter{RepositoryID: "repo-wire-shape"})
			return err
		}},
		{"ContainerImageIdentityInventory", func() error {
			_, err := admission.ContainerImageIdentityInventory(ctx, supplychain.ContainerImageIdentityAggregateFilter{RepositoryID: "repo-wire-shape"}, supplychain.ContainerImageIdentityInventoryByOutcome, 10, 0)
			return err
		}},
		{"ListSecurityAlertReconciliations", func() error {
			_, err := admission.ListSecurityAlertReconciliations(ctx, recon)
			return err
		}},
		{"CountSecurityAlertReconciliations", func() error {
			_, err := admission.CountSecurityAlertReconciliations(ctx, reconAgg)
			return err
		}},
		{"SecurityAlertReconciliationInventory", func() error {
			_, err := admission.SecurityAlertReconciliationInventory(ctx, reconAgg, supplychain.SecurityAlertReconciliationInventoryByStatus, 10, 0)
			return err
		}},
		{"SecurityAlertCollectorConfigurations", func() error {
			_, err := admission.SecurityAlertCollectorConfigurations(ctx)
			return err
		}},
		{"SecurityAlertReconciliationQueueStates", func() error {
			_, err := admission.SecurityAlertReconciliationQueueStates(ctx, []string{"repo-wire-shape"})
			return err
		}},
		{"SecurityAlertTargetStates", func() error {
			_, err := admission.SecurityAlertTargetStates(ctx, []string{"repo-wire-shape"}, []string{"target-wire-shape"})
			return err
		}},
		{"ListAdvisoryCatalog", func() error {
			_, err := admission.ListAdvisoryCatalog(ctx, advisory.CatalogFilter{Query: "CVE-wire-shape"})
			return err
		}},
		{"ListAdvisoryEvidence", func() error {
			_, err := admission.ListAdvisoryEvidence(ctx, advisory.EvidenceFilter{RepositoryID: "repo-wire-shape"})
			return err
		}},
		{"CurrentAuthorizedCloudResourceUIDs", func() error {
			_, err := admission.CurrentAuthorizedCloudResourceUIDs(ctx, []string{"uid-wire-shape"}, true, nil, nil)
			return err
		}},
		{"CurrentAuthorizedCloudResourcesByDigest", func() error {
			_, err := admission.CurrentAuthorizedCloudResourcesByDigest(ctx, []string{"sha256:wire-shape"}, true, nil, nil)
			return err
		}},
		{"CurrentAuthorizedKubernetesRuntimeWorkloads", func() error {
			// A non-empty candidate batch: the empty-candidates
			// short-circuit answers an exact empty set without touching
			// the store, so only a real batch proves the SQL path fails
			// closed.
			_, err := admission.CurrentAuthorizedKubernetesRuntimeWorkloads(ctx, []supplychain.KubernetesRuntimeCandidate{{WorkloadUID: "uid-wire-shape", Digest: "sha256:wire-shape"}}, true, nil, nil)
			return err
		}},
	}
	for _, tc := range cases {
		if err := tc.call(); err == nil {
			t.Errorf("%s over an unavailable store = nil error, want a fail-closed error", tc.name)
		}
	}
}

// TestSupplyChainReadAdmissionMirrorStaysHonest proves the production
// composition behind the pin answers the same transports the handler
// legs admit through: the composed admission satisfies the D6 pin and
// every handler-facing narrow port it embeds, so satellite consumption
// moves no read off the shape the routes already admit through.
func TestSupplyChainReadAdmissionMirrorStaysHonest(t *testing.T) {
	t.Parallel()

	admission := NewSupplyChainReadAdmissionWithReadStore(errSupplyWireShapeStore{err: errSupplyWireShapeProbe}, true, nil)
	var _ supplychain.SupplyChainReadAdmission = (*supplyChainReadAdmission)(nil)
	var _ supplychain.SBOMAttestationAttachmentStore = admission
	var _ supplychain.SBOMAttestationAttachmentAggregateStore = admission
	var _ supplychain.ContainerImageIdentityStore = admission
	var _ supplychain.ContainerImageIdentityAggregateStore = admission
	var _ supplychain.SecurityAlertReconciliationStore = admission
	var _ supplychain.SecurityAlertReconciliationAggregateStore = admission
	var _ supplychain.SecurityAlertCollectionStateStore = admission
	var _ supplychain.CloudResourceCurrentInventoryFilter = admission
	var _ supplychain.CloudResourceRuntimeDigestResolver = admission
	var _ supplychain.KubernetesWorkloadCurrentInventoryFilter = admission
	var _ impact.FindingStore = admission
	var _ impact.AggregateStore = admission
	var _ impact.GroupedInventoryStore = admission
	var _ impact.ExplanationStore = admission
	var _ impact.ReadinessStore = admission
	var _ advisory.EvidenceStore = admission
	var _ advisory.CatalogStore = admission
}
