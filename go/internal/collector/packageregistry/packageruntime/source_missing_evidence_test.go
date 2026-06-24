// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	observedAt := time.Date(2026, time.May, 26, 21, 30, 0, 0, time.UTC)
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
			return observedAt
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
	if !collected.Generation.ObservedAt.Equal(observedAt) {
		t.Fatalf("generation ObservedAt = %s, want %s", collected.Generation.ObservedAt, observedAt)
	}
	if !collected.Generation.IngestedAt.Equal(observedAt) {
		t.Fatalf("generation IngestedAt = %s, want %s", collected.Generation.IngestedAt, observedAt)
	}

	var warnings int
	for envelope := range collected.Facts {
		if envelope.FactKind != facts.PackageRegistryWarningFactKind {
			t.Fatalf("FactKind = %q, want only warning evidence", envelope.FactKind)
		}
		warnings++
		if !envelope.ObservedAt.Equal(observedAt) {
			t.Fatalf("warning ObservedAt = %s, want %s", envelope.ObservedAt, observedAt)
		}
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

func TestClaimedSourceCompletesMetadataTooLargeAsCoverageGapWarning(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 28, 14, 30, 0, 0, time.UTC)
	source := newTestClaimedSource(t, TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "npmjs",
			Ecosystem:    packageregistry.EcosystemNPM,
			Registry:     "https://registry.npmjs.org",
			ScopeID:      "package-registry://npmjs/npm/oversized",
			Packages:     []string{"oversized"},
			PackageLimit: 1,
			VersionLimit: 1,
		},
		MetadataURL: "https://registry.npmjs.org/oversized?token=secret",
	}, staticMetadataProvider{})
	source.provider = failingMetadataProvider{
		err: newMetadataTooLargeError(maxMetadataDocumentBytes),
	}
	source.now = func() time.Time { return observedAt }

	collected, ok, err := source.NextClaimed(
		context.Background(),
		testPackageRegistryWorkItemForScope("package-registry://npmjs/npm/oversized"),
	)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil coverage-gap warning", err)
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
		if got, want := envelope.Payload["warning_code"], warningCodeMetadataTooLarge; got != want {
			t.Fatalf("warning_code = %#v, want %q", got, want)
		}
		if got, want := envelope.Payload["package_id"], "npm://registry.npmjs.org/oversized"; got != want {
			t.Fatalf("package_id = %#v, want %q", got, want)
		}
		if got, want := envelope.Payload["ecosystem"], "npm"; got != want {
			t.Fatalf("ecosystem = %#v, want %q", got, want)
		}
		message, _ := envelope.Payload["message"].(string)
		if !strings.Contains(message, "configured byte limit") {
			t.Fatalf("warning message = %q, want configured byte-limit explanation", message)
		}
		for _, leaked := range []string{"oversized", "token=secret", "registry.npmjs.org"} {
			if strings.Contains(message, leaked) {
				t.Fatalf("warning message leaked %q: %q", leaked, message)
			}
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

func TestClaimedSourceCompletesUnsupportedDerivedMetadataSourceAsWarning(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		ecosystem packageregistry.Ecosystem
		scopeID   string
	}{
		{name: "go", ecosystem: packageregistry.EcosystemGoModule, scopeID: "gomod://proxy.golang.org/example.com/acme/lib/v2"},
		{name: "maven", ecosystem: packageregistry.EcosystemMaven, scopeID: "maven://repo.maven.apache.org/maven2/org.example:demo-core"},
		{name: "nuget", ecosystem: packageregistry.EcosystemNuGet, scopeID: "nuget://api.nuget.org/v3/index.json/newtonsoft.json"},
		{name: "composer", ecosystem: packageregistry.EcosystemComposer, scopeID: "composer://repo.packagist.org/symfony/console"},
		{name: "rubygems", ecosystem: packageregistry.EcosystemRubyGems, scopeID: "rubygems://rubygems.org/rails"},
		{name: "cargo", ecosystem: packageregistry.EcosystemCargo, scopeID: "cargo://crates.io/serde_json"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			source, err := NewClaimedSource(SourceConfig{
				CollectorInstanceID: "collector-package-registry",
				DerivedTargets: DerivedTargetConfig{
					Enabled:    true,
					Ecosystems: []packageregistry.Ecosystem{tc.ecosystem},
				},
				Provider: staticMetadataProvider{document: []byte(`{}`)},
				Now: func() time.Time {
					return time.Date(2026, time.June, 1, 11, 0, 0, 0, time.UTC)
				},
			})
			if err != nil {
				t.Fatalf("NewClaimedSource() error = %v", err)
			}

			collected, ok, err := source.NextClaimed(context.Background(), testPackageRegistryWorkItemForScope(tc.scopeID))
			if err != nil {
				t.Fatalf("NextClaimed() error = %v, want nil unsupported-source warning", err)
			}
			if !ok {
				t.Fatal("NextClaimed() ok = false, want completed warning generation")
			}
			assertSinglePackageRegistryWarningCode(t, collected, warningCodeUnsupportedMetadataSource)
		})
	}
}

func TestClaimedSourceCompletesMalformedMetadataAsWarning(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry",
		DerivedTargets: DerivedTargetConfig{
			Enabled:    true,
			Ecosystems: []packageregistry.Ecosystem{packageregistry.EcosystemPyPI},
		},
		Provider: staticMetadataProvider{document: []byte(`{"info":`)},
		Now: func() time.Time {
			return time.Date(2026, time.June, 1, 11, 15, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), testPackageRegistryWorkItemForScope("pypi://pypi.org/pypi/friendly-bard"))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil malformed-metadata warning", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want completed warning generation")
	}
	assertSinglePackageRegistryWarningCode(t, collected, warningCodeMalformedMetadata)
}

func TestClaimedSourceCompletesMissingCredentialsAsWarning(t *testing.T) {
	t.Parallel()

	source := newTestClaimedSource(t, TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "generic-private",
			Ecosystem:    packageregistry.EcosystemGeneric,
			Registry:     "https://registry.example.com",
			ScopeID:      "generic://registry.example.com/team/tool",
			Packages:     []string{"team/tool"},
			PackageLimit: 1,
			VersionLimit: 1,
			Visibility:   packageregistry.VisibilityPrivate,
		},
		MetadataURL: "https://registry.example.com/team/tool",
	}, staticMetadataProvider{document: []byte(`{}`)})

	collected, ok, err := source.NextClaimed(context.Background(), testPackageRegistryWorkItemForScope("generic://registry.example.com/team/tool"))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil missing-credentials warning", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want completed warning generation")
	}
	envelope := assertSinglePackageRegistryWarningCode(t, collected, warningCodeCredentialsMissing)
	for _, privateField := range []string{"registry", "package_id", "source_uri"} {
		if value := envelope.Payload[privateField]; value != nil && value != "" {
			t.Fatalf("credentials warning payload[%q] = %#v, want omitted private target detail", privateField, value)
		}
	}
	if value := collected.Scope.Metadata["registry"]; value != "" {
		t.Fatalf("credentials warning scope registry metadata = %q, want omitted private registry URL", value)
	}
	if got, want := envelope.Payload["ecosystem"], string(packageregistry.EcosystemGeneric); got != want {
		t.Fatalf("credentials warning ecosystem = %#v, want %q", got, want)
	}
}

func assertSinglePackageRegistryWarningCode(t *testing.T, collected collector.CollectedGeneration, want string) facts.Envelope {
	t.Helper()

	var warnings int
	var warning facts.Envelope
	for envelope := range collected.Facts {
		if envelope.FactKind != facts.PackageRegistryWarningFactKind {
			t.Fatalf("FactKind = %q, want only warning evidence", envelope.FactKind)
		}
		warning = envelope
		warnings++
		if got := envelope.Payload["warning_code"]; got != want {
			t.Fatalf("warning_code = %#v, want %q", got, want)
		}
		message, _ := envelope.Payload["message"].(string)
		for _, leaked := range []string{"example.com/acme", "org.example", "newtonsoft", "symfony", "rails", "serde", "team/tool", "registry.example.com"} {
			if strings.Contains(strings.ToLower(message), strings.ToLower(leaked)) {
				t.Fatalf("warning message leaked %q: %q", leaked, message)
			}
		}
	}
	if warnings != 1 {
		t.Fatalf("warning facts = %d, want 1", warnings)
	}
	return warning
}

type failingMetadataProvider struct {
	err error
}

func (p failingMetadataProvider) FetchMetadata(context.Context, TargetConfig) (MetadataDocument, error) {
	return MetadataDocument{}, p.err
}
