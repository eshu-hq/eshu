// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amplify

import (
	"context"
	"time"
)

// Client is the metadata-only AWS Amplify read surface consumed by Scanner.
// Runtime adapters translate AWS SDK responses into these scanner-owned records.
// The contract lists no create, update, delete, start-job, start-deployment, or
// token-read call, so the scanner cannot mutate Amplify state or reach an app's
// environment variables, build-spec secrets, or repository access tokens.
type Client interface {
	// ListApps returns app metadata for the boundary. Environment variables,
	// build-spec bodies, and basic-auth credentials are dropped by the adapter and
	// never reach these records.
	ListApps(context.Context) ([]App, error)
	// ListBranches returns branch metadata for one app. Branch environment
	// variables, build-spec bodies, and basic-auth credentials are dropped by the
	// adapter and never reach these records.
	ListBranches(context.Context, string) ([]Branch, error)
	// ListDomainAssociations returns custom-domain association metadata for one
	// app, including the subdomain DNS records used to resolve CloudFront and
	// Route 53 targets.
	ListDomainAssociations(context.Context, string) ([]DomainAssociation, error)
}

// App is the scanner-owned representation of one Amplify app. It carries
// identity, the Git repository URL (host and path only, never userinfo or a
// token), the service/compute IAM role ARNs, and the default domain. No field
// can hold an environment-variable value, build-spec body, or basic-auth
// credential.
type App struct {
	// ID is the Amplify app id.
	ID string
	// ARN is the Amplify app ARN as reported by AWS.
	ARN string
	// Name is the Amplify app name.
	Name string
	// Platform is the app platform (WEB, WEB_COMPUTE, WEB_DYNAMIC).
	Platform string
	// RepositoryURL is the Git repository the app deploys from, normalized to
	// host and path only so any embedded userinfo or token is stripped.
	RepositoryURL string
	// RepositoryCloneMethod is the AWS-reported clone method (TOKEN, SIGV4, SSH).
	// It is a method label, never a credential.
	RepositoryCloneMethod string
	// DefaultDomain is the *.amplifyapp.com default domain Amplify serves the app
	// from.
	DefaultDomain string
	// ServiceRoleARN is the IAM service-role ARN for the app, when set.
	ServiceRoleARN string
	// ComputeRoleARN is the IAM compute-role ARN for an SSR app, when set.
	ComputeRoleARN string
	// ProductionBranchName is the app's production branch name, when reported.
	ProductionBranchName string
	// CreateTime is when Amplify created the app.
	CreateTime time.Time
	// UpdateTime is when Amplify last updated the app.
	UpdateTime time.Time
	// Tags carries the app's AWS tags.
	Tags map[string]string
}

// Branch is the scanner-owned representation of one Amplify branch. It carries
// identity, stage, framework, and the owning app id. No field can hold an
// environment-variable value, build-spec body, or basic-auth credential.
type Branch struct {
	// AppID is the id of the app the branch belongs to.
	AppID string
	// Name is the branch name.
	Name string
	// ARN is the branch ARN as reported by AWS.
	ARN string
	// DisplayName is the branch display name (the default subdomain prefix).
	DisplayName string
	// Stage is the branch stage (PRODUCTION, BETA, DEVELOPMENT, EXPERIMENTAL,
	// PULL_REQUEST).
	Stage string
	// Framework is the branch framework label.
	Framework string
	// EnableAutoBuild reports whether auto-build on push is enabled.
	EnableAutoBuild bool
	// ComputeRoleARN is the IAM compute-role ARN for an SSR branch, when set.
	ComputeRoleARN string
	// CustomDomainCount is the number of custom domains attached to the branch.
	CustomDomainCount int
	// CreateTime is when Amplify created the branch.
	CreateTime time.Time
	// UpdateTime is when Amplify last updated the branch.
	UpdateTime time.Time
	// Tags carries the branch's AWS tags.
	Tags map[string]string
}

// DomainAssociation is the scanner-owned representation of one Amplify
// custom-domain association. It carries the domain name, status, and the
// subdomain DNS records used to resolve CloudFront and Route 53 targets. It
// never carries certificate bodies or verification secret material.
type DomainAssociation struct {
	// AppID is the id of the app the domain association belongs to.
	AppID string
	// ARN is the domain-association ARN as reported by AWS.
	ARN string
	// DomainName is the custom domain root (for example example.com).
	DomainName string
	// Status is the AWS-reported domain status.
	Status string
	// SubDomains lists the per-subdomain DNS records for the association.
	SubDomains []SubDomain
}

// SubDomain is the scanner-owned representation of one Amplify subdomain DNS
// record. DNSRecord is the CNAME value Amplify publishes for the subdomain; it
// typically points at a CloudFront distribution domain.
type SubDomain struct {
	// Prefix is the subdomain prefix (for example www).
	Prefix string
	// BranchName is the branch the subdomain serves.
	BranchName string
	// DNSRecord is the DNS record value for the subdomain (a CNAME target).
	DNSRecord string
	// Verified reports whether the subdomain is verified.
	Verified bool
}
