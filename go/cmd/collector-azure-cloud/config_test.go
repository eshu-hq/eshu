package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud/azureruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func envFunc(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

const targetsJSON = `[{
  "tenant_id": "tenant-abc",
  "scope_kind": "subscription",
  "provider_scope_id": "11111111-1111-1111-1111-111111111111",
  "resource_type_family": "microsoft.compute",
  "location_bucket": "eastus",
  "credential_ref": "azure-read-only-spn",
  "fencing_token": 7
}]`

func TestLoadRuntimeConfigParsesTargets(t *testing.T) {
	config, err := loadRuntimeConfig(envFunc(map[string]string{
		envCollectorInstanceID: "azure-collector-1",
		envTargetsJSON:         targetsJSON,
		envPollInterval:        "30s",
	}))
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}
	if config.CollectorInstanceID != "azure-collector-1" {
		t.Fatalf("instance id = %q", config.CollectorInstanceID)
	}
	if len(config.Targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(config.Targets))
	}
	if config.Targets[0].CredentialRef != "azure-read-only-spn" {
		t.Fatalf("credential ref = %q", config.Targets[0].CredentialRef)
	}
}

func TestLoadRuntimeConfigRequiresInstanceAndTargets(t *testing.T) {
	if _, err := loadRuntimeConfig(envFunc(map[string]string{})); err == nil {
		t.Fatal("expected error for missing instance id")
	}
	if _, err := loadRuntimeConfig(envFunc(map[string]string{
		envCollectorInstanceID: "azure-collector-1",
	})); err == nil {
		t.Fatal("expected error for missing targets")
	}
}

func TestBuildProviderFactoryDefaultsToGatedLiveSeam(t *testing.T) {
	factory, err := buildProviderFactory(envFunc(map[string]string{}))
	if err != nil {
		t.Fatalf("buildProviderFactory: %v", err)
	}
	if _, ok := factory.(azureruntime.LiveProviderFactory); !ok {
		t.Fatalf("default factory = %T, want gated LiveProviderFactory", factory)
	}
	if _, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, azureruntime.TargetConfig{}); err == nil {
		t.Fatal("gated live factory must not return a live provider")
	}
}

// TestSmokeFixtureBackedSourceYieldsGeneration proves the binary's declarative
// config plus the file-backed offline provider produce a committable Azure
// generation without any live Azure call.
func TestSmokeFixtureBackedSourceYieldsGeneration(t *testing.T) {
	getenv := envFunc(map[string]string{
		envCollectorInstanceID: "azure-collector-1",
		envTargetsJSON:         targetsJSON,
		envFixturePagesJSON: `{"page_paths": ["` +
			filepath.Join("testdata", "resources_page1.json") + `","` +
			filepath.Join("testdata", "resources_page2.json") + `"]}`,
	})
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}
	factory, err := buildProviderFactory(getenv)
	if err != nil {
		t.Fatalf("buildProviderFactory: %v", err)
	}
	source := &azureruntime.Source{Config: config, ProviderFactory: factory}
	collected, ok, err := source.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next ok=%v err=%v", ok, err)
	}
	var resources int
	for env := range collected.Facts {
		if env.FactKind == facts.AzureCloudResourceFactKind {
			resources++
		}
	}
	if resources != 3 {
		t.Fatalf("fixture smoke emitted %d resources, want 3", resources)
	}
}
