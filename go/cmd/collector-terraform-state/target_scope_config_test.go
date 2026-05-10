package main

import "testing"

func TestLoadRuntimeConfigParsesCentralAssumeRoleTargetScope(t *testing.T) {
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

	if got, want := len(config.AWSTargetScopes), 1; got != want {
		t.Fatalf("len(AWSTargetScopes) = %d, want %d", got, want)
	}
	scope := config.AWSTargetScopes[0]
	if got, want := scope.TargetScopeID, "aws-prod"; got != want {
		t.Fatalf("TargetScopeID = %q, want %q", got, want)
	}
	if got, want := scope.Credentials.Mode, awsCredentialModeCentralAssumeRole; got != want {
		t.Fatalf("Credentials.Mode = %q, want %q", got, want)
	}
	if got, want := scope.Credentials.RoleARN, "arn:aws:iam::123456789012:role/eshu-tfstate-reader"; got != want {
		t.Fatalf("Credentials.RoleARN = %q, want %q", got, want)
	}
	if got, want := scope.Credentials.ExternalID, "external-123"; got != want {
		t.Fatalf("Credentials.ExternalID = %q, want %q", got, want)
	}
	if got, want := config.AWSCredentials.Mode, awsCredentialModeCentralAssumeRole; got != want {
		t.Fatalf("AWSCredentials.Mode = %q, want %q", got, want)
	}
	if got, want := config.AWSCredentials.ExternalID, "external-123"; got != want {
		t.Fatalf("AWSCredentials.ExternalID = %q, want %q", got, want)
	}
	if got, want := config.AWSRoleARN, "arn:aws:iam::123456789012:role/eshu-tfstate-reader"; got != want {
		t.Fatalf("AWSRoleARN = %q, want %q", got, want)
	}
}

func TestLoadRuntimeConfigParsesAccountLocalTargetScope(t *testing.T) {
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
						"target_scopes": [
							{
								"target_scope_id": "aws-prod",
								"provider": "aws",
								"deployment_mode": "account_local",
								"credential_mode": "local_workload_identity",
								"allowed_regions": ["us-east-1"],
								"allowed_backends": ["s3"]
							}
						],
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

	if got, want := config.AWSCredentials.Mode, awsCredentialModeLocalWorkloadIdentity; got != want {
		t.Fatalf("AWSCredentials.Mode = %q, want %q", got, want)
	}
	if config.AWSCredentials.RoleARN != "" {
		t.Fatalf("AWSCredentials.RoleARN = %q, want blank for default chain", config.AWSCredentials.RoleARN)
	}
	if config.AWSCredentials.ExternalID != "" {
		t.Fatalf("AWSCredentials.ExternalID = %q, want blank for default chain", config.AWSCredentials.ExternalID)
	}
}

func TestLoadRuntimeConfigAcceptsDivergentTargetScopeCredentials(t *testing.T) {
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
						"target_scopes": [
							{
								"target_scope_id": "aws-prod-a",
								"provider": "aws",
								"deployment_mode": "central",
								"credential_mode": "central_assume_role",
								"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader-a"
							},
							{
								"target_scope_id": "aws-prod-b",
								"provider": "aws",
								"deployment_mode": "central",
								"credential_mode": "central_assume_role",
								"role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-reader-b"
							}
						],
						"discovery": {
							"seeds": [
								{
									"kind": "s3",
									"target_scope_id": "aws-prod-a",
									"bucket": "app-tfstate-prod-a",
									"key": "services/api/terraform.tfstate",
									"region": "us-east-1"
								},
								{
									"kind": "s3",
									"target_scope_id": "aws-prod-b",
									"bucket": "app-tfstate-prod-b",
									"key": "services/api/terraform.tfstate",
									"region": "us-east-1"
								}
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
	if got, want := len(config.AWSTargetScopes), 2; got != want {
		t.Fatalf("len(AWSTargetScopes) = %d, want %d", got, want)
	}
	if got, want := config.AWSTargetScopes[0].Credentials.RoleARN, "arn:aws:iam::123456789012:role/eshu-tfstate-reader-a"; got != want {
		t.Fatalf("scope A role ARN = %q, want %q", got, want)
	}
	if got, want := config.AWSTargetScopes[1].Credentials.RoleARN, "arn:aws:iam::123456789012:role/eshu-tfstate-reader-b"; got != want {
		t.Fatalf("scope B role ARN = %q, want %q", got, want)
	}
}

func TestNewAWSClientsCarryExternalIDWithoutLoadingAWSConfig(t *testing.T) {
	t.Parallel()

	credentials := awsCredentialConfig{
		Mode:       awsCredentialModeCentralAssumeRole,
		RoleARN:    "arn:aws:iam::123456789012:role/eshu-tfstate-reader",
		ExternalID: "external-123",
	}
	s3Client, ok := newAWSS3ObjectClient(credentials).(*awsS3ObjectClient)
	if !ok {
		t.Fatalf("newAWSS3ObjectClient() type = %T, want *awsS3ObjectClient", newAWSS3ObjectClient(credentials))
	}
	if got, want := s3Client.externalID, "external-123"; got != want {
		t.Fatalf("s3 externalID = %q, want %q", got, want)
	}

	lockClient, ok := newAWSDynamoDBLockMetadataClient(credentials).(*awsDynamoDBLockMetadataClient)
	if !ok {
		t.Fatalf("newAWSDynamoDBLockMetadataClient() type = %T, want *awsDynamoDBLockMetadataClient", newAWSDynamoDBLockMetadataClient(credentials))
	}
	if got, want := lockClient.externalID, "external-123"; got != want {
		t.Fatalf("dynamodb externalID = %q, want %q", got, want)
	}
}
