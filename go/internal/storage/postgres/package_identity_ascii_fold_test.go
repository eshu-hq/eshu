// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// storageSimpleLocaleFoldPattern matches a LOWER(TRIM(...)),
// LOWER(BTRIM(...)), or LOWER(COALESCE(...)) fold with no COLLATE inside it,
// that is a fold that follows the database collation.
var storageSimpleLocaleFoldPattern = regexp.MustCompile(`LOWER\((TRIM|BTRIM|COALESCE)\([^()]*\)\)`)

// storageIdentityTokenPattern reports whether a SQL fold expression folds a
// package-identity, ecosystem, distro, or vendor spelling.
var storageIdentityTokenPattern = regexp.MustCompile(`(?i)\b(ecosystem|package_manager|distro|vendor_advisory_source|repository_class|package_name)\b`)

// TestStoragePackageIdentityFoldsASCIIOnly pins the #9622 SQL contract for the
// storage readers: every fold of an ecosystem, distro, package-manager, vendor
// source, or repository-class spelling uses LOWER under COLLATE "C"
// (packageidentity.LowerASCII, the fold the Go normalizers below write),
// never a locale LOWER.
func TestStoragePackageIdentityFoldsASCIIOnly(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"os package advisory targets":      listOSPackageAdvisoryTargetsQuery(),
		"sbom component advisory targets":  listSBOMComponentAdvisoryTargetsQuery(),
		"os package advisory for packages": listOSPackageAdvisoryTargetsForPackagesQuery(),
		"owned package ecosystem raw":      ownedPackageEcosystemExpression(false),
		"owned package ecosystem expanded": ownedPackageEcosystemExpression(true),
		"stored advisory names":            storedAdvisoryNamesQuery,
		"registry metadata target status":  registryMetadataTargetStatusQuery,
		"owned package dependency targets": listOwnedPackageDependencyTargetsQuery(false, false),
		"owned package dependency aliased": listOwnedPackageDependencyTargetsQuery(true, true),
		"manifest dependency active facts": listActivePackageManifestDependencyFactsQuery,
	}

	oldLocaleFold := []string{
		`LOWER(COALESCE(NULLIF(fact.payload->>'vendor_advisory_source', ''), fact.payload->>'distro'))`,
		`LOWER(COALESCE(NULLIF(c.payload->>'vendor_advisory_source', ''), c.payload->>'distro'))`,
		`LOWER(SPLIT_PART(SUBSTRING(COALESCE(component.payload->>'purl', '') FROM 5), '/', 1))`,
		`LOWER(fact.payload->'entity_metadata'->>'package_manager')`,
		`LOWER(BTRIM(COALESCE(fact.payload->>'ecosystem', '')))`,
		`LOWER(BTRIM(fact.payload->>'ecosystem'))`,
		`LOWER(TRIM(fact.payload->>'entity_name'))`,
	}

	for name, sql := range queries {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, old := range oldLocaleFold {
				if strings.Contains(sql, old) {
					t.Errorf("storage SQL keeps locale fold %q; use LOWER(x COLLATE \"C\") COLLATE \"default\"", old)
				}
			}
			for _, got := range storageSimpleLocaleFoldPattern.FindAllString(sql, -1) {
				if storageIdentityTokenPattern.MatchString(got) {
					t.Errorf("storage SQL keeps locale identity fold %q; use LOWER(x COLLATE \"C\") COLLATE \"default\"", got)
				}
			}
			if !strings.Contains(sql, `COLLATE "C") COLLATE "default"`) && storageIdentityTokenPattern.MatchString(sql) {
				t.Errorf("storage SQL folds identity spellings with no ASCII-only fold")
			}
		})
	}
}

// TestNormalizeOSPackageAdvisoryTargetKeepsLookalikesApart pins that the Go
// normalizer folds ASCII letters only: U+0130 (İ) and U+212A (K) spellings
// keep their bytes apart from the ASCII ecosystem instead of joining it.
func TestNormalizeOSPackageAdvisoryTargetKeepsLookalikesApart(t *testing.T) {
	t.Parallel()

	for _, lookalike := range []string{"PİP", "PİK", "pip\u0130", " École "} {
		target := workflow.OSPackageAdvisoryTarget{Ecosystem: lookalike}
		normalizeOSPackageAdvisoryTarget(&target)
		want := packageidentity.LowerASCII(strings.TrimSpace(lookalike))
		if target.Ecosystem != want {
			t.Errorf("normalizeOSPackageAdvisoryTarget(%q) = %q, want %q", lookalike, target.Ecosystem, want)
		}
		if target.Ecosystem == "pip" || target.Ecosystem == "pipi" || target.Ecosystem == "école" {
			t.Errorf("normalizeOSPackageAdvisoryTarget(%q) = %q, joins the ASCII ecosystem", lookalike, target.Ecosystem)
		}
	}

	ascii := workflow.OSPackageAdvisoryTarget{
		Ecosystem:            " NPM ",
		Distro:               " Ubuntu ",
		PackageManager:       " APT ",
		VendorAdvisorySource: " Ubuntu ",
		RepositoryClass:      " Vendor ",
	}
	normalizeOSPackageAdvisoryTarget(&ascii)
	if ascii.Ecosystem != "npm" || ascii.Distro != "ubuntu" || ascii.PackageManager != "apt" ||
		ascii.VendorAdvisorySource != "ubuntu" || ascii.RepositoryClass != "vendor" {
		t.Errorf("normalizeOSPackageAdvisoryTarget ASCII case = %+v, want folded lowercase", ascii)
	}
}

// TestInstalledAdvisoryPURLTypeEcosystemKeepsLookalikesApart pins that the PURL
// type fallback folds ASCII letters only.
func TestInstalledAdvisoryPURLTypeEcosystemKeepsLookalikesApart(t *testing.T) {
	t.Parallel()

	if got := installedAdvisoryPURLTypeEcosystem("PİP"); got == "pip" {
		t.Errorf("installedAdvisoryPURLTypeEcosystem(%q) = %q, joins pip", "PİP", got)
	}
	for raw, want := range map[string]string{
		"golang": "go",
		"GOLANG": "go",
		"gem":    "rubygems",
		"GEM":    "rubygems",
		"maven":  "maven",
		"MAVEN":  "maven",
	} {
		if got := installedAdvisoryPURLTypeEcosystem(raw); got != want {
			t.Errorf("installedAdvisoryPURLTypeEcosystem(%q) = %q, want %q", raw, got, want)
		}
	}
}
