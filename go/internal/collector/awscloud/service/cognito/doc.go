// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cognito maps Amazon Cognito user-pool and identity-pool metadata into
// AWS cloud collector facts.
//
// The package owns scanner-level normalization only. It never calls the AWS SDK
// directly, never reads Cognito user records (ListUsers, AdminGetUser,
// AdminListGroupsForUser, ListUsersInGroup are unreachable through the Client
// interface), never persists app-client ClientSecret values, and never persists
// identity-provider ProviderDetails secrets. SDK adapters provide UserPool,
// UserPoolClient, IdentityProvider, ResourceServer, Group, and IdentityPool
// values; Scanner emits aws_resource facts plus user-pool-client, lambda-trigger,
// and identity-pool relationship evidence.
//
// Scanner requires a non-zero redaction key so operator-supplied free text such
// as identity-pool developer provider names and group descriptions always passes
// through the shared AWS redaction path before persistence.
package cognito
