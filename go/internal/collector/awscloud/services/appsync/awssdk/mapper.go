// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappsynctypes "github.com/aws/aws-sdk-go-v2/service/appsync/types"

	appsyncservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appsync"
)

// mapGraphQLAPI maps a GraphQL API to scanner-owned metadata. It collects the
// primary and additional authentication providers (Cognito user pools and OIDC
// issuers) for relationship evidence. The Lambda authorizer config is not mapped
// as a data-source target; it is a separate authorization concern.
func mapGraphQLAPI(api awsappsynctypes.GraphqlApi) appsyncservice.GraphQLAPI {
	out := appsyncservice.GraphQLAPI{
		ID:                 aws.ToString(api.ApiId),
		ARN:                aws.ToString(api.Arn),
		Name:               aws.ToString(api.Name),
		AuthenticationType: string(api.AuthenticationType),
		XrayEnabled:        api.XrayEnabled,
		APIType:            string(api.ApiType),
		Visibility:         string(api.Visibility),
		WAFWebACLARN:       aws.ToString(api.WafWebAclArn),
		Tags:               mapStringMap(api.Tags),
	}
	if api.LogConfig != nil {
		out.LogConfig = &appsyncservice.LogConfig{
			FieldLogLevel:         string(api.LogConfig.FieldLogLevel),
			CloudWatchLogsRoleARN: aws.ToString(api.LogConfig.CloudWatchLogsRoleArn),
			ExcludeVerboseContent: api.LogConfig.ExcludeVerboseContent,
		}
	}
	if api.UserPoolConfig != nil {
		out.UserPools = append(out.UserPools, appsyncservice.UserPoolRef{
			UserPoolID: aws.ToString(api.UserPoolConfig.UserPoolId),
			AwsRegion:  aws.ToString(api.UserPoolConfig.AwsRegion),
		})
	}
	if api.OpenIDConnectConfig != nil {
		if issuer := strings.TrimSpace(aws.ToString(api.OpenIDConnectConfig.Issuer)); issuer != "" {
			out.OIDCIssuers = append(out.OIDCIssuers, issuer)
		}
	}
	for _, provider := range api.AdditionalAuthenticationProviders {
		if provider.UserPoolConfig != nil {
			out.UserPools = append(out.UserPools, appsyncservice.UserPoolRef{
				UserPoolID: aws.ToString(provider.UserPoolConfig.UserPoolId),
				AwsRegion:  aws.ToString(provider.UserPoolConfig.AwsRegion),
			})
		}
		if provider.OpenIDConnectConfig != nil {
			if issuer := strings.TrimSpace(aws.ToString(provider.OpenIDConnectConfig.Issuer)); issuer != "" {
				out.OIDCIssuers = append(out.OIDCIssuers, issuer)
			}
		}
	}
	return out
}

// mapDataSource maps an AppSync data source to scanner-owned metadata. It records
// the backing-resource identity for each supported data source type without
// inlining credentials or authorization headers.
func mapDataSource(ds awsappsynctypes.DataSource) appsyncservice.DataSource {
	out := appsyncservice.DataSource{
		Name:           aws.ToString(ds.Name),
		ARN:            aws.ToString(ds.DataSourceArn),
		Type:           string(ds.Type),
		ServiceRoleARN: aws.ToString(ds.ServiceRoleArn),
	}
	if ds.LambdaConfig != nil {
		out.LambdaFunctionARN = aws.ToString(ds.LambdaConfig.LambdaFunctionArn)
	}
	if ds.DynamodbConfig != nil {
		out.DynamoDBTableName = aws.ToString(ds.DynamodbConfig.TableName)
		out.DynamoDBAwsRegion = aws.ToString(ds.DynamodbConfig.AwsRegion)
	}
	if ds.OpenSearchServiceConfig != nil {
		out.OpenSearchEndpoint = aws.ToString(ds.OpenSearchServiceConfig.Endpoint)
	}
	if ds.ElasticsearchConfig != nil && out.OpenSearchEndpoint == "" {
		out.OpenSearchEndpoint = aws.ToString(ds.ElasticsearchConfig.Endpoint)
	}
	if ds.HttpConfig != nil {
		// Only the endpoint is read; the AuthorizationConfig (which can carry IAM
		// signing config) is intentionally not mapped.
		out.HTTPEndpoint = aws.ToString(ds.HttpConfig.Endpoint)
	}
	if ds.RelationalDatabaseConfig != nil && ds.RelationalDatabaseConfig.RdsHttpEndpointConfig != nil {
		rds := ds.RelationalDatabaseConfig.RdsHttpEndpointConfig
		out.RDSClusterARN = aws.ToString(rds.DbClusterIdentifier)
		out.RDSAwsRegion = aws.ToString(rds.AwsRegion)
		// AwsSecretStoreArn is intentionally not mapped; it points at the
		// credentials secret and is not a join target.
	}
	return out
}

// mapResolver maps an AppSync resolver to scanner-owned metadata. The
// RequestMappingTemplate, ResponseMappingTemplate, and Code fields returned by
// AWS are intentionally never read.
func mapResolver(resolver awsappsynctypes.Resolver) appsyncservice.Resolver {
	out := appsyncservice.Resolver{
		TypeName:       aws.ToString(resolver.TypeName),
		FieldName:      aws.ToString(resolver.FieldName),
		Kind:           string(resolver.Kind),
		DataSourceName: aws.ToString(resolver.DataSourceName),
		ARN:            aws.ToString(resolver.ResolverArn),
	}
	if resolver.Runtime != nil {
		out.RuntimeName = string(resolver.Runtime.Name)
		out.RuntimeVersion = aws.ToString(resolver.Runtime.RuntimeVersion)
	}
	if resolver.PipelineConfig != nil {
		out.PipelineFunctionIDs = cloneStrings(resolver.PipelineConfig.Functions)
	}
	return out
}

// mapFunction maps an AppSync pipeline function to scanner-owned metadata. The
// Code, RequestMappingTemplate, and ResponseMappingTemplate fields returned by
// AWS are intentionally never read.
func mapFunction(function awsappsynctypes.FunctionConfiguration) appsyncservice.Function {
	out := appsyncservice.Function{
		ID:              aws.ToString(function.FunctionId),
		Name:            aws.ToString(function.Name),
		ARN:             aws.ToString(function.FunctionArn),
		DataSourceName:  aws.ToString(function.DataSourceName),
		FunctionVersion: aws.ToString(function.FunctionVersion),
	}
	if function.Runtime != nil {
		out.RuntimeName = string(function.Runtime.Name)
		out.RuntimeVersion = aws.ToString(function.Runtime.RuntimeVersion)
	}
	return out
}

// mapAPIKey maps an AppSync API key to scanner-owned metadata. The key value is
// never returned by ListApiKeys and the scanner-owned type has no field for it.
// Expires and Deletes are epoch seconds in the SDK.
func mapAPIKey(key awsappsynctypes.ApiKey) appsyncservice.APIKey {
	return appsyncservice.APIKey{
		ID:          aws.ToString(key.Id),
		Description: aws.ToString(key.Description),
		Expires:     epochSeconds(key.Expires),
		Deletes:     epochSeconds(key.Deletes),
	}
}

// epochSeconds converts AppSync epoch-second timestamps into time.Time, mapping
// zero (unset) to the zero time so attributes omit it.
func epochSeconds(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(value, 0).UTC()
}

func mapStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		output[key] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
