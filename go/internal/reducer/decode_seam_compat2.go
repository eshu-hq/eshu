// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// This file is the transitional compatibility surface for the per-fact-kind
// decoders that moved to [schemadecode] (issue #6061). Every entry binds the
// reducer root's original lowercase spelling to the exported name in that
// package, so the 45 root call sites keep their current spelling; each entry is
// deleted once its last caller has moved into a family subpackage. The 17
// decodeObservability* entries were removed when their only callers moved into
// internal/reducer/obscoverage, and the three Vault entries when theirs moved
// into internal/reducer/secretsiam; both subpackages import schemadecode directly.

var (
	decodeRDSInstancePosture                   = schemadecode.DecodeRDSInstancePosture
	decodeReducerPackageConsumptionCorrelation = schemadecode.DecodeReducerPackageConsumptionCorrelation
	decodeReducerPackageOwnershipCorrelation   = schemadecode.DecodeReducerPackageOwnershipCorrelation
	decodeReducerPackagePublicationCorrelation = schemadecode.DecodeReducerPackagePublicationCorrelation
	decodeS3BucketPosture                      = schemadecode.DecodeS3BucketPosture
	decodeS3ExternalPrincipalGrant             = schemadecode.DecodeS3ExternalPrincipalGrant
	decodeScannerWorkerAnalysis                = schemadecode.DecodeScannerWorkerAnalysis
	decodeSubmodulePin                         = schemadecode.DecodeSubmodulePin
	decodeVulnerabilityAffectedPackage         = schemadecode.DecodeVulnerabilityAffectedPackage
	decodeVulnerabilityAffectedProduct         = schemadecode.DecodeVulnerabilityAffectedProduct
	decodeVulnerabilityCVE                     = schemadecode.DecodeVulnerabilityCVE
	decodeVulnerabilityEPSSScore               = schemadecode.DecodeVulnerabilityEPSSScore
	decodeVulnerabilityGoCallReachability      = schemadecode.DecodeVulnerabilityGoCallReachability
	decodeVulnerabilityGoModuleEvidence        = schemadecode.DecodeVulnerabilityGoModuleEvidence
	decodeVulnerabilityKnownExploited          = schemadecode.DecodeVulnerabilityKnownExploited
	decodeVulnerabilityOSPackage               = schemadecode.DecodeVulnerabilityOSPackage
)
