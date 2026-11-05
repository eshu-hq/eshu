// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"slices"
	"testing"
)

// TestEcosystemAliasesRoundTripThroughNormalizeEcosystem proves the alias
// list a SQL prefilter binds (#9874) is exactly the set NormalizeEcosystem
// accepts: every listed spelling normalizes back to its canonical name, the
// canonical name is listed, and the raw GitLab Gemnasium slug tokens that
// affected-package facts store (go, packagist, gem, pypi, maven, nuget, npm)
// are covered.
func TestEcosystemAliasesRoundTripThroughNormalizeEcosystem(t *testing.T) {
	t.Parallel()

	gitlabSlugs := map[Ecosystem]string{
		EcosystemGoModule: "go", EcosystemComposer: "packagist", EcosystemRubyGems: "gem",
		EcosystemPyPI: "pypi", EcosystemMaven: "maven", EcosystemNuGet: "nuget", EcosystemNPM: "npm",
	}
	for _, canonical := range []Ecosystem{
		EcosystemNPM, EcosystemPyPI, EcosystemGoModule, EcosystemMaven, EcosystemNuGet,
		EcosystemComposer, EcosystemRubyGems, EcosystemCargo, EcosystemSwift, EcosystemHex,
		EcosystemPub, EcosystemOS, EcosystemGeneric,
	} {
		aliases := EcosystemAliases(canonical)
		if !slices.Contains(aliases, string(canonical)) {
			t.Fatalf("EcosystemAliases(%q) = %v, want the canonical name listed", canonical, aliases)
		}
		if slug, ok := gitlabSlugs[canonical]; ok && !slices.Contains(aliases, slug) {
			t.Fatalf("EcosystemAliases(%q) = %v, want the GitLab slug %q", canonical, aliases, slug)
		}
		for _, alias := range aliases {
			if got := NormalizeEcosystem(Ecosystem(alias)); got != canonical {
				t.Fatalf("NormalizeEcosystem(%q) = %q, want %q", alias, got, canonical)
			}
		}
	}
	if got := EcosystemAliases("conan"); len(got) != 0 {
		t.Fatalf("EcosystemAliases(unknown) = %v, want none", got)
	}
	if got := EcosystemAliases(" GoMod "); !slices.Contains(got, "golang") {
		t.Fatalf("EcosystemAliases(\" GoMod \") = %v, want the gomod spellings", got)
	}
}
