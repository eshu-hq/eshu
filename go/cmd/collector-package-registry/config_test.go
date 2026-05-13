package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestLoadClaimedRuntimeConfigSelectsPackageRegistryInstance(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"collector-package-registry",
			"collector_kind":"package_registry",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{"targets":[{
				"provider":"jfrog",
				"ecosystem":"generic",
				"registry":"https://artifactory.example.com",
				"scope_id":"package-registry://jfrog/generic/team-api",
				"packages":["team-api"],
				"package_limit":1,
				"version_limit":2,
				"metadata_url":"https://artifactory.example.com/api/storage/generic/team-api",
				"bearer_token_env":"PACKAGE_TOKEN"
			}]}
		}]`,
		"PACKAGE_TOKEN": "token-123",
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.CollectorKind, scope.CollectorPackageRegistry; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := config.Source.Targets[0].BearerToken, "token-123"; got != want {
		t.Fatalf("BearerToken = %q, want %q", got, want)
	}
	if got := config.Source.Targets[0].Base.SourceURI; strings.Contains(got, "token-123") {
		t.Fatalf("SourceURI = %q, want no credential material", got)
	}
}

func TestLoadClaimedRuntimeConfigRejectsClaimsDisabledInstance(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"collector-package-registry",
			"collector_kind":"package_registry",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":false,
			"configuration":{"targets":[{
				"provider":"jfrog",
				"ecosystem":"generic",
				"registry":"https://artifactory.example.com",
				"scope_id":"package-registry://jfrog/generic/team-api",
				"packages":["team-api"],
				"metadata_url":"https://artifactory.example.com/api/storage/generic/team-api"
			}]}
		}]`,
	}

	err := selectAndValidateForTest(env)
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want claims_enabled rejection")
	}
	if got := err.Error(); !strings.Contains(got, "claim-enabled") {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, want claim-enabled rejection", got)
	}
}

func selectAndValidateForTest(env map[string]string) error {
	_, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	return err
}
