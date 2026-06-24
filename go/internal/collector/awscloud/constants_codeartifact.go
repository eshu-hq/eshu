// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCodeArtifact identifies the regional AWS CodeArtifact
	// metadata-only scan slice covering package-registry domains and
	// repositories. The scanner never reads, downloads, publishes, copies, or
	// deletes package versions or package assets; it observes only domain and
	// repository metadata plus the external-connection and upstream-repository
	// identifiers that AWS reports.
	ServiceCodeArtifact = "codeartifact"
)

const (
	// ResourceTypeCodeArtifactDomain identifies an AWS CodeArtifact domain
	// metadata resource. A domain is the container that owns repositories and
	// the KMS key that encrypts package assets. The scanner emits the domain
	// name, ARN, owner account, encryption-key ARN, repository count, and asset
	// size; it never reads package contents stored under the domain.
	ResourceTypeCodeArtifactDomain = "aws_codeartifact_domain"
	// ResourceTypeCodeArtifactRepository identifies an AWS CodeArtifact
	// repository metadata resource. The scanner emits the repository name, ARN,
	// owning domain, external-connection identifiers, and upstream-repository
	// identifiers; it never reads package versions or assets in the repository.
	ResourceTypeCodeArtifactRepository = "aws_codeartifact_repository"
)

const (
	// RelationshipCodeArtifactRepositoryInDomain records a CodeArtifact
	// repository's membership in the domain that contains it. The target is the
	// domain identified by its name, matching the domain scanner resource_id.
	RelationshipCodeArtifactRepositoryInDomain = "codeartifact_repository_in_domain"
	// RelationshipCodeArtifactDomainUsesKMSKey records the KMS key a
	// CodeArtifact domain uses to encrypt package assets. The target is the KMS
	// key identified by the encryption-key ARN AWS reports for the domain.
	RelationshipCodeArtifactDomainUsesKMSKey = "codeartifact_domain_uses_kms_key"
	// RelationshipCodeArtifactRepositoryUpstreamRepository records a
	// CodeArtifact repository's reported upstream repository. CodeArtifact
	// consults upstream repositories in priority order when resolving a
	// requested package version. The target is the upstream repository
	// identified within the same domain.
	RelationshipCodeArtifactRepositoryUpstreamRepository = "codeartifact_repository_upstream_repository"
	// RelationshipCodeArtifactRepositoryExternalConnection records a
	// CodeArtifact repository's external connection to a public package
	// registry (for example public:npmjs or public:pypi). The target is a
	// labeled non-AWS public-registry identity, not a scanned AWS resource.
	RelationshipCodeArtifactRepositoryExternalConnection = "codeartifact_repository_external_connection"
)
