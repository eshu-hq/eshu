// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
)

func TestValidateTerraformStateCollectorConfigurationAcceptsBackendFilterWithoutLocalRepos(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"target_scopes": [{
			"target_scope_id": "platform-qa-aws",
			"provider": "aws",
			"deployment_mode": "account_local",
			"credential_mode": "local_workload_identity",
			"allowed_regions": ["us-east-1"],
			"allowed_backends": ["s3"]
		}],
		"discovery": {
			"graph": true,
			"backend_filters": [{
				"target_scope_id": "platform-qa-aws",
				"backend_kind": "s3",
				"bucket": "example-platform-qa-terraform-state",
				"key": "services/api/terraform.tfstate",
				"region": "us-east-1"
			}]
		}
	}`)
	if err != nil {
		t.Fatalf("ValidateTerraformStateCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsUntrimmedBackendFilterKey(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"target_scopes": [{
			"target_scope_id": "platform-qa-aws",
			"provider": "aws",
			"deployment_mode": "account_local",
			"credential_mode": "local_workload_identity",
			"allowed_regions": ["us-east-1"],
			"allowed_backends": ["s3"]
		}],
		"discovery": {
			"graph": true,
			"backend_filters": [{
				"target_scope_id": "platform-qa-aws",
				"backend_kind": "s3",
				"bucket": "example-platform-qa-terraform-state",
				"key": "/services/api/terraform.tfstate",
				"region": "us-east-1"
			}]
		}
	}`)
	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want non-nil")
	}
	if got, want := err.Error(), "key must be relative and trimmed"; !strings.Contains(got, want) {
		t.Fatalf("error = %q, want substring %q", got, want)
	}
}

func TestValidateTerraformStateCollectorConfigurationAcceptsBroadBackendFilterWithLegacyRole(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"aws": {
			"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader"
		},
		"discovery": {
			"graph": true,
			"backend_filters": [{
				"backend_kind": "s3"
			}]
		}
	}`)
	if err != nil {
		t.Fatalf("ValidateTerraformStateCollectorConfiguration() error = %v, want nil", err)
	}
}
