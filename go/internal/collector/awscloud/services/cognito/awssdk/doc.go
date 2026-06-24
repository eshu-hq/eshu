// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Cognito clients to the Cognito
// scanner contract.
//
// The package owns Cognito user-pool (cognito-idp) and identity-pool
// (cognito-identity) pagination, describe calls, SDK response mapping, AWS API
// telemetry, throttle detection, and pagination spans.
//
// The adapter never calls user-record APIs (ListUsers, AdminGetUser,
// AdminListGroupsForUser, ListUsersInGroup), never calls ListUserPoolClientSecrets,
// never calls identity-record APIs (ListIdentities, GetCredentialsForIdentity),
// and never calls a mutation. It maps DescribeUserPoolClient without ClientSecret
// and identity providers without ProviderDetails. Reflection tests fail the build
// if any forbidden method is added to the adapter's SDK interfaces.
package awssdk
