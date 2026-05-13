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
