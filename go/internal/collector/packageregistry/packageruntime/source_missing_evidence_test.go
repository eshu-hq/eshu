package packageruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestClaimedSourceCompletesDerivedNotFoundAsWarning(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry",
		DerivedTargets: DerivedTargetConfig{
			Enabled:    true,
			Ecosystems: []packageregistry.Ecosystem{packageregistry.EcosystemNPM},
		},
		Provider: failingMetadataProvider{
			err: collector.RegistryHTTPFailure("npm", "npm", "fetch_metadata", 404, nil),
		},
		Now: func() time.Time {
			return time.Date(2026, time.May, 26, 21, 30, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	collected, ok, err := source.NextClaimed(
		context.Background(),
		testPackageRegistryWorkItemForScope("npm://registry.npmjs.org/@scope/private"),
	)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil missing-evidence warning", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want completed warning generation")
	}

	var warnings int
	for envelope := range collected.Facts {
		if envelope.FactKind != facts.PackageRegistryWarningFactKind {
			t.Fatalf("FactKind = %q, want only warning evidence", envelope.FactKind)
		}
		warnings++
		if got, want := envelope.Payload["warning_code"], warningCodeMetadataNotFound; got != want {
			t.Fatalf("warning_code = %#v, want %q", got, want)
		}
		message, _ := envelope.Payload["message"].(string)
		if strings.Contains(message, "@scope/private") {
			t.Fatalf("warning message leaked package name: %#v", message)
		}
	}
	if warnings != 1 {
		t.Fatalf("warning facts = %d, want 1", warnings)
	}
}

func TestClaimedSourceKeepsConfiguredNotFoundAsError(t *testing.T) {
	t.Parallel()

	source := newTestClaimedSource(t, TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "npm",
			Ecosystem:    packageregistry.EcosystemNPM,
			Registry:     "https://registry.npmjs.org",
			ScopeID:      "npm://registry.npmjs.org/@scope/private",
			Packages:     []string{"@scope/private"},
			PackageLimit: 1,
			VersionLimit: 1,
		},
		MetadataURL: "https://registry.npmjs.org/%40scope%2Fprivate",
	}, staticMetadataProvider{})
	source.provider = failingMetadataProvider{
		err: collector.RegistryHTTPFailure("npm", "npm", "fetch_metadata", 404, nil),
	}

	_, _, err := source.NextClaimed(
		context.Background(),
		testPackageRegistryWorkItemForScope("npm://registry.npmjs.org/@scope/private"),
	)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want configured not-found to remain a collector failure")
	}
	var failure interface{ FailureClass() string }
	if !errors.As(err, &failure) || failure.FailureClass() != collector.RegistryFailureNotFound {
		t.Fatalf("NextClaimed() error = %v, want registry_not_found classification", err)
	}
}

type failingMetadataProvider struct {
	err error
}

func (p failingMetadataProvider) FetchMetadata(context.Context, TargetConfig) (MetadataDocument, error) {
	return MetadataDocument{}, p.err
}
