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

func secretsIAMWildcardVaultAuthRoleObservations(roles []secretsIAMVaultRole) []SecretsIAMPrivilegePostureObservation {
	var observations []SecretsIAMPrivilegePostureObservation
	for _, role := range roles {
		if !boolOrFalse(role.decoded.BoundServiceAccountSelectorWildcard) {
			continue
		}
		subject := secretsIAMFingerprint("vault_auth_role", role.decoded.RoleJoinKey)
		if subject == "" {
			subject = secretsIAMFingerprint("vault_auth_role", role.env.FactID)
		}
		observations = append(observations, SecretsIAMPrivilegePostureObservation{
			ObservationID:      secretsIAMID("privilege_posture_observation", "wildcard_vault_service_account_selector", subject, role.env.FactID),
			RiskType:           "wildcard_vault_service_account_selector",
			Severity:           "high",
			State:              SecretsIAMTrustChainStatePartial,
			Confidence:         "partial",
			SubjectFingerprint: subject,
			Reason:             "Vault Kubernetes auth role contains a wildcard service account selector",
			EvidenceFactIDs:    []string{role.env.FactID},
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

// vaultPolicyRules flattens a decoded vault_acl_policy's typed
// []secretsiamv1.VaultACLPolicyRule into the reducer's own vaultPolicyRule
// shape. Every field on the typed nested struct is optional (see
// secretsiam/v1's VaultACLPolicyRule doc comment), so a rule entry with a nil
// PathFingerprint or nil Capabilities degrades to an empty string / nil slice
// here — the same tolerant behavior the pre-typing raw-map parsing had for a
// missing key on one rule in a heterogeneous array, matching how the
// secret-access-path resolution already skips a rule whose PathFingerprint
// does not join to any vault_kv_metadata fact.
func vaultPolicyRules(policy secretsIAMVaultPolicy) []vaultPolicyRule {
	out := make([]vaultPolicyRule, 0, len(policy.decoded.Rules))
	for _, rule := range policy.decoded.Rules {
		out = append(out, vaultPolicyRule{
			pathFingerprint: stringOrEmpty(rule.PathFingerprint),
			capabilities:    rule.Capabilities,
		})
	}
	return out
}
