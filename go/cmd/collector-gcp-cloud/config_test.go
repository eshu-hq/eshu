// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/gcpruntime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestLoadFileConfigDerivesRuntimeConfig proves the declarative document parses
// into a valid runtime config with the contract-derived scope id.
func TestLoadFileConfigDerivesRuntimeConfig(t *testing.T) {
	t.Parallel()

	fileCfg, err := loadFileConfig("testdata/config.json")
	if err != nil {
		t.Fatalf("loadFileConfig: %v", err)
	}
	runtimeCfg, err := fileCfg.runtimeConfig()
	if err != nil {
		t.Fatalf("runtimeConfig: %v", err)
	}
	scopes := runtimeCfg.ResolvedScopes()
	if len(scopes) != 1 {
		t.Fatalf("scopes = %d, want 1", len(scopes))
	}
	want := "gcp:project:my-project:mixed:resource:global"
	if scopes[0].ScopeID != want {
		t.Fatalf("scope id = %q, want %q", scopes[0].ScopeID, want)
	}
	files := fileCfg.fixtureFiles(runtimeCfg)
	if len(files[want]) != 2 {
		t.Fatalf("fixture files = %d, want 2", len(files[want]))
	}
}

// TestRuntimeConfigRejectsMissingCredentialRef proves a scope without a
// credential reference fails fast so the runtime never falls back to ambient
// credentials.
func TestRuntimeConfigRejectsMissingCredentialRef(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	doc := `{
      "collector_instance_id": "gcp-instance-1",
      "scopes": [
        {"parent_scope_kind": "project", "parent_scope_id": "p", "fencing_token": 1}
      ]
    }`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	fileCfg, err := loadFileConfig(path)
	if err != nil {
		t.Fatalf("loadFileConfig: %v", err)
	}
	if _, err := fileCfg.runtimeConfig(); err == nil {
		t.Fatal("expected missing credential_ref error, got nil")
	}
}

// TestBuildCollectorServiceWiresGCPSource proves the full service wiring builds a
// fixture-backed source with the configured poll interval.
func TestBuildCollectorServiceWiresGCPSource(t *testing.T) {
	t.Parallel()

	service, err := buildCollectorService(
		postgres.SQLDB{},
		"testdata/config.json",
		smokeRedactionKey(t),
		noop.NewTracerProvider().Tracer("test"),
		metricnoop.NewMeterProvider().Meter("test"),
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildCollectorService: %v", err)
	}
	if got, want := service.PollInterval, 15*time.Minute; got != want {
		t.Fatalf("poll interval = %v, want %v", got, want)
	}
	if _, ok := service.Source.(*gcpruntime.Source); !ok {
		t.Fatalf("source type = %T, want *gcpruntime.Source", service.Source)
	}
}

// TestLoadRedactionKeyFromFile proves the key file is read into a usable key and
// that a blank key file is rejected so facts never carry unkeyed markers.
func TestLoadRedactionKeyFromFile(t *testing.T) {
	t.Parallel()

	key, err := loadRedactionKey("testdata/redaction.key")
	if err != nil {
		t.Fatalf("loadRedactionKey: %v", err)
	}
	if key.IsZero() {
		t.Fatal("loaded redaction key is zero")
	}

	dir := t.TempDir()
	blank := filepath.Join(dir, "blank.key")
	if err := os.WriteFile(blank, []byte("   "), 0o600); err != nil {
		t.Fatalf("write blank key: %v", err)
	}
	if _, err := loadRedactionKey(blank); err == nil {
		t.Fatal("expected blank key rejection, got nil")
	}
}

// TestParseArgsRequiresConfigAndKey proves the binary refuses to start without
// the declarative config and read-only redaction key file paths.
func TestParseArgsRequiresConfigAndKey(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"no config": {"-redaction-key-file", "k"},
		"no key":    {"-config", "c"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseArgs(args); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
	opts, err := parseArgs([]string{"-config", "c.json", "-redaction-key-file", "k.key"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.configPath != "c.json" || opts.redactionKeyPath != "k.key" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestParseArgsAcceptsExplicitClaimedLiveModeWithoutFixtureConfig(t *testing.T) {
	t.Parallel()

	opts, err := parseArgs([]string{"-mode", "claimed-live", "-redaction-key-file", "k.key"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v, want nil", err)
	}
	if got, want := opts.mode, launchModeClaimedLive; got != want {
		t.Fatalf("mode = %q, want %q", got, want)
	}
	if opts.configPath != "" {
		t.Fatalf("configPath = %q, want empty for claimed-live mode", opts.configPath)
	}
}

func TestLoadClaimedRuntimeConfigSelectsGCPInstance(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envCollectorInstances:
			return `[{
				"instance_id": "gcp-primary",
				"collector_kind": "gcp",
				"mode": "continuous",
				"enabled": true,
				"claims_enabled": true,
				"configuration": {
					"live_collection_enabled": true,
					"scopes": [{
						"enabled": true,
						"parent_scope_kind": "project",
						"parent_scope_id": "project-alpha",
						"asset_type_family": "compute",
						"content_family": "resource",
						"location_bucket": "global",
						"credential_ref": "readonly-ref",
						"direct_tags_enabled": true,
						"effective_tags_enabled": true
					}]
				}
			}]`
		case envCollectorInstanceID:
			return "gcp-primary"
		case envPollInterval:
			return "2s"
		case envClaimLeaseTTL:
			return "2m"
		case envHeartbeatInterval:
			return "30s"
		case envOwnerID:
			return "pod-1"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.InstanceID, "gcp-primary"; got != want {
		t.Fatalf("InstanceID = %q, want %q", got, want)
	}
	if got, want := config.OwnerID, "pod-1"; got != want {
		t.Fatalf("OwnerID = %q, want %q", got, want)
	}
	if got, want := config.PollInterval, 2*time.Second; got != want {
		t.Fatalf("PollInterval = %v, want %v", got, want)
	}
	if got, want := len(config.Source.Scopes), 1; got != want {
		t.Fatalf("Source scopes = %d, want %d", got, want)
	}
	scopeCfg := config.Source.Scopes[0]
	if got, want := scopeCfg.ScopeID, "gcp:project:project-alpha:compute:resource:global"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if scopeCfg.FencingToken != 0 {
		t.Fatalf("FencingToken = %d, want work-item supplied token", scopeCfg.FencingToken)
	}
	if got, want := scopeCfg.CredentialRef, "readonly-ref"; got != want {
		t.Fatalf("CredentialRef = %q, want %q", got, want)
	}
	if !scopeCfg.DirectTagsEnabled {
		t.Fatal("DirectTagsEnabled = false, want true")
	}
	if !scopeCfg.EffectiveTagsEnabled {
		t.Fatal("EffectiveTagsEnabled = false, want true")
	}
}

func TestLoadClaimedRuntimeConfigRejectsLiveModeDisabled(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		if key != envCollectorInstances {
			return ""
		}
		return `[{
			"instance_id": "gcp-primary",
			"collector_kind": "gcp",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"scopes": [{
					"enabled": true,
					"parent_scope_kind": "project",
					"parent_scope_id": "project-alpha",
					"credential_ref": "readonly-ref"
				}]
			}
		}]`
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want live mode rejection")
	}
}
