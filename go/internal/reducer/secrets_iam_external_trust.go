// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// secretsIAMExternalTrustRiskType is the privilege posture risk type for a role
// trust that lets an external or cross-account principal assume the role via
// sts:AssumeRole without an sts:ExternalId condition (the confused-deputy risk).
const secretsIAMExternalTrustRiskType = "external_trust_without_external_id"

// confusedDeputyMitigationKeys are IAM condition keys that constrain a broad
// principal enough to mitigate the confused-deputy risk even without
// sts:ExternalId (organization or source scoping).
var confusedDeputyMitigationKeys = []string{
	"aws:principalorgid",
	"aws:principalorgpaths",
	"aws:sourceaccount",
	"aws:sourcearn",
	"aws:sourceowner",
}

// secretsIAMExternalTrustObservations flags AWS IAM roles whose trust policy
// allows an external (wildcard or cross-account) principal to sts:AssumeRole
// without an sts:ExternalId condition. This is the confused-deputy posture the
// issue names ("which roles have external trust without sts:ExternalId?"). It is
// provenance-only: the observation never becomes an exact path.
//
// The action filter requires sts:AssumeRole specifically, which excludes
// web-identity (sts:AssumeRoleWithWebIdentity) and SAML federation — those use
// OIDC/SAML subject conditions, not sts:ExternalId, and are flagged separately
// by the wildcard web-identity rule.
//
// NotPrincipal-based trust statements are not modeled: the trust-policy fact
// captures only the Principal element, so a NotPrincipal Allow (an AWS
// anti-pattern) is invisible to this rule.
func secretsIAMExternalTrustObservations(
	trusts map[string][]facts.Envelope,
) []SecretsIAMPrivilegePostureObservation {
	var observations []SecretsIAMPrivilegePostureObservation
	for roleARN, envelopes := range trusts {
		roleAccount := awsAccountFromARN(roleARN)
		for _, envelope := range envelopes {
			if payloadString(envelope.Payload, "effect") != "Allow" {
				continue
			}
			if !actionsAllowAssumeRole(payloadStrings(envelope.Payload, "", "actions")) {
				continue
			}
			conditionKeys := payloadStrings(envelope.Payload, "", "condition_keys")
			if secretsIAMContainsLower(conditionKeys, "sts:externalid") {
				continue
			}
			if hasConfusedDeputyMitigation(conditionKeys) {
				continue
			}
			external, wildcard := externalAssumePrincipals(
				payloadStrings(envelope.Payload, "", "assume_principals"),
				roleAccount,
			)
			if !external {
				continue
			}

			subject := secretsIAMFingerprint("iam_role", roleARN)
			severity := "medium"
			reason := "role trust allows a cross-account principal to assume the role via sts:AssumeRole without an sts:ExternalId condition (confused-deputy risk)"
			if wildcard {
				severity = "high"
				reason = "role trust allows a wildcard principal to assume the role via sts:AssumeRole without an sts:ExternalId condition (confused-deputy risk)"
			}
			observations = append(observations, SecretsIAMPrivilegePostureObservation{
				ObservationID:      secretsIAMID("privilege_posture_observation", secretsIAMExternalTrustRiskType, subject, envelope.FactID),
				RiskType:           secretsIAMExternalTrustRiskType,
				Severity:           severity,
				State:              SecretsIAMTrustChainStatePartial,
				Confidence:         "partial",
				SubjectFingerprint: subject,
				Reason:             reason,
				EvidenceFactIDs:    []string{envelope.FactID},
			})
		}
	}
	return observations
}

// actionsAllowAssumeRole reports whether a trust statement's actions grant
// sts:AssumeRole, directly or via the sts:* or * wildcards.
func actionsAllowAssumeRole(actions []string) bool {
	for _, action := range actions {
		switch strings.ToLower(strings.TrimSpace(action)) {
		case "sts:assumerole", "sts:*", "*":
			return true
		}
	}
	return false
}

// externalAssumePrincipals classifies a trust statement's assume principals
// relative to the role's own account. It returns whether any principal is
// external (a wildcard, or a principal in a different account) and whether any
// principal is a wildcard. AWS service principals and same-account principals
// are not external. Both an ARN principal and a bare 12-digit account id are
// treated as account principals, since AWS treats "123456789012" as identical
// to "arn:aws:iam::123456789012:root".
func externalAssumePrincipals(principals []string, roleAccount string) (external bool, wildcard bool) {
	for _, principal := range principals {
		trimmed := strings.TrimSpace(principal)
		if trimmed == "*" {
			return true, true
		}
		account := assumePrincipalAccount(trimmed)
		if account == "" {
			// Service principal (e.g. ec2.amazonaws.com) or a form without an
			// account segment: not the sts:ExternalId confused-deputy pattern.
			continue
		}
		// Guard the empty role account so an unparseable role ARN cannot turn a
		// same-account trust into a false positive; role ARNs always carry an
		// account, so this is defensive only.
		if roleAccount != "" && account != roleAccount {
			external = true
		}
	}
	return external, wildcard
}

// assumePrincipalAccount returns the AWS account id of an assume principal,
// accepting either a bare 12-digit account id or an ARN. It returns "" for AWS
// service principals and any other non-account form.
func assumePrincipalAccount(principal string) string {
	if isAWSAccountID(principal) {
		return principal
	}
	if strings.HasPrefix(principal, "arn:") {
		return awsAccountFromARN(principal)
	}
	return ""
}

// isAWSAccountID reports whether s is a bare 12-digit AWS account id.
func isAWSAccountID(s string) bool {
	if len(s) != 12 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// hasConfusedDeputyMitigation reports whether the condition keys include an
// organization or source scoping condition that mitigates the confused-deputy
// risk even without sts:ExternalId.
func hasConfusedDeputyMitigation(conditionKeys []string) bool {
	for _, key := range confusedDeputyMitigationKeys {
		if secretsIAMContainsLower(conditionKeys, key) {
			return true
		}
	}
	return false
}

// awsAccountFromARN returns the account-id segment of an AWS ARN, or "" if the
// value is not an ARN with an account segment (for example "*" or an AWS
// service principal).
func awsAccountFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) < 5 || parts[0] != "arn" {
		return ""
	}
	return strings.TrimSpace(parts[4])
}
