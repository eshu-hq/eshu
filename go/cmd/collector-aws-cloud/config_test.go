package main

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
)

func TestLoadRuntimeConfigMapsAWSTargetScopes(t *testing.T) {
	getenv := mapEnv(map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": `[{
			"instance_id":"collector-aws-1",
			"collector_kind":"aws",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"target_scopes":[{
					"account_id":"123456789012",
					"allowed_regions":["us-east-1"],
					"allowed_services":["iam"],
					"max_concurrent_claims":2,
					"credentials":{
						"mode":"central_assume_role",
						"role_arn":"arn:aws:iam::123456789012:role/eshu-readonly",
						"external_id":"external-1"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID":        "collector-aws-1",
		"ESHU_AWS_COLLECTOR_POLL_INTERVAL":      "2s",
		"ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL":    "1m",
		"ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL": "10s",
		"HOSTNAME":                              "aws-worker-1",
	})

	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}
	if got := config.Instance.InstanceID; got != "collector-aws-1" {
		t.Fatalf("InstanceID = %q, want collector-aws-1", got)
	}
	if config.OwnerID != "aws-worker-1" {
		t.Fatalf("OwnerID = %q, want aws-worker-1", config.OwnerID)
	}
	if config.PollInterval != 2*time.Second {
		t.Fatalf("PollInterval = %s, want 2s", config.PollInterval)
	}
	target := config.AWS.Targets[0]
	if target.AccountID != "123456789012" {
		t.Fatalf("AccountID = %q", target.AccountID)
	}
	if target.AllowedRegions[0] != "us-east-1" {
		t.Fatalf("AllowedRegions = %v", target.AllowedRegions)
	}
	if target.AllowedServices[0] != awscloud.ServiceIAM {
		t.Fatalf("AllowedServices = %v", target.AllowedServices)
	}
	if target.MaxConcurrentClaims != 2 {
		t.Fatalf("MaxConcurrentClaims = %d, want 2", target.MaxConcurrentClaims)
	}
	if target.Credentials.Mode != awsruntime.CredentialModeCentralAssumeRole {
		t.Fatalf("Credential mode = %q", target.Credentials.Mode)
	}
	if target.Credentials.ExternalID != "external-1" {
		t.Fatalf("ExternalID = %q", target.Credentials.ExternalID)
	}
}

func TestLoadRuntimeConfigRejectsStaticCredentialFields(t *testing.T) {
	getenv := mapEnv(map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": `[{
			"instance_id":"collector-aws-1",
			"collector_kind":"aws",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"target_scopes":[{
					"account_id":"123456789012",
					"allowed_regions":["us-east-1"],
					"allowed_services":["iam"],
					"credentials":{
						"mode":"central_assume_role",
						"role_arn":"arn:aws:iam::123456789012:role/eshu-readonly",
						"access_key_id":"AKIA..."
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
	})

	_, err := loadRuntimeConfig(getenv)
	if err == nil {
		t.Fatalf("loadRuntimeConfig() error = nil, want static credential rejection")
	}
}

func TestLoadRuntimeConfigRequiresRedactionKeyForECS(t *testing.T) {
	getenv := mapEnv(map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": `[{
			"instance_id":"collector-aws-1",
			"collector_kind":"aws",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"target_scopes":[{
					"account_id":"123456789012",
					"allowed_regions":["us-east-1"],
					"allowed_services":["ecs"],
					"credentials":{
						"mode":"local_workload_identity"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
	})

	_, err := loadRuntimeConfig(getenv)
	if err == nil {
		t.Fatalf("loadRuntimeConfig() error = nil, want missing redaction key rejection")
	}
	if !strings.Contains(err.Error(), "ESHU_AWS_REDACTION_KEY") {
		t.Fatalf("loadRuntimeConfig() error = %v, want ESHU_AWS_REDACTION_KEY", err)
	}
}

func TestLoadRuntimeConfigMapsRedactionKeyForECS(t *testing.T) {
	getenv := mapEnv(map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": `[{
			"instance_id":"collector-aws-1",
			"collector_kind":"aws",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"target_scopes":[{
					"account_id":"123456789012",
					"allowed_regions":["us-east-1"],
					"allowed_services":["ecs"],
					"credentials":{
						"mode":"local_workload_identity"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
		"ESHU_AWS_REDACTION_KEY":         "aws-redaction-key",
	})

	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}
	if config.AWSRedactionKey.IsZero() {
		t.Fatalf("AWSRedactionKey is zero, want configured key")
	}
}

func mapEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
