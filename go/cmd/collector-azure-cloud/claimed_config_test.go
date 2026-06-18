package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud/azureruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func claimedRuntimeEnv(key string) string {
	switch key {
	case envCollectorInstances:
		return `[{
			"instance_id": "azure-primary",
			"collector_kind": "azure",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"live_collection_enabled": true,
				"scopes": [{
					"enabled": true,
					"tenant_id": "tenant-abc",
					"scope_kind": "subscription",
					"provider_scope_id": "11111111-1111-1111-1111-111111111111",
					"resource_type_family": "microsoft.compute",
					"location_bucket": "eastus",
					"credential_ref": "azure-read-only-spn"
				}]
			}
		}]`
	case envCollectorInstanceID:
		return "azure-primary"
	case envOwnerID:
		return "pod-1"
	default:
		return ""
	}
}

func TestLoadClaimedRuntimeConfigSelectsAzureInstance(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envPollInterval:
			return "2s"
		case envClaimLeaseTTL:
			return "2m"
		case envHeartbeatInterval:
			return "30s"
		default:
			return claimedRuntimeEnv(key)
		}
	})
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.InstanceID, "azure-primary"; got != want {
		t.Fatalf("InstanceID = %q, want %q", got, want)
	}
	if got, want := config.OwnerID, "pod-1"; got != want {
		t.Fatalf("OwnerID = %q, want %q", got, want)
	}
	if got, want := config.PollInterval, 2*time.Second; got != want {
		t.Fatalf("PollInterval = %v, want %v", got, want)
	}
	if got, want := config.ClaimLeaseTTL, 2*time.Minute; got != want {
		t.Fatalf("ClaimLeaseTTL = %v, want %v", got, want)
	}
	if got, want := config.HeartbeatInterval, 30*time.Second; got != want {
		t.Fatalf("HeartbeatInterval = %v, want %v", got, want)
	}
	if got, want := len(config.Source.Targets), 1; got != want {
		t.Fatalf("Source targets = %d, want %d", got, want)
	}
	if got, want := config.CredentialRef, "azure-read-only-spn"; got != want {
		t.Fatalf("CredentialRef = %q, want %q", got, want)
	}
	target := config.Source.Targets[0]
	if target.FencingToken != 0 {
		t.Fatalf("FencingToken = %d, want 0 (claim supplies the token)", target.FencingToken)
	}
}

func TestLoadClaimedRuntimeConfigRejectsLiveModeDisabled(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		if key != envCollectorInstances {
			return ""
		}
		return `[{
			"instance_id": "azure-primary",
			"collector_kind": "azure",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"scopes": [{
					"enabled": true,
					"tenant_id": "tenant-abc",
					"scope_kind": "subscription",
					"provider_scope_id": "11111111-1111-1111-1111-111111111111",
					"credential_ref": "azure-read-only-spn"
				}]
			}
		}]`
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want live mode rejection")
	}
}

func TestLoadClaimedRuntimeConfigRejectsMultipleCredentialRefs(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		if key != envCollectorInstances {
			return ""
		}
		return `[{
			"instance_id": "azure-primary",
			"collector_kind": "azure",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"live_collection_enabled": true,
				"scopes": [
					{"enabled": true, "tenant_id": "t", "scope_kind": "subscription", "provider_scope_id": "s1", "credential_ref": "ref-a"},
					{"enabled": true, "tenant_id": "t", "scope_kind": "subscription", "provider_scope_id": "s2", "credential_ref": "ref-b"}
				]
			}
		}]`
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want one-credential-per-instance rejection")
	}
}

func TestLoadClaimedRuntimeConfigRejectsNonLiveSourceLane(t *testing.T) {
	t.Parallel()

	for _, lane := range []string{"resource_changes", "arm_fallback"} {
		lane := lane
		t.Run(lane, func(t *testing.T) {
			t.Parallel()
			_, err := loadClaimedRuntimeConfig(func(key string) string {
				if key != envCollectorInstances {
					return ""
				}
				return `[{
					"instance_id": "azure-primary",
					"collector_kind": "azure",
					"mode": "continuous",
					"enabled": true,
					"claims_enabled": true,
					"configuration": {
						"live_collection_enabled": true,
						"scopes": [{
							"enabled": true,
							"tenant_id": "tenant-abc",
							"scope_kind": "subscription",
							"provider_scope_id": "11111111-1111-1111-1111-111111111111",
							"credential_ref": "azure-read-only-spn",
							"source_lane": "` + lane + `"
						}]
					}
				}]`
			})
			if err == nil {
				t.Fatalf("loadClaimedRuntimeConfig() error = nil, want rejection of non-live source_lane %q", lane)
			}
		})
	}
}

func TestLoadClaimedRuntimeConfigRejectsHeartbeatNotLessThanLease(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envClaimLeaseTTL:
			return "30s"
		case envHeartbeatInterval:
			return "30s"
		default:
			return claimedRuntimeEnv(key)
		}
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want heartbeat>=lease rejection")
	}
}

func TestParseArgsDefaultsToFixtureMode(t *testing.T) {
	t.Parallel()

	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs(nil) error = %v, want nil", err)
	}
	if opts.mode != launchModeFixture {
		t.Fatalf("mode = %q, want %q", opts.mode, launchModeFixture)
	}
}

func TestParseArgsClaimedLiveRequiresRedactionKey(t *testing.T) {
	t.Parallel()

	if _, err := parseArgs([]string{"-mode", "claimed-live"}); err == nil {
		t.Fatal("parseArgs() error = nil, want -redaction-key-file requirement")
	}
	opts, err := parseArgs([]string{"-mode", "claimed-live", "-redaction-key-file", "k.key"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v, want nil", err)
	}
	if opts.mode != launchModeClaimedLive || opts.redactionKeyPath != "k.key" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestParseArgsRejectsUnknownMode(t *testing.T) {
	t.Parallel()

	if _, err := parseArgs([]string{"-mode", "bogus"}); err == nil {
		t.Fatal("parseArgs() error = nil, want unsupported mode rejection")
	}
}

func TestBuildClaimedServiceWiresLiveClaimRuntime(t *testing.T) {
	oldFactory := newAzureLiveProviderFactory
	t.Cleanup(func() {
		newAzureLiveProviderFactory = oldFactory
	})
	var gotCredentialRef string
	fixture := azureruntime.StaticFixtureFactory(
		azureruntime.NewFixturePageProvider(nil, azurecloud.ScopeAccess{}),
	)
	newAzureLiveProviderFactory = func(_ context.Context, credentialRef string) (azureruntime.PageProviderFactory, error) {
		gotCredentialRef = credentialRef
		return fixture, nil
	}

	service, err := buildClaimedService(
		context.Background(),
		postgres.SQLDB{},
		smokeRedactionKey(t),
		claimedRuntimeEnv,
		noop.NewTracerProvider().Tracer("test"),
		metricnoop.NewMeterProvider().Meter("test"),
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if got, want := service.CollectorInstanceID, "azure-primary"; got != want {
		t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := service.OwnerID, "pod-1"; got != want {
		t.Fatalf("OwnerID = %q, want %q", got, want)
	}
	if got, want := gotCredentialRef, "azure-read-only-spn"; got != want {
		t.Fatalf("credential ref = %q, want %q", got, want)
	}
	if _, ok := service.Source.(*azureruntime.Source); !ok {
		t.Fatalf("Source type = %T, want *azureruntime.Source", service.Source)
	}
}

func smokeRedactionKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("azure-cmd-smoke-redaction-key"))
	if err != nil {
		t.Fatalf("redact.NewKey: %v", err)
	}
	return key
}
