// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

// This file preserves the root package query surface cmd/api,
// cmd/mcp-server, the staying supply-chain handlers, probes, and tests still
// use for the impact read-model types. The declarations moved to
// internal/query/supplychain/impact (#6060 lane A); these aliases forward
// unchanged so the staying call sites compile without touching other lanes.
// The hub PR3 owns the final alias surface for this family (store and
// constructor aliases join here when the handlers move); keep this file to
// the type-level compatibility the staying probes and rows need until then.

// SupplyChainRuntimeContext is one repository's read-time-resolved runtime
// context. See impact.SupplyChainRuntimeContext.
type SupplyChainRuntimeContext = impact.SupplyChainRuntimeContext

// SupplyChainRuntimeContextResult is the response-side runtime-context
// envelope attached to one impact finding. See
// impact.SupplyChainRuntimeContextResult.
type SupplyChainRuntimeContextResult = impact.SupplyChainRuntimeContextResult

// SupplyChainRuntimeEnvironmentEvidenceProbe describes the bounded current
// confirmation work for one finding's environment candidates. See
// impact.SupplyChainRuntimeEnvironmentEvidenceProbe.
type SupplyChainRuntimeEnvironmentEvidenceProbe = impact.SupplyChainRuntimeEnvironmentEvidenceProbe

// KubernetesRuntimeWorkloadRef is one current, authorized Kubernetes workload
// observed running a finding's exact subject digest. See
// impact.KubernetesRuntimeWorkloadRef.
type KubernetesRuntimeWorkloadRef = impact.KubernetesRuntimeWorkloadRef

// KubernetesRuntimeProbeMetadata describes the bounded, page-weighted
// digest-local candidate budget. See impact.KubernetesRuntimeProbeMetadata.
type KubernetesRuntimeProbeMetadata = impact.KubernetesRuntimeProbeMetadata

// SupplyChainImpactProfilePrecise selects exact installed-version
// anchored findings only. See impact.SupplyChainImpactProfilePrecise.
const SupplyChainImpactProfilePrecise = impact.SupplyChainImpactProfilePrecise

// SupplyChainImpactProfileComprehensive selects every owned-anchor
// finding including range-only manifest, SBOM/CPE-derived,
// malformed range, and missing-version rows. See
// impact.SupplyChainImpactProfileComprehensive.
const SupplyChainImpactProfileComprehensive = impact.SupplyChainImpactProfileComprehensive

// SupplyChainRuntimeEnvironmentCandidate identifies one finding-bound
// digest/environment pair that must be revalidated against current accepted
// CI/CD correlation facts before it can enter read-time runtime_context.
// See impact.SupplyChainRuntimeEnvironmentCandidate.
type SupplyChainRuntimeEnvironmentCandidate = impact.SupplyChainRuntimeEnvironmentCandidate

// VulnerabilitySuppressionMutationResult identifies the durable generation
// containing an operator suppression. See
// impact.VulnerabilitySuppressionMutationResult.
type VulnerabilitySuppressionMutationResult = impact.VulnerabilitySuppressionMutationResult

// The remaining aliases preserve the root package query surface cmd/api,
// cmd/mcp-server, internal/serviceintelhttp, internal/cli, and
// internal/storage tests still use for the impact read models. The
// declarations moved to internal/query/supplychain/impact (#6060 lane A);
// these aliases forward unchanged so those call sites compile without
// touching other lanes.

type (
	SupplyChainImpactAggregateCount               = impact.SupplyChainImpactAggregateCount
	SupplyChainImpactAggregateFilter              = impact.SupplyChainImpactAggregateFilter
	SupplyChainImpactAggregateStore               = impact.SupplyChainImpactAggregateStore
	SupplyChainImpactEvidenceFactSummary          = impact.SupplyChainImpactEvidenceFactSummary
	SupplyChainImpactExplanationAnchors           = impact.SupplyChainImpactExplanationAnchors
	SupplyChainImpactExplanationFilter            = impact.SupplyChainImpactExplanationFilter
	SupplyChainImpactExplanationFreshness         = impact.SupplyChainImpactExplanationFreshness
	SupplyChainImpactExplanationResult            = impact.SupplyChainImpactExplanationResult
	SupplyChainImpactFindingFilter                = impact.SupplyChainImpactFindingFilter
	SupplyChainImpactFindingResult                = impact.SupplyChainImpactFindingResult
	SupplyChainImpactFindingRow                   = impact.SupplyChainImpactFindingRow
	SupplyChainImpactFindingStore                 = impact.SupplyChainImpactFindingStore
	SupplyChainImpactInventoryDimension           = impact.SupplyChainImpactInventoryDimension
	SupplyChainImpactInventoryRow                 = impact.SupplyChainImpactInventoryRow
	SupplyChainImpactPathHop                      = impact.SupplyChainImpactPathHop
	SupplyChainImpactReadinessEnvelope            = impact.SupplyChainImpactReadinessEnvelope
	PostgresSupplyChainImpactFindingStore         = impact.PostgresSupplyChainImpactFindingStore
	PostgresSupplyChainImpactAggregateStore       = impact.PostgresSupplyChainImpactAggregateStore
	PostgresSupplyChainImpactReadinessStore       = impact.PostgresSupplyChainImpactReadinessStore
	PostgresVulnerabilitySuppressionMutationStore = impact.PostgresVulnerabilitySuppressionMutationStore
)

const (
	SupplyChainImpactAggregateMaxLimit       = impact.SupplyChainImpactAggregateMaxLimit
	ReadinessStateReadyWithFindings          = impact.ReadinessStateReadyWithFindings
	SupplyChainImpactInventoryByImpactStatus = impact.SupplyChainImpactInventoryByImpactStatus
	SupplyChainImpactWinnersReadEnv          = impact.SupplyChainImpactWinnersReadEnv
)

func SupplyChainImpactWinnersReadEnabled(value string) bool {
	return impact.SupplyChainImpactWinnersReadEnabled(value)
}

func NewPostgresSupplyChainImpactFindingStore(db impact.SupplyChainImpactFindingQueryer) PostgresSupplyChainImpactFindingStore {
	return impact.NewPostgresSupplyChainImpactFindingStore(db)
}

func NewPostgresSupplyChainImpactFindingStoreWithReadModel(db impact.SupplyChainImpactFindingQueryer, readFromWinners bool) PostgresSupplyChainImpactFindingStore {
	return impact.NewPostgresSupplyChainImpactFindingStoreWithReadModel(db, readFromWinners)
}

func NewPostgresSupplyChainImpactAggregateStore(db impact.SupplyChainImpactAggregateQueryer) PostgresSupplyChainImpactAggregateStore {
	return impact.NewPostgresSupplyChainImpactAggregateStore(db)
}

func NewPostgresSupplyChainImpactReadinessStore(db impact.SupplyChainImpactReadinessQueryer) PostgresSupplyChainImpactReadinessStore {
	return impact.NewPostgresSupplyChainImpactReadinessStore(db)
}

func NewPostgresVulnerabilitySuppressionMutationStore(db *sql.DB) *PostgresVulnerabilitySuppressionMutationStore {
	return impact.NewPostgresVulnerabilitySuppressionMutationStore(db)
}

// listSupplyChainImpactReadinessQuery re-exposes the readiness SQL shape
// under its pre-move bare name for the staying root tests. The gocritic
// argOrder heuristic misfires on the qualified impact.X form inside
// strings.Contains assertions (a bare identifier of the same name passes,
// as the container-image query tests show), so the tests keep the exact
// pre-move call shape through this shim. It goes away in hub PR3 when the
// tests move into the impact package with the handlers they drive.
var listSupplyChainImpactReadinessQuery = impact.ListSupplyChainImpactReadinessQuery
