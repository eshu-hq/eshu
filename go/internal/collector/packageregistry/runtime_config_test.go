package packageregistry

import (
	"slices"
	"testing"
	"time"
)

func TestRuntimeConfigValidatedBoundsPackageRegistryTargets(t *testing.T) {
	t.Parallel()

	config := RuntimeConfig{
		CollectorInstanceID: " package-registry-fixture ",
		Targets: []TargetConfig{{
			Provider:     " artifactory ",
			Ecosystem:    EcosystemNPM,
			Registry:     " https://jfrog.example/artifactory/api/npm/npm-local/ ",
			ScopeID:      " npm://jfrog.example/artifactory/api/npm/npm-local/@team/web ",
			Packages:     []string{" @team/web ", "", "@team/web", "left-pad"},
			PackageLimit: 0,
			VersionLimit: 0,
			Visibility:   VisibilityPrivate,
			SourceURI:    " https://jfrog.example/artifactory/api/npm/npm-local/@team/web ",
			FencingToken: 9,
		}},
	}

	validated, err := config.validated()
	if err != nil {
		t.Fatalf("RuntimeConfig.validated() error = %v", err)
	}

	if got, want := validated.CollectorInstanceID, "package-registry-fixture"; got != want {
		t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := validated.PollInterval, defaultRuntimePollInterval; got != want {
		t.Fatalf("PollInterval = %s, want %s", got, want)
	}
	target := validated.Targets[0]
	if got, want := target.Provider, "artifactory"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := target.Registry, "jfrog.example/artifactory/api/npm/npm-local"; got != want {
		t.Fatalf("Registry = %q, want %q", got, want)
	}
	if got, want := target.ScopeID, "npm://jfrog.example/artifactory/api/npm/npm-local/@team/web"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := target.Packages, []string{"@team/web", "left-pad"}; !slices.Equal(got, want) {
		t.Fatalf("Packages = %#v, want %#v", got, want)
	}
	if got, want := target.PackageLimit, defaultPackageLimit; got != want {
		t.Fatalf("PackageLimit = %d, want %d", got, want)
	}
	if got, want := target.VersionLimit, defaultVersionLimit; got != want {
		t.Fatalf("VersionLimit = %d, want %d", got, want)
	}
}

func TestRuntimeConfigValidatedRejectsUnboundedTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config RuntimeConfig
	}{
		{name: "missing collector id", config: RuntimeConfig{}},
		{
			name: "missing targets",
			config: RuntimeConfig{
				CollectorInstanceID: "collector",
			},
		},
		{
			name: "blank ecosystem",
			config: RuntimeConfig{
				CollectorInstanceID: "collector",
				Targets:             []TargetConfig{{Provider: "artifactory", Registry: "registry.example", ScopeID: "scope"}},
			},
		},
		{
			name: "blank registry",
			config: RuntimeConfig{
				CollectorInstanceID: "collector",
				Targets:             []TargetConfig{{Provider: "artifactory", Ecosystem: EcosystemNPM, ScopeID: "scope"}},
			},
		},
		{
			name: "unbounded packages",
			config: RuntimeConfig{
				CollectorInstanceID: "collector",
				Targets: []TargetConfig{{
					Provider:     "artifactory",
					Ecosystem:    EcosystemNPM,
					Registry:     "registry.example",
					ScopeID:      "scope",
					PackageLimit: maxPackageLimit + 1,
				}},
			},
		},
		{
			name: "unbounded versions",
			config: RuntimeConfig{
				CollectorInstanceID: "collector",
				Targets: []TargetConfig{{
					Provider:     "artifactory",
					Ecosystem:    EcosystemNPM,
					Registry:     "registry.example",
					ScopeID:      "scope",
					VersionLimit: maxVersionLimit + 1,
					PackageLimit: 1,
				}},
			},
		},
		{
			name: "negative poll interval",
			config: RuntimeConfig{
				CollectorInstanceID: "collector",
				PollInterval:        -time.Second,
				Targets: []TargetConfig{{
					Provider:  "artifactory",
					Ecosystem: EcosystemNPM,
					Registry:  "registry.example",
					ScopeID:   "scope",
				}},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := tt.config.validated(); err == nil {
				t.Fatal("RuntimeConfig.validated() error = nil, want error")
			}
		})
	}
}
