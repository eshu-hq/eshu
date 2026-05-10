package main

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLoadRuntimeConfigSelectsTerraformStateInstance(t *testing.T) {
	t.Parallel()

	config, err := loadRuntimeConfig(func(key string) string {
		values := map[string]string{
			"ESHU_COLLECTOR_INSTANCES_JSON": `[
				{
					"instance_id": "collector-git-primary",
					"collector_kind": "git",
					"mode": "continuous",
					"enabled": true,
					"claims_enabled": false,
					"configuration": {}
				},
				{
					"instance_id": "terraform-state-prod",
					"collector_kind": "terraform_state",
					"mode": "continuous",
					"enabled": true,
					"claims_enabled": true,
					"display_name": "Terraform State Prod",
					"configuration": {
						"aws": {
							"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-read",
							"dynamodb_table": "tfstate-locks"
						},
						"discovery": {
							"seeds": [
								{
									"kind": "local",
									"path": "/tmp/prod.tfstate",
									"repo_id": "platform-infra"
								}
							]
						}
					}
				}
			]`,
			"ESHU_TFSTATE_COLLECTOR_INSTANCE_ID":     "terraform-state-prod",
			"ESHU_TFSTATE_COLLECTOR_OWNER_ID":        "worker-a",
			"ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL":   "3s",
			"ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL": "45s",
			"ESHU_TFSTATE_COLLECTOR_HEARTBEAT":       "10s",
			"ESHU_TFSTATE_REDACTION_KEY":             "test-redaction-key",
			"ESHU_TFSTATE_SOURCE_MAX_BYTES":          "1048576",
		}
		return values[key]
	})
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}

	if got, want := config.Instance.InstanceID, "terraform-state-prod"; got != want {
		t.Fatalf("InstanceID = %q, want %q", got, want)
	}
	if got, want := config.Instance.CollectorKind, scope.CollectorTerraformState; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := config.Instance.Mode, workflow.CollectorModeContinuous; got != want {
		t.Fatalf("Mode = %q, want %q", got, want)
	}
	if got, want := config.OwnerID, "worker-a"; got != want {
		t.Fatalf("OwnerID = %q, want %q", got, want)
	}
	if got, want := config.PollInterval, 3*time.Second; got != want {
		t.Fatalf("PollInterval = %v, want %v", got, want)
	}
	if got, want := config.ClaimLeaseTTL, 45*time.Second; got != want {
		t.Fatalf("ClaimLeaseTTL = %v, want %v", got, want)
	}
	if got, want := config.HeartbeatInterval, 10*time.Second; got != want {
		t.Fatalf("HeartbeatInterval = %v, want %v", got, want)
	}
	if config.RedactionKey.IsZero() {
		t.Fatal("RedactionKey is zero, want configured key")
	}
	if got, want := config.SourceMaxBytes, int64(1048576); got != want {
		t.Fatalf("SourceMaxBytes = %d, want %d", got, want)
	}
	if got, want := config.AWSRoleARN, "arn:aws:iam::123456789012:role/eshu-tfstate-read"; got != want {
		t.Fatalf("AWSRoleARN = %q, want %q", got, want)
	}
	if got, want := config.AWSDynamoDBLockTable, "tfstate-locks"; got != want {
		t.Fatalf("AWSDynamoDBLockTable = %q, want %q", got, want)
	}
}

func TestLoadRuntimeConfigAcceptsLegacyDynamoDBLockTableName(t *testing.T) {
	t.Parallel()

	config, err := loadRuntimeConfig(func(key string) string {
		values := map[string]string{
			"ESHU_COLLECTOR_INSTANCES_JSON": `[
				{
					"instance_id": "terraform-state-prod",
					"collector_kind": "terraform_state",
					"mode": "continuous",
					"enabled": true,
					"claims_enabled": true,
					"configuration": {
						"aws": {
							"dynamodb_lock_table": "legacy-tfstate-locks"
						},
						"discovery": {
							"seeds": [
								{"kind": "local", "path": "/tmp/prod.tfstate"}
							]
						}
					}
				}
			]`,
			"ESHU_TFSTATE_REDACTION_KEY": "test-redaction-key",
		}
		return values[key]
	})
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.AWSDynamoDBLockTable, "legacy-tfstate-locks"; got != want {
		t.Fatalf("AWSDynamoDBLockTable = %q, want %q", got, want)
	}
}

func TestLoadRuntimeConfigRejectsMissingRedactionKey(t *testing.T) {
	t.Parallel()

	_, err := loadRuntimeConfig(func(key string) string {
		if key == "ESHU_COLLECTOR_INSTANCES_JSON" {
			return singleTerraformStateInstanceJSON()
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want non-nil")
	}
	if got, want := err.Error(), "ESHU_TFSTATE_REDACTION_KEY"; !strings.Contains(got, want) {
		t.Fatalf("loadRuntimeConfig() error = %q, want %q", got, want)
	}
}

func TestLoadRuntimeConfigHeartbeatAliasLeaseErrorNamesBothVariables(t *testing.T) {
	t.Parallel()

	_, err := loadRuntimeConfig(func(key string) string {
		values := map[string]string{
			"ESHU_COLLECTOR_INSTANCES_JSON":          singleTerraformStateInstanceJSON(),
			"ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL": "30s",
			"ESHU_TFSTATE_COLLECTOR_HEARTBEAT":       "30s",
			"ESHU_TFSTATE_REDACTION_KEY":             "test-redaction-key",
		}
		return values[key]
	})
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want non-nil")
	}
	for _, want := range []string{
		"ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL",
		"ESHU_TFSTATE_COLLECTOR_HEARTBEAT",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("loadRuntimeConfig() error = %q, want %q", err.Error(), want)
		}
	}
}

func TestLoadRuntimeConfigRequiresUnambiguousTerraformStateInstance(t *testing.T) {
	t.Parallel()

	_, err := loadRuntimeConfig(func(key string) string {
		values := map[string]string{
			"ESHU_COLLECTOR_INSTANCES_JSON": `[
				{
					"instance_id": "terraform-state-a",
					"collector_kind": "terraform_state",
					"mode": "continuous",
					"enabled": true,
					"claims_enabled": true,
					"configuration": {"discovery": {"seeds": [{"kind": "local", "path": "/tmp/a.tfstate"}]}}
				},
				{
					"instance_id": "terraform-state-b",
					"collector_kind": "terraform_state",
					"mode": "continuous",
					"enabled": true,
					"claims_enabled": true,
					"configuration": {"discovery": {"seeds": [{"kind": "local", "path": "/tmp/b.tfstate"}]}}
				}
			]`,
			"ESHU_TFSTATE_REDACTION_KEY": "test-redaction-key",
		}
		return values[key]
	})
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want non-nil")
	}
	if got, want := err.Error(), "ESHU_TFSTATE_COLLECTOR_INSTANCE_ID"; !strings.Contains(got, want) {
		t.Fatalf("loadRuntimeConfig() error = %q, want %q", got, want)
	}
}

func singleTerraformStateInstanceJSON() string {
	return `[
		{
			"instance_id": "terraform-state-prod",
			"collector_kind": "terraform_state",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"discovery": {
					"seeds": [
						{"kind": "local", "path": "/tmp/prod.tfstate"}
					]
				}
			}
		}
	]`
}
