// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cognito

import (
	"context"
	"time"
)

// Client is the Cognito read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned types.
//
// The interface deliberately exposes no user-record reads (ListUsers,
// AdminGetUser, AdminListGroupsForUser, ListUsersInGroup) and no mutation
// operations. Cognito user records are PII and are never observed by this
// collector. A reflection test in the package and in the SDK adapter enforces
// that these forbidden methods stay absent.
type Client interface {
	ListUserPools(context.Context) ([]UserPool, error)
	ListUserPoolClients(context.Context, string) ([]UserPoolClient, error)
	ListIdentityProviders(context.Context, string) ([]IdentityProvider, error)
	ListResourceServers(context.Context, string) ([]ResourceServer, error)
	ListGroups(context.Context, string) ([]Group, error)
	ListIdentityPools(context.Context) ([]IdentityPool, error)
}

// UserPool is the scanner-owned representation of a Cognito user pool. It holds
// control-plane metadata only and never carries user records.
type UserPool struct {
	ID                 string
	ARN                string
	Name               string
	MFAConfiguration   string
	DeletionProtection string
	Domain             string
	CustomDomain       string
	EstimatedNumUsers  int32
	CreatedAt          time.Time
	LastModifiedAt     time.Time
	PasswordPolicy     *PasswordPolicy
	LambdaTriggers     []LambdaTrigger
	Tags               map[string]string
}

// PasswordPolicy summarizes a user pool password-complexity policy. It carries
// no secret material, only the bounded complexity knobs.
type PasswordPolicy struct {
	MinimumLength                 int32
	RequireUppercase              bool
	RequireLowercase              bool
	RequireNumbers                bool
	RequireSymbols                bool
	TemporaryPasswordValidityDays int32
	PasswordHistorySize           int32
}

// LambdaTrigger names one Cognito user pool Lambda trigger. Trigger is the
// trigger slot (for example "PreSignUp"); ARN is the Lambda function ARN.
type LambdaTrigger struct {
	Trigger string
	ARN     string
}

// UserPoolClient is the scanner-owned representation of a user pool app client.
//
// It never carries ClientSecret. The SDK adapter does not request the secret
// and the scanner has no field to persist it.
type UserPoolClient struct {
	ID                              string
	Name                            string
	UserPoolID                      string
	AllowedOAuthFlows               []string
	AllowedOAuthScopes              []string
	AllowedOAuthFlowsUserPoolClient bool
	CallbackURLs                    []string
	LogoutURLs                      []string
	SupportedIdentityProviders      []string
	ExplicitAuthFlows               []string
	CreatedAt                       time.Time
	LastModifiedAt                  time.Time
}

// IdentityProvider is the scanner-owned representation of a user pool identity
// provider.
//
// It never carries ProviderDetails. Those values include client_secret,
// google_client_secret, and other federation secrets, so the SDK adapter drops
// the map entirely and the scanner has no field to persist it.
type IdentityProvider struct {
	UserPoolID     string
	ProviderName   string
	ProviderType   string
	CreatedAt      time.Time
	LastModifiedAt time.Time
}

// ResourceServer is the scanner-owned representation of a user pool resource
// server (custom OAuth scope namespace).
type ResourceServer struct {
	UserPoolID string
	Identifier string
	Name       string
	Scopes     []string
}

// Group is the scanner-owned representation of a user pool group. Groups are
// authorization metadata, not user records; no user membership is read.
type Group struct {
	UserPoolID     string
	Name           string
	Description    string
	RoleARN        string
	Precedence     *int32
	CreatedAt      time.Time
	LastModifiedAt time.Time
}

// IdentityPool is the scanner-owned representation of a Cognito identity pool.
//
// The identity-pool APIs return no ARN, so the adapter synthesizes one from the
// account, region, and identity pool ID. RolesSummary carries the bounded set
// of role keys (for example "authenticated") and their role ARNs.
type IdentityPool struct {
	ID                             string
	ARN                            string
	Name                           string
	AllowUnauthenticatedIdentities bool
	AllowClassicFlow               bool
	DeveloperProviderName          string
	UserPoolProviders              []IdentityPoolUserPoolProvider
	OpenIDConnectProviderARNs      []string
	SAMLProviderARNs               []string
	RolesSummary                   map[string]string
	Tags                           map[string]string
}

// IdentityPoolUserPoolProvider links an identity pool to a user pool through
// the user pool app client it trusts. ClientID is an app client identifier, not
// a secret.
type IdentityPoolUserPoolProvider struct {
	ProviderName string
	ClientID     string
}
