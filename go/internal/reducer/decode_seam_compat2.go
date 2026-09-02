// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// This file is the transitional compatibility surface for the per-fact-kind
// decoders that moved to [schemadecode] (issue #6061). Every entry binds the
// reducer root's original lowercase spelling to the exported name in that
// package, so the 63 root call sites keep their current spelling; each entry is
// deleted once its last caller has moved into a family subpackage.

var (
	decodeOCIImageManifestForIndex             = schemadecode.DecodeOCIImageManifestForIndex
	decodeOCIImageTagObservationForIndex       = schemadecode.DecodeOCIImageTagObservationForIndex
	decodeOCIRegistryWarning                   = schemadecode.DecodeOCIRegistryWarning
	decodeObservabilityAppliedResource         = schemadecode.DecodeObservabilityAppliedResource
	decodeObservabilityAppliedSyncState        = schemadecode.DecodeObservabilityAppliedSyncState
	decodeObservabilityCoverageWarning         = schemadecode.DecodeObservabilityCoverageWarning
	decodeObservabilityDeclaredAlertRule       = schemadecode.DecodeObservabilityDeclaredAlertRule
	decodeObservabilityDeclaredDashboard       = schemadecode.DecodeObservabilityDeclaredDashboard
	decodeObservabilityDeclaredDatasource      = schemadecode.DecodeObservabilityDeclaredDatasource
	decodeObservabilityDeclaredFolder          = schemadecode.DecodeObservabilityDeclaredFolder
	decodeObservabilityDeclaredLogRoute        = schemadecode.DecodeObservabilityDeclaredLogRoute
	decodeObservabilityDeclaredMetricRoute     = schemadecode.DecodeObservabilityDeclaredMetricRoute
	decodeObservabilityDeclaredMetricRule      = schemadecode.DecodeObservabilityDeclaredMetricRule
	decodeObservabilityDeclaredScrapeConfig    = schemadecode.DecodeObservabilityDeclaredScrapeConfig
	decodeObservabilityDeclaredTraceRoute      = schemadecode.DecodeObservabilityDeclaredTraceRoute
	decodeObservabilityObservedDashboard       = schemadecode.DecodeObservabilityObservedDashboard
	decodeObservabilityObservedLogSignal       = schemadecode.DecodeObservabilityObservedLogSignal
	decodeObservabilityObservedRule            = schemadecode.DecodeObservabilityObservedRule
	decodeObservabilityObservedTarget          = schemadecode.DecodeObservabilityObservedTarget
	decodeObservabilityObservedTraceSignal     = schemadecode.DecodeObservabilityObservedTraceSignal
	decodeRDSInstancePosture                   = schemadecode.DecodeRDSInstancePosture
	decodeReducerPackageConsumptionCorrelation = schemadecode.DecodeReducerPackageConsumptionCorrelation
	decodeReducerPackageOwnershipCorrelation   = schemadecode.DecodeReducerPackageOwnershipCorrelation
	decodeReducerPackagePublicationCorrelation = schemadecode.DecodeReducerPackagePublicationCorrelation
	decodeS3BucketPosture                      = schemadecode.DecodeS3BucketPosture
	decodeS3ExternalPrincipalGrant             = schemadecode.DecodeS3ExternalPrincipalGrant
	decodeScannerWorkerAnalysis                = schemadecode.DecodeScannerWorkerAnalysis
	decodeSecurityAlertRepositoryAlert         = schemadecode.DecodeSecurityAlertRepositoryAlert
	decodeServiceCatalogEntity                 = schemadecode.DecodeServiceCatalogEntity
	decodeServiceCatalogOwnership              = schemadecode.DecodeServiceCatalogOwnership
	decodeServiceCatalogRepositoryLink         = schemadecode.DecodeServiceCatalogRepositoryLink
	decodeSubmodulePin                         = schemadecode.DecodeSubmodulePin
	decodeVaultACLPolicy                       = schemadecode.DecodeVaultACLPolicy
	decodeVaultAuthRole                        = schemadecode.DecodeVaultAuthRole
	decodeVaultKVMetadata                      = schemadecode.DecodeVaultKVMetadata
	decodeVulnerabilityAffectedPackage         = schemadecode.DecodeVulnerabilityAffectedPackage
	decodeVulnerabilityAffectedProduct         = schemadecode.DecodeVulnerabilityAffectedProduct
	decodeVulnerabilityCVE                     = schemadecode.DecodeVulnerabilityCVE
	decodeVulnerabilityEPSSScore               = schemadecode.DecodeVulnerabilityEPSSScore
	decodeVulnerabilityGoCallReachability      = schemadecode.DecodeVulnerabilityGoCallReachability
	decodeVulnerabilityGoModuleEvidence        = schemadecode.DecodeVulnerabilityGoModuleEvidence
	decodeVulnerabilityKnownExploited          = schemadecode.DecodeVulnerabilityKnownExploited
	decodeVulnerabilityOSPackage               = schemadecode.DecodeVulnerabilityOSPackage
)
