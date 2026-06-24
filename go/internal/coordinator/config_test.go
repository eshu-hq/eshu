// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigParsesCollectorInstances(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "dark"
		case "ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS":
			return "false"
		case "ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL":
			return "45s"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"collector-git-primary","collector_kind":"git","mode":"continuous","enabled":true,"bootstrap":true,"configuration":{"provider":"github"}}]`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := cfg.ReconcileInterval, 45*time.Second; got != want {
		t.Fatalf("ReconcileInterval = %v, want %v", got, want)
	}
	if got, want := cfg.DeploymentMode, "dark"; got != want {
		t.Fatalf("DeploymentMode = %q, want %q", got, want)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("len(CollectorInstances) = %d, want %d", got, want)
	}
}

func TestLoadConfigParsesActiveRuntimeControls(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL":
			return "45s"
		case "ESHU_WORKFLOW_COORDINATOR_RUN_RECONCILE_INTERVAL":
			return "10s"
		case "ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL":
			return "15s"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL":
			return "60s"
		case "ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL":
			return "20s"
		case "ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_LIMIT":
			return "50"
		case "ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_REQUEUE_DELAY":
			return "7s"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"collector-git-primary","collector_kind":"git","mode":"continuous","enabled":true,"bootstrap":true,"claims_enabled":true,"configuration":{"provider":"github"}}]`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := cfg.DeploymentMode, "active"; got != want {
		t.Fatalf("DeploymentMode = %q, want %q", got, want)
	}
	if got, want := cfg.ReapInterval, 15*time.Second; got != want {
		t.Fatalf("ReapInterval = %v, want %v", got, want)
	}
	if got, want := cfg.RunReconcileInterval, 10*time.Second; got != want {
		t.Fatalf("RunReconcileInterval = %v, want %v", got, want)
	}
	if got, want := cfg.ClaimLeaseTTL, 60*time.Second; got != want {
		t.Fatalf("ClaimLeaseTTL = %v, want %v", got, want)
	}
	if got, want := cfg.HeartbeatInterval, 20*time.Second; got != want {
		t.Fatalf("HeartbeatInterval = %v, want %v", got, want)
	}
	if got, want := cfg.ExpiredClaimLimit, 50; got != want {
		t.Fatalf("ExpiredClaimLimit = %d, want %d", got, want)
	}
	if got, want := cfg.ExpiredClaimRequeueDelay, 7*time.Second; got != want {
		t.Fatalf("ExpiredClaimRequeueDelay = %v, want %v", got, want)
	}
}

func TestLoadConfigRejectsInstanceClaimsWhenCoordinatorClaimsDisabled(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "dark"
		case "ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS":
			return "false"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"collector-git-primary","collector_kind":"git","mode":"continuous","enabled":true,"claims_enabled":true}]`
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want non-nil")
	}
}

func TestLoadConfigRejectsActiveModeWithoutClaimsEnabled(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "false"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"collector-git-primary","collector_kind":"git","mode":"continuous","enabled":true,"claims_enabled":true}]`
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want non-nil")
	}
}

func TestLoadConfigRejectsActiveModeWithoutClaimEnabledCollectors(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"collector-git-primary","collector_kind":"git","mode":"continuous","enabled":true,"claims_enabled":false}]`
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want non-nil")
	}
}

func TestLoadConfigRejectsGCPClaimEnabledWithoutExplicitLiveMode(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{
				"instance_id":"collector-gcp-primary",
				"collector_kind":"gcp",
				"mode":"continuous",
				"enabled":true,
				"claims_enabled":true,
				"configuration":{"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project-alpha","credential_ref":"credential-handle"}]}
			}]`
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want GCP live-mode opt-in rejection")
	}
	if got := err.Error(); !strings.Contains(got, `collector_kind "gcp"`) ||
		!strings.Contains(got, "live_collection_enabled=true") {
		t.Fatalf("LoadConfig() error = %q, want explicit GCP live-mode message", got)
	}
}

func TestLoadConfigAcceptsGCPClaimEnabledWithExplicitLiveMode(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{
				"instance_id":"collector-gcp-primary",
				"collector_kind":"gcp",
				"mode":"continuous",
				"enabled":true,
				"claims_enabled":true,
				"configuration":{
					"live_collection_enabled":true,
					"scopes":[{
						"enabled":true,
						"parent_scope_kind":"project",
						"parent_scope_id":"project-alpha",
						"asset_type_family":"compute",
						"content_family":"resource",
						"location_bucket":"global",
						"credential_ref":"credential-handle"
					}]
				}
			}]`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
}

func TestLoadConfigAcceptsClaimDisabledGCPRegistration(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[
				{"instance_id":"collector-git-primary","collector_kind":"git","mode":"continuous","enabled":true,"claims_enabled":true},
				{"instance_id":"collector-gcp-registration","collector_kind":"gcp","mode":"continuous","enabled":true,"claims_enabled":false}
			]`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
}

func TestLoadConfigRejectsHeartbeatAtOrAboveLeaseTTL(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL":
			return "20s"
		case "ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL":
			return "20s"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"collector-git-primary","collector_kind":"git","mode":"continuous","enabled":true,"claims_enabled":true}]`
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want non-nil")
	}
}
