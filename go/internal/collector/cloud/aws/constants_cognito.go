// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCognito identifies the regional Amazon Cognito service scan slice.
	// It covers both Cognito User Pools (cognito-idp) and Cognito Identity
	// Pools (cognito-identity).
	ServiceCognito = "cognito"
)

const (
	// ResourceTypeCognitoUserPool identifies a Cognito user pool.
	ResourceTypeCognitoUserPool = "aws_cognito_user_pool"
	// ResourceTypeCognitoUserPoolClient identifies a Cognito user pool app
	// client.
	ResourceTypeCognitoUserPoolClient = "aws_cognito_user_pool_client"
	// ResourceTypeCognitoIdentityProvider identifies a Cognito user pool
	// identity provider.
	ResourceTypeCognitoIdentityProvider = "aws_cognito_identity_provider"
	// ResourceTypeCognitoResourceServer identifies a Cognito user pool resource
	// server.
	ResourceTypeCognitoResourceServer = "aws_cognito_resource_server"
	// ResourceTypeCognitoUserPoolGroup identifies a Cognito user pool group.
	ResourceTypeCognitoUserPoolGroup = "aws_cognito_user_pool_group"
	// ResourceTypeCognitoIdentityPool identifies a Cognito identity pool.
	ResourceTypeCognitoIdentityPool = "aws_cognito_identity_pool"
)

const (
	// RelationshipCognitoUserPoolClientUsesUserPool records the user pool an app
	// client belongs to.
	RelationshipCognitoUserPoolClientUsesUserPool = "cognito_user_pool_client_uses_user_pool"
	// RelationshipCognitoUserPoolUsesLambdaTrigger records a Lambda trigger ARN
	// configured on a user pool.
	RelationshipCognitoUserPoolUsesLambdaTrigger = "cognito_user_pool_uses_lambda_trigger"
	// RelationshipCognitoIdentityPoolUsesUserPool records a user pool wired into
	// an identity pool as a Cognito login provider.
	RelationshipCognitoIdentityPoolUsesUserPool = "cognito_identity_pool_uses_user_pool"
	// RelationshipCognitoIdentityPoolUsesIdentityProvider records an external
	// OIDC or SAML provider attached to an identity pool.
	RelationshipCognitoIdentityPoolUsesIdentityProvider = "cognito_identity_pool_uses_identity_provider"
)
