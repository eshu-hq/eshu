// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeartifact

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// externalConnectionTargetType labels a repository external-connection
// relationship target. A public package registry (npmjs, PyPI, Maven Central,
// NuGet gallery, crates.io, etc.) is an external (non-AWS-resource) endpoint, so
// it carries a stable non-empty target type without claiming a scanned AWS
// resource node, mirroring how the CodeBuild scanner labels git_repository
// targets and the Lambda scanner labels container_image targets. The value is
// documented in relguard.KnownTargetTypeAllowlist.
const externalConnectionTargetType = "public_package_registry"

// repositoryResourceID is the stable identity the repository resource publishes
// as its resource_id. The ARN is preferred so ARN-equality joins resolve; when
// AWS reports no ARN the domain-qualified name is the fallback, which matches
// the upstream-repository join key within the same domain.
func repositoryResourceID(repository Repository) string {
	return firstNonEmpty(strings.TrimSpace(repository.ARN), repositoryDomainQualifiedName(repository))
}

// repositoryDomainQualifiedName is the "<domain>/<repository>" identity used as
// a correlation anchor on the repository resource and as the join key for the
// repository-to-upstream-repository edge. CodeArtifact repository names are
// unique only within a domain, so the domain prefix disambiguates the upstream
// target.
func repositoryDomainQualifiedName(repository Repository) string {
	domainName := strings.TrimSpace(repository.DomainName)
	name := strings.TrimSpace(repository.Name)
	switch {
	case domainName != "" && name != "":
		return domainName + "/" + name
	case name != "":
		return name
	default:
		return ""
	}
}

// repositoryInDomainRelationship records the repository's membership in the
// domain that contains it. The target is the domain keyed by its name, matching
// the domain resource_id the same scanner publishes.
func repositoryInDomainRelationship(boundary awscloud.Boundary, repository Repository) *awscloud.RelationshipObservation {
	repositoryID := repositoryResourceID(repository)
	domainName := strings.TrimSpace(repository.DomainName)
	if repositoryID == "" || domainName == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodeArtifactRepositoryInDomain,
		SourceResourceID: repositoryID,
		SourceARN:        strings.TrimSpace(repository.ARN),
		TargetResourceID: domainName,
		TargetType:       awscloud.ResourceTypeCodeArtifactDomain,
		Attributes: map[string]any{
			"domain_owner": strings.TrimSpace(repository.DomainOwner),
		},
		SourceRecordID: repositoryID + "->" + awscloud.RelationshipCodeArtifactRepositoryInDomain + ":" + domainName,
	}
}

// domainKMSKeyRelationship records the KMS key the domain uses to encrypt
// package assets. CodeArtifact reports the encryption key as a full KMS key ARN,
// and the KMS scanner publishes its key resource_id as firstNonEmpty(keyID,
// keyARN), so a key ARN joins the key node directly. The edge is emitted only
// when AWS reports an ARN-shaped key.
func domainKMSKeyRelationship(boundary awscloud.Boundary, domain Domain) *awscloud.RelationshipObservation {
	keyARN := strings.TrimSpace(domain.EncryptionKey)
	if !strings.HasPrefix(keyARN, "arn:") {
		return nil
	}
	domainID := firstNonEmpty(strings.TrimSpace(domain.Name), strings.TrimSpace(domain.ARN))
	if domainID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodeArtifactDomainUsesKMSKey,
		SourceResourceID: domainID,
		SourceARN:        strings.TrimSpace(domain.ARN),
		TargetResourceID: keyARN,
		TargetARN:        keyARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   domainID + "->" + awscloud.RelationshipCodeArtifactDomainUsesKMSKey + ":" + keyARN,
	}
}

// upstreamRepositoryRelationships record the upstream repositories CodeArtifact
// consults in priority order when resolving a requested package version. Each
// upstream lives in the same domain, so the target is keyed by
// "<domain>/<upstream>", matching the domain-qualified name correlation anchor
// the upstream repository resource publishes. Duplicate upstream names collapse
// to one edge.
func upstreamRepositoryRelationships(boundary awscloud.Boundary, repository Repository) []awscloud.RelationshipObservation {
	repositoryID := repositoryResourceID(repository)
	domainName := strings.TrimSpace(repository.DomainName)
	if repositoryID == "" || domainName == "" || len(repository.Upstreams) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(repository.Upstreams))
	seen := make(map[string]struct{}, len(repository.Upstreams))
	for priority, upstream := range repository.Upstreams {
		upstreamName := strings.TrimSpace(upstream)
		if upstreamName == "" {
			continue
		}
		targetID := domainName + "/" + upstreamName
		if _, ok := seen[targetID]; ok {
			continue
		}
		seen[targetID] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCodeArtifactRepositoryUpstreamRepository,
			SourceResourceID: repositoryID,
			SourceARN:        strings.TrimSpace(repository.ARN),
			TargetResourceID: targetID,
			TargetType:       awscloud.ResourceTypeCodeArtifactRepository,
			Attributes: map[string]any{
				"upstream_repository_name": upstreamName,
				"priority":                 priority,
			},
			SourceRecordID: repositoryID + "->" + awscloud.RelationshipCodeArtifactRepositoryUpstreamRepository + ":" + targetID,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}

// externalConnectionRelationships record the repository's external connections
// to public package registries. The target is a labeled non-AWS public-registry
// identity (for example public:npmjs), not a scanned AWS resource, so the edge
// carries the externalConnectionTargetType label and no target ARN. Duplicate
// connection names collapse to one edge.
func externalConnectionRelationships(boundary awscloud.Boundary, repository Repository) []awscloud.RelationshipObservation {
	repositoryID := repositoryResourceID(repository)
	if repositoryID == "" || len(repository.ExternalConnections) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(repository.ExternalConnections))
	seen := make(map[string]struct{}, len(repository.ExternalConnections))
	for _, connection := range repository.ExternalConnections {
		connectionName := strings.TrimSpace(connection.Name)
		if connectionName == "" {
			continue
		}
		if _, ok := seen[connectionName]; ok {
			continue
		}
		seen[connectionName] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCodeArtifactRepositoryExternalConnection,
			SourceResourceID: repositoryID,
			SourceARN:        strings.TrimSpace(repository.ARN),
			TargetResourceID: connectionName,
			TargetType:       externalConnectionTargetType,
			Attributes: map[string]any{
				"package_format": strings.TrimSpace(connection.PackageFormat),
				"status":         strings.TrimSpace(connection.Status),
			},
			SourceRecordID: repositoryID + "->" + awscloud.RelationshipCodeArtifactRepositoryExternalConnection + ":" + connectionName,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}
