// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewGCPTrustPolicyEnvelope builds a gcp_iam_trust_policy source fact for one
// IAM binding on a GCP ServiceAccount resource that grants act-as or token
// creation privilege. The fact carries only redaction-safe target, member, and
// Workload Identity join anchors.
func NewGCPTrustPolicyEnvelope(observation GCPTrustPolicyObservation) (facts.Envelope, error) {
	if err := validateGCPContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	targetFingerprint := strings.TrimSpace(observation.TargetPrincipalFingerprint)
	if targetFingerprint == "" {
		return facts.Envelope{}, fmt.Errorf("gcp trust policy observation requires target_principal_fingerprint")
	}
	trustedFingerprint := strings.TrimSpace(observation.TrustedMemberFingerprint)
	if trustedFingerprint == "" {
		return facts.Envelope{}, fmt.Errorf("gcp trust policy observation requires trusted_member_fingerprint")
	}
	role := strings.TrimSpace(observation.Role)
	if role == "" {
		return facts.Envelope{}, fmt.Errorf("gcp trust policy observation requires role")
	}
	mode := strings.TrimSpace(observation.ImpersonationMode)
	if mode == "" {
		return facts.Envelope{}, fmt.Errorf("gcp trust policy observation requires impersonation_mode")
	}
	emailDigest := strings.TrimSpace(observation.TargetServiceAccountEmailDigest)
	if emailDigest == "" {
		return facts.Envelope{}, fmt.Errorf("gcp trust policy observation requires target_service_account_email_digest")
	}
	conditionFingerprint := strings.TrimSpace(observation.ConditionFingerprint)
	stableKey := facts.StableID(facts.GCPIAMTrustPolicyFactKind, map[string]any{
		"condition_fingerprint":               conditionFingerprint,
		"gcp_workload_identity_subject":       strings.TrimSpace(observation.GCPWorkloadIdentitySubjectFingerprint),
		"impersonation_mode":                  mode,
		"role":                                role,
		"target_principal_fingerprint":        targetFingerprint,
		"target_service_account_email_digest": emailDigest,
		"trusted_member_fingerprint":          trustedFingerprint,
	})
	payload := gcpCommonPayload(observation.Context)
	payload["target_principal_fingerprint"] = targetFingerprint
	payload["target_service_account_email_digest"] = emailDigest
	payload["target_service_account_cloud_resource_uid"] = strings.TrimSpace(observation.TargetServiceAccountCloudResourceUID)
	payload["trusted_member_fingerprint"] = trustedFingerprint
	payload["trusted_member_class"] = strings.TrimSpace(observation.TrustedMemberClass)
	payload["role"] = role
	payload["impersonation_mode"] = mode
	payload["condition_present"] = observation.ConditionPresent
	if conditionFingerprint != "" {
		payload["condition_fingerprint"] = conditionFingerprint
	}
	if subject := strings.TrimSpace(observation.GCPWorkloadIdentitySubjectFingerprint); subject != "" {
		payload["gcp_workload_identity_subject_fingerprint"] = subject
	}
	if memberClass := strings.TrimSpace(observation.GCPWorkloadIdentityMemberClass); memberClass != "" {
		payload["gcp_workload_identity_member_class"] = memberClass
	}
	return newEnvelope(
		gcpToEnvelopeContext(observation.Context),
		facts.GCPIAMTrustPolicyFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, targetFingerprint+"|"+trustedFingerprint+"|"+role),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}
