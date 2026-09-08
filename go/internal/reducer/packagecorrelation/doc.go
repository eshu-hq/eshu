// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packagecorrelation holds the package correlation family: the
// package-source (ownership), package-consumption, and package-publication
// correlation builders, their Postgres writers, the repo-dependency intent
// lane, the admission writers, and the manifest-dependency bridge the
// securityalert family matches provider alerts through (issue #6061).
//
// The family reads package-registry package/version facts, source-hint
// facts, repository facts, and manifest/lockfile dependency evidence, and
// writes reducer-derived ownership/consumption/publication correlation facts
// plus repo-to-repo DEPENDS_ON projection intents. Its handler is driven by
// the reducer runtime through the default domain catalog like every other
// family; see the parent package's registry for construction.
//
// Dependency rule: this package imports the shared tier (contract, factload,
// factwrite, crossrepo, sharedintent, payloadcore, admissiondecision,
// packagesourcecore), facts, packageidentity, and telemetry, and the
// already-extracted securityalert subpackage one-way (only its exported
// alert/consumption types). It never imports the parent reducer package, and
// the parent references it only through packagecorrelation-qualified names —
// no compatibility aliases point at this family.
//
// The exported surface is the join contract other families program against:
// manifest-dependency extraction and keys (ExtractPackageManifestDependencies,
// PackageConsumptionKeys, PackageConsumptionNameCandidates,
// PackageRegistryIdentity), owner resolution (ResolvePackageOwners,
// ExtractPackageRegistryIdentities), the security-alert bridge
// (ExtractSecurityAlertManifestConsumptions,
// SecurityAlertPackageNameMatchesDependency,
// SecurityAlertPackageNameMatches), the correlation builders and writers,
// the durable fact-kind consts, and the narrow loader interfaces the
// supply-chain handler shares. Test seams for the reducer root's own test
// files live in package_provenance_root_compat_exports.go, following the
// containerimage precedent.
package packagecorrelation
