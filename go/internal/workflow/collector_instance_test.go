package workflow

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestDesiredCollectorInstanceValidateAcceptsWellFormedInstance(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-git-primary",
		CollectorKind: scope.CollectorGit,
		Mode:          CollectorModeContinuous,
		Enabled:       true,
		Bootstrap:     true,
		Configuration: `{"provider":"github"}`,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestDesiredCollectorInstanceValidateRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-git-primary",
		CollectorKind: scope.CollectorGit,
		Mode:          CollectorModeContinuous,
		Configuration: "{",
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestDesiredCollectorInstanceValidateAcceptsTerraformStateGraphDiscoveryWithRepoScope(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          CollectorModeScheduled,
		Configuration: `{"discovery":{"graph":true,"local_repos":["platform-infra"]}}`,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestDesiredCollectorInstanceValidateRejectsTerraformStateGraphDiscoveryWithoutRepoScope(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          CollectorModeScheduled,
		Configuration: `{"discovery":{"graph":true}}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestDesiredCollectorInstanceValidateAcceptsTerraformStateS3SeedWithRole(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          CollectorModeScheduled,
		Configuration: `{
			"discovery": {
				"seeds": [{
					"kind": "s3",
					"bucket": "app-tfstate-prod",
					"key": "services/api/terraform.tfstate",
					"region": "us-east-1"
				}]
			},
			"aws": {
				"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader"
			}
		}`,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestDesiredCollectorInstanceValidateRejectsTerraformStateMissingDiscovery(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          CollectorModeScheduled,
		Configuration: `{}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestDesiredCollectorInstanceValidateRejectsTerraformStateBlankLocalRepo(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          CollectorModeScheduled,
		Configuration: `{"discovery":{"local_repos":[" "]}}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestDesiredCollectorInstanceValidateRejectsTerraformStateS3SeedWithoutRole(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          CollectorModeScheduled,
		Configuration: `{
			"discovery": {
				"seeds": [{
					"kind": "s3",
					"bucket": "app-tfstate-prod",
					"key": "services/api/terraform.tfstate",
					"region": "us-east-1"
				}]
			}
		}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestDesiredCollectorInstanceMaterializeNormalizesConfiguration(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC)
	instance := DesiredCollectorInstance{
		InstanceID:    "collector-git-primary",
		CollectorKind: scope.CollectorGit,
		Mode:          CollectorModeContinuous,
	}

	got := instance.Materialize(observedAt)

	if got.Configuration != "{}" {
		t.Fatalf("Configuration = %q, want {}", got.Configuration)
	}
	if !got.LastObservedAt.Equal(observedAt) {
		t.Fatalf("LastObservedAt = %v, want %v", got.LastObservedAt, observedAt)
	}
}

func TestCollectorInstanceValidateRejectsBackwardsTimes(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC)
	instance := CollectorInstance{
		InstanceID:     "collector-git-primary",
		CollectorKind:  scope.CollectorGit,
		Mode:           CollectorModeContinuous,
		Configuration:  `{}`,
		LastObservedAt: createdAt.Add(-time.Second),
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}
