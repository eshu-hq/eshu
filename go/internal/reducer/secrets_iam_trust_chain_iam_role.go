// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// iamRoleCloudResourceType is the CloudResource resource_type the AWS resource
// projection assigns to IAM roles. It mirrors awscloud.ResourceTypeIAMRole; the
// reducer duplicates the literal so it does not import the collector package for
// one constant, matching the iam_can_assume slice's resolution.
const iamRoleCloudResourceType = "aws_iam_role"

// secretsIAMRoleCloudResourceUID returns the redaction-safe CloudResource node
// uid for an assumed IAM role, or "" when the read model cannot resolve a
// CloudResource-joinable identity.
//
// The canonical IAM-role CloudResource uid is
// cloudResourceUID(account_id, region, "aws_iam_role", role_arn) where the AWS
// resource collector sets resource_id = role_arn (services/iam roleObservation).
// The only inputs not in the role ARN are the AWS scan boundary's account_id and
// region. Those are carried by the aws_iam_principal source fact this chain
// already requires (secretsIAMExactChains rejects the chain when it is absent),
// so resolving the uid here adds no new collector, source field, or cross-source
// join: it reuses a fact already in hand at the existing build site.
//
// The raw ARN never leaves this function; cloudResourceUID hashes it into the
// one-way node uid, the same value the AWS resource projection and the
// iam_can_assume edge slice compute. When the principal fact omits account_id or
// region the uid stays blank and the graph edge remains skipped+counted (ADR
// #1314 §5.1). It does not parse account/region out of the ARN string: the
// canonical uid uses the collector boundary values, and a parsed region could
// diverge from the boundary's region literal and fabricate a non-matching uid.
func secretsIAMRoleCloudResourceUID(roleARN string, principals []facts.Envelope) string {
	for _, principal := range principals {
		accountID := payloadString(principal.Payload, "account_id")
		region := payloadString(principal.Payload, "region")
		if accountID == "" || region == "" {
			continue
		}
		return cloudResourceUID(accountID, region, iamRoleCloudResourceType, roleARN)
	}
	return ""
}

// secretsIAMRoleAssumeMode classifies the bounded assume-mode for the IAM-role
// edge from the IAM-role evidence kind. EKS Pod Identity associations are
// pod_identity; IRSA annotations are web_identity. It never encodes a role name,
// ARN, or account value.
func secretsIAMRoleAssumeMode(roleEvidenceKind string) string {
	if roleEvidenceKind == facts.EKSPodIdentityAssociationFactKind {
		return secretsIAMAssumeModePodIdentity
	}
	return secretsIAMAssumeModeWebIdentity
}
