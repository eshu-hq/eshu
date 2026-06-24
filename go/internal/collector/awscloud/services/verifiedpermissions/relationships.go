// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedpermissions

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// policyInStoreRelationship records a policy's membership in its parent policy
// store. storeID is the resource_id the policy store node publishes (its ARN
// when available), so the edge joins the store node exactly. It returns nil
// when either endpoint identity is missing.
func policyInStoreRelationship(
	boundary awscloud.Boundary,
	storeID string,
	policy Policy,
) *awscloud.RelationshipObservation {
	sourceID := policyResourceID(policy)
	storeID = strings.TrimSpace(storeID)
	if sourceID == "" || storeID == "" {
		return nil
	}
	targetARN := ""
	if isARN(storeID) {
		targetARN = storeID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipVerifiedPermissionsPolicyInStore,
		SourceResourceID: sourceID,
		TargetResourceID: storeID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeVerifiedPermissionsPolicyStore,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipVerifiedPermissionsPolicyInStore + ":" + storeID,
	}
}

// identitySourceInStoreRelationship records an identity source's membership in
// its parent policy store. storeID is the resource_id the policy store node
// publishes. It returns nil when either endpoint identity is missing.
func identitySourceInStoreRelationship(
	boundary awscloud.Boundary,
	storeID string,
	source IdentitySource,
) *awscloud.RelationshipObservation {
	sourceID := identitySourceResourceID(source)
	storeID = strings.TrimSpace(storeID)
	if sourceID == "" || storeID == "" {
		return nil
	}
	targetARN := ""
	if isARN(storeID) {
		targetARN = storeID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipVerifiedPermissionsIdentitySourceInStore,
		SourceResourceID: sourceID,
		TargetResourceID: storeID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeVerifiedPermissionsPolicyStore,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipVerifiedPermissionsIdentitySourceInStore + ":" + storeID,
	}
}

// identitySourceCognitoRelationship records an identity source's dependency on
// an Amazon Cognito user pool. The identity source reports a user pool ARN, but
// the Cognito scanner publishes a user pool node's resource_id as the bare user
// pool id, so the scanner keys the target on the id parsed out of the ARN
// (arn:...:userpool/<user-pool-id>). It returns nil when no Cognito user pool
// is configured or the ARN cannot be parsed, skipping the edge rather than
// dangling it.
func identitySourceCognitoRelationship(
	boundary awscloud.Boundary,
	source IdentitySource,
) *awscloud.RelationshipObservation {
	poolARN := strings.TrimSpace(source.CognitoUserPoolARN)
	if poolARN == "" {
		return nil
	}
	poolID := cognitoUserPoolID(poolARN)
	if poolID == "" {
		return nil
	}
	sourceID := identitySourceResourceID(source)
	if sourceID == "" {
		return nil
	}
	// The Cognito scanner publishes a user pool node's resource_id as the BARE
	// user pool id, so the edge keys the target on poolID and leaves target_arn
	// empty: relguard rejects a bare target_resource_id paired with an ARN-shaped
	// target_arn, and a bare id is exactly what joins the user pool node.
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool,
		SourceResourceID: sourceID,
		TargetResourceID: poolID,
		TargetType:       awscloud.ResourceTypeCognitoUserPool,
		Attributes:       map[string]any{"user_pool_arn": poolARN},
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool + ":" + poolID,
	}
}
