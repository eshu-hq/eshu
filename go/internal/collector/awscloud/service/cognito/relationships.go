// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cognito

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// identityProviderTargetType labels the external federation provider an
// identity pool trusts. The ARNs are IAM SAML or OIDC provider entities; this
// scanner reports them as join evidence without claiming ownership of an
// IAM-owned resource type.
const identityProviderTargetType = "aws_iam_identity_provider"

// userPoolClientRelationship records the user pool an app client belongs to.
func userPoolClientRelationship(
	boundary awscloud.Boundary,
	client UserPoolClient,
	userPoolARN string,
) *awscloud.RelationshipObservation {
	clientID := strings.TrimSpace(client.ID)
	userPoolID := strings.TrimSpace(client.UserPoolID)
	if clientID == "" || userPoolID == "" {
		return nil
	}
	target := strings.TrimSpace(userPoolARN)
	if target == "" {
		target = userPoolID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCognitoUserPoolClientUsesUserPool,
		SourceResourceID: clientID,
		TargetResourceID: target,
		TargetARN:        strings.TrimSpace(userPoolARN),
		TargetType:       awscloud.ResourceTypeCognitoUserPool,
		SourceRecordID:   clientID + "#user-pool#" + userPoolID,
	}
}

// userPoolLambdaTriggerRelationships records each Lambda trigger ARN a user
// pool invokes. Trigger slots are reported as relationship attributes so a
// single Lambda used by multiple slots still emits distinct evidence.
func userPoolLambdaTriggerRelationships(
	boundary awscloud.Boundary,
	pool UserPool,
) []awscloud.RelationshipObservation {
	poolID := strings.TrimSpace(pool.ID)
	poolARN := strings.TrimSpace(pool.ARN)
	source := poolARN
	if source == "" {
		source = poolID
	}
	if source == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, trigger := range pool.LambdaTriggers {
		arn := strings.TrimSpace(trigger.ARN)
		slot := strings.TrimSpace(trigger.Trigger)
		if arn == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCognitoUserPoolUsesLambdaTrigger,
			SourceResourceID: source,
			SourceARN:        poolARN,
			TargetResourceID: arn,
			TargetARN:        arn,
			TargetType:       awscloud.ResourceTypeLambdaFunction,
			Attributes: map[string]any{
				"trigger": slot,
			},
			SourceRecordID: source + "#lambda-trigger#" + slot + "#" + arn,
		})
	}
	return observations
}

// identityPoolRelationships records the user pool app clients and external
// providers an identity pool trusts.
func identityPoolRelationships(
	boundary awscloud.Boundary,
	pool IdentityPool,
) []awscloud.RelationshipObservation {
	poolARN := strings.TrimSpace(pool.ARN)
	source := poolARN
	if source == "" {
		source = strings.TrimSpace(pool.ID)
	}
	if source == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, provider := range pool.UserPoolProviders {
		providerName := strings.TrimSpace(provider.ProviderName)
		if providerName == "" {
			continue
		}
		// AWS reports the Cognito login provider as
		// "cognito-idp.<region>.amazonaws.com/<userPoolId>". The user pool
		// resource fact publishes the bare pool ID as its resource_id and
		// correlation anchor, so the edge must target that pool ID rather than
		// the compound provider-name string. Emitting the full provider name
		// produces a dangling edge that never joins the user pool node.
		userPoolID := userPoolIDFromProviderName(providerName)
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCognitoIdentityPoolUsesUserPool,
			SourceResourceID: source,
			SourceARN:        poolARN,
			TargetResourceID: userPoolID,
			TargetType:       awscloud.ResourceTypeCognitoUserPool,
			Attributes: map[string]any{
				"client_id":     strings.TrimSpace(provider.ClientID),
				"provider_name": providerName,
				"user_pool_id":  userPoolID,
			},
			SourceRecordID: source + "#user-pool-provider#" + providerName,
		})
	}
	for _, providerARN := range externalProviderARNs(pool) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCognitoIdentityPoolUsesIdentityProvider,
			SourceResourceID: source,
			SourceARN:        poolARN,
			TargetResourceID: providerARN,
			TargetARN:        providerARN,
			TargetType:       identityProviderTargetType,
			SourceRecordID:   source + "#identity-provider#" + providerARN,
		})
	}
	return observations
}

// userPoolIDFromProviderName extracts the user pool ID a Cognito login provider
// references. AWS reports the provider name as
// "cognito-idp.<region>.amazonaws.com/<userPoolId>", so the user pool ID is the
// final path segment. When the name is not in that compound form (an
// unexpected shape), the trimmed provider name is returned unchanged so the
// edge still carries the strongest available join key rather than an empty
// target.
func userPoolIDFromProviderName(providerName string) string {
	providerName = strings.TrimSpace(providerName)
	if idx := strings.LastIndex(providerName, "/"); idx >= 0 {
		if poolID := strings.TrimSpace(providerName[idx+1:]); poolID != "" {
			return poolID
		}
	}
	return providerName
}

// externalProviderARNs returns the OIDC and SAML provider ARNs attached to an
// identity pool, in a stable order.
func externalProviderARNs(pool IdentityPool) []string {
	var arns []string
	for _, arn := range pool.SAMLProviderARNs {
		if trimmed := strings.TrimSpace(arn); trimmed != "" {
			arns = append(arns, trimmed)
		}
	}
	for _, arn := range pool.OpenIDConnectProviderARNs {
		if trimmed := strings.TrimSpace(arn); trimmed != "" {
			arns = append(arns, trimmed)
		}
	}
	return arns
}
