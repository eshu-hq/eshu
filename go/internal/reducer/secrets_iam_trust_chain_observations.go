// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func secretsIAMWildcardTrustObservations(trusts map[string][]facts.Envelope) []SecretsIAMPrivilegePostureObservation {
	var observations []SecretsIAMPrivilegePostureObservation
	for roleARN, envelopes := range trusts {
		for _, envelope := range envelopes {
			if !payloadBool(envelope.Payload, "web_identity_subject_wildcard") {
				continue
			}
			if payloadString(envelope.Payload, "effect") != "Allow" {
				continue
			}
			if !secretsIAMContainsLower(payloadStrings(envelope.Payload, "", "actions"), "sts:assumerolewithwebidentity") {
				continue
			}
			subject := secretsIAMFingerprint("iam_role", roleARN)
			observations = append(observations, SecretsIAMPrivilegePostureObservation{
				ObservationID:      secretsIAMID("privilege_posture_observation", "wildcard_web_identity_subject", subject, envelope.FactID),
				RiskType:           "wildcard_web_identity_subject",
				Severity:           "high",
				State:              SecretsIAMTrustChainStatePartial,
				Confidence:         "partial",
				SubjectFingerprint: subject,
				Reason:             "web identity trust contains a wildcard or broad subject selector",
				EvidenceFactIDs:    []string{envelope.FactID},
			})
		}
	}
	return observations
}

func secretsIAMWildcardVaultAuthRoleObservations(envelopes []facts.Envelope) []SecretsIAMPrivilegePostureObservation {
	var observations []SecretsIAMPrivilegePostureObservation
	for _, envelope := range envelopes {
		if !payloadBool(envelope.Payload, "bound_service_account_selector_wildcard") {
			continue
		}
		subject := secretsIAMFingerprint("vault_auth_role", payloadString(envelope.Payload, "role_join_key"))
		if subject == "" {
			subject = secretsIAMFingerprint("vault_auth_role", envelope.FactID)
		}
		observations = append(observations, SecretsIAMPrivilegePostureObservation{
			ObservationID:      secretsIAMID("privilege_posture_observation", "wildcard_vault_service_account_selector", subject, envelope.FactID),
			RiskType:           "wildcard_vault_service_account_selector",
			Severity:           "high",
			State:              SecretsIAMTrustChainStatePartial,
			Confidence:         "partial",
			SubjectFingerprint: subject,
			Reason:             "Vault Kubernetes auth role contains a wildcard service account selector",
			EvidenceFactIDs:    []string{envelope.FactID},
		})
	}
	return observations
}

func secretsIAMGap(
	gapType string,
	state SecretsIAMTrustChainState,
	reason string,
	serviceAccountKey string,
	evidenceFactIDs []string,
	missingEvidence []string,
	unsupportedLayers []string,
) SecretsIAMPostureGap {
	return SecretsIAMPostureGap{
		GapID:                 secretsIAMID("posture_gap", gapType, serviceAccountKey, strings.Join(evidenceFactIDs, "|")),
		GapType:               gapType,
		State:                 state,
		Reason:                reason,
		ServiceAccountJoinKey: serviceAccountKey,
		EvidenceFactIDs:       uniqueSortedStrings(evidenceFactIDs),
		MissingEvidence:       uniqueSortedStrings(missingEvidence),
		UnsupportedLayers:     uniqueSortedStrings(unsupportedLayers),
	}
}

type vaultPolicyRule struct {
	pathFingerprint string
	capabilities    []string
}

func vaultPolicyRules(policy facts.Envelope) []vaultPolicyRule {
	raw, ok := policy.Payload["rules"]
	if !ok {
		return nil
	}
	var out []vaultPolicyRule
	switch typed := raw.(type) {
	case []map[string]any:
		for _, rule := range typed {
			out = append(out, vaultPolicyRule{
				pathFingerprint: payloadString(rule, "path_fingerprint"),
				capabilities:    payloadStrings(rule, "", "capabilities"),
			})
		}
	case []any:
		for _, item := range typed {
			rule, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, vaultPolicyRule{
				pathFingerprint: payloadString(rule, "path_fingerprint"),
				capabilities:    payloadStrings(rule, "", "capabilities"),
			})
		}
	}
	return out
}
