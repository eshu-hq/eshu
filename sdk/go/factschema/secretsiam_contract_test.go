// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"os"
	"path/filepath"
	"testing"

	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

func TestSecretsIAMFamilyHasSchemaForEverySourceKind(t *testing.T) {
	t.Parallel()

	schemaFiles := []string{
		"aws_iam_principal.v1.schema.json",
		"aws_iam_trust_policy.v1.schema.json",
		"aws_iam_permission_policy.v1.schema.json",
		"aws_iam_policy_attachment.v1.schema.json",
		"aws_iam_permission_boundary.v1.schema.json",
		"aws_iam_instance_profile.v1.schema.json",
		"aws_iam_access_analyzer_finding.v1.schema.json",
		"gcp_iam_principal.v1.schema.json",
		"gcp_iam_trust_policy.v1.schema.json",
		"gcp_iam_permission_policy.v1.schema.json",
		"k8s_service_account.v1.schema.json",
		"k8s_rbac_role.v1.schema.json",
		"k8s_rbac_binding.v1.schema.json",
		"k8s_workload_identity_use.v1.schema.json",
		"k8s_gcp_workload_identity_binding.v1.schema.json",
		"k8s_service_account_token_posture.v1.schema.json",
		"eks_irsa_annotation.v1.schema.json",
		"eks_pod_identity_association.v1.schema.json",
		"vault_auth_mount.v1.schema.json",
		"vault_auth_role.v1.schema.json",
		"vault_acl_policy.v1.schema.json",
		"vault_identity_entity.v1.schema.json",
		"vault_identity_alias.v1.schema.json",
		"vault_kv_metadata.v1.schema.json",
		"vault_secret_engine_mount.v1.schema.json",
		"secrets_iam_coverage_warning.v1.schema.json",
	}

	for _, file := range schemaFiles {
		file := file
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			if _, err := os.Stat(filepath.Join("schema", file)); err != nil {
				t.Fatalf("schema/%s missing or unreadable: %v", file, err)
			}
			if _, err := os.Stat(filepath.Join("fixturepack", "schema", file)); err != nil {
				t.Fatalf("fixturepack/schema/%s missing or unreadable: %v", file, err)
			}
		})
	}
}

func TestSecretsIAMCoverageWarningModelsVaultDiagnostics(t *testing.T) {
	messageFingerprint := "redacted:hmac-sha256:vault-warning-message"
	attributeCount := 2
	warning := secretsiamv1.CoverageWarning{
		Provider:                 "vault",
		CollectorInstanceID:      "collector-vault",
		RedactionPolicyVersion:   "secrets-iam-v1",
		WarningKind:              "vault_read_forbidden",
		SourceState:              "partial",
		VaultClusterID:           contractStringPtr("vault-prod"),
		ResourceScope:            contractStringPtr("secret/data/payments"),
		ErrorClass:               contractStringPtr("permission_denied"),
		MessagePresent:           boolPtr(true),
		MessageFingerprint:       &messageFingerprint,
		AttributeCount:           &attributeCount,
		AttributeKeyFingerprints: []string{"redacted:hmac-sha256:key-a", "redacted:hmac-sha256:key-b"},
	}

	payload, err := EncodeSecretsIAMCoverageWarning(warning)
	if err != nil {
		t.Fatalf("EncodeSecretsIAMCoverageWarning: %v", err)
	}
	decoded, err := DecodeSecretsIAMCoverageWarning(Envelope{
		FactKind:      FactKindSecretsIAMCoverageWarning,
		SchemaVersion: "1.0.0",
		Payload:       payload,
	})
	if err != nil {
		t.Fatalf("DecodeSecretsIAMCoverageWarning: %v", err)
	}

	if decoded.MessagePresent == nil || !*decoded.MessagePresent {
		t.Fatalf("MessagePresent = %#v, want true", decoded.MessagePresent)
	}
	if decoded.MessageFingerprint == nil || *decoded.MessageFingerprint != messageFingerprint {
		t.Fatalf("MessageFingerprint = %#v, want %q", decoded.MessageFingerprint, messageFingerprint)
	}
	if decoded.AttributeCount == nil || *decoded.AttributeCount != attributeCount {
		t.Fatalf("AttributeCount = %#v, want %d", decoded.AttributeCount, attributeCount)
	}
	if len(decoded.AttributeKeyFingerprints) != 2 {
		t.Fatalf("AttributeKeyFingerprints = %#v, want 2 entries", decoded.AttributeKeyFingerprints)
	}
}

func contractStringPtr(value string) *string {
	return &value
}
