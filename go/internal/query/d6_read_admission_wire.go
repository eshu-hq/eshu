// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/kubernetes"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/alerts"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// NewSupplyChainReadAdmissionWithReadStore builds the versioned #9669 D6
// admission satisfier for the SupplyChain family reads: the 18
// SupplyChain READ routes admit through exactly these family reader
// ports. Pass the guarded db.ReadStore the same way the API wiring
// does; the admission serves every pinned read over nothing wider than
// that handle.
//
// The constructor delegates to the existing Postgres store constructors
// deliberately instead of minting parallel wiring: the fail-closed
// proof over a guarded ReadStore plus the frozen method-set contract
// test covers the satellite path end to end, and a second construction
// path would be a second place for the guarded handle to be dropped.
//
// Placement note: this constructor lives in package query rather than
// beside the pin in supply/chain because two of the family ports (the
// cloud-resource inventory filter and the security-alert collection
// state) are built by root-query constructors, and supply/chain cannot
// import the root package back. The pin type stays canonical in
// supply/chain; this file only composes it.
//
// Explicitly OUT (same standing as the pin comment): the suppressions
// mutation row (Never-satellite survivor via SuppressionAdmission),
// the shared GraphQuery/ContentStore ports (blanket-pin forbidden),
// the shared root CollectorListReadinessStore, and the
// ImpactPacketResponder writer. A nil reader yields a nil admission —
// the legs' existing nil-port 503s keep failing closed.
//
// D-wave consumer: D6 (Status + SupplyChain reads, per the arbiter
// D-wave ruling on #9669). Rollback: revert this file. No handler,
// route, or caller depends on it yet, so rollback moves no traffic and
// changes no data.
func NewSupplyChainReadAdmissionWithReadStore(reader db.ReadStore, readImpactFromWinners bool, logger *slog.Logger) supplychain.SupplyChainReadAdmission {
	if reader == nil {
		return nil
	}
	cloudResources := NewPostgresCloudResourceListStoreWithReadStore(reader)
	return &supplyChainReadAdmission{
		SBOMAttestationAttachmentStore:            NewPostgresSBOMAttestationAttachmentStoreWithReadStore(reader),
		SBOMAttestationAttachmentAggregateStore:   NewPostgresSBOMAttestationAttachmentAggregateStoreWithReadStore(reader),
		ContainerImageIdentityStore:               NewPostgresContainerImageIdentityStoreWithReadStore(reader),
		ContainerImageIdentityAggregateStore:      NewPostgresContainerImageIdentityAggregateStoreWithReadStore(reader),
		SecurityAlertReconciliationStore:          alerts.NewPostgresStoreWithReadStore(reader),
		SecurityAlertReconciliationAggregateStore: alerts.NewPostgresAggregateStoreWithReadStore(reader),
		SecurityAlertCollectionStateStore:         NewPostgresSecurityAlertCollectionStateStoreWithReadStore(reader),
		CloudResourceCurrentInventoryFilter:       cloudResources,
		CloudResourceRuntimeDigestResolver:        cloudResources,
		KubernetesWorkloadCurrentInventoryFilter:  kubernetes.NewPostgresRuntimeWorkloadStoreWithReadStore(reader),
		FindingStore:                              impact.NewPostgresFindingStoreWithReadStore(reader, readImpactFromWinners),
		AggregateStore:                            impact.NewPostgresAggregateStoreWithReadStore(reader),
		GroupedInventoryStore:                     impact.NewPostgresGroupedInventoryStoreWithReadStore(reader, readImpactFromWinners, logger),
		ExplanationStore:                          impact.NewPostgresFindingStoreWithReadStore(reader, false),
		ReadinessStore:                            impact.NewPostgresReadinessStoreWithReadStore(reader),
		EvidenceStore:                             advisory.NewPostgresEvidenceStoreWithReadStore(reader),
		CatalogStore:                              advisory.NewPostgresCatalogStoreWithReadStore(reader),
	}
}

// supplyChainReadAdmission is the production composition behind the D6
// supply-chain pin: the seventeen family-owned reader ports, nothing
// wider.
type supplyChainReadAdmission struct {
	supplychain.SBOMAttestationAttachmentStore
	supplychain.SBOMAttestationAttachmentAggregateStore
	supplychain.ContainerImageIdentityStore
	supplychain.ContainerImageIdentityAggregateStore
	supplychain.SecurityAlertReconciliationStore
	supplychain.SecurityAlertReconciliationAggregateStore
	supplychain.SecurityAlertCollectionStateStore
	supplychain.CloudResourceCurrentInventoryFilter
	supplychain.CloudResourceRuntimeDigestResolver
	supplychain.KubernetesWorkloadCurrentInventoryFilter
	impact.FindingStore
	impact.AggregateStore
	impact.GroupedInventoryStore
	impact.ExplanationStore
	impact.ReadinessStore
	advisory.EvidenceStore
	advisory.CatalogStore
}

// The composition satisfies the pin with the minimal shape: the
// seventeen embedded family ports, no shared graph/content port, no
// mutation, no writer.
var _ supplychain.SupplyChainReadAdmission = (*supplyChainReadAdmission)(nil)
