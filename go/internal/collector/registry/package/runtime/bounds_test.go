// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/registry/package"
)

func TestBoundedParsedMetadataRejectsPackageLimitExceeded(t *testing.T) {
	t.Parallel()

	target := packages.TargetConfig{
		Provider:     "jfrog",
		Ecosystem:    packages.EcosystemGeneric,
		Registry:     "https://artifactory.example.com",
		ScopeID:      "package-registry://jfrog/generic/repo",
		PackageLimit: 1,
		VersionLimit: 2,
	}
	parsed := packages.ParsedMetadata{
		Packages: []packages.PackageObservation{
			{Identity: packages.PackageIdentity{
				Ecosystem: packages.EcosystemGeneric,
				Registry:  "https://artifactory.example.com",
				RawName:   "team-api",
			}},
			{Identity: packages.PackageIdentity{
				Ecosystem: packages.EcosystemGeneric,
				Registry:  "https://artifactory.example.com",
				RawName:   "team-worker",
			}},
		},
	}

	_, err := boundedParsedMetadata(target, parsed)
	if err == nil {
		t.Fatal("boundedParsedMetadata() error = nil, want package_limit rejection")
	}
	if got := err.Error(); !strings.Contains(got, "package_limit") {
		t.Fatalf("boundedParsedMetadata() error = %q, want package_limit rejection", got)
	}
}

func TestBoundedParsedMetadataKeepsAdvisoriesAndEventsForAllowedVersions(t *testing.T) {
	t.Parallel()

	identity := packages.PackageIdentity{
		Ecosystem: packages.EcosystemNPM,
		Registry:  "registry.npmjs.org",
		RawName:   "left-pad",
	}
	target := packages.TargetConfig{
		Provider:     "npmjs",
		Ecosystem:    packages.EcosystemNPM,
		Registry:     "registry.npmjs.org",
		ScopeID:      "npm://registry.npmjs.org/left-pad",
		PackageLimit: 1,
		VersionLimit: 1,
	}
	parsed := packages.ParsedMetadata{
		Packages: []packages.PackageObservation{
			{Identity: identity},
		},
		Versions: []packages.PackageVersionObservation{
			{Package: identity, Version: "1.0.0"},
		},
		Vulnerables: []packages.VulnerabilityHintObservation{
			{Package: identity, Version: "1.0.0", AdvisoryID: "GHSA-left-pad", AdvisorySource: "npm-audit"},
			{Package: identity, Version: "2.0.0", AdvisoryID: "GHSA-other", AdvisorySource: "npm-audit"},
		},
		Events: []packages.RegistryEventObservation{
			{Package: identity, Version: "1.0.0", EventKey: "serial:1", EventType: "publish"},
			{Package: identity, Version: "2.0.0", EventKey: "serial:2", EventType: "publish"},
		},
	}

	bounded, err := boundedParsedMetadata(target, parsed)
	if err != nil {
		t.Fatalf("boundedParsedMetadata() error = %v, want nil", err)
	}
	if len(bounded.Vulnerables) != 1 || bounded.Vulnerables[0].AdvisoryID != "GHSA-left-pad" {
		t.Fatalf("Vulnerables = %#v, want only allowed version advisory", bounded.Vulnerables)
	}
	if len(bounded.Events) != 1 || bounded.Events[0].EventKey != "serial:1" {
		t.Fatalf("Events = %#v, want only allowed version event", bounded.Events)
	}
}

func TestBoundedParsedMetadataTruncatesVersionsAndEmitsWarning(t *testing.T) {
	t.Parallel()

	identity := packages.PackageIdentity{
		Ecosystem: packages.EcosystemNPM,
		Registry:  "registry.npmjs.org",
		RawName:   "left-pad",
	}
	target := packages.TargetConfig{
		Provider:     "npmjs",
		Ecosystem:    packages.EcosystemNPM,
		Registry:     "registry.npmjs.org",
		ScopeID:      "npm://registry.npmjs.org/left-pad",
		PackageLimit: 1,
		VersionLimit: 2,
	}
	parsed := packages.ParsedMetadata{
		Packages: []packages.PackageObservation{
			{Identity: identity, ScopeID: target.ScopeID, GenerationID: "generation-1", CollectorInstanceID: "public-npm"},
		},
		Versions: []packages.PackageVersionObservation{
			{Package: identity, Version: "1.0.0", ScopeID: target.ScopeID, GenerationID: "generation-1", CollectorInstanceID: "public-npm"},
			{Package: identity, Version: "1.1.0", ScopeID: target.ScopeID, GenerationID: "generation-1", CollectorInstanceID: "public-npm"},
			{Package: identity, Version: "1.2.0", ScopeID: target.ScopeID, GenerationID: "generation-1", CollectorInstanceID: "public-npm"},
		},
		Artifacts: []packages.PackageArtifactObservation{
			{Package: identity, Version: "1.2.0", ArtifactKey: "left-pad-1.2.0.tgz"},
		},
	}

	bounded, err := boundedParsedMetadata(target, parsed)
	if err != nil {
		t.Fatalf("boundedParsedMetadata() error = %v, want nil", err)
	}
	if got := versionStrings(bounded.Versions); strings.Join(got, ",") != "1.0.0,1.1.0" {
		t.Fatalf("bounded versions = %#v, want first two versions", got)
	}
	if len(bounded.Artifacts) != 0 {
		t.Fatalf("Artifacts = %#v, want artifacts for truncated versions removed", bounded.Artifacts)
	}
	if len(bounded.Warnings) != 1 {
		t.Fatalf("Warnings = %#v, want one truncation warning", bounded.Warnings)
	}
	warning := bounded.Warnings[0]
	if got, want := warning.WarningCode, "version_limit_truncated"; got != want {
		t.Fatalf("WarningCode = %q, want %q", got, want)
	}
	if warning.Package == nil {
		t.Fatal("Warning.Package = nil, want package-scoped warning")
	}
	if got := warning.Message; !strings.Contains(got, "3 versions") || !strings.Contains(got, "version_limit 2") {
		t.Fatalf("Warning message = %q, want count and limit", got)
	}
}

func versionStrings(observations []packages.PackageVersionObservation) []string {
	versions := make([]string, 0, len(observations))
	for _, observation := range observations {
		versions = append(versions, observation.Version)
	}
	return versions
}
