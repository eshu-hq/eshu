// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lakeformation

import (
	"context"
	"time"
)

// Client lists metadata-only AWS Lake Formation observations for one claimed
// account and region. It exposes only the three read APIs the scanner contract
// allows (GetDataLakeSettings, ListResources, ListPermissions) and never grants,
// revokes, registers, or deregisters anything.
type Client interface {
	// GetDataLakeSettings reads the data-lake administrator and read-only
	// administrator principal identifiers for the boundary. Permission policy
	// bodies stay outside the contract.
	GetDataLakeSettings(ctx context.Context) (Settings, error)
	// ListResources reads the registered data-location entries (S3 location ARN,
	// registering role ARN, hybrid-access flag) for the boundary.
	ListResources(ctx context.Context) ([]RegisteredResource, error)
	// ListPermissions reads the principal/resource permission grants for the
	// boundary. Only grant identities, principal identifiers, governed resource
	// references, and bounded privilege enum names are returned; condition
	// expressions and LF-Tag values are dropped by the adapter.
	ListPermissions(ctx context.Context) ([]Permission, error)
}

// Settings is the scanner-owned view of AWS Lake Formation data-lake settings.
// It carries only the administrator principal identifiers; it never carries the
// default-database or default-table permission bodies, LF-Tag values, or
// session-tag credential material.
type Settings struct {
	// Admins is the list of data-lake administrator principal identifiers (IAM
	// ARNs as AWS reports them). Identifiers only; no policy body.
	Admins []string
	// ReadOnlyAdmins is the list of read-only data-lake administrator principal
	// identifiers. Identifiers only; no policy body.
	ReadOnlyAdmins []string
}

// RegisteredResource is the scanner-owned view of one AWS Lake Formation
// registered data location (a `ListResources` entry). Federation context and
// LF-Tag values stay outside the contract.
type RegisteredResource struct {
	// ResourceARN is the registered data-location ARN AWS reports (an S3 location
	// ARN such as `arn:<partition>:s3:::bucket/prefix`). It is the resource_id.
	ResourceARN string
	// RoleARN is the IAM role that registered the location.
	RoleARN string
	// HybridAccessEnabled reports whether both Lake Formation and S3 bucket
	// policies can manage data access for the location.
	HybridAccessEnabled bool
	// WithFederation reports whether the registered resource is federated.
	WithFederation bool
	// WithPrivilegedAccess reports whether the registering principal holds all
	// supported operations on the location.
	WithPrivilegedAccess bool
	// VerificationStatus is the registered-role access verification status AWS
	// reports (VERIFIED / NOT_VERIFIED / VERIFICATION_FAILED).
	VerificationStatus string
	// ExpectedResourceOwnerAccount is the account that owns the Glue tables
	// associated with the location, as AWS reports it.
	ExpectedResourceOwnerAccount string
	// LastModified is the location's last-modified timestamp.
	LastModified time.Time
}

// Permission is the scanner-owned view of one AWS Lake Formation
// principal/resource permission grant (a `ListPermissions` entry). It carries
// the grant identity only: the principal identifier, the governed resource
// reference, and the bounded AWS privilege enum names. Condition expressions,
// LF-Tag values, and policy bodies are never carried.
type Permission struct {
	// PrincipalID is the Lake Formation principal identifier the grant targets
	// (an IAM ARN, or a special principal such as IAM_ALLOWED_PRINCIPALS).
	PrincipalID string
	// ResourceKind names which governed-resource shape the grant applies to
	// (database, table, data_location, catalog, lf_tag, ...), so downstream code
	// can route the edge without re-parsing the resource reference.
	ResourceKind string
	// DatabaseName is the governed Glue database name when the grant is on a
	// database or a table.
	DatabaseName string
	// TableName is the governed Glue table name when the grant is on a table.
	// Empty for a table-wildcard (database-wide) grant.
	TableName string
	// TableWildcard reports whether the grant covers every table under the
	// database (a table-wildcard grant) rather than a single named table.
	TableWildcard bool
	// DataLocationARN is the governed data-location ARN when the grant is on a
	// data location.
	DataLocationARN string
	// CatalogID is the governed Data Catalog identifier when AWS reports one.
	CatalogID string
	// Privileges is the bounded set of AWS privilege enum names granted
	// (SELECT, ALTER, DROP, ALL, ...). These are closed-vocabulary grant
	// identities, not free-form policy bodies.
	Privileges []string
	// GrantablePrivileges is the subset of privileges granted with the grant
	// option (the principal may re-grant them).
	GrantablePrivileges []string
	// LastUpdated is the grant's last-updated timestamp.
	LastUpdated time.Time
}
