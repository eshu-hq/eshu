// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildPackageConsumptionDecisionsAdmitsPyPIManifestEvidence proves that
// every Python dependency parser the supply-chain reducer relies on
// produces evidence that joins to PyPI registry identity. Before this test
// existed, PyPI repositories surfaced as `evidence_incomplete` regardless of
// whether requirements.txt, pyproject.toml, Pipfile, Pipfile.lock, or
// poetry.lock declared a dependency.
func TestBuildPackageConsumptionDecisionsAdmitsPyPIManifestEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 25, 9, 0, 0, 0, time.UTC)
	repositories := []facts.Envelope{
		packageRegistryPackageFact(
			"pkg:pypi://pypi.org/simple/requests",
			"pypi",
			"requests",
			"",
			observedAt,
		),
		packageSourceRepositoryFact(
			"repo-service",
			"service",
			"https://github.com/acme/service",
			false,
			observedAt,
		),
	}

	for _, tc := range []struct {
		name          string
		relativePath  string
		dependency    string
		value         string
		section       string
		extraMetadata map[string]any
		// wantChain is true when the reducer must build a single-element
		// chain because the source is a manifest, not a lockfile. Lockfile
		// rows surface DependencyPath=nil and DirectDependency=nil unless
		// the lockfile itself proved the chain (npm package-lock.json is
		// the only PyPI lockfile that proves chains today, and Pipfile.lock
		// / poetry.lock do not).
		wantChain bool
		wantRange string
	}{
		{
			name:         "requirements.txt pinned dependency joins as direct consumption",
			relativePath: "requirements.txt",
			dependency:   "requests",
			value:        "==2.31.0",
			section:      "requirements",
			wantChain:    true,
			wantRange:    "==2.31.0",
		},
		{
			name:         "pyproject.toml PEP 621 range dependency joins as direct consumption",
			relativePath: "pyproject.toml",
			dependency:   "requests",
			value:        ">=2.0,<3",
			section:      "project.dependencies",
			wantChain:    true,
			wantRange:    ">=2.0,<3",
		},
		{
			name:         "Pipfile package range joins as direct consumption",
			relativePath: "Pipfile",
			dependency:   "requests",
			value:        "*",
			section:      "packages",
			wantChain:    true,
			wantRange:    "*",
		},
		{
			name:         "Pipfile.lock exact version joins as lockfile consumption with unknown directness",
			relativePath: "Pipfile.lock",
			dependency:   "requests",
			value:        "2.31.0",
			section:      "default",
			extraMetadata: map[string]any{
				"lockfile": true,
			},
			wantChain: false,
			wantRange: "2.31.0",
		},
		{
			name:         "poetry.lock exact version joins as lockfile consumption with unknown directness",
			relativePath: "poetry.lock",
			dependency:   "requests",
			value:        "2.31.0",
			section:      "package",
			extraMetadata: map[string]any{
				"lockfile": true,
			},
			wantChain: false,
			wantRange: "2.31.0",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := append([]facts.Envelope(nil), repositories...)
			if len(tc.extraMetadata) > 0 {
				metadata := map[string]any{"section": tc.section}
				for key, value := range tc.extraMetadata {
					metadata[key] = value
				}
				envelopes = append(envelopes, packageManifestDependencyFactWithMetadata(
					"repo-service",
					"service",
					tc.relativePath,
					tc.dependency,
					"pypi",
					tc.value,
					observedAt,
					metadata,
				))
			} else {
				envelope := packageManifestDependencyFact(
					"repo-service",
					"service",
					tc.relativePath,
					tc.dependency,
					"pypi",
					tc.value,
					observedAt,
				)
				if tc.section != "" {
					metadata, _ := envelope.Payload["entity_metadata"].(map[string]any)
					if metadata != nil {
						metadata["section"] = tc.section
					}
				}
				envelopes = append(envelopes, envelope)
			}

			decisions := BuildPackageConsumptionDecisions(envelopes)
			if got, want := len(decisions), 1; got != want {
				t.Fatalf("len(decisions) = %d, want %d for %s", got, want, tc.name)
			}
			decision := decisions[0]
			if got, want := decision.Outcome, PackageConsumptionManifestDeclared; got != want {
				t.Fatalf("Outcome = %q, want %q", got, want)
			}
			if got, want := decision.Ecosystem, "pypi"; got != want {
				t.Fatalf("Ecosystem = %q, want %q", got, want)
			}
			if got, want := decision.PackageID, "pkg:pypi://pypi.org/simple/requests"; got != want {
				t.Fatalf("PackageID = %q, want %q", got, want)
			}
			if got, want := decision.RelativePath, tc.relativePath; got != want {
				t.Fatalf("RelativePath = %q, want %q", got, want)
			}
			if got, want := decision.DependencyRange, tc.wantRange; got != want {
				t.Fatalf("DependencyRange = %q, want %q", got, want)
			}
			if tc.wantChain {
				if !reflect.DeepEqual(decision.DependencyPath, []string{tc.dependency}) {
					t.Fatalf("DependencyPath = %#v, want direct package path %q", decision.DependencyPath, tc.dependency)
				}
				if decision.DirectDependency == nil || !*decision.DirectDependency {
					t.Fatalf("DirectDependency = %#v, want true for manifest evidence", decision.DirectDependency)
				}
			} else {
				if decision.DependencyPath != nil {
					t.Fatalf("DependencyPath = %#v, want nil for lockfile evidence without proven chain", decision.DependencyPath)
				}
				if decision.DirectDependency != nil {
					t.Fatalf("DirectDependency = %#v, want nil for lockfile evidence without proven chain", decision.DirectDependency)
				}
			}
			if decision.CanonicalWrites != 1 {
				t.Fatalf("CanonicalWrites = %d, want 1", decision.CanonicalWrites)
			}
		})
	}
}

// TestBuildPackageConsumptionDecisionsNormalizesPyPINameUnderscoresAndDots
// proves that the reducer joins `Friendly_Bard.plugin` from a requirements
// file to the PyPI registry identity `friendly-bard-plugin` because PEP 503
// normalization collapses [._-]+ to '-'. Without this, repositories that use
// underscores or mixed case would silently fail to correlate to vulnerability
// advisories.
func TestBuildPackageConsumptionDecisionsNormalizesPyPINameUnderscoresAndDots(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 25, 10, 30, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		packageRegistryPackageFact(
			"pkg:pypi://pypi.org/simple/friendly-bard-plugin",
			"pypi",
			"friendly-bard-plugin",
			"",
			observedAt,
		),
		packageSourceRepositoryFact("repo-service", "service", "https://github.com/acme/service", false, observedAt),
		packageManifestDependencyFact(
			"repo-service",
			"service",
			"requirements.txt",
			"Friendly_Bard.plugin",
			"pypi",
			"==1.0.0",
			observedAt,
		),
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d (requested PEP 503 normalization join)", got, want)
	}
	if got, want := decisions[0].PackageID, "pkg:pypi://pypi.org/simple/friendly-bard-plugin"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
}

// TestBuildPackageConsumptionDecisionsRejectsPyPIVCSEvidence proves that a
// VCS Python dependency does NOT produce a consumption decision against the
// registry. VCS provenance carries no PyPI-version evidence, so the
// supply-chain reducer must stay silent until a real `dependency` row joins.
func TestBuildPackageConsumptionDecisionsRejectsPyPIVCSEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 25, 11, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		packageRegistryPackageFact(
			"pkg:pypi://pypi.org/simple/requests",
			"pypi",
			"requests",
			"",
			observedAt,
		),
		packageSourceRepositoryFact("repo-service", "service", "https://github.com/acme/service", false, observedAt),
		{
			FactID:        "manifest-dep:repo-service:requests",
			FactKind:      factKindContentEntity,
			ObservedAt:    observedAt,
			IsTombstone:   false,
			SourceRef:     facts.Ref{SourceSystem: "git"},
			StableFactKey: "content_entity:repo-service:requests",
			Payload: map[string]any{
				"repo_id":       "repo-service",
				"relative_path": "requirements.txt",
				"entity_type":   "Variable",
				"entity_name":   "requests",
				"entity_metadata": map[string]any{
					"config_kind":     "vcs_dependency",
					"package_manager": "pypi",
					"section":         "requirements",
					"value":           "git+https://github.com/psf/requests.git",
				},
			},
		},
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if len(decisions) != 0 {
		t.Fatalf("vcs Python dep produced %d decisions; want 0 (config_kind=vcs_dependency must be ignored): %#v", len(decisions), decisions)
	}
}
