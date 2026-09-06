// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package supplychainmodel owns the shape of the supply-chain-impact family's
// evidence DTOs: the vulnerability advisory, affected-package, consumption,
// SBOM, OS-package, attachment, deployment/workload/service-context, and
// risk-signal rows that [reducer.classifySupplyChainImpactPackage] and its
// helpers read to classify one CVE-x-package finding. [ScopeGenerationKey]
// carries the pure key-derivation helper those rows are joined by.
//
// # Why this is a leaf
//
// The supply_chain_impact + suppression family is 67 files, over the repo's
// 40-file dirgate cap for a single package, and its central handler passes
// these types into six clusters (vulnerability intelligence, SBOM/OCI
// evidence, OS-package evidence, deployment/runtime context, reachability,
// and risk signals). A mechanical split of that family therefore needs a
// shared, cycle-free vocabulary for the DTOs every cluster reads and
// constructs — otherwise each cluster subpackage would need to import the
// reducer root for these shapes, and the root imports the clusters back. This
// package is that vocabulary: plain data only, no method reaches into the
// reducer root, and no orchestration logic lives here.
//
// [reducer.supplyChainImpactIndex], the struct that aggregates these DTOs
// alongside the Go/JS-TS/Python/JVM reachability indexes and the
// container-image-identity type, deliberately stays at the reducer root: those
// four reachability shapes and the image-identity type are out of this
// package's scope, so the aggregate that references them cannot be a leaf
// without dragging them along too. Every reducer-root caller that reads one of
// these types spells the qualified name directly (for example
// [ImpactCVE] as supplychainmodel.ImpactCVE); there is no root-side alias
// under the original unexported names.
package supplychainmodel
