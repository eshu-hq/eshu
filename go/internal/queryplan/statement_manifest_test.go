// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"strings"
	"testing"
)

func builderManifestFixture() BuilderManifest {
	return BuilderManifest{
		Version: 1,
		Builders: []StatementBuilderCoverage{
			{
				File: "writer.go",
				Builders: []StatementBuilder{
					{
						Symbol:    "buildUpsert",
						Count:     1,
						Operation: "sourcecypher.OperationCanonicalUpsert",
						Variants: []StatementVariant{
							{Template: "MATCH (n) RETURN n"},
							{Template: "MATCH (m) RETURN m"},
						},
						SourceDigest: "aaa",
					},
					{
						Symbol: "buildDynamic",
						Count:  1,
						Variants: []StatementVariant{
							{Fragments: []string{"MATCH (n:", ") RETURN n"}},
						},
						SourceDigest: "bbb",
					},
				},
			},
		},
	}
}

func discoveredBuildersFixture() []StatementBuilderCoverage {
	manifest := builderManifestFixture()
	return manifest.Builders
}

func TestValidateBuilderManifestAcceptsMatchingDiscovery(t *testing.T) {
	if err := ValidateBuilderManifest(builderManifestFixture(), discoveredBuildersFixture()); err != nil {
		t.Fatalf("ValidateBuilderManifest() error = %v", err)
	}
}

func TestValidateBuilderManifestRejectsUnregisteredBuilder(t *testing.T) {
	discovered := append(discoveredBuildersFixture(), StatementBuilderCoverage{
		File:     "writer.go",
		Builders: []StatementBuilder{{Symbol: "buildNew", Count: 1, SourceDigest: "ccc"}},
	})
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "unregistered statement builder writer.go:buildNew") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want unregistered builder", err)
	}
}

func TestValidateBuilderManifestRejectsStaleRegistration(t *testing.T) {
	manifest := builderManifestFixture()
	manifest.Builders[0].Builders = append(manifest.Builders[0].Builders, StatementBuilder{
		Symbol:       "buildRemoved",
		Count:        1,
		SourceDigest: "ddd",
	})
	err := ValidateBuilderManifest(manifest, discoveredBuildersFixture())
	if err == nil || !strings.Contains(err.Error(), "stale statement builder registration writer.go:buildRemoved") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want stale registration", err)
	}
}

func TestValidateBuilderManifestRejectsCountMismatch(t *testing.T) {
	discovered := discoveredBuildersFixture()
	discovered[0].Builders[0].Count = 3
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "discovered build count 3, manifest requires 1") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want count mismatch", err)
	}
}

func TestValidateBuilderManifestRejectsDigestMismatch(t *testing.T) {
	discovered := discoveredBuildersFixture()
	discovered[0].Builders[0].SourceDigest = "changed"
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "source_sha256 does not match production symbol") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want digest mismatch", err)
	}
}

func TestValidateBuilderManifestRejectsVariantDrift(t *testing.T) {
	discovered := discoveredBuildersFixture()
	discovered[0].Builders[0].Variants = discovered[0].Builders[0].Variants[:1]
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "statement variants do not match production symbol") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want variant drift", err)
	}
}

func TestValidateBuilderManifestRejectsFragmentDrift(t *testing.T) {
	discovered := discoveredBuildersFixture()
	discovered[0].Builders[1].Variants[0].Fragments = []string{"MATCH (m:"}
	err := ValidateBuilderManifest(builderManifestFixture(), discovered)
	if err == nil || !strings.Contains(err.Error(), "statement variants do not match production symbol") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want fragment drift", err)
	}
}

func TestValidateBuilderManifestRejectsBlankExemption(t *testing.T) {
	manifest := builderManifestFixture()
	manifest.Builders[0].Builders[0].Exempt = "  "
	err := ValidateBuilderManifest(manifest, discoveredBuildersFixture())
	if err == nil || !strings.Contains(err.Error(), "exemption requires a reason") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want blank exemption rejection", err)
	}
}

func TestValidateBuilderManifestRejectsBlankReadExemption(t *testing.T) {
	manifest := builderManifestFixture()
	manifest.ReadExemptions = []ReadExemption{{Statement: "MATCH (n) RETURN n"}}
	err := ValidateBuilderManifest(manifest, discoveredBuildersFixture())
	if err == nil || !strings.Contains(err.Error(), "read exemption requires a reason") {
		t.Fatalf("ValidateBuilderManifest() error = %v, want blank read exemption rejection", err)
	}
}

// TestStatementBuildersManifestMatchesProduction pins the checked-in
// manifest against fresh discovery over the real tree: adding, removing,
// or editing a builder without regenerating the manifest fails here, and
// any discovery nondeterminism surfaces as a mismatch on re-runs.
func TestStatementBuildersManifestMatchesProduction(t *testing.T) {
	manifest, err := LoadBuilderManifest("testdata/statement-builders.yaml")
	if err != nil {
		t.Fatalf("LoadBuilderManifest() error = %v", err)
	}
	discovered, err := DiscoverStatementBuilders("../..")
	if err != nil {
		t.Fatalf("DiscoverStatementBuilders() error = %v", err)
	}
	if err := ValidateBuilderManifest(manifest, discovered); err != nil {
		t.Fatalf("production statement builders manifest: %v", err)
	}
}
