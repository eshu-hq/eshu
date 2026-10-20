// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssoadmin

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client is the IAM Identity Center read surface consumed by Scanner. Runtime
// adapters translate AWS sso-admin and identitystore responses into these
// scanner-owned metadata records. The surface intentionally exposes no
// mutation and no inline-policy or access-scope body read.
type Client interface {
	Snapshot(context.Context) (Snapshot, error)
}

// Snapshot is the metadata-only scanner view of IAM Identity Center for one
// claimed org-scoped account. It never carries permission set inline policy
// bodies, customer-managed policy bodies, or application access-scope filters.
type Snapshot struct {
	Instances    []Instance
	Applications []Application
	Principals   []Principal
	Warnings     []awscloud.WarningObservation
}

// Instance is the metadata-only scanner view of an IAM Identity Center
// instance plus the permission sets, assignments, and trusted token issuers
// scanned beneath it.
type Instance struct {
	ARN                 string
	IdentityStoreID     string
	Name                string
	OwnerAccountID      string
	Status              string
	CreatedAt           time.Time
	PermissionSets      []PermissionSet
	AccountAssignments  []AccountAssignment
	TrustedTokenIssuers []TrustedTokenIssuer
	Tags                map[string]string
}

// PermissionSet is the metadata-only scanner view of an Identity Center
// permission set. It carries name, description, session duration, relay state,
// and policy references by ARN or name. Inline policy bodies, permissions
// boundary bodies, and customer-managed policy bodies are intentionally outside
// this contract.
type PermissionSet struct {
	ARN                     string
	InstanceARN             string
	Name                    string
	Description             string
	SessionDuration         string
	RelayState              string
	CreatedAt               time.Time
	ManagedPolicies         []ManagedPolicyReference
	CustomerManagedPolicies []CustomerManagedPolicyReference
	Tags                    map[string]string
}

// ManagedPolicyReference is an AWS managed policy attached to a permission set,
// referenced by ARN. The policy document body lives in IAM and is never read
// here.
type ManagedPolicyReference struct {
	ARN  string
	Name string
}

// CustomerManagedPolicyReference is a customer-managed policy attachment on a
// permission set, referenced by name and path only. The IAM policy body is
// never read or persisted by this scanner.
type CustomerManagedPolicyReference struct {
	Name string
	Path string
}

// AccountAssignment is the metadata-only scanner view of one Identity Center
// account assignment: a principal (group or user) granted a permission set in a
// target AWS account.
type AccountAssignment struct {
	InstanceARN      string
	PermissionSetARN string
	AccountID        string
	PrincipalID      string
	PrincipalType    string
}

// TrustedTokenIssuer is the metadata-only scanner view of an Identity Center
// trusted token issuer configuration. It carries the issuer name, ARN, and type
// only; issuer JWKS endpoints and claim mappings are outside this contract.
type TrustedTokenIssuer struct {
	ARN         string
	InstanceARN string
	Name        string
	Type        string
}

// Application is the metadata-only scanner view of an Identity Center
// application instance. Application access-scope attributes that can include
// sensitive group filters are never part of this contract.
type Application struct {
	ARN                    string
	InstanceARN            string
	Name                   string
	Description            string
	ApplicationAccountID   string
	ApplicationProviderARN string
	IdentityStoreARN       string
	Status                 string
	PortalVisibility       string
	CreatedAt              time.Time
}

// Principal is the metadata-only scanner view of an Identity Center principal
// resolved from the connected identity store. The display name is redacted
// before persistence; addresses, emails, phone numbers, and other identity
// store attributes are never read.
type Principal struct {
	ID          string
	Type        string
	DisplayName string
}
