package workflow

import (
	"strings"
	"testing"
)

func TestValidateTerraformStateCollectorConfigurationAcceptsTargetScopeS3Seed(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"target_scopes": [
			{
				"target_scope_id": "aws-prod",
				"provider": "aws",
				"deployment_mode": "central",
				"credential_mode": "central_assume_role",
				"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader",
				"external_id": "external-123",
				"allowed_regions": ["us-east-1"],
				"allowed_backends": ["s3"],
				"redaction_policy_ref": "tfstate-prod"
			}
		],
		"discovery": {
			"seeds": [{
				"kind": "s3",
				"target_scope_id": "aws-prod",
				"bucket": "app-tfstate-prod",
				"key": "services/api/terraform.tfstate",
				"region": "us-east-1"
			}]
		}
	}`)

	if err != nil {
		t.Fatalf("ValidateTerraformStateCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsMixedLegacyRoleAndTargetScopes(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"aws": {
			"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader"
		},
		"target_scopes": [
			{
				"target_scope_id": "aws-prod",
				"provider": "aws",
				"deployment_mode": "account_local",
				"credential_mode": "local_workload_identity"
			}
		],
		"discovery": {
			"seeds": [{
				"kind": "local",
				"path": "/tmp/prod.tfstate"
			}]
		}
	}`)

	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want non-nil")
	}
	if got, want := err.Error(), "must not mix aws.role_arn with target_scopes"; !strings.Contains(got, want) {
		t.Fatalf("error = %q, want substring %q", got, want)
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsS3SeedOutsideAllowedRegions(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"target_scopes": [
			{
				"target_scope_id": "aws-prod",
				"provider": "aws",
				"deployment_mode": "central",
				"credential_mode": "central_assume_role",
				"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader",
				"allowed_regions": ["us-west-2"],
				"allowed_backends": ["s3"]
			}
		],
		"discovery": {
			"seeds": [{
				"kind": "s3",
				"target_scope_id": "aws-prod",
				"bucket": "app-tfstate-prod",
				"key": "services/api/terraform.tfstate",
				"region": "us-east-1"
			}]
		}
	}`)

	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want non-nil")
	}
	if got, want := err.Error(), "outside allowed_regions"; !strings.Contains(got, want) {
		t.Fatalf("error = %q, want substring %q", got, want)
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsS3SeedOutsideAllowedBackends(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"target_scopes": [
			{
				"target_scope_id": "aws-prod",
				"provider": "aws",
				"deployment_mode": "account_local",
				"credential_mode": "local_workload_identity",
				"allowed_backends": ["local"]
			}
		],
		"discovery": {
			"seeds": [{
				"kind": "s3",
				"target_scope_id": "aws-prod",
				"bucket": "app-tfstate-prod",
				"key": "services/api/terraform.tfstate",
				"region": "us-east-1"
			}]
		}
	}`)

	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want non-nil")
	}
	if got, want := err.Error(), "outside allowed_backends"; !strings.Contains(got, want) {
		t.Fatalf("error = %q, want substring %q", got, want)
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsAmbiguousS3SeedTargetScope(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"target_scopes": [
			{
				"target_scope_id": "aws-prod-a",
				"provider": "aws",
				"deployment_mode": "account_local",
				"credential_mode": "local_workload_identity"
			},
			{
				"target_scope_id": "aws-prod-b",
				"provider": "aws",
				"deployment_mode": "account_local",
				"credential_mode": "local_workload_identity"
			}
		],
		"discovery": {
			"seeds": [{
				"kind": "s3",
				"bucket": "app-tfstate-prod",
				"key": "services/api/terraform.tfstate",
				"region": "us-east-1"
			}]
		}
	}`)

	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want non-nil")
	}
	if got, want := err.Error(), "target_scope_id"; !strings.Contains(got, want) {
		t.Fatalf("error = %q, want substring %q", got, want)
	}
}

func TestValidateTerraformStateCollectorConfigurationAcceptsApprovedLocalCandidateTargetScope(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"target_scopes": [
			{
				"target_scope_id": "aws-prod",
				"provider": "aws",
				"deployment_mode": "central",
				"credential_mode": "central_assume_role",
				"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader",
				"allowed_backends": ["local"]
			}
		],
		"discovery": {
			"graph": true,
			"local_repos": ["platform-infra"],
			"local_state_candidates": {
				"mode": "approved_candidates",
				"approved": [
					{
						"repo_id": "platform-infra",
						"path": "env/prod/terraform.tfstate",
						"target_scope_id": "aws-prod"
					}
				]
			}
		}
	}`)
	if err != nil {
		t.Fatalf("ValidateTerraformStateCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsUnknownLocalCandidateTargetScope(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"target_scopes": [
			{
				"target_scope_id": "aws-prod",
				"provider": "aws",
				"deployment_mode": "central",
				"credential_mode": "central_assume_role",
				"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader",
				"allowed_backends": ["local"]
			}
		],
		"discovery": {
			"graph": true,
			"local_repos": ["platform-infra"],
			"local_state_candidates": {
				"mode": "approved_candidates",
				"approved": [
					{
						"repo_id": "platform-infra",
						"path": "env/prod/terraform.tfstate",
						"target_scope_id": "aws-dev"
					}
				]
			}
		}
	}`)
	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want unknown target_scope_id rejection")
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsSeedTargetScopeWithoutTargetScopes(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"aws": {
			"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader"
		},
		"discovery": {
			"seeds": [
				{
					"kind": "s3",
					"target_scope_id": "aws-prod",
					"bucket": "app-tfstate-prod",
					"key": "services/api/terraform.tfstate",
					"region": "us-east-1"
				}
			]
		}
	}`)
	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want target_scope_id without target_scopes rejection")
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsConflictingLocalCandidateTargetScopes(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"target_scopes": [
			{
				"target_scope_id": "aws-prod-a",
				"provider": "aws",
				"deployment_mode": "central",
				"credential_mode": "central_assume_role",
				"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader-a",
				"allowed_backends": ["local"]
			},
			{
				"target_scope_id": "aws-prod-b",
				"provider": "aws",
				"deployment_mode": "central",
				"credential_mode": "central_assume_role",
				"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader-b",
				"allowed_backends": ["local"]
			}
		],
		"discovery": {
			"graph": true,
			"local_repos": ["platform-infra"],
			"local_state_candidates": {
				"mode": "approved_candidates",
				"approved": [
					{
						"repo_id": "platform-infra",
						"path": "./env/prod/terraform.tfstate",
						"target_scope_id": "aws-prod-a"
					},
					{
						"repo_id": "platform-infra",
						"path": "env/prod/terraform.tfstate",
						"target_scope_id": "aws-prod-b"
					}
				]
			}
		}
	}`)
	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want conflicting approved target_scope_id rejection")
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsNonCanonicalTargetScopeFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		field   string
		value   string
		wantErr string
	}{
		{name: "provider whitespace", field: "provider", value: " aws ", wantErr: "provider must be lowercase and trimmed"},
		{name: "provider uppercase", field: "provider", value: "AWS", wantErr: "provider must be lowercase and trimmed"},
		{name: "deployment whitespace", field: "deployment_mode", value: " central ", wantErr: "deployment_mode must be lowercase and trimmed"},
		{name: "deployment uppercase", field: "deployment_mode", value: "Central", wantErr: "deployment_mode must be lowercase and trimmed"},
		{name: "credential whitespace", field: "credential_mode", value: " central_assume_role ", wantErr: "credential_mode must be lowercase and trimmed"},
		{name: "credential uppercase", field: "credential_mode", value: "Central_Assume_Role", wantErr: "credential_mode must be lowercase and trimmed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := strings.ReplaceAll(`{
				"target_scopes": [
					{
						"target_scope_id": "aws-prod",
						"provider": "aws",
						"deployment_mode": "central",
						"credential_mode": "central_assume_role",
						"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader"
					}
				],
				"discovery": {
					"seeds": [{
						"kind": "s3",
						"target_scope_id": "aws-prod",
						"bucket": "app-tfstate-prod",
						"key": "services/api/terraform.tfstate",
						"region": "us-east-1"
					}]
				}
			}`, `"`+tt.field+`": "`+canonicalTargetScopeFieldValue(tt.field)+`"`, `"`+tt.field+`": "`+tt.value+`"`)

			err := ValidateTerraformStateCollectorConfiguration(config)
			if err == nil {
				t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want non-nil")
			}
			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", got, tt.wantErr)
			}
		})
	}
}

func canonicalTargetScopeFieldValue(field string) string {
	switch field {
	case "provider":
		return "aws"
	case "deployment_mode":
		return "central"
	case "credential_mode":
		return "central_assume_role"
	default:
		return ""
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsMalformedLocalCandidateApproval(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"discovery": {
			"graph": true,
			"local_repos": ["platform-infra"],
			"local_state_candidates": {
				"mode": "approved_candidates",
				"approved": [{
					"repo_id": "platform-infra",
					"path": "/absolute/terraform.tfstate"
				}]
			}
		}
	}`)

	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want non-nil")
	}
	if got, want := err.Error(), "local_state_candidates"; !strings.Contains(got, want) {
		t.Fatalf("error = %q, want substring %q", got, want)
	}
}

func TestValidateTerraformStateCollectorConfigurationRejectsApprovedModeWithoutApprovals(t *testing.T) {
	t.Parallel()

	err := ValidateTerraformStateCollectorConfiguration(`{
		"discovery": {
			"graph": true,
			"local_repos": ["platform-infra"],
			"local_state_candidates": {
				"mode": "approved_candidates"
			}
		}
	}`)

	if err == nil {
		t.Fatal("ValidateTerraformStateCollectorConfiguration() error = nil, want non-nil")
	}
	if got, want := err.Error(), "approved"; !strings.Contains(got, want) {
		t.Fatalf("error = %q, want substring %q", got, want)
	}
}
