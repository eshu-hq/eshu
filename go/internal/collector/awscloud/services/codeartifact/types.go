// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeartifact

import (
	"context"
	"time"
)

// Client is the CodeArtifact metadata read surface consumed by Scanner.
// Runtime adapters translate AWS SDK responses into these scanner-owned types.
// The interface exposes only domain and repository metadata reads; it has no
// method that reads, publishes, copies, or deletes a package version or
// package asset, so the scanner cannot reach package contents by construction.
type Client interface {
	// ListDomains returns the CodeArtifact domains visible in the claimed
	// account and region, each enriched with the describe-domain metadata the
	// scanner emits (encryption key, owner, repository count, asset size).
	ListDomains(context.Context) ([]Domain, error)
	// ListRepositories returns the CodeArtifact repositories visible in the
	// claimed account and region, each enriched with the describe-repository
	// metadata the scanner emits (external connections and upstream
	// repositories).
	ListRepositories(context.Context) ([]Repository, error)
}

// Domain is the scanner-owned representation of a CodeArtifact domain. It
// carries identity and metadata only; package assets stored in the domain are
// never read.
type Domain struct {
	// Name is the domain name. It is the join key repositories reference as
	// their owning domain.
	Name string
	// ARN is the domain Amazon Resource Name reported by AWS.
	ARN string
	// Owner is the 12-digit account number that owns the domain.
	Owner string
	// EncryptionKey is the ARN of the KMS key the domain uses to encrypt
	// package assets, as reported by AWS. It is empty when AWS reports no key.
	EncryptionKey string
	// S3BucketARN is the ARN of the S3 bucket that stores the domain's package
	// assets, as reported by AWS. It is recorded as metadata only.
	S3BucketARN string
	// RepositoryCount is the number of repositories in the domain.
	RepositoryCount int32
	// AssetSizeBytes is the total size of all assets in the domain. It is a
	// reported aggregate; no asset content is read.
	AssetSizeBytes int64
	// Status is the current domain status (for example Active).
	Status string
	// CreatedTime is when the domain was created.
	CreatedTime time.Time
}

// Repository is the scanner-owned representation of a CodeArtifact repository.
// It carries identity, owning-domain, external-connection, and upstream
// metadata only; package versions and assets in the repository are never read.
type Repository struct {
	// Name is the repository name, unique within its domain.
	Name string
	// ARN is the repository Amazon Resource Name reported by AWS.
	ARN string
	// DomainName is the name of the domain that contains the repository. It is
	// the join key for the repository-in-domain relationship.
	DomainName string
	// DomainOwner is the 12-digit account number that owns the domain.
	DomainOwner string
	// AdministratorAccount is the account that manages the repository.
	AdministratorAccount string
	// Description is the repository description reported by AWS.
	Description string
	// CreatedTime is when the repository was created.
	CreatedTime time.Time
	// ExternalConnections are the public-registry connections associated with
	// the repository (for example public:npmjs, public:pypi).
	ExternalConnections []ExternalConnection
	// Upstreams are the upstream repositories CodeArtifact consults in priority
	// order when resolving a requested package version.
	Upstreams []string
}

// ExternalConnection is the scanner-owned representation of a CodeArtifact
// repository external connection to a public package registry. It carries the
// connection name, package format, and status only; no package content from
// the external registry is read.
type ExternalConnection struct {
	// Name is the external-connection identifier (for example public:npmjs).
	Name string
	// PackageFormat is the package format associated with the connection (for
	// example npm, pypi, maven, nuget, generic, ruby, swift, cargo).
	PackageFormat string
	// Status is the connection status reported by AWS (for example Available).
	Status string
}
