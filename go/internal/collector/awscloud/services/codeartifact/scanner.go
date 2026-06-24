// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeartifact

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS CodeArtifact metadata-only facts for one claimed account
// and region. It observes package-registry domains and repositories and the
// relationships between them. It never reads, downloads, publishes, copies, or
// deletes a package version or package asset; only domain and repository
// metadata plus external-connection and upstream-repository identifiers are
// emitted.
type Scanner struct {
	// Client is the CodeArtifact metadata read surface. The scanner requires a
	// non-nil client.
	Client Client
}

// Scan observes CodeArtifact domains and repositories through the configured
// client and emits aws_resource facts for each domain and repository plus
// aws_relationship facts for repository-to-domain membership, domain-to-KMS-key
// encryption, repository-to-upstream-repository routing, and
// repository-to-external-connection (public registry) links. Package versions
// and assets stay outside the scanner contract.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("codeartifact scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCodeArtifact:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCodeArtifact
	default:
		return nil, fmt.Errorf("codeartifact scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	domains, err := s.Client.ListDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodeArtifact domains: %w", err)
	}
	for _, domain := range domains {
		next, err := domainEnvelopes(boundary, domain)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	repositories, err := s.Client.ListRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CodeArtifact repositories: %w", err)
	}
	for _, repository := range repositories {
		next, err := repositoryEnvelopes(boundary, repository)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	return envelopes, nil
}

// domainEnvelopes builds the resource fact for one domain plus its
// domain-to-KMS-key relationship when AWS reports an encryption key.
func domainEnvelopes(boundary awscloud.Boundary, domain Domain) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(domainObservation(boundary, domain))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := domainKMSKeyRelationship(boundary, domain); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// repositoryEnvelopes builds the resource fact for one repository plus its
// in-domain, upstream-repository, and external-connection relationships.
func repositoryEnvelopes(boundary awscloud.Boundary, repository Repository) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(repositoryObservation(boundary, repository))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	if relationship := repositoryInDomainRelationship(boundary, repository); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, relationship := range upstreamRepositoryRelationships(boundary, repository) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, relationship := range externalConnectionRelationships(boundary, repository) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

// domainObservation maps a domain into the resource observation the scanner
// emits. The domain resource_id is its name, which is the join key repositories
// reference as their owning domain. The encryption-key and S3-bucket ARNs come
// from the API and are recorded as reported metadata; the scanner reads no
// package assets stored in the domain.
func domainObservation(boundary awscloud.Boundary, domain Domain) awscloud.ResourceObservation {
	name := strings.TrimSpace(domain.Name)
	domainARN := strings.TrimSpace(domain.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          domainARN,
		ResourceID:   firstNonEmpty(name, domainARN),
		ResourceType: awscloud.ResourceTypeCodeArtifactDomain,
		Name:         name,
		State:        strings.TrimSpace(domain.Status),
		Attributes: map[string]any{
			"owner":            strings.TrimSpace(domain.Owner),
			"encryption_key":   strings.TrimSpace(domain.EncryptionKey),
			"s3_bucket_arn":    strings.TrimSpace(domain.S3BucketARN),
			"repository_count": domain.RepositoryCount,
			"asset_size_bytes": domain.AssetSizeBytes,
			"created_at":       timeOrNil(domain.CreatedTime),
		},
		CorrelationAnchors: []string{name, domainARN},
		SourceRecordID:     firstNonEmpty(domainARN, name),
	}
}

// repositoryObservation maps a repository into the resource observation the
// scanner emits. The repository resource_id is its ARN when reported, falling
// back to the "<domain>/<name>" identity; both are correlation anchors so
// upstream-repository edges from sibling repositories resolve. External
// connections are summarized by identity, format, and status; upstream
// repositories by name. No package versions or assets are read.
func repositoryObservation(boundary awscloud.Boundary, repository Repository) awscloud.ResourceObservation {
	name := strings.TrimSpace(repository.Name)
	repositoryARN := strings.TrimSpace(repository.ARN)
	domainQualified := repositoryDomainQualifiedName(repository)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          repositoryARN,
		ResourceID:   repositoryResourceID(repository),
		ResourceType: awscloud.ResourceTypeCodeArtifactRepository,
		Name:         name,
		Attributes: map[string]any{
			"domain_name":           strings.TrimSpace(repository.DomainName),
			"domain_owner":          strings.TrimSpace(repository.DomainOwner),
			"administrator_account": strings.TrimSpace(repository.AdministratorAccount),
			"description":           strings.TrimSpace(repository.Description),
			"created_at":            timeOrNil(repository.CreatedTime),
			"external_connections":  externalConnectionNames(repository.ExternalConnections),
			"upstream_repositories": upstreamNames(repository.Upstreams),
		},
		CorrelationAnchors: []string{repositoryARN, domainQualified, name},
		SourceRecordID:     firstNonEmpty(repositoryARN, domainQualified),
	}
}

// externalConnectionNames returns the trimmed, non-empty external-connection
// identifiers for a repository, preserving the reported order.
func externalConnectionNames(connections []ExternalConnection) []string {
	if len(connections) == 0 {
		return nil
	}
	names := make([]string, 0, len(connections))
	for _, connection := range connections {
		if trimmed := strings.TrimSpace(connection.Name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// upstreamNames returns the trimmed, non-empty upstream-repository names,
// preserving the reported priority order.
func upstreamNames(upstreams []string) []string {
	if len(upstreams) == 0 {
		return nil
	}
	names := make([]string, 0, len(upstreams))
	for _, upstream := range upstreams {
		if trimmed := strings.TrimSpace(upstream); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}
