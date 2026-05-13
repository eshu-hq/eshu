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

func TestDesiredCollectorInstanceValidateAcceptsOCIRegistryWithClaimsEnabled(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-oci-registry",
		CollectorKind: scope.CollectorOCIRegistry,
		Mode:          CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[{"provider":"dockerhub","registry":"registry-1.docker.io","repository":"library/busybox"}]}`,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestDesiredCollectorInstanceValidateAcceptsOCIRegistryProviderEndpointFields(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-oci-registry",
		CollectorKind: scope.CollectorOCIRegistry,
		Mode:          CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[
			{"provider":"ecr","registry_id":"123456789012","region":"us-east-1","repository":"team/api","references":["latest"]},
			{"provider":"google_artifact_registry","registry_host":"us-west1-docker.pkg.dev","repository":"example-project/team-api/service","references":["sha256:abc"]},
			{"provider":"azure_container_registry","registry_host":"example.azurecr.io","repository":"Samples/Artifact","references":["readme"]},
			{"provider":"jfrog","base_url":"https://example.jfrog.io","repository_key":"docker-local","repository":"service-api","references":["latest"]},
			{"provider":"harbor","base_url":"https://harbor.example.com","repository":"Project/API","references":["latest"]}
		]}`,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestDesiredCollectorInstanceValidateRejectsOCIRegistryClaimsEnabledWithoutTargets(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-oci-registry",
		CollectorKind: scope.CollectorOCIRegistry,
		Mode:          CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
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

func TestDesiredCollectorInstanceValidateRejectsTerraformStateLocalSeedRelativePath(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          CollectorModeScheduled,
		Configuration: `{
			"discovery": {
				"seeds": [{
					"kind": "local",
					"path": "relative/terraform.tfstate"
				}]
			}
		}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestDesiredCollectorInstanceValidateRejectsTerraformStateSeedVersionWhitespace(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          CollectorModeScheduled,
		Configuration: `{
			"discovery": {
				"seeds": [{
					"kind": "local",
					"path": "/workspace/terraform.tfstate",
					"version_id": " version-1 "
				}]
			}
		}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestDesiredCollectorInstanceValidateRejectsTerraformStateLocalSeedVersion(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "collector-tfstate-primary",
		CollectorKind: scope.CollectorTerraformState,
		Mode:          CollectorModeScheduled,
		Configuration: `{
			"discovery": {
				"seeds": [{
					"kind": "local",
					"path": "/workspace/terraform.tfstate",
					"version_id": "version-1"
				}]
			}
		}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
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

func TestCollectorInstanceValidateAcceptsOCIRegistryClaimsEnabled(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 12, 19, 30, 0, 0, time.UTC)
	instance := CollectorInstance{
		InstanceID:     "collector-oci-registry",
		CollectorKind:  scope.CollectorOCIRegistry,
		Mode:           CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[{"provider":"dockerhub","registry":"registry-1.docker.io","repository":"library/busybox"}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
