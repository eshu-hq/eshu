package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildPackageConsumptionDecisionsMatchesManifestDependencies(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact(
			"pkg:npm://registry.example/@eshu/core-api",
			"npm",
			"core-api",
			"@eshu",
			observedAt,
		),
		packageSourceRepositoryFact("repo-web", "web", "https://github.com/acme/web", false, observedAt),
		packageManifestDependencyFact(
			"repo-web",
			"web",
			"package.json",
			"@eshu/core-api",
			"npm",
			"^1.2.0",
			observedAt,
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Outcome, PackageConsumptionManifestDeclared; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := decision.PackageID, "pkg:npm://registry.example/@eshu/core-api"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.RepositoryID, "repo-web"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := decision.RelativePath, "package.json"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if got, want := decision.DependencyRange, "^1.2.0"; got != want {
		t.Fatalf("DependencyRange = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(decision.DependencyPath, []string{"@eshu/core-api"}) {
		t.Fatalf("DependencyPath = %#v, want direct package path", decision.DependencyPath)
	}
	if got, want := decision.DependencyDepth, 1; got != want {
		t.Fatalf("DependencyDepth = %d, want %d", got, want)
	}
	if decision.DirectDependency == nil || !*decision.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want true for manifest dependency", decision.DirectDependency)
	}
	if decision.ProvenanceOnly {
		t.Fatal("ProvenanceOnly = true, want false for manifest-backed consumption truth")
	}
	if got, want := decision.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
}

func TestBuildPackageConsumptionDecisionsNormalizesManifestPackageIdentity(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 11, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact(
			"pypi://pypi.org/simple/friendly-bard",
			"pypi",
			"friendly-bard",
			"",
			observedAt,
		),
		packageSourceRepositoryFact("repo-service", "service", "https://github.com/acme/service", false, observedAt),
		packageManifestDependencyFact(
			"repo-service",
			"service",
			"requirements.txt",
			"friendly.bard",
			"python",
			"==1.2.3",
			observedAt,
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.PackageID, "pypi://pypi.org/simple/friendly-bard"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.Ecosystem, "pypi"; got != want {
		t.Fatalf("Ecosystem = %q, want %q", got, want)
	}
	if got, want := decision.PackageName, "friendly.bard"; got != want {
		t.Fatalf("PackageName = %q, want source manifest spelling %q", got, want)
	}
}

func TestBuildPackageConsumptionDecisionsRejectsRegistryEvidenceWithoutManifest(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 9, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact(
			"pkg:maven://repo.maven.apache.org/maven2/org.apache.logging.log4j:log4j-core",
			"maven",
			"log4j-core",
			"org.apache.logging.log4j",
			observedAt,
		),
		packageSourceRepositoryFact("repo-maven", "maven-app", "https://github.com/acme/maven-app", false, observedAt),
	})

	if len(decisions) != 0 {
		t.Fatalf("BuildPackageConsumptionDecisions admitted consumption without a manifest dependency fact: %#v", decisions)
	}
}

func TestBuildPackageConsumptionDecisionsRejectsGapEcosystemEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 9, 30, 0, 0, time.UTC)

	gapManifest := facts.Envelope{
		FactID:        "manifest-dep:repo-rust:serde",
		FactKind:      factKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:repo-rust:serde",
		Payload: map[string]any{
			"repo_id":       "repo-rust",
			"relative_path": "Cargo.toml",
			"entity_type":   "Variable",
			"entity_name":   "serde",
			"entity_metadata": map[string]any{
				"section":     "dependencies",
				"value":       "1.0.0",
				"config_kind": "rust_table",
			},
		},
	}

	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:cargo://crates.io/serde", "cargo", "serde", "", observedAt),
		packageSourceRepositoryFact("repo-rust", "rust-app", "https://github.com/acme/rust-app", false, observedAt),
		gapManifest,
	})

	if len(decisions) != 0 {
		t.Fatalf("BuildPackageConsumptionDecisions admitted consumption from a non-dependency content_entity fact: %#v", decisions)
	}
}

func TestBuildPackageConsumptionDecisionsPreservesLockfileDependencyChain(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact("pkg:npm/fsevents", "npm", "fsevents", "", observedAt),
		packageSourceRepositoryFact("repo-web", "web", "https://github.com/acme/web", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-web",
			"web",
			"package-lock.json",
			"fsevents",
			"npm",
			"2.3.3",
			observedAt,
			map[string]any{
				"section":           "package-lock",
				"lockfile":          true,
				"dependency_path":   []any{"vite", "rollup", "fsevents"},
				"dependency_depth":  3,
				"direct_dependency": false,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if !reflect.DeepEqual(decision.DependencyPath, []string{"vite", "rollup", "fsevents"}) {
		t.Fatalf("DependencyPath = %#v, want vite -> rollup -> fsevents", decision.DependencyPath)
	}
	if got, want := decision.DependencyDepth, 3; got != want {
		t.Fatalf("DependencyDepth = %d, want %d", got, want)
	}
	if decision.DirectDependency == nil || *decision.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want false for transitive lockfile dependency", decision.DirectDependency)
	}
}

// TestBuildPackageConsumptionDecisionsAdmitsComposerLockfileExactVersion
// proves the Composer lockfile coverage acceptance criterion from
// issue #647: a composer.lock fact (section "packages", lockfile flag,
// exact installed version) must produce a package-consumption decision
// against a registry identity, while the lockfile shape implies that
// directness is not yet known so the reducer must surface that as nil
// instead of inventing a direct-dependency claim.
func TestBuildPackageConsumptionDecisionsAdmitsComposerLockfileExactVersion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact(
			"pkg:composer/monolog/monolog",
			"composer",
			"monolog",
			"monolog",
			observedAt,
		),
		packageSourceRepositoryFact("repo-php", "php-app", "https://github.com/acme/php-app", false, observedAt),
		packageManifestDependencyFactWithMetadata(
			"repo-php",
			"php-app",
			"composer.lock",
			"monolog/monolog",
			"composer",
			"2.9.1",
			observedAt,
			map[string]any{
				"section":  "packages",
				"lockfile": true,
			},
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.PackageID, "pkg:composer/monolog/monolog"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.Ecosystem, "composer"; got != want {
		t.Fatalf("Ecosystem = %q, want %q", got, want)
	}
	if got, want := decision.RepositoryID, "repo-php"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := decision.RelativePath, "composer.lock"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if got, want := decision.ManifestSection, "packages"; got != want {
		t.Fatalf("ManifestSection = %q, want %q", got, want)
	}
	if got, want := decision.DependencyRange, "2.9.1"; got != want {
		t.Fatalf("DependencyRange = %q, want %q", got, want)
	}
	if decision.DirectDependency != nil {
		t.Fatalf("DirectDependency = %#v, want nil for composer.lock entry without explicit chain", decision.DirectDependency)
	}
	if got, want := decision.DependencyDepth, 0; got != want {
		t.Fatalf("DependencyDepth = %d, want %d for lockfile entry without explicit chain", got, want)
	}
	if got, want := decision.Outcome, PackageConsumptionManifestDeclared; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := decision.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
}

// TestBuildPackageConsumptionDecisionsKeepsComposerDevLockfileSeparate
// guards the dev/runtime split for composer.lock evidence. Both
// `packages` and `packages-dev` rows must reach the reducer as distinct
// consumption decisions so impact reporting can bound dev-only
// vulnerabilities away from production code.
func TestBuildPackageConsumptionDecisionsKeepsComposerDevLockfileSeparate(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		packageRegistryPackageFact(
			"pkg:composer/phpunit/phpunit",
			"composer",
			"phpunit",
			"phpunit",
			observedAt,
		),
		packageRegistryPackageFact(
			"pkg:composer/monolog/monolog",
			"composer",
			"monolog",
			"monolog",
			observedAt,
		),
		packageSourceRepositoryFact("repo-php", "php-app", "https://github.com/acme/php-app", false, observedAt),
		composerLockManifestDependencyFact(
			"repo-php",
			"php-app",
			"monolog/monolog",
			"2.9.1",
			"packages",
			"dep-runtime",
			observedAt,
		),
		composerLockManifestDependencyFact(
			"repo-php",
			"php-app",
			"phpunit/phpunit",
			"9.6.13",
			"packages-dev",
			"dep-dev",
			observedAt,
		),
	}

	decisions := BuildPackageConsumptionDecisions(envelopes)
	if got, want := len(decisions), 2; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}

	bySection := map[string]PackageConsumptionDecision{}
	for _, decision := range decisions {
		bySection[decision.ManifestSection] = decision
	}
	runtime, ok := bySection["packages"]
	if !ok {
		t.Fatalf("missing packages decision: %#v", decisions)
	}
	if got, want := runtime.PackageID, "pkg:composer/monolog/monolog"; got != want {
		t.Fatalf("runtime PackageID = %q, want %q", got, want)
	}
	dev, ok := bySection["packages-dev"]
	if !ok {
		t.Fatalf("missing packages-dev decision: %#v", decisions)
	}
	if got, want := dev.PackageID, "pkg:composer/phpunit/phpunit"; got != want {
		t.Fatalf("dev PackageID = %q, want %q", got, want)
	}
	if got, want := dev.DependencyRange, "9.6.13"; got != want {
		t.Fatalf("dev exact version = %q, want %q", got, want)
	}
	if got, want := runtime.DependencyRange, "2.9.1"; got != want {
		t.Fatalf("runtime exact version = %q, want %q", got, want)
	}
}

func composerLockManifestDependencyFact(
	repositoryID string,
	repositoryName string,
	dependencyName string,
	exactVersion string,
	section string,
	factSuffix string,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactID:        "composer-lock-dep:" + repositoryID + ":" + factSuffix,
		FactKind:      factKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:" + repositoryID + ":composer.lock:" + dependencyName,
		Payload: map[string]any{
			"repo_id":       repositoryID,
			"relative_path": "composer.lock",
			"entity_type":   "Variable",
			"entity_name":   dependencyName,
			"entity_metadata": map[string]any{
				"config_kind":     "dependency",
				"package_manager": "composer",
				"section":         section,
				"value":           exactVersion,
				"lockfile":        true,
			},
			"repo_name": repositoryName,
		},
	}
}

func TestPostgresPackageCorrelationWriterPersistsOwnershipAndConsumptionFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresPackageCorrelationWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WritePackageCorrelations(context.Background(), PackageCorrelationWrite{
		IntentID:     "intent-package",
		ScopeID:      "scope-package",
		GenerationID: "generation-package",
		SourceSystem: "package_registry",
		Cause:        "package source hints observed",
		OwnershipDecisions: []PackageSourceCorrelationDecision{
			{
				PackageID:       "pkg:npm://registry.example/team-api",
				HintKind:        "repository",
				SourceURL:       "https://github.com/acme/team-api",
				RepositoryID:    "repo-team-api",
				RepositoryName:  "team-api",
				Outcome:         PackageSourceCorrelationExact,
				Reason:          "source hint matches repository remote exactly",
				ProvenanceOnly:  true,
				CanonicalWrites: 0,
			},
		},
		ConsumptionDecisions: []PackageConsumptionDecision{
			{
				PackageID:        "pkg:npm://registry.example/team-api",
				Ecosystem:        "npm",
				PackageName:      "team-api",
				RepositoryID:     "repo-web",
				RepositoryName:   "web",
				RelativePath:     "package.json",
				ManifestSection:  "dependencies",
				DependencyRange:  "^1.2.0",
				DependencyPath:   []string{"platform-api", "team-api"},
				DependencyDepth:  2,
				DirectDependency: boolPtr(false),
				Outcome:          PackageConsumptionManifestDeclared,
				Reason:           "git manifest dependency matches package registry identity",
				CanonicalWrites:  1,
				EvidenceFactIDs:  []string{"dep-fact"},
			},
		},
		PublicationDecisions: []PackagePublicationDecision{
			{
				PackageID:       "pkg:npm://registry.example/team-api",
				VersionID:       "pkg:npm://registry.example/team-api@1.2.0",
				Version:         "1.2.0",
				RepositoryID:    "repo-team-api",
				RepositoryName:  "team-api",
				SourceURL:       "https://github.com/acme/team-api",
				Outcome:         PackageSourceCorrelationExact,
				Reason:          "source hint matches repository remote exactly",
				ProvenanceOnly:  true,
				CanonicalWrites: 0,
				EvidenceFactIDs: []string{"package-version-fact"},
			},
		},
	})
	if err != nil {
		t.Fatalf("WritePackageCorrelations() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := result.FactsWritten, 3; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 3; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	ownershipPayload := unmarshalPackageCorrelationPayload(t, db.execs[0].args[14])
	if got, want := ownershipPayload["correlation_kind"], packageOwnershipCorrelationFactKind; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := ownershipPayload["relationship_kind"], "ownership"; got != want {
		t.Fatalf("relationship_kind = %#v, want %#v", got, want)
	}
	if got, want := ownershipPayload["provenance_only"], true; got != want {
		t.Fatalf("provenance_only = %#v, want %#v", got, want)
	}
	consumptionPayload := unmarshalPackageCorrelationPayload(t, db.execs[1].args[14])
	if got, want := consumptionPayload["correlation_kind"], packageConsumptionCorrelationFactKind; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["canonical_writes"], float64(1); got != want {
		t.Fatalf("canonical_writes = %#v, want %#v", got, want)
	}
	if got, want := packageCorrelationStringSliceFromAny(consumptionPayload["dependency_path"]), []string{"platform-api", "team-api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependency_path = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["dependency_depth"], float64(2); got != want {
		t.Fatalf("dependency_depth = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["direct_dependency"], false; got != want {
		t.Fatalf("direct_dependency = %#v, want %#v", got, want)
	}
	publicationPayload := unmarshalPackageCorrelationPayload(t, db.execs[2].args[14])
	if got, want := publicationPayload["correlation_kind"], packagePublicationCorrelationFactKind; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := publicationPayload["relationship_kind"], "publication"; got != want {
		t.Fatalf("relationship_kind = %#v, want %#v", got, want)
	}
	if got, want := publicationPayload["version_id"], "pkg:npm://registry.example/team-api@1.2.0"; got != want {
		t.Fatalf("version_id = %#v, want %#v", got, want)
	}
	if got, want := publicationPayload["provenance_only"], true; got != want {
		t.Fatalf("provenance_only = %#v, want %#v", got, want)
	}
}

func TestPackagePublicationIdentityIncludesSourceHintIdentity(t *testing.T) {
	t.Parallel()

	write := PackageCorrelationWrite{
		ScopeID:      "scope-package",
		GenerationID: "generation-package",
	}
	repositoryHint := PackagePublicationDecision{
		PackageID:           "pkg:npm://registry.example/team-api",
		VersionID:           "pkg:npm://registry.example/team-api@1.2.0",
		SourceURL:           "https://github.com/acme/team-api",
		SourceHintFactID:    "source-hint-repository",
		SourceHintKind:      "repository",
		SourceHintVersionID: "pkg:npm://registry.example/team-api@1.2.0",
	}
	homepageHint := PackagePublicationDecision{
		PackageID:           repositoryHint.PackageID,
		VersionID:           repositoryHint.VersionID,
		SourceURL:           repositoryHint.SourceURL,
		SourceHintFactID:    "source-hint-homepage",
		SourceHintKind:      "homepage",
		SourceHintVersionID: repositoryHint.SourceHintVersionID,
	}

	if got, want := packagePublicationFactID(write, repositoryHint), packagePublicationFactID(write, homepageHint); got == want {
		t.Fatalf("packagePublicationFactID collapsed distinct source hints to %q", got)
	}
	if got, want := packagePublicationStableFactKey(write, repositoryHint), packagePublicationStableFactKey(write, homepageHint); got == want {
		t.Fatalf("packagePublicationStableFactKey collapsed distinct source hints to %q", got)
	}
	payload := packagePublicationPayload(write, repositoryHint)
	if got, want := payload["source_hint_kind"], "repository"; got != want {
		t.Fatalf("source_hint_kind = %#v, want %#v", got, want)
	}
	if got, want := payload["source_hint_fact_id"], "source-hint-repository"; got != want {
		t.Fatalf("source_hint_fact_id = %#v, want %#v", got, want)
	}
}
