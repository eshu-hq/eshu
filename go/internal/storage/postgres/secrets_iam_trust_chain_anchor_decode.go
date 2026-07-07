// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// postgresFactDecodeError wraps a classified factschema decode failure for
// storage loaders that feed reducer work. It keeps malformed payloads terminal
// and operator-visible instead of retrying or harvesting zero-value anchors.
type postgresFactDecodeError struct {
	factKind string
	err      *factschema.DecodeError
}

func (e *postgresFactDecodeError) Error() string {
	return fmt.Sprintf("decode %s payload: %s", e.factKind, e.err.Error())
}

func (e *postgresFactDecodeError) Unwrap() error {
	return e.err
}

func (e *postgresFactDecodeError) Retryable() bool {
	return false
}

func (e *postgresFactDecodeError) FailureClass() string {
	return e.err.Classification
}

// newPostgresFactDecodeError preserves factschema classification metadata for
// storage-loader errors that flow into reducer queue failure handling.
func newPostgresFactDecodeError(factKind string, err error) *postgresFactDecodeError {
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		return &postgresFactDecodeError{factKind: factKind, err: decodeErr}
	}
	return &postgresFactDecodeError{
		factKind: factKind,
		err: &factschema.DecodeError{
			FactKind:       factKind,
			Classification: factschema.ClassificationInputInvalid,
			Err:            err,
		},
	}
}

// postgresFactschemaEnvelope adapts the internal fact envelope to the SDK
// decoder input while treating persisted "0.0.0" as a versionless v1 payload.
func postgresFactschemaEnvelope(env facts.Envelope) factschema.Envelope {
	schemaVersion := env.SchemaVersion
	if schemaVersion == "" || schemaVersion == postgresPersistedVersionlessSchemaVersion {
		schemaVersion = postgresDefaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      env.FactKind,
		SchemaVersion: schemaVersion,
		Payload:       env.Payload,
	}
}

const (
	postgresDefaultSchemaMajorVersion         = "1.0.0"
	postgresPersistedVersionlessSchemaVersion = "0.0.0"
)

// addEnvelope decodes one secrets/IAM fact through factschema, then records only
// the join anchors the existing trust-chain SQL predicate can expand.
func (a *secretsIAMTrustChainAnchors) addEnvelope(env facts.Envelope) error {
	schemaEnv := postgresFactschemaEnvelope(env)
	switch env.FactKind {
	case facts.AWSIAMPrincipalFactKind,
		facts.AWSIAMTrustPolicyFactKind,
		facts.AWSIAMPermissionPolicyFactKind,
		facts.AWSIAMPolicyAttachmentFactKind,
		facts.AWSIAMPermissionBoundaryFactKind,
		facts.AWSIAMInstanceProfileFactKind,
		facts.AWSIAMAccessAnalyzerFindingFactKind:
		return a.addAWSEnvelope(schemaEnv, env.FactKind)
	case facts.GCPIAMPrincipalFactKind,
		facts.GCPIAMTrustPolicyFactKind,
		facts.GCPIAMPermissionPolicyFactKind:
		return a.addGCPEnvelope(schemaEnv, env.FactKind)
	case facts.KubernetesServiceAccountFactKind,
		facts.KubernetesWorkloadIdentityUseFactKind,
		facts.KubernetesGCPWorkloadIdentityBindingFactKind,
		facts.KubernetesRBACRoleFactKind,
		facts.KubernetesRBACBindingFactKind,
		facts.KubernetesServiceAccountTokenPostureFactKind,
		facts.EKSIRSAAnnotationFactKind,
		facts.EKSPodIdentityAssociationFactKind:
		return a.addKubernetesEnvelope(schemaEnv, env.FactKind)
	case facts.VaultAuthMountFactKind,
		facts.VaultAuthRoleFactKind,
		facts.VaultACLPolicyFactKind,
		facts.VaultIdentityEntityFactKind,
		facts.VaultIdentityAliasFactKind,
		facts.VaultKVMetadataFactKind,
		facts.VaultSecretEngineMountFactKind:
		return a.addVaultEnvelope(schemaEnv, env.FactKind)
	case facts.SecretsIAMCoverageWarningFactKind:
		if _, err := factschema.DecodeSecretsIAMCoverageWarning(schemaEnv); err != nil {
			return newPostgresFactDecodeError(factschema.FactKindSecretsIAMCoverageWarning, err)
		}
	}
	return nil
}

func (a *secretsIAMTrustChainAnchors) addAWSEnvelope(
	schemaEnv factschema.Envelope,
	factKind string,
) error {
	switch factKind {
	case facts.AWSIAMPrincipalFactKind:
		principal, err := factschema.DecodeAWSIAMPrincipal(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindAWSIAMPrincipal, err)
		}
		a.roleARNs.add(principal.PrincipalARN)
	case facts.AWSIAMTrustPolicyFactKind:
		policy, err := factschema.DecodeAWSIAMTrustPolicy(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindAWSIAMTrustPolicy, err)
		}
		a.roleARNs.add(policy.RoleARN)
		a.webIdentitySubjectFingerprints.addAll(policy.WebIdentitySubjectFingerprints)
	case facts.AWSIAMPermissionPolicyFactKind:
		policy, err := factschema.DecodeAWSIAMPermissionPolicy(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindAWSIAMPermissionPolicy, err)
		}
		a.roleARNs.add(policy.PrincipalARN)
	case facts.AWSIAMPolicyAttachmentFactKind:
		attachment, err := factschema.DecodeAWSIAMPolicyAttachment(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindAWSIAMPolicyAttachment, err)
		}
		a.roleARNs.add(attachment.PrincipalARN)
	case facts.AWSIAMPermissionBoundaryFactKind:
		boundary, err := factschema.DecodeAWSIAMPermissionBoundary(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindAWSIAMPermissionBoundary, err)
		}
		a.roleARNs.add(boundary.PrincipalARN)
	case facts.AWSIAMInstanceProfileFactKind:
		if _, err := factschema.DecodeAWSIAMInstanceProfile(schemaEnv); err != nil {
			return newPostgresFactDecodeError(factschema.FactKindAWSIAMInstanceProfile, err)
		}
	case facts.AWSIAMAccessAnalyzerFindingFactKind:
		if _, err := factschema.DecodeAWSIAMAccessAnalyzerFinding(schemaEnv); err != nil {
			return newPostgresFactDecodeError(factschema.FactKindAWSIAMAccessAnalyzerFinding, err)
		}
	}
	return nil
}

func (a *secretsIAMTrustChainAnchors) addGCPEnvelope(
	schemaEnv factschema.Envelope,
	factKind string,
) error {
	switch factKind {
	case facts.GCPIAMPrincipalFactKind:
		principal, err := factschema.DecodeGCPIAMPrincipal(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindGCPIAMPrincipal, err)
		}
		a.gcpPrincipalFingerprints.add(principal.PrincipalFingerprint)
	case facts.GCPIAMTrustPolicyFactKind:
		policy, err := factschema.DecodeGCPIAMTrustPolicy(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindGCPIAMTrustPolicy, err)
		}
		a.gcpPrincipalFingerprints.add(policy.TargetPrincipalFingerprint)
		a.gcpServiceAccountEmailDigests.add(policy.TargetServiceAccountEmailDigest)
		a.webIdentitySubjectFingerprints.add(stringPtrValue(policy.GCPWorkloadIdentitySubjectFingerprint))
	case facts.GCPIAMPermissionPolicyFactKind:
		policy, err := factschema.DecodeGCPIAMPermissionPolicy(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindGCPIAMPermissionPolicy, err)
		}
		a.gcpPrincipalFingerprints.add(policy.PrincipalFingerprint)
	}
	return nil
}

func (a *secretsIAMTrustChainAnchors) addKubernetesEnvelope(
	schemaEnv factschema.Envelope,
	factKind string,
) error {
	switch factKind {
	case facts.KubernetesServiceAccountFactKind:
		account, err := factschema.DecodeKubernetesServiceAccount(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindKubernetesServiceAccount, err)
		}
		a.serviceAccountJoinKeys.add(account.ServiceAccountJoinKey)
	case facts.KubernetesWorkloadIdentityUseFactKind:
		use, err := factschema.DecodeKubernetesWorkloadIdentityUse(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindKubernetesWorkloadIdentityUse, err)
		}
		a.serviceAccountJoinKeys.add(use.ServiceAccountJoinKey)
	case facts.KubernetesGCPWorkloadIdentityBindingFactKind:
		binding, err := factschema.DecodeKubernetesGCPWorkloadIdentityBinding(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindKubernetesGCPWorkloadIdentityBinding, err)
		}
		a.serviceAccountJoinKeys.add(binding.ServiceAccountJoinKey)
		a.gcpServiceAccountEmailDigests.add(binding.GCPServiceAccountEmailDigest)
		a.webIdentitySubjectFingerprints.add(binding.GCPWorkloadIdentitySubjectFingerprint)
	case facts.KubernetesRBACRoleFactKind:
		if _, err := factschema.DecodeKubernetesRBACRole(schemaEnv); err != nil {
			return newPostgresFactDecodeError(factschema.FactKindKubernetesRBACRole, err)
		}
	case facts.KubernetesRBACBindingFactKind:
		if _, err := factschema.DecodeKubernetesRBACBinding(schemaEnv); err != nil {
			return newPostgresFactDecodeError(factschema.FactKindKubernetesRBACBinding, err)
		}
	case facts.KubernetesServiceAccountTokenPostureFactKind:
		posture, err := factschema.DecodeKubernetesServiceAccountTokenPosture(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindKubernetesServiceAccountTokenPosture, err)
		}
		a.serviceAccountJoinKeys.add(posture.ServiceAccountJoinKey)
	case facts.EKSIRSAAnnotationFactKind:
		annotation, err := factschema.DecodeEKSIRSAAnnotation(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindEKSIRSAAnnotation, err)
		}
		a.serviceAccountJoinKeys.add(annotation.ServiceAccountJoinKey)
		a.roleARNs.add(annotation.RoleARN)
		a.webIdentitySubjectFingerprints.add(stringPtrValue(annotation.WebIdentitySubjectFingerprint))
	case facts.EKSPodIdentityAssociationFactKind:
		association, err := factschema.DecodeEKSPodIdentityAssociation(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindEKSPodIdentityAssociation, err)
		}
		a.serviceAccountJoinKeys.add(association.ServiceAccountJoinKey)
		a.roleARNs.add(association.RoleARN)
	}
	return nil
}

func (a *secretsIAMTrustChainAnchors) addVaultEnvelope(
	schemaEnv factschema.Envelope,
	factKind string,
) error {
	switch factKind {
	case facts.VaultAuthMountFactKind:
		if _, err := factschema.DecodeVaultAuthMount(schemaEnv); err != nil {
			return newPostgresFactDecodeError(factschema.FactKindVaultAuthMount, err)
		}
	case facts.VaultAuthRoleFactKind:
		role, err := factschema.DecodeVaultAuthRole(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindVaultAuthRole, err)
		}
		a.serviceAccountJoinKeys.addAll(role.BoundServiceAccountJoinKeys)
		a.vaultPolicyJoinKeys.addAll(role.TokenPolicyJoinKeys)
	case facts.VaultACLPolicyFactKind:
		policy, err := factschema.DecodeVaultACLPolicy(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindVaultACLPolicy, err)
		}
		a.vaultPolicyJoinKeys.add(policy.PolicyJoinKey)
		for _, rule := range policy.Rules {
			a.vaultKVPathFingerprints.add(stringPtrValue(rule.PathFingerprint))
		}
	case facts.VaultIdentityEntityFactKind:
		if _, err := factschema.DecodeVaultIdentityEntity(schemaEnv); err != nil {
			return newPostgresFactDecodeError(factschema.FactKindVaultIdentityEntity, err)
		}
	case facts.VaultIdentityAliasFactKind:
		if _, err := factschema.DecodeVaultIdentityAlias(schemaEnv); err != nil {
			return newPostgresFactDecodeError(factschema.FactKindVaultIdentityAlias, err)
		}
	case facts.VaultKVMetadataFactKind:
		metadata, err := factschema.DecodeVaultKVMetadata(schemaEnv)
		if err != nil {
			return newPostgresFactDecodeError(factschema.FactKindVaultKVMetadata, err)
		}
		a.vaultKVPathFingerprints.add(metadata.KVPathFingerprint)
	case facts.VaultSecretEngineMountFactKind:
		if _, err := factschema.DecodeVaultSecretEngineMount(schemaEnv); err != nil {
			return newPostgresFactDecodeError(factschema.FactKindVaultSecretEngineMount, err)
		}
	}
	return nil
}

// stringPtrValue returns the pointed string or an empty string for optional
// fields whose absence should not create an anchor.
func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
