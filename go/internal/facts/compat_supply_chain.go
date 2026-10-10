// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// This file is the facts root's transitional compatibility surface for the
// supply-chain fact families, which moved to [chain] in issue #6776 so the
// root directory drops back under the 40-file dirgate cap. Every entry is an
// alias or a thin forwarder with no behavior change: the value, the type
// identity, and the returned bytes are the same ones the root declared
// before the move.
//
// It carries only the names that still have a caller -- the facts root's own
// schemaVersionFamilies registry wiring and registry tests plus the
// collectors, reducers, projectors, and query surfaces that reach them as
// facts.X today. A later family move adds a stanza to this file and never
// creates a new compat_*.go, and each entry here is deleted once its last
// caller has moved to the chain package directly (see the importer-migration
// follow-up, #6950).

import "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"

// Stanza: vulnerability_intelligence.go (moved to chain/vulnerability_intelligence.go).
const (
	// VulnerabilityAffectedPackageFactKind identifies package-native advisory
	// applicability evidence. See [chain.VulnerabilityAffectedPackageFactKind].
	VulnerabilityAffectedPackageFactKind = chain.VulnerabilityAffectedPackageFactKind
	// VulnerabilityAffectedProductFactKind identifies source-reported product or
	// CPE applicability evidence. See
	// [chain.VulnerabilityAffectedProductFactKind].
	VulnerabilityAffectedProductFactKind = chain.VulnerabilityAffectedProductFactKind
	// VulnerabilityCVEFactKind identifies one CVE or advisory identity record.
	// See [chain.VulnerabilityCVEFactKind].
	VulnerabilityCVEFactKind = chain.VulnerabilityCVEFactKind
	// VulnerabilityEPSSScoreFactKind identifies one EPSS score observation. See
	// [chain.VulnerabilityEPSSScoreFactKind].
	VulnerabilityEPSSScoreFactKind = chain.VulnerabilityEPSSScoreFactKind
	// VulnerabilityGoCallReachabilityFactKind identifies one govulncheck-compat
	// reachability observation for a Go advisory. The payload preserves the
	// govulncheck JSON evidence (osv id, module, package, symbol, call stack,
	// reachable flag) so reducers can classify call-graph reachability without
	// re-running govulncheck. See
	// [chain.VulnerabilityGoCallReachabilityFactKind].
	VulnerabilityGoCallReachabilityFactKind = chain.VulnerabilityGoCallReachabilityFactKind
	// VulnerabilityGoModuleEvidenceFactKind identifies one Go-module requirement
	// observed in a repository's go.mod (with optional go.sum hash anchor) so
	// reducers can correlate Go vulnerability advisories to a repo-anchored
	// module path, required version, replacement target, and indirect flag. See
	// [chain.VulnerabilityGoModuleEvidenceFactKind].
	VulnerabilityGoModuleEvidenceFactKind = chain.VulnerabilityGoModuleEvidenceFactKind
	// VulnerabilityIntelligenceSchemaVersionV1 is the first vulnerability
	// intelligence fact schema. See
	// [chain.VulnerabilityIntelligenceSchemaVersionV1].
	VulnerabilityIntelligenceSchemaVersionV1 = chain.VulnerabilityIntelligenceSchemaVersionV1
	// VulnerabilityKnownExploitedFactKind identifies one CISA KEV observation.
	// See [chain.VulnerabilityKnownExploitedFactKind].
	VulnerabilityKnownExploitedFactKind = chain.VulnerabilityKnownExploitedFactKind
	// VulnerabilityOSPackageFactKind identifies one installed OS package
	// observation. Carries distro, package manager, epoch/version/release, arch,
	// source package, repository class, and vendor advisory source so reducers
	// can match vendor advisories against installed evidence without promoting
	// upstream fixed-version strings to backported builds. See
	// [chain.VulnerabilityOSPackageFactKind].
	VulnerabilityOSPackageFactKind = chain.VulnerabilityOSPackageFactKind
	// VulnerabilityReferenceFactKind identifies one source advisory reference.
	// See [chain.VulnerabilityReferenceFactKind].
	VulnerabilityReferenceFactKind = chain.VulnerabilityReferenceFactKind
	// VulnerabilitySourceSnapshotFactKind identifies one vulnerability source
	// observation boundary and freshness checkpoint. See
	// [chain.VulnerabilitySourceSnapshotFactKind].
	VulnerabilitySourceSnapshotFactKind = chain.VulnerabilitySourceSnapshotFactKind
	// VulnerabilityWarningFactKind identifies non-fatal vulnerability source
	// collection or normalization warnings. See
	// [chain.VulnerabilityWarningFactKind].
	VulnerabilityWarningFactKind = chain.VulnerabilityWarningFactKind
)

// VulnerabilityIntelligenceFactKinds returns the accepted vulnerability
// intelligence fact kinds in source-contract order. See
// [chain.VulnerabilityIntelligenceFactKinds].
func VulnerabilityIntelligenceFactKinds() []string {
	return chain.VulnerabilityIntelligenceFactKinds()
}

// VulnerabilityIntelligenceSchemaVersion returns the schema version for a
// vulnerability intelligence fact kind. See
// [chain.VulnerabilityIntelligenceSchemaVersion].
func VulnerabilityIntelligenceSchemaVersion(factKind string) (string, bool) {
	return chain.VulnerabilityIntelligenceSchemaVersion(factKind)
}
