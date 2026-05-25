package reducer

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsCarriesRubyGemsLockfileEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-rails", "CVE-2026-6480", 7.5),
		vulnerabilityAffectedPackageFact(
			"affected-rails",
			"CVE-2026-6480",
			"pkg:gem/rails",
			"rubygems",
			"rails",
			"7.1.3",
			"7.1.4",
		),
		packageConsumptionFactWithChain(
			"consume-rails",
			"pkg:gem/rails",
			testImpactRepositoryID,
			"7.1.3",
			[]string{"rails"},
			1,
			true,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-6480"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, testImpactRepositoryID)
	}
	if got.ObservedVersion != "7.1.3" {
		t.Fatalf("ObservedVersion = %q, want Bundler lockfile version 7.1.3", got.ObservedVersion)
	}
	if got.RequestedRange != "7.1.3" {
		t.Fatalf("RequestedRange = %q, want consumption dependency range 7.1.3", got.RequestedRange)
	}
	if !reflect.DeepEqual(got.DependencyPath, []string{"rails"}) {
		t.Fatalf("DependencyPath = %#v, want rails", got.DependencyPath)
	}
	if got.DependencyDepth != 1 {
		t.Fatalf("DependencyDepth = %d, want 1", got.DependencyDepth)
	}
	if got.DirectDependency == nil || !*got.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want true", got.DirectDependency)
	}
	if got.MatchReason != supplyChainVersionReasonUnsupportedEcosystem {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonUnsupportedEcosystem)
	}
}
