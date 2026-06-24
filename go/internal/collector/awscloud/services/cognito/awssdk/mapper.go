// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsidentity "github.com/aws/aws-sdk-go-v2/service/cognitoidentity"
	awsidentitytypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentity/types"
	awsidptypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cognitoservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cognito"
)

func mapUserPool(pool awsidptypes.UserPoolType) cognitoservice.UserPool {
	return cognitoservice.UserPool{
		ID:  aws.ToString(pool.Id),
		ARN: aws.ToString(pool.Arn),
		// pool.Status is deprecated in the Cognito API ("no longer available")
		// and always empty, so it is intentionally not mapped.
		Name:               aws.ToString(pool.Name),
		MFAConfiguration:   string(pool.MfaConfiguration),
		DeletionProtection: string(pool.DeletionProtection),
		Domain:             aws.ToString(pool.Domain),
		CustomDomain:       aws.ToString(pool.CustomDomain),
		EstimatedNumUsers:  pool.EstimatedNumberOfUsers,
		CreatedAt:          aws.ToTime(pool.CreationDate),
		LastModifiedAt:     aws.ToTime(pool.LastModifiedDate),
		PasswordPolicy:     mapPasswordPolicy(pool.Policies),
		LambdaTriggers:     mapLambdaTriggers(pool.LambdaConfig),
		Tags:               mapStringMap(pool.UserPoolTags),
	}
}

func mapPasswordPolicy(policies *awsidptypes.UserPoolPolicyType) *cognitoservice.PasswordPolicy {
	if policies == nil || policies.PasswordPolicy == nil {
		return nil
	}
	policy := policies.PasswordPolicy
	return &cognitoservice.PasswordPolicy{
		MinimumLength:                 aws.ToInt32(policy.MinimumLength),
		RequireUppercase:              policy.RequireUppercase,
		RequireLowercase:              policy.RequireLowercase,
		RequireNumbers:                policy.RequireNumbers,
		RequireSymbols:                policy.RequireSymbols,
		TemporaryPasswordValidityDays: policy.TemporaryPasswordValidityDays,
		PasswordHistorySize:           aws.ToInt32(policy.PasswordHistorySize),
	}
}

// mapLambdaTriggers extracts only the ARN-shaped trigger slots. Custom sender
// triggers (CustomEmailSender, CustomSMSSender) and KMSKeyID are intentionally
// dropped; the custom sender configs can carry message template material and the
// scanner reports trigger ARNs only.
func mapLambdaTriggers(config *awsidptypes.LambdaConfigType) []cognitoservice.LambdaTrigger {
	if config == nil {
		return nil
	}
	slots := []struct {
		trigger string
		arn     *string
	}{
		{"CreateAuthChallenge", config.CreateAuthChallenge},
		{"CustomMessage", config.CustomMessage},
		{"DefineAuthChallenge", config.DefineAuthChallenge},
		{"PostAuthentication", config.PostAuthentication},
		{"PostConfirmation", config.PostConfirmation},
		{"PreAuthentication", config.PreAuthentication},
		{"PreSignUp", config.PreSignUp},
		{"PreTokenGeneration", config.PreTokenGeneration},
		{"UserMigration", config.UserMigration},
		{"VerifyAuthChallengeResponse", config.VerifyAuthChallengeResponse},
	}
	var triggers []cognitoservice.LambdaTrigger
	for _, slot := range slots {
		arn := strings.TrimSpace(aws.ToString(slot.arn))
		if arn == "" {
			continue
		}
		triggers = append(triggers, cognitoservice.LambdaTrigger{Trigger: slot.trigger, ARN: arn})
	}
	return triggers
}

// mapUserPoolClient copies app-client metadata but never reads ClientSecret.
func mapUserPoolClient(client awsidptypes.UserPoolClientType) cognitoservice.UserPoolClient {
	return cognitoservice.UserPoolClient{
		ID:                              aws.ToString(client.ClientId),
		Name:                            aws.ToString(client.ClientName),
		UserPoolID:                      aws.ToString(client.UserPoolId),
		AllowedOAuthFlows:               mapOAuthFlows(client.AllowedOAuthFlows),
		AllowedOAuthScopes:              cloneStrings(client.AllowedOAuthScopes),
		AllowedOAuthFlowsUserPoolClient: aws.ToBool(client.AllowedOAuthFlowsUserPoolClient),
		CallbackURLs:                    cloneStrings(client.CallbackURLs),
		LogoutURLs:                      cloneStrings(client.LogoutURLs),
		SupportedIdentityProviders:      cloneStrings(client.SupportedIdentityProviders),
		ExplicitAuthFlows:               mapExplicitAuthFlows(client.ExplicitAuthFlows),
		CreatedAt:                       aws.ToTime(client.CreationDate),
		LastModifiedAt:                  aws.ToTime(client.LastModifiedDate),
	}
}

// mapIdentityProvider copies provider identity but never reads ProviderDetails,
// which carries client_secret and similar federation secrets.
func mapIdentityProvider(poolID string, provider awsidptypes.ProviderDescription) cognitoservice.IdentityProvider {
	return cognitoservice.IdentityProvider{
		UserPoolID:     poolID,
		ProviderName:   aws.ToString(provider.ProviderName),
		ProviderType:   string(provider.ProviderType),
		CreatedAt:      aws.ToTime(provider.CreationDate),
		LastModifiedAt: aws.ToTime(provider.LastModifiedDate),
	}
}

func mapResourceServer(resourceServer awsidptypes.ResourceServerType) cognitoservice.ResourceServer {
	scopes := make([]string, 0, len(resourceServer.Scopes))
	for _, scope := range resourceServer.Scopes {
		if name := strings.TrimSpace(aws.ToString(scope.ScopeName)); name != "" {
			scopes = append(scopes, name)
		}
	}
	if len(scopes) == 0 {
		scopes = nil
	}
	return cognitoservice.ResourceServer{
		UserPoolID: aws.ToString(resourceServer.UserPoolId),
		Identifier: aws.ToString(resourceServer.Identifier),
		Name:       aws.ToString(resourceServer.Name),
		Scopes:     scopes,
	}
}

func mapGroup(group awsidptypes.GroupType) cognitoservice.Group {
	return cognitoservice.Group{
		UserPoolID:     aws.ToString(group.UserPoolId),
		Name:           aws.ToString(group.GroupName),
		Description:    aws.ToString(group.Description),
		RoleARN:        aws.ToString(group.RoleArn),
		Precedence:     group.Precedence,
		CreatedAt:      aws.ToTime(group.CreationDate),
		LastModifiedAt: aws.ToTime(group.LastModifiedDate),
	}
}

func mapIdentityPool(
	output *awsidentity.DescribeIdentityPoolOutput,
	boundary awscloud.Boundary,
	roles map[string]string,
) cognitoservice.IdentityPool {
	poolID := aws.ToString(output.IdentityPoolId)
	return cognitoservice.IdentityPool{
		ID:                             poolID,
		ARN:                            identityPoolARN(boundary, poolID),
		Name:                           aws.ToString(output.IdentityPoolName),
		AllowUnauthenticatedIdentities: output.AllowUnauthenticatedIdentities,
		AllowClassicFlow:               aws.ToBool(output.AllowClassicFlow),
		DeveloperProviderName:          aws.ToString(output.DeveloperProviderName),
		UserPoolProviders:              mapUserPoolProviders(output.CognitoIdentityProviders),
		OpenIDConnectProviderARNs:      cloneStrings(output.OpenIdConnectProviderARNs),
		SAMLProviderARNs:               cloneStrings(output.SamlProviderARNs),
		RolesSummary:                   mapStringMap(roles),
	}
}

func mapUserPoolProviders(providers []awsidentitytypes.CognitoIdentityProvider) []cognitoservice.IdentityPoolUserPoolProvider {
	if len(providers) == 0 {
		return nil
	}
	output := make([]cognitoservice.IdentityPoolUserPoolProvider, 0, len(providers))
	for _, provider := range providers {
		output = append(output, cognitoservice.IdentityPoolUserPoolProvider{
			ProviderName: aws.ToString(provider.ProviderName),
			ClientID:     aws.ToString(provider.ClientId),
		})
	}
	return output
}

// identityPoolARN synthesizes the identity pool ARN. The cognito-identity APIs
// return only the bare pool ID, so the adapter builds the ARN from the claim
// boundary so reducers have a stable, partition-shaped identity.
func identityPoolARN(boundary awscloud.Boundary, poolID string) string {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return ""
	}
	return fmt.Sprintf(
		"arn:%s:cognito-identity:%s:%s:identitypool/%s",
		awscloud.PartitionForBoundary(boundary),
		strings.TrimSpace(boundary.Region),
		strings.TrimSpace(boundary.AccountID),
		poolID,
	)
}

func mapOAuthFlows(flows []awsidptypes.OAuthFlowType) []string {
	if len(flows) == 0 {
		return nil
	}
	output := make([]string, 0, len(flows))
	for _, flow := range flows {
		output = append(output, string(flow))
	}
	return output
}

func mapExplicitAuthFlows(flows []awsidptypes.ExplicitAuthFlowsType) []string {
	if len(flows) == 0 {
		return nil
	}
	output := make([]string, 0, len(flows))
	for _, flow := range flows {
		output = append(output, string(flow))
	}
	return output
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
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}
