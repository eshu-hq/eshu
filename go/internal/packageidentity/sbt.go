// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import "strings"

// SBT cross-version labels the sbt parser writes to sbt_cross_version
// (go/internal/parser/sbt/element.go). Only crossNone and crossBinary name a
// Maven coordinate; the others are deliberately keyless.
const (
	sbtPackageManager = "sbt"
	sbtCrossNone      = "none"
	sbtCrossBinary    = "binary"
)

// ManifestKeyInput is one manifest dependency row's join inputs: the package
// manager, the dependency name forms, and the sbt-only fields that decide
// which published Maven artifact an sbt row names.
type ManifestKeyInput struct {
	// PackageManager is the row's package_manager value, such as "npm",
	// "gradle", or "sbt".
	PackageManager string
	// Names are the dependency name forms the row can join on.
	Names []string
	// SBTCrossVersion is the row's sbt_cross_version label. It is read only
	// when PackageManager is sbt.
	SBTCrossVersion string
	// SBTBinaryArtifact is the row's sbt_binary_artifact, the
	// organization:name_<binary> coordinate the sbt parser derived when one
	// Scala binary version was known. It is read only for a binary row.
	SBTBinaryArtifact string
}

// ManifestConsumptionKeys returns the registry-identity join keys for one
// manifest dependency row. Every package manager but sbt goes through
// ConsumptionKeys unchanged. An sbt row joins the Maven identity of the
// artifact it names, never of the unsuffixed organization:name that `%%`
// rows carry in their name:
//
//   - `%` (none) names the exact Maven coordinate, so the row joins Names.
//   - `%%` (binary) joins only SBTBinaryArtifact, and joins nothing when the
//     parser could not derive one (no or conflicting scalaVersion, a
//     pre-release, a Scala release before 2.10).
//   - `%%%` (scalajs), a custom `cross`, and a missing label join nothing:
//     the artifact suffix is not known from the manifest alone.
//
// NormalizeEcosystem("sbt") stays empty on purpose: an alias would let any
// caller that passes the unsuffixed name misjoin, so sbt resolves only here.
func ManifestConsumptionKeys(input ManifestKeyInput) []ConsumptionKey {
	if !isSBTPackageManager(input.PackageManager) {
		return ConsumptionKeys(input.PackageManager, input.Names...)
	}
	switch strings.TrimSpace(input.SBTCrossVersion) {
	case sbtCrossNone:
		return ConsumptionKeys(string(EcosystemMaven), input.Names...)
	case sbtCrossBinary:
		artifact := strings.TrimSpace(input.SBTBinaryArtifact)
		if artifact == "" {
			return nil
		}
		return ConsumptionKeys(string(EcosystemMaven), artifact)
	}
	return nil
}

// ManifestPackageManagersForEcosystem returns the sorted package_manager
// values whose manifest rows can join a registry identity of ecosystem. It is
// a read prefilter, not an identity rule: ManifestConsumptionKeys is the
// accuracy gate for every row it admits. Maven identities are declared by
// Maven, Gradle, and sbt manifests.
func ManifestPackageManagersForEcosystem(ecosystem string) []string {
	normalized := NormalizeEcosystem(Ecosystem(ecosystem))
	if normalized == EcosystemMaven {
		return []string{"gradle", string(EcosystemMaven), sbtPackageManager}
	}
	if normalized == EcosystemGoModule {
		// go.mod rows use the parser's package_manager="go". Keep gomod
		// for existing normalized rows; the join still checks identity.
		return []string{"go", string(EcosystemGoModule)}
	}
	return []string{ecosystem}
}

func isSBTPackageManager(packageManager string) bool {
	return LowerASCII(strings.TrimSpace(packageManager)) == sbtPackageManager
}
