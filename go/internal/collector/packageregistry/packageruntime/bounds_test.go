package packageruntime

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
)

func TestBoundedParsedMetadataRejectsPackageLimitExceeded(t *testing.T) {
	t.Parallel()

	target := packageregistry.TargetConfig{
		Provider:     "jfrog",
		Ecosystem:    packageregistry.EcosystemGeneric,
		Registry:     "https://artifactory.example.com",
		ScopeID:      "package-registry://jfrog/generic/repo",
		PackageLimit: 1,
		VersionLimit: 2,
	}
	parsed := packageregistry.ParsedMetadata{
		Packages: []packageregistry.PackageObservation{
			{Identity: packageregistry.PackageIdentity{
				Ecosystem: packageregistry.EcosystemGeneric,
				Registry:  "https://artifactory.example.com",
				RawName:   "team-api",
			}},
			{Identity: packageregistry.PackageIdentity{
				Ecosystem: packageregistry.EcosystemGeneric,
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

	identity := packageregistry.PackageIdentity{
		Ecosystem: packageregistry.EcosystemNPM,
		Registry:  "registry.npmjs.org",
		RawName:   "left-pad",
	}
	target := packageregistry.TargetConfig{
		Provider:     "npmjs",
		Ecosystem:    packageregistry.EcosystemNPM,
		Registry:     "registry.npmjs.org",
		ScopeID:      "npm://registry.npmjs.org/left-pad",
		PackageLimit: 1,
		VersionLimit: 1,
	}
	parsed := packageregistry.ParsedMetadata{
		Packages: []packageregistry.PackageObservation{
			{Identity: identity},
		},
		Versions: []packageregistry.PackageVersionObservation{
			{Package: identity, Version: "1.0.0"},
		},
		Vulnerables: []packageregistry.VulnerabilityHintObservation{
			{Package: identity, Version: "1.0.0", AdvisoryID: "GHSA-left-pad", AdvisorySource: "npm-audit"},
			{Package: identity, Version: "2.0.0", AdvisoryID: "GHSA-other", AdvisorySource: "npm-audit"},
		},
		Events: []packageregistry.RegistryEventObservation{
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
