// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestPackageRegistryCollectorConfigurationAcceptsBoundedTargets(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com","scope_id":"package-registry://jfrog/generic/team-api","packages":["team-api"],"package_limit":10,"version_limit":25,"document_format":"artifactory_package","metadata_url":"https://artifactory.example.com/api/storage/generic/team-api"}]}`

	if err := ValidatePackageRegistryCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestPackageRegistryCollectorConfigurationRejectsUnknownDocumentFormat(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com","scope_id":"package-registry://jfrog/generic/team-api","packages":["team-api"],"package_limit":10,"version_limit":25,"document_format":"full_registry_crawl","metadata_url":"https://artifactory.example.com/api/storage/generic/team-api"}]}`

	err := ValidatePackageRegistryCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidatePackageRegistryCollectorConfiguration() error = nil, want document_format rejection")
	}
	if got := err.Error(); !strings.Contains(got, `unsupported document_format "full_registry_crawl"`) {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %q, want document_format rejection", got)
	}
}

func TestPackageRegistryCollectorConfigurationRejectsUnboundedTargets(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com","scope_id":"package-registry://jfrog/generic/team-api","package_limit":101,"metadata_url":"https://artifactory.example.com/api/storage/generic/team-api"}]}`

	err := ValidatePackageRegistryCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidatePackageRegistryCollectorConfiguration() error = nil, want limit rejection")
	}
	if got := err.Error(); !strings.Contains(got, "package_limit must be between 0 and 100") {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %q, want package_limit rejection", got)
	}
}

func TestPackageRegistryCollectorConfigurationRejectsBlankDerivedEcosystem(t *testing.T) {
	t.Parallel()

	raw := `{"derive_from_owned_packages":{"enabled":true,"ecosystems":[""]}}`

	err := ValidatePackageRegistryCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidatePackageRegistryCollectorConfiguration() error = nil, want blank ecosystem rejection")
	}
	if got := err.Error(); !strings.Contains(got, "derive_from_owned_packages.ecosystems[0]: ecosystem must not be blank") {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %q, want blank ecosystem rejection", got)
	}
}

func TestPackageRegistryCollectorConfigurationAcceptsFullCorpusDerivedTargetLimit(t *testing.T) {
	t.Parallel()

	raw := `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"planning_mode":"single_pass","target_limit":1000,"package_limit":1,"version_limit":200}}`

	if err := ValidatePackageRegistryCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestPackageRegistryCollectorConfigurationAcceptsDerivedSupportedEcosystems(t *testing.T) {
	t.Parallel()

	raw := `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm","pypi","go","maven","nuget","composer","rubygems","cargo"],"planning_mode":"single_pass","target_limit":1000,"package_limit":1,"version_limit":200}}`

	if err := ValidatePackageRegistryCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestPackageRegistryCollectorConfigurationAcceptsExplicitUnsupportedAdapterTargets(t *testing.T) {
	t.Parallel()

	for _, ecosystem := range []string{"composer", "rubygems", "cargo"} {
		ecosystem := ecosystem
		t.Run(ecosystem, func(t *testing.T) {
			t.Parallel()

			raw := `{"targets":[{"provider":"fixture","ecosystem":"` + ecosystem + `","registry":"https://registry.example.com","scope_id":"` + ecosystem + `://registry.example.com/example-package","packages":["example-package"],"package_limit":1,"version_limit":1,"metadata_url":"https://registry.example.com/example-package"}]}`

			if err := ValidatePackageRegistryCollectorConfiguration(raw); err != nil {
				t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %v, want nil", err)
			}
		})
	}
}

func TestPackageRegistryCollectorConfigurationRejectsUnknownDerivedPlanningMode(t *testing.T) {
	t.Parallel()

	raw := `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"planning_mode":"forever"}}`

	err := ValidatePackageRegistryCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidatePackageRegistryCollectorConfiguration() error = nil, want planning mode rejection")
	}
	if got := err.Error(); !strings.Contains(got, `derive_from_owned_packages.planning_mode: must be "rotating" or "single_pass"`) {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %q, want planning mode rejection", got)
	}
}

func TestPackageRegistryCollectorConfigurationRejectsOverwideDerivedTargetLimit(t *testing.T) {
	t.Parallel()

	raw := `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":5001}}`

	err := ValidatePackageRegistryCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidatePackageRegistryCollectorConfiguration() error = nil, want derived target limit rejection")
	}
	if got := err.Error(); !strings.Contains(got, "derive_from_owned_packages.target_limit must be between 0 and 5000") {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %q, want derived target limit rejection", got)
	}
}

func TestPackageRegistryCollectorConfigurationRejectsMissingMetadataURL(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com","scope_id":"package-registry://jfrog/generic/team-api","packages":["team-api"],"package_limit":1,"version_limit":2}]}`

	err := ValidatePackageRegistryCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidatePackageRegistryCollectorConfiguration() error = nil, want metadata_url rejection")
	}
	if got := err.Error(); !strings.Contains(got, "metadata_url is required") {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %q, want metadata_url rejection", got)
	}
}

func TestPackageRegistryCollectorInstanceValidationUsesPackageRegistryConfig(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 13, 17, 0, 0, 0, time.UTC)
	instance := CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[{"provider":"npm","ecosystem":"npm","registry":"https://registry.npmjs.org","scope_id":"package-registry://npm/npm/@scope/pkg","packages":["@scope/pkg"],"package_limit":1,"version_limit":2,"metadata_url":"https://registry.npmjs.org/@scope%2fpkg"}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("CollectorInstance.Validate() error = %v, want nil", err)
	}
}
