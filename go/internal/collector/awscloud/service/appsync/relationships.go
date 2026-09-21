// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appsync

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// apiAuthRelationships records the Cognito user pools and OIDC issuers a GraphQL
// API authenticates against.
func apiAuthRelationships(boundary awscloud.Boundary, api GraphQLAPI) []awscloud.RelationshipObservation {
	apiID := strings.TrimSpace(api.ID)
	apiARN := strings.TrimSpace(api.ARN)
	if apiID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	for _, ref := range api.UserPools {
		// AppSync reports the bare Cognito user pool ID. The Cognito scanner
		// publishes the user pool node with resource_id firstNonEmpty(poolID,
		// poolARN), so the bare pool ID is the correct join key. Targeting the
		// compound "cognito-idp.<region>.amazonaws.com/<poolId>" provider string
		// would dangle, the same defect fixed in the Cognito scanner.
		poolID := strings.TrimSpace(ref.UserPoolID)
		if poolID == "" {
			continue
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppSyncAPIUsesUserPool,
			SourceResourceID: apiID,
			SourceARN:        apiARN,
			TargetResourceID: poolID,
			TargetType:       awscloud.ResourceTypeCognitoUserPool,
			Attributes: map[string]any{
				"user_pool_region": strings.TrimSpace(ref.AwsRegion),
			},
			SourceRecordID: apiID + "#user-pool#" + poolID,
		})
	}
	for _, issuer := range api.OIDCIssuers {
		issuer = strings.TrimSpace(issuer)
		if issuer == "" {
			continue
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppSyncAPIUsesOIDCIssuer,
			SourceResourceID: apiID,
			SourceARN:        apiARN,
			TargetResourceID: issuer,
			TargetType:       awscloud.AppSyncOIDCIssuerTargetType,
			SourceRecordID:   apiID + "#oidc-issuer#" + issuer,
		})
	}
	return relationships
}

// dataSourceRelationships records the API-to-data-source edge and the
// data-source-to-backing-resource edge for one data source.
func dataSourceRelationships(boundary awscloud.Boundary, api GraphQLAPI, ds DataSource) []awscloud.RelationshipObservation {
	apiID := strings.TrimSpace(api.ID)
	dsID := dataSourceResourceID(api.ID, ds.Name)
	if apiID == "" || strings.TrimSpace(ds.Name) == "" {
		return nil
	}
	relationships := []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppSyncAPIHasDataSource,
		SourceResourceID: apiID,
		SourceARN:        strings.TrimSpace(api.ARN),
		TargetResourceID: dsID,
		TargetARN:        strings.TrimSpace(ds.ARN),
		TargetType:       awscloud.ResourceTypeAppSyncDataSource,
		Attributes: map[string]any{
			"data_source_name": strings.TrimSpace(ds.Name),
			"type":             strings.TrimSpace(ds.Type),
		},
		SourceRecordID: apiID + "#data-source#" + strings.TrimSpace(ds.Name),
	}}
	if target := dataSourceTarget(api, ds); target != nil {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppSyncDataSourceTargetsResource,
			SourceResourceID: dsID,
			SourceARN:        strings.TrimSpace(ds.ARN),
			TargetResourceID: target.resourceID,
			TargetARN:        target.arn,
			TargetType:       target.targetType,
			Attributes: map[string]any{
				"data_source_type": strings.TrimSpace(ds.Type),
			},
			SourceRecordID: dsID + "#targets#" + target.resourceID,
		})
	}
	return relationships
}

// resolverDataSourceRelationship records the data source a resolver invokes.
func resolverDataSourceRelationship(boundary awscloud.Boundary, api GraphQLAPI, resolver Resolver) *awscloud.RelationshipObservation {
	apiID := strings.TrimSpace(api.ID)
	dsName := strings.TrimSpace(resolver.DataSourceName)
	resolverID := resolverResourceID(api.ID, resolver.TypeName, resolver.FieldName)
	if apiID == "" || dsName == "" || resolverID == "" {
		return nil
	}
	dsID := dataSourceResourceID(api.ID, dsName)
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppSyncResolverUsesDataSource,
		SourceResourceID: resolverID,
		SourceARN:        strings.TrimSpace(resolver.ARN),
		TargetResourceID: dsID,
		TargetType:       awscloud.ResourceTypeAppSyncDataSource,
		Attributes: map[string]any{
			"data_source_name": dsName,
		},
		SourceRecordID: resolverID + "#uses#" + dsID,
	}
}

// functionDataSourceRelationship records the data source a pipeline function
// invokes.
func functionDataSourceRelationship(boundary awscloud.Boundary, api GraphQLAPI, function Function) *awscloud.RelationshipObservation {
	apiID := strings.TrimSpace(api.ID)
	dsName := strings.TrimSpace(function.DataSourceName)
	functionID := functionResourceID(api.ID, function.ID, function.Name)
	if apiID == "" || dsName == "" || functionID == "" {
		return nil
	}
	dsID := dataSourceResourceID(api.ID, dsName)
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppSyncFunctionUsesDataSource,
		SourceResourceID: functionID,
		SourceARN:        strings.TrimSpace(function.ARN),
		TargetResourceID: dsID,
		TargetType:       awscloud.ResourceTypeAppSyncDataSource,
		Attributes: map[string]any{
			"data_source_name": dsName,
		},
		SourceRecordID: functionID + "#uses#" + dsID,
	}
}

// dataSourceTargetRef carries the join key, ARN, and target type for a
// data-source-to-backing-resource edge.
type dataSourceTargetRef struct {
	resourceID string
	arn        string
	targetType string
}

// dataSourceTarget derives the backing AWS resource a data source connects to.
// It returns nil for data source types without a join target (for example NONE,
// EVENTBRIDGE, or BEDROCK). Each target sets a non-empty target_type and a join
// key matching how the target scanner publishes its resource_id.
func dataSourceTarget(api GraphQLAPI, ds DataSource) *dataSourceTargetRef {
	if arn := strings.TrimSpace(ds.LambdaFunctionARN); arn != "" {
		// The Lambda scanner publishes the function ARN as its resource_id.
		return &dataSourceTargetRef{resourceID: arn, arn: arn, targetType: awscloud.ResourceTypeLambdaFunction}
	}
	if name := strings.TrimSpace(ds.DynamoDBTableName); name != "" {
		// The DynamoDB scanner prefers the table ARN as resource_id and carries
		// the bare table name as a correlation anchor. AppSync reports the table
		// name and region but no ARN, so synthesize the ARN with a partition
		// derived from a known source ARN. The bare table name is recorded as the
		// fallback join key when the ARN cannot be synthesized.
		tableARN := dynamoDBTableARN(api, ds)
		resourceID := name
		if tableARN != "" {
			resourceID = tableARN
		}
		return &dataSourceTargetRef{resourceID: resourceID, arn: tableARN, targetType: awscloud.ResourceTypeDynamoDBTable}
	}
	if arn := strings.TrimSpace(ds.RDSClusterARN); arn != "" {
		// The RDS scanner publishes the cluster ARN as its resource_id.
		return &dataSourceTargetRef{resourceID: arn, arn: arn, targetType: awscloud.ResourceTypeRDSDBCluster}
	}
	if endpoint := strings.TrimSpace(ds.OpenSearchEndpoint); endpoint != "" {
		return &dataSourceTargetRef{resourceID: endpoint, targetType: awscloud.AppSyncDataSourceTargetTypeOpenSearch}
	}
	if endpoint := strings.TrimSpace(ds.HTTPEndpoint); endpoint != "" {
		return &dataSourceTargetRef{resourceID: endpoint, targetType: awscloud.AppSyncDataSourceTargetTypeHTTPEndpoint}
	}
	return nil
}

// dynamoDBTableARN synthesizes a DynamoDB table ARN from the data-source region
// and table name, deriving the partition and account from a known source ARN so
// no partition is hardcoded. It returns "" when no source ARN or account is
// available to anchor the synthesis.
func dynamoDBTableARN(api GraphQLAPI, ds DataSource) string {
	name := strings.TrimSpace(ds.DynamoDBTableName)
	if name == "" {
		return ""
	}
	partition, account := arnPartitionAndAccount(api.ARN, ds.ARN, ds.ServiceRoleARN, ds.LambdaFunctionARN)
	region := firstNonEmpty(strings.TrimSpace(ds.DynamoDBAwsRegion), strings.TrimSpace(api.regionHint()))
	if partition == "" || account == "" || region == "" {
		return ""
	}
	return "arn:" + partition + ":dynamodb:" + region + ":" + account + ":table/" + name
}

// regionHint extracts the region from the API ARN so a synthesized table ARN can
// fall back to it when the data source omits a region.
func (api GraphQLAPI) regionHint() string {
	parts := strings.Split(strings.TrimSpace(api.ARN), ":")
	if len(parts) < 4 {
		return ""
	}
	return parts[3]
}

// arnPartitionAndAccount returns the partition and account ID from the first
// well-formed ARN provided, so synthesized ARNs never hardcode arn:aws:.
func arnPartitionAndAccount(candidates ...string) (string, string) {
	for _, candidate := range candidates {
		parts := strings.Split(strings.TrimSpace(candidate), ":")
		if len(parts) < 6 || parts[0] != "arn" {
			continue
		}
		partition := strings.TrimSpace(parts[1])
		account := strings.TrimSpace(parts[4])
		if partition != "" && account != "" {
			return partition, account
		}
	}
	return "", ""
}
