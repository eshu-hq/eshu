// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"slices"
	"strings"
)

// ownedPackageManagerAliasTargets maps a raw dependency-fact package_manager
// value to the canonical ecosystem name the collectors and OSV use. It is the
// single source of truth for both the owned-package reader's SQL selection and
// the derivation's ecosystem mapping, so the two cannot disagree about which
// raw values belong to an ecosystem. A value that is its own canonical name
// (maven, go, npm, ...) is deliberately absent.
//
// Gradle declares Maven coordinates (group:artifact), and OSV publishes their
// advisories under the Maven ecosystem. The Go module spellings come from
// older normalized rows; the go.mod parser itself emits "go".
//
// This table is separate from packageidentity.NormalizeEcosystem and
// packageidentity.ManifestPackageManagersForEcosystem on purpose: OSV names the
// Go ecosystem "go" where packageidentity uses "gomod", and sbt must not be
// aliased to maven because its unsuffixed coordinates are not published
// artifact names (see packageidentity/sbt.go). Change the tables together.
var ownedPackageManagerAliasTargets = map[string]string{
	"gradle":    "maven",
	"gomod":     "go",
	"go-module": "go",
	"go_module": "go",
}

// OwnedPackageManagerAlias pairs one raw package_manager alias with the
// canonical ecosystem it reports under.
type OwnedPackageManagerAlias struct {
	Alias     string
	Canonical string
}

// CanonicalOwnedPackageEcosystem returns the canonical ecosystem for a raw
// package_manager value: a known alias maps to its target, anything else is
// returned lowercased and trimmed. It never invents an ecosystem, so a value
// outside the configured set still fails the caller's membership check.
func CanonicalOwnedPackageEcosystem(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if canonical, ok := ownedPackageManagerAliasTargets[value]; ok {
		return canonical
	}
	return value
}

// OwnedPackageManagerAliases returns the sorted raw package_manager values that
// belong to ecosystem, including the canonical name itself. A name that is an
// alias is resolved to its canonical ecosystem first, so "gradle" and "maven"
// both return [gradle maven].
func OwnedPackageManagerAliases(ecosystem string) []string {
	canonical := CanonicalOwnedPackageEcosystem(ecosystem)
	if canonical == "" {
		return nil
	}
	values := []string{canonical}
	for alias, target := range ownedPackageManagerAliasTargets {
		if target == canonical {
			values = append(values, alias)
		}
	}
	slices.Sort(values)
	return values
}

// OwnedPackageManagerAliasTable returns every known alias with its canonical
// ecosystem, sorted by alias, for callers that must apply the mapping where Go
// code cannot, such as inside a SQL expression.
func OwnedPackageManagerAliasTable() []OwnedPackageManagerAlias {
	table := make([]OwnedPackageManagerAlias, 0, len(ownedPackageManagerAliasTargets))
	for alias, canonical := range ownedPackageManagerAliasTargets {
		table = append(table, OwnedPackageManagerAlias{Alias: alias, Canonical: canonical})
	}
	slices.SortFunc(table, func(a, b OwnedPackageManagerAlias) int {
		return strings.Compare(a.Alias, b.Alias)
	})
	return table
}
