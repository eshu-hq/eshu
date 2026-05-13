package packageruntime

import (
	"context"
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
