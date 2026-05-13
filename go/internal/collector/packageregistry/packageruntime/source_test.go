package packageruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceParsesMetadataIntoPackageRegistryFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 13, 18, 0, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry",
		Targets: []TargetConfig{
			{
				Base: packageregistry.TargetConfig{
					Provider:     "jfrog",
					Ecosystem:    packageregistry.EcosystemGeneric,
					Registry:     "https://artifactory.example.com",
					ScopeID:      "package-registry://jfrog/generic/team-api",
					Packages:     []string{"team-api"},
					PackageLimit: 1,
					VersionLimit: 2,
					Visibility:   packageregistry.VisibilityPrivate,
				},
			},
		},
		Provider: staticMetadataProvider{
			document: []byte(`{
				"provider":"jfrog",
				"repository":"generic-local",
				"repository_type":"local",
				"name":"team-api",
				"namespace":"payments",
				"version":"1.2.3",
				"visibility":"private",
				"source_url":"https://github.com/eshu-hq/team-api",
				"artifacts":[{"key":"team-api-1.2.3.tgz","type":"tgz","sha256":"abc123"}]
			}`),
			sourceURI: "https://artifactory.example.com/api/storage/generic-local/team-api",
		},
		Now: func() time.Time { return observedAt },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "package-registry-work-item-1",
		CollectorKind:       scope.CollectorPackageRegistry,
		CollectorInstanceID: "collector-package-registry",
		ScopeID:             "package-registry://jfrog/generic/team-api",
		GenerationID:        "package_registry:generation-1",
		SourceRunID:         "package_registry:generation-1",
		CurrentFencingToken: 42,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Scope.ScopeID, "package-registry://jfrog/generic/team-api"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := collected.Scope.SourceSystem, string(scope.CollectorPackageRegistry); got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
	if got, want := collected.Scope.ScopeKind, scope.KindPackageRegistry; got != want {
		t.Fatalf("ScopeKind = %q, want %q", got, want)
	}
	if got, want := collected.Generation.GenerationID, "package_registry:generation-1"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}

	gotKinds := map[string]int{}
	for envelope := range collected.Facts {
		gotKinds[envelope.FactKind]++
		if envelope.FencingToken != 42 {
			t.Fatalf("FencingToken = %d, want 42 for %#v", envelope.FencingToken, envelope)
		}
	}
	for _, wantKind := range []string{
		facts.PackageRegistryPackageFactKind,
		facts.PackageRegistryPackageVersionFactKind,
		facts.PackageRegistryPackageArtifactFactKind,
		facts.PackageRegistrySourceHintFactKind,
		facts.PackageRegistryRepositoryHostingFactKind,
	} {
		if gotKinds[wantKind] == 0 {
			t.Fatalf("fact kinds = %#v, missing %q", gotKinds, wantKind)
		}
	}
}

func TestEnvelopesFromParsedMetadataIncludesAdvisoriesAndEvents(t *testing.T) {
	t.Parallel()

	basePackage := packageregistry.PackageIdentity{
		Ecosystem: packageregistry.EcosystemNPM,
		Registry:  "registry.npmjs.org",
		RawName:   "left-pad",
	}
	parsed := packageregistry.ParsedMetadata{
		Vulnerables: []packageregistry.VulnerabilityHintObservation{
			{
				Package:             basePackage,
				AdvisoryID:          "GHSA-left-pad",
				AdvisorySource:      "npm-audit",
				ScopeID:             "npm://registry.npmjs.org/left-pad",
				GenerationID:        "etag:advisory",
				CollectorInstanceID: "public-npm",
				ObservedAt:          time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC),
			},
		},
		Events: []packageregistry.RegistryEventObservation{
			{
				Package:             basePackage,
				EventKey:            "serial:44",
				EventType:           "publish",
				ScopeID:             "npm://registry.npmjs.org/left-pad",
				GenerationID:        "etag:event",
				CollectorInstanceID: "public-npm",
				ObservedAt:          time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC),
			},
		},
	}

	envs, err := envelopesFromParsedMetadata(parsed)
	if err != nil {
		t.Fatalf("envelopesFromParsedMetadata() error = %v", err)
	}
	gotKinds := map[string]bool{}
	for _, envelope := range envs {
		gotKinds[envelope.FactKind] = true
	}
	for _, wantKind := range []string{
		facts.PackageRegistryVulnerabilityHintFactKind,
		facts.PackageRegistryRegistryEventFactKind,
	} {
		if !gotKinds[wantKind] {
			t.Fatalf("fact kinds = %#v, missing %q", gotKinds, wantKind)
		}
	}
}

func TestClaimedSourceParsesArtifactoryPackageDocumentFormat(t *testing.T) {
	t.Parallel()

	source := newTestClaimedSource(t, TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "jfrog",
			Ecosystem:    packageregistry.EcosystemNPM,
			Registry:     "https://artifactory.example.com/artifactory/api/npm/npm-local",
			ScopeID:      "package-registry://jfrog/npm/@scope/pkg",
			Packages:     []string{"@scope/pkg"},
			PackageLimit: 1,
			VersionLimit: 2,
		},
		DocumentFormat: "artifactory_package",
	}, staticMetadataProvider{
		document: []byte(`{
			"provider":"jfrog",
			"repository":"npm-local",
			"repository_type":"local",
			"package_type":"npm",
			"metadata":{
				"name":"@scope/pkg",
				"versions":{
					"1.0.0":{
						"dist":{"tarball":"https://artifactory.example.com/artifactory/api/npm/npm-local/@scope/pkg/-/pkg-1.0.0.tgz"}
					}
				}
			}
		}`),
	})

	collected, ok, err := source.NextClaimed(context.Background(), testPackageRegistryWorkItemForScope("package-registry://jfrog/npm/@scope/pkg"))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	gotKinds := map[string]int{}
	for envelope := range collected.Facts {
		gotKinds[envelope.FactKind]++
	}
	for _, wantKind := range []string{
		facts.PackageRegistryPackageFactKind,
		facts.PackageRegistryPackageVersionFactKind,
		facts.PackageRegistryPackageArtifactFactKind,
		facts.PackageRegistryRepositoryHostingFactKind,
	} {
		if gotKinds[wantKind] == 0 {
			t.Fatalf("fact kinds = %#v, missing %q", gotKinds, wantKind)
		}
	}
}

func TestClaimedSourceArtifactoryPackageUsesConfiguredParserRegistry(t *testing.T) {
	t.Parallel()

	source := newTestClaimedSource(t, TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "jfrog",
			Ecosystem:    packageregistry.EcosystemNPM,
			Registry:     "https://artifactory.example.com/artifactory/api/npm/npm-local",
			ScopeID:      "package-registry://jfrog/npm/@scope/pkg",
			Packages:     []string{"@scope/pkg"},
			PackageLimit: 1,
			VersionLimit: 2,
		},
		DocumentFormat: DocumentFormatArtifactoryPackage,
	}, staticMetadataProvider{
		document: []byte(`{
			"provider":"jfrog",
			"repository":"npm-local",
			"package_type":"npm",
			"metadata":{"name":"default-parser-package","versions":{}}
		}`),
	})
	var registry packageregistry.MetadataParserRegistry
	if err := registry.Register(packageregistry.EcosystemNPM, func(ctx packageregistry.MetadataParserContext, document []byte) (packageregistry.ParsedMetadata, error) {
		identity := packageregistry.PackageIdentity{
			Ecosystem: packageregistry.EcosystemNPM,
			Registry:  ctx.Registry,
			RawName:   "@scope/pkg",
		}
		return packageregistry.ParsedMetadata{
			Packages: []packageregistry.PackageObservation{
				{
					Identity:            identity,
					ScopeID:             ctx.ScopeID,
					GenerationID:        ctx.GenerationID,
					CollectorInstanceID: ctx.CollectorInstanceID,
					FencingToken:        ctx.FencingToken,
					ObservedAt:          ctx.ObservedAt,
					Visibility:          packageregistry.VisibilityUnknown,
					SourceURI:           ctx.SourceURI,
				},
			},
		}, nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	source.parserRegistry = registry

	collected, ok, err := source.NextClaimed(context.Background(), testPackageRegistryWorkItemForScope("package-registry://jfrog/npm/@scope/pkg"))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	var sawCustomPackage bool
	for envelope := range collected.Facts {
		if envelope.FactKind == facts.PackageRegistryPackageFactKind &&
			envelope.Payload["raw_name"] == "@scope/pkg" {
			sawCustomPackage = true
		}
	}
	if !sawCustomPackage {
		t.Fatal("collected facts did not include package emitted by configured parser registry")
	}
}

func TestClaimedSourceRejectsMetadataForUnexpectedPackage(t *testing.T) {
	t.Parallel()

	source := newTestClaimedSource(t, TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "jfrog",
			Ecosystem:    packageregistry.EcosystemGeneric,
			Registry:     "https://artifactory.example.com",
			ScopeID:      "package-registry://jfrog/generic/team-api",
			Packages:     []string{"team-api"},
			PackageLimit: 1,
			VersionLimit: 2,
		},
	}, staticMetadataProvider{
		document: []byte(`{"name":"other-api","version":"1.0.0"}`),
	})

	_, _, err := source.NextClaimed(context.Background(), testPackageRegistryWorkItem())
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want configured package mismatch")
	}
	if got := err.Error(); !strings.Contains(got, "configured packages") {
		t.Fatalf("NextClaimed() error = %q, want configured package mismatch", got)
	}
}

func TestClaimedSourceRejectsMetadataOverVersionLimit(t *testing.T) {
	t.Parallel()

	source := newTestClaimedSource(t, TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "npm",
			Ecosystem:    packageregistry.EcosystemNPM,
			Registry:     "https://registry.npmjs.org",
			ScopeID:      "package-registry://npm/npm/@scope/pkg",
			Packages:     []string{"@scope/pkg"},
			PackageLimit: 1,
			VersionLimit: 1,
		},
	}, staticMetadataProvider{
		document: []byte(`{
			"name":"@scope/pkg",
			"versions":{
				"1.0.0":{"dist":{"tarball":"https://registry.npmjs.org/@scope/pkg/-/pkg-1.0.0.tgz"}},
				"1.1.0":{"dist":{"tarball":"https://registry.npmjs.org/@scope/pkg/-/pkg-1.1.0.tgz"}}
			}
		}`),
	})

	_, _, err := source.NextClaimed(context.Background(), testPackageRegistryWorkItemForScope("package-registry://npm/npm/@scope/pkg"))
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want version_limit rejection")
	}
	if got := err.Error(); !strings.Contains(got, "version_limit") {
		t.Fatalf("NextClaimed() error = %q, want version_limit rejection", got)
	}
}

func TestClaimedSourceSanitizesSourceURIBeforeFactEmission(t *testing.T) {
	t.Parallel()

	source := newTestClaimedSource(t, TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "jfrog",
			Ecosystem:    packageregistry.EcosystemGeneric,
			Registry:     "https://artifactory.example.com",
			ScopeID:      "package-registry://jfrog/generic/team-api",
			Packages:     []string{"team-api"},
			PackageLimit: 1,
			VersionLimit: 2,
		},
	}, staticMetadataProvider{
		document:  []byte(`{"name":"team-api","version":"1.2.3"}`),
		sourceURI: "https://artifactory.example.com/api/storage/generic/team-api?token=secret&safe=1#metadata",
	})

	collected, ok, err := source.NextClaimed(context.Background(), testPackageRegistryWorkItem())
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	for envelope := range collected.Facts {
		if strings.Contains(envelope.SourceRef.SourceURI, "secret") ||
			strings.Contains(envelope.SourceRef.SourceURI, "?") ||
			strings.Contains(envelope.SourceRef.SourceURI, "#") {
			t.Fatalf("SourceRef.SourceURI = %q, want sanitized source URI", envelope.SourceRef.SourceURI)
		}
	}
}

func newTestClaimedSource(
	t *testing.T,
	target TargetConfig,
	provider staticMetadataProvider,
) *ClaimedSource {
	t.Helper()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry",
		Targets:             []TargetConfig{target},
		Provider:            provider,
		Now: func() time.Time {
			return time.Date(2026, time.May, 13, 18, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}
	return source
}

func testPackageRegistryWorkItem() workflow.WorkItem {
	return testPackageRegistryWorkItemForScope("package-registry://jfrog/generic/team-api")
}

func testPackageRegistryWorkItemForScope(scopeID string) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "package-registry-work-item-1",
		CollectorKind:       scope.CollectorPackageRegistry,
		CollectorInstanceID: "collector-package-registry",
		ScopeID:             scopeID,
		GenerationID:        "package_registry:generation-1",
		SourceRunID:         "package_registry:generation-1",
		CurrentFencingToken: 42,
	}
}

type staticMetadataProvider struct {
	document  []byte
	sourceURI string
}

func (p staticMetadataProvider) FetchMetadata(context.Context, TargetConfig) (MetadataDocument, error) {
	return MetadataDocument{
		Body:         p.document,
		SourceURI:    p.sourceURI,
		DocumentType: "generic",
	}, nil
}
