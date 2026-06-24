// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageruntime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
)

func TestClaimedSourceDefaultsDerivedNPMTargetToIdentityOnly(t *testing.T) {
	t.Parallel()

	provider := &recordingMetadataProvider{
		staticMetadataProvider: staticMetadataProvider{
			document: []byte(`{
				"name":"vite",
				"versions":{
					"5.4.21":{"dist":{"tarball":"https://registry.npmjs.org/vite/-/vite-5.4.21.tgz"}},
					"6.0.0":{"dist":{"tarball":"https://registry.npmjs.org/vite/-/vite-6.0.0.tgz"}}
				}
			}`),
		},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry",
		DerivedTargets: DerivedTargetConfig{
			Enabled:    true,
			Ecosystems: []packageregistry.Ecosystem{packageregistry.EcosystemNPM},
		},
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, time.May, 26, 11, 45, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	_, ok, err := source.NextClaimed(
		context.Background(),
		testPackageRegistryWorkItemForScope("npm://registry.npmjs.org/vite"),
	)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := provider.targets[0].Base.PackageLimit, 1; got != want {
		t.Fatalf("PackageLimit = %d, want %d", got, want)
	}
	if got, want := provider.targets[0].Base.VersionLimit, 1; got != want {
		t.Fatalf("VersionLimit = %d, want %d", got, want)
	}
}

func TestClaimedSourceResolvesDerivedNPMTarget(t *testing.T) {
	t.Parallel()

	provider := &recordingMetadataProvider{
		staticMetadataProvider: staticMetadataProvider{
			document: []byte(`{
				"name":"vite",
				"versions":{"5.4.21":{"dist":{"tarball":"https://registry.npmjs.org/vite/-/vite-5.4.21.tgz"}}}
			}`),
		},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry",
		DerivedTargets: DerivedTargetConfig{
			Enabled:      true,
			Ecosystems:   []packageregistry.Ecosystem{packageregistry.EcosystemNPM},
			PackageLimit: 1,
			VersionLimit: 50,
		},
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, time.May, 23, 22, 30, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	collected, ok, err := source.NextClaimed(
		context.Background(),
		testPackageRegistryWorkItemForScope("npm://registry.npmjs.org/vite"),
	)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := provider.targets[0].MetadataURL, "https://registry.npmjs.org/vite"; got != want {
		t.Fatalf("MetadataURL = %q, want %q", got, want)
	}
	if got, want := provider.targets[0].Base.PackageLimit, 1; got != want {
		t.Fatalf("PackageLimit = %d, want %d", got, want)
	}
	if got, want := provider.targets[0].Base.VersionLimit, 50; got != want {
		t.Fatalf("VersionLimit = %d, want %d", got, want)
	}
	if got, want := collected.Scope.ScopeID, "npm://registry.npmjs.org/vite"; got != want {
		t.Fatalf("collected scope = %q, want %q", got, want)
	}
}

func TestClaimedSourceRejectsDerivedTargetWhenDisabled(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry",
		Targets: []TargetConfig{{
			Base: packageregistry.TargetConfig{
				Provider:     "jfrog",
				Ecosystem:    packageregistry.EcosystemGeneric,
				Registry:     "https://artifactory.example.com",
				ScopeID:      "package-registry://jfrog/generic/team-api",
				Packages:     []string{"team-api"},
				PackageLimit: 1,
				VersionLimit: 2,
			},
		}},
		Provider: staticMetadataProvider{document: []byte(`{}`)},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	_, _, err = source.NextClaimed(context.Background(), testPackageRegistryWorkItemForScope("npm://registry.npmjs.org/vite"))
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want derived target disabled rejection")
	}
}

func TestClaimedSourceResolvesDerivedPyPITarget(t *testing.T) {
	t.Parallel()

	provider := &recordingMetadataProvider{
		staticMetadataProvider: staticMetadataProvider{
			document: []byte(`{
				"info":{"name":"Friendly_Bard","version":"2.0.0"},
				"releases":{"2.0.0":[{"filename":"friendly_bard-2.0.0-py3-none-any.whl"}]}
			}`),
		},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry",
		DerivedTargets: DerivedTargetConfig{
			Enabled:      true,
			Ecosystems:   []packageregistry.Ecosystem{packageregistry.EcosystemPyPI},
			PackageLimit: 1,
			VersionLimit: 1,
		},
		Provider: provider,
		Now: func() time.Time {
			return time.Date(2026, time.June, 1, 10, 30, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	collected, ok, err := source.NextClaimed(
		context.Background(),
		testPackageRegistryWorkItemForScope("pypi://pypi.org/pypi/friendly-bard"),
	)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := provider.targets[0].MetadataURL, "https://pypi.org/pypi/friendly-bard/json"; got != want {
		t.Fatalf("MetadataURL = %q, want %q", got, want)
	}
	if got, want := collected.Scope.ScopeID, "pypi://pypi.org/pypi/friendly-bard"; got != want {
		t.Fatalf("collected scope = %q, want %q", got, want)
	}
}
