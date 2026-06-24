// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsMatchesGoModuleRangeAndReachability(t *testing.T) {
	t.Parallel()

	packageID := "gomod://proxy.golang.org/golang.org/x/text"
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("go-cve-1", "GO-2022-1059", 7.5),
		vulnerabilityAffectedPackageGoRangeFact(
			"go-affected-1",
			"GO-2022-1059",
			packageID,
			"golang.org/x/text",
			"0.3.8",
		),
		packageConsumptionFactWithRange("go-consume-1", packageID, testImpactRepositoryID, "v0.3.7"),
		goModuleEvidenceEnvelope(t, testImpactRepositoryID, "go.mod", "golang.org/x/text", "v0.3.7", false, "", ""),
		goReachabilityEnvelope(t, testImpactRepositoryID, "GO-2022-1059", "golang.org/x/text", "v0.3.7", "golang.org/x/text/language", "Parse", "symbol"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["GO-2022-1059"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "v0.3.7" || got.RequestedRange != "v0.3.7" {
		t.Fatalf("version readback = observed %q requested %q, want go.mod version on both fields", got.ObservedVersion, got.RequestedRange)
	}
	if got.MatchReason != "go_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, "go_semver_affected_range")
	}
	if got.RuntimeReachability != string(GoVulnReachabilitySymbolReachable) {
		t.Fatalf("RuntimeReachability = %q, want symbol reachability", got.RuntimeReachability)
	}
	if containsString(got.MissingEvidence, "govulncheck call-graph evidence missing") {
		t.Fatalf("MissingEvidence = %#v, should not hide present call-path proof", got.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactFindingsUsesGoModResolvedReplacementVersion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	packageID := "gomod://proxy.golang.org/golang.org/x/text"
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("go-cve-fixed", "GO-2022-1059", 7.5),
		vulnerabilityAffectedPackageGoRangeFact(
			"go-affected-fixed",
			"GO-2022-1059",
			packageID,
			"golang.org/x/text",
			"0.3.8",
		),
		packageManifestDependencyFactWithMetadata(
			testImpactRepositoryID,
			"go-service",
			"go.mod",
			"golang.org/x/text",
			"go",
			"v0.3.7",
			observedAt,
			map[string]any{
				"section":              "require",
				"direct_dependency":    true,
				"dependency_path":      []any{"golang.org/x/text"},
				"dependency_depth":     1,
				"replacement_path":     "golang.org/x/text",
				"replacement_version":  "v0.21.0",
				"resolved_module_path": "golang.org/x/text",
				"resolved_version":     "v0.21.0",
			},
		),
		goModuleEvidenceEnvelope(t, testImpactRepositoryID, "go.mod", "golang.org/x/text", "v0.3.7", false, "golang.org/x/text", "v0.21.0"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["GO-2022-1059"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.RequestedRange != "v0.3.7" {
		t.Fatalf("RequestedRange = %q, want declared go.mod require version", got.RequestedRange)
	}
	if got.ObservedVersion != "v0.21.0" {
		t.Fatalf("ObservedVersion = %q, want resolved replacement version", got.ObservedVersion)
	}
	if got.MatchReason != "go_semver_known_fixed" {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, "go_semver_known_fixed")
	}
	if !containsString(got.MissingEvidence, "govulncheck call-graph evidence missing") {
		t.Fatalf("MissingEvidence = %#v, want explicit missing govulncheck evidence", got.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactFindingsRejectsGoSumChecksumOnlyEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 12, 15, 0, 0, time.UTC)
	packageID := "gomod://proxy.golang.org/golang.org/x/text"
	checksum := facts.Envelope{
		FactID:      "checksum-only-go-sum",
		FactKind:    factKindContentEntity,
		ObservedAt:  observedAt,
		SourceRef:   facts.Ref{SourceSystem: "git"},
		IsTombstone: false,
		Payload: map[string]any{
			"repo_id":       testImpactRepositoryID,
			"repo_name":     "go-service",
			"relative_path": "go.sum",
			"entity_type":   "Variable",
			"entity_name":   "golang.org/x/text",
			"entity_metadata": map[string]any{
				"config_kind":     "dependency_checksum",
				"package_manager": "go",
				"value":           "v0.3.7",
				"section":         "go-sum",
				"lockfile":        true,
				"ambiguous":       true,
				"checksum_kind":   "module",
			},
		},
	}

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("go-cve-checksum", "GO-2022-1059", 7.5),
		vulnerabilityAffectedPackageGoRangeFact(
			"go-affected-checksum",
			"GO-2022-1059",
			packageID,
			"golang.org/x/text",
			"0.3.8",
		),
		checksum,
	})

	if len(findings) != 0 {
		t.Fatalf("len(findings) = %d, want zero; go.sum checksum-only evidence must not prove installed Go module impact: %#v", len(findings), findings)
	}
}

func TestBuildSupplyChainImpactFindingsFailsClosedForMalformedGoAdvisoryRange(t *testing.T) {
	t.Parallel()

	packageID := "gomod://proxy.golang.org/golang.org/x/text"
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("go-cve-malformed", "GO-2026-998", 6.1),
		vulnerabilityAffectedPackageMalformedRangeFact(
			"go-affected-malformed",
			"GO-2026-998",
			packageID,
			"gomod",
			"golang.org/x/text",
			"not-a-version",
			"0.3.8",
		),
		packageConsumptionFactWithRange("go-consume-malformed", packageID, testImpactRepositoryID, "v0.3.7"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["GO-2026-998"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.MatchReason != "malformed_advisory_range" {
		t.Fatalf("MatchReason = %q, want malformed_advisory_range", got.MatchReason)
	}
	assertContainsString(t, got.MissingEvidence, "advisory version range malformed")
	assertContainsString(t, got.MissingEvidence, "govulncheck call-graph evidence missing")
}

func vulnerabilityAffectedPackageGoRangeFact(
	factID string,
	cveID string,
	packageID string,
	modulePath string,
	fixedVersion string,
) facts.Envelope {
	envelope := vulnerabilityAffectedPackageRangeFact(factID, cveID, packageID, "gomod", modulePath, fixedVersion)
	envelope.Payload["purl"] = "pkg:golang/" + strings.TrimSpace(modulePath)
	return envelope
}
