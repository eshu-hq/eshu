package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
)

func TestLoadRuntimeConfigResolvesTargetsAndCredentialsFromEnv(t *testing.T) {
	env := map[string]string{
		envCollectorInstanceID: "oci-registry-test",
		envTargetsJSON: `[{
			"provider":"jfrog",
			"base_url":"https://example.jfrog.io",
			"repository_key":"docker-local",
			"repository":"service-api",
			"references":["latest"],
			"tag_limit":1,
			"username_env":"JFROG_USER",
			"password_env":"JFROG_TOKEN"
		}]`,
		"JFROG_USER":  "user",
		"JFROG_TOKEN": "token",
	}

	config, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}
	if got, want := config.CollectorInstanceID, "oci-registry-test"; got != want {
		t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := len(config.Targets), 1; got != want {
		t.Fatalf("len(Targets) = %d, want %d", got, want)
	}
	target := config.Targets[0]
	if got, want := target.Provider, ociregistry.ProviderJFrog; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := target.Registry, "https://example.jfrog.io/artifactory/api/docker/docker-local"; got != want {
		t.Fatalf("Registry = %q, want %q", got, want)
	}
	if got, want := target.Username, "user"; got != want {
		t.Fatalf("Username = %q, want %q", got, want)
	}
	if got, want := target.Password, "token"; got != want {
		t.Fatalf("Password = %q, want %q", got, want)
	}
}

func TestLoadRuntimeConfigDefaultsDockerHubRegistry(t *testing.T) {
	env := map[string]string{
		envCollectorInstanceID: "oci-registry-test",
		envTargetsJSON:         `[{"provider":"dockerhub","repository":"busybox","references":["latest"]}]`,
	}
	config, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}
	target := config.Targets[0]
	if got, want := target.Registry, "docker.io"; got != want {
		t.Fatalf("Registry = %q, want %q", got, want)
	}
	if got, want := target.Repository, "library/busybox"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
}

func TestLoadRuntimeConfigMapsHarborGARAndACRTargets(t *testing.T) {
	env := map[string]string{
		envCollectorInstanceID: "oci-registry-test",
		envTargetsJSON: `[
			{"provider":"harbor","base_url":"https://harbor.example.com","repository":"Project/API","references":["latest"]},
			{"provider":"google_artifact_registry","registry_host":"us-west1-docker.pkg.dev","repository":"example-project/team-api/service","references":["sha256:abc"]},
			{"provider":"azure_container_registry","registry_host":"example.azurecr.io","repository":"Samples/Artifact","references":["readme"]}
		]`,
	}

	config, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}
	if got, want := len(config.Targets), 3; got != want {
		t.Fatalf("len(Targets) = %d, want %d", got, want)
	}
	if got, want := config.Targets[0].Registry, "https://harbor.example.com"; got != want {
		t.Fatalf("Harbor Registry = %q, want %q", got, want)
	}
	if got, want := config.Targets[0].Repository, "project/api"; got != want {
		t.Fatalf("Harbor Repository = %q, want %q", got, want)
	}
	if got, want := config.Targets[1].Registry, "https://us-west1-docker.pkg.dev"; got != want {
		t.Fatalf("GAR Registry = %q, want %q", got, want)
	}
	if got, want := config.Targets[2].Registry, "https://example.azurecr.io"; got != want {
		t.Fatalf("ACR Registry = %q, want %q", got, want)
	}
	if got, want := config.Targets[2].Repository, "samples/artifact"; got != want {
		t.Fatalf("ACR Repository = %q, want %q", got, want)
	}
}

func TestLoadClaimedRuntimeConfigSelectsClaimEnabledOCIInstance(t *testing.T) {
	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"collector-oci-registry",
			"collector_kind":"oci_registry",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"provider":"dockerhub",
					"registry":"docker.io",
					"repository":"busybox",
					"references":["latest"],
					"tag_limit":1,
					"username_env":"DOCKERHUB_USER",
					"password_env":"DOCKERHUB_TOKEN"
				}]
			}
		}]`,
		envOwnerID:        "oci-owner-a",
		"DOCKERHUB_USER":  "docker-user",
		"DOCKERHUB_TOKEN": "docker-token",
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v", err)
	}
	if got, want := config.Instance.InstanceID, "collector-oci-registry"; got != want {
		t.Fatalf("InstanceID = %q, want %q", got, want)
	}
	if got, want := config.OwnerID, "oci-owner-a"; got != want {
		t.Fatalf("OwnerID = %q, want %q", got, want)
	}
	if got, want := config.OCI.CollectorInstanceID, "collector-oci-registry"; got != want {
		t.Fatalf("OCI CollectorInstanceID = %q, want %q", got, want)
	}
	target := config.OCI.Targets[0]
	if got, want := target.Provider, ociregistry.ProviderDockerHub; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := target.Registry, "docker.io"; got != want {
		t.Fatalf("Registry = %q, want %q", got, want)
	}
	if got, want := target.Repository, "library/busybox"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
	if got, want := target.Username, "docker-user"; got != want {
		t.Fatalf("Username = %q, want %q", got, want)
	}
	if got, want := target.Password, "docker-token"; got != want {
		t.Fatalf("Password = %q, want %q", got, want)
	}
}

func TestLoadClaimedRuntimeConfigRejectsClaimsDisabledOCIInstance(t *testing.T) {
	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"collector-oci-registry",
			"collector_kind":"oci_registry",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":false,
			"configuration":{"targets":[{"provider":"dockerhub","registry":"registry-1.docker.io","repository":"library/busybox"}]}
		}]`,
	}

	if _, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] }); err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want non-nil")
	}
}
