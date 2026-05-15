package main

import (
	"strings"
	"testing"
)

func TestLoadRuntimeConfigRequiresCentralExternalID(t *testing.T) {
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
						"role_arn":"arn:aws:iam::123456789012:role/eshu-readonly"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
	})

	_, err := loadRuntimeConfig(getenv)
	if err == nil {
		t.Fatalf("loadRuntimeConfig() error = nil, want missing external ID rejection")
	}
	if !strings.Contains(err.Error(), "external_id") {
		t.Fatalf("loadRuntimeConfig() error = %v, want external_id", err)
	}
}

func TestLoadRuntimeConfigRejectsCentralRoleAccountMismatch(t *testing.T) {
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
						"role_arn":"arn:aws:iam::999999999999:role/eshu-readonly",
						"external_id":"external-1"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
	})

	_, err := loadRuntimeConfig(getenv)
	if err == nil {
		t.Fatalf("loadRuntimeConfig() error = nil, want role account mismatch rejection")
	}
	if !strings.Contains(err.Error(), "must match account_id") {
		t.Fatalf("loadRuntimeConfig() error = %v, want account_id mismatch", err)
	}
}

func TestLoadRuntimeConfigRejectsLocalCredentialRoutingFields(t *testing.T) {
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
						"mode":"local_workload_identity",
						"role_arn":"arn:aws:iam::123456789012:role/eshu-readonly"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
	})

	_, err := loadRuntimeConfig(getenv)
	if err == nil {
		t.Fatalf("loadRuntimeConfig() error = nil, want local credential routing rejection")
	}
	if !strings.Contains(err.Error(), "local_workload_identity") {
		t.Fatalf("loadRuntimeConfig() error = %v, want local_workload_identity", err)
	}
}

func TestLoadRuntimeConfigRejectsBroadTargetScope(t *testing.T) {
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
					"allowed_regions":["*"],
					"allowed_services":["iam"],
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
		t.Fatalf("loadRuntimeConfig() error = nil, want wildcard scope rejection")
	}
	if !strings.Contains(err.Error(), "wildcard") {
		t.Fatalf("loadRuntimeConfig() error = %v, want wildcard", err)
	}
}

func TestLoadRuntimeConfigRejectsUnknownAllowedService(t *testing.T) {
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
					"allowed_services":["unknown"],
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
		t.Fatalf("loadRuntimeConfig() error = nil, want unknown service rejection")
	}
	if !strings.Contains(err.Error(), "unsupported allowed service") {
		t.Fatalf("loadRuntimeConfig() error = %v, want unsupported allowed service", err)
	}
}
