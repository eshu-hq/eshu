package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestPackageRegistryCollectorConfigurationAcceptsBoundedTargets(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com","scope_id":"package-registry://jfrog/generic/team-api","packages":["team-api"],"package_limit":10,"version_limit":25,"metadata_url":"https://artifactory.example.com/api/storage/generic/team-api"}]}`

	if err := ValidatePackageRegistryCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestPackageRegistryCollectorConfigurationRejectsUnboundedTargets(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com","scope_id":"package-registry://jfrog/generic/team-api","package_limit":101}]}`

	err := ValidatePackageRegistryCollectorConfiguration(raw)
	if err == nil {
		t.Fatal("ValidatePackageRegistryCollectorConfiguration() error = nil, want limit rejection")
	}
	if got := err.Error(); !strings.Contains(got, "package_limit must be between 0 and 100") {
		t.Fatalf("ValidatePackageRegistryCollectorConfiguration() error = %q, want package_limit rejection", got)
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
		Configuration:  `{"targets":[{"provider":"npm","ecosystem":"npm","registry":"https://registry.npmjs.org","scope_id":"package-registry://npm/npm/@scope/pkg","packages":["@scope/pkg"],"package_limit":1,"version_limit":2}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("CollectorInstance.Validate() error = %v, want nil", err)
	}
}
