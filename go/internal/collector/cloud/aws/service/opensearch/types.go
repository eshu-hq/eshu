// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearch

import "context"

// Client lists OpenSearch metadata for one claimed account and region. It is
// the scanner-facing surface that adapter packages implement; the contract is
// intentionally narrow so the scanner cannot reach the OpenSearch HTTP API
// (_search, _index, _doc, _bulk, and similar), domain endpoint contents,
// serverless saved-object bodies, or master user passwords.
type Client interface {
	// ListDomains returns OpenSearch Service provisioned domain metadata.
	ListDomains(ctx context.Context) ([]Domain, error)
	// ListPackages returns OpenSearch custom package metadata (name, type,
	// status); the package body is never read.
	ListPackages(ctx context.Context) ([]Package, error)
	// ListPackageAssociations returns the domain associations reported for one
	// package by ListDomainsForPackage.
	ListPackageAssociations(ctx context.Context, packageID string) ([]PackageAssociation, error)
	// ListCollections returns OpenSearch Serverless collection metadata.
	ListCollections(ctx context.Context) ([]Collection, error)
	// ListSecurityConfigs returns OpenSearch Serverless security configuration
	// summaries (id, type, version) across every security config type.
	ListSecurityConfigs(ctx context.Context) ([]SecurityConfig, error)
	// ListVPCEndpoints returns OpenSearch Serverless managed VPC endpoint
	// metadata.
	ListVPCEndpoints(ctx context.Context) ([]VPCEndpoint, error)
}

// Domain is the scanner-owned OpenSearch Service provisioned domain model. It
// carries control-plane metadata only. Domain endpoint contents, access policy
// bodies, and master user passwords are intentionally excluded; the AWS
// DescribeDomains response does not return the master user password, and the
// adapter never persists the raw endpoint or access policy body.
type Domain struct {
	ARN                     string
	ID                      string
	Name                    string
	EngineVersion           string
	State                   string
	InstanceType            string
	InstanceCount           int32
	DedicatedMasterEnabled  bool
	DedicatedMasterType     string
	DedicatedMasterCount    int32
	ZoneAwarenessEnabled    bool
	EncryptionAtRestEnabled bool
	NodeToNodeEncryptionOn  bool
	KMSKeyID                string
	VPCID                   string
	SubnetIDs               []string
	SecurityGroupIDs        []string
	AvailabilityZones       []string
	AdvancedSecurityEnabled bool
	InternalUserDBEnabled   bool
	SAMLEnabled             bool
	IAMFederationEnabled    bool
	// MasterUserRoleARNs are IAM role ARNs referenced by the domain's
	// master-user mapping or resource access policy. The adapter resolves these
	// from the access policy principals and never persists the raw policy body.
	MasterUserRoleARNs []string
	Tags               map[string]string
}

// Package is the scanner-owned OpenSearch custom package model. Only the
// package name, type, status, and owning identity are persisted; the package
// body (the analyzer, dictionary, or plugin file) is never read.
type Package struct {
	ID            string
	Name          string
	Type          string
	Status        string
	Description   string
	EngineVersion string
	Owner         string
}

// PackageAssociation is the scanner-owned association between a custom package
// and an OpenSearch Service domain reported by ListDomainsForPackage.
type PackageAssociation struct {
	PackageID         string
	DomainName        string
	DomainPackageStat string
	ReferencePath     string
}

// Collection is the scanner-owned OpenSearch Serverless collection model.
// Collection endpoints, dashboard endpoints, and indexed data are never read or
// persisted; only control-plane identity and configuration metadata is kept.
type Collection struct {
	ARN                string
	ID                 string
	Name               string
	Type               string
	Status             string
	Description        string
	KMSKeyARN          string
	StandbyReplicas    string
	DeletionProtection string
}

// SecurityConfig is the scanner-owned OpenSearch Serverless security
// configuration summary. SAML metadata XML, IAM Identity Center secrets, and
// policy bodies stay outside this type; only the id, type, and version summary
// is persisted.
type SecurityConfig struct {
	ID          string
	Type        string
	Description string
	Version     string
}

// VPCEndpoint is the scanner-owned OpenSearch Serverless managed VPC endpoint
// model. It carries the endpoint id, name, status, VPC, subnet, and security
// group placement reported by the control plane.
type VPCEndpoint struct {
	ID               string
	Name             string
	Status           string
	VPCID            string
	SubnetIDs        []string
	SecurityGroupIDs []string
}
