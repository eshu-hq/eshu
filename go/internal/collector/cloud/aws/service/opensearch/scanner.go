// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearch

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS OpenSearch metadata facts for one claimed account and
// region. It covers OpenSearch Service provisioned domains, custom packages,
// and OpenSearch Serverless collections, security configurations, and managed
// VPC endpoints. It never calls the OpenSearch HTTP API (_search, _index,
// _doc, _bulk, and similar), never calls a domain or collection mutation API,
// and never persists master user passwords, domain endpoint contents, or
// serverless saved-object bodies.
type Scanner struct {
	Client Client
}

// Scan observes OpenSearch resources through the configured client and emits
// resource and relationship facts. Packages are joined to their associated
// domains, and serverless collections are joined to managed VPC endpoints in
// the same scan window.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("opensearch scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceOpenSearch:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceOpenSearch
	default:
		return nil, fmt.Errorf("opensearch scanner received service_kind %q", boundary.ServiceKind)
	}

	domains, err := s.Client.ListDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("list OpenSearch domains: %w", err)
	}
	packages, err := s.Client.ListPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("list OpenSearch packages: %w", err)
	}
	collections, err := s.Client.ListCollections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list OpenSearch Serverless collections: %w", err)
	}
	securityConfigs, err := s.Client.ListSecurityConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list OpenSearch Serverless security configs: %w", err)
	}
	vpcEndpoints, err := s.Client.ListVPCEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("list OpenSearch Serverless VPC endpoints: %w", err)
	}

	domainARNs := domainARNIndex(domains)

	var envelopes []facts.Envelope
	for _, domain := range domains {
		resource, err := aws.NewResourceEnvelope(domainObservation(boundary, domain))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range domainRelationships(boundary, domain) {
			envelope, err := aws.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}

	for _, pkg := range packages {
		resource, err := aws.NewResourceEnvelope(packageObservation(boundary, pkg))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		associations, err := s.Client.ListPackageAssociations(ctx, pkg.ID)
		if err != nil {
			return nil, fmt.Errorf("list OpenSearch domains for package %q: %w", pkg.ID, err)
		}
		for _, association := range associations {
			relationship, ok := packageDomainRelationship(boundary, pkg, association, domainARNs)
			if !ok {
				continue
			}
			envelope, err := aws.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}

	for _, collection := range collections {
		resource, err := aws.NewResourceEnvelope(collectionObservation(boundary, collection))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range collectionRelationships(boundary, collection) {
			envelope, err := aws.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}

	for _, config := range securityConfigs {
		resource, err := aws.NewResourceEnvelope(securityConfigObservation(boundary, config))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	for _, endpoint := range vpcEndpoints {
		resource, err := aws.NewResourceEnvelope(vpcEndpointObservation(boundary, endpoint))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	return envelopes, nil
}

func domainObservation(boundary aws.Boundary, domain Domain) aws.ResourceObservation {
	domainARN := strings.TrimSpace(domain.ARN)
	name := strings.TrimSpace(domain.Name)
	resourceID := firstNonEmpty(domainARN, name, strings.TrimSpace(domain.ID))
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          domainARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeOpenSearchDomain,
		Name:         name,
		State:        strings.TrimSpace(domain.State),
		Tags:         cloneStringMap(domain.Tags),
		Attributes: map[string]any{
			"domain_id":                      strings.TrimSpace(domain.ID),
			"engine_version":                 strings.TrimSpace(domain.EngineVersion),
			"instance_type":                  strings.TrimSpace(domain.InstanceType),
			"instance_count":                 domain.InstanceCount,
			"dedicated_master_enabled":       domain.DedicatedMasterEnabled,
			"dedicated_master_type":          strings.TrimSpace(domain.DedicatedMasterType),
			"dedicated_master_count":         domain.DedicatedMasterCount,
			"zone_awareness_enabled":         domain.ZoneAwarenessEnabled,
			"encryption_at_rest_enabled":     domain.EncryptionAtRestEnabled,
			"node_to_node_encryption_on":     domain.NodeToNodeEncryptionOn,
			"kms_key_id":                     strings.TrimSpace(domain.KMSKeyID),
			"vpc_id":                         strings.TrimSpace(domain.VPCID),
			"subnet_ids":                     cloneStrings(domain.SubnetIDs),
			"security_group_ids":             cloneStrings(domain.SecurityGroupIDs),
			"availability_zones":             cloneStrings(domain.AvailabilityZones),
			"advanced_security_enabled":      domain.AdvancedSecurityEnabled,
			"internal_user_database_enabled": domain.InternalUserDBEnabled,
			"saml_enabled":                   domain.SAMLEnabled,
			"iam_federation_enabled":         domain.IAMFederationEnabled,
		},
		CorrelationAnchors: []string{domainARN, name, strings.TrimSpace(domain.ID)},
		SourceRecordID:     resourceID,
	}
}

func packageObservation(boundary aws.Boundary, pkg Package) aws.ResourceObservation {
	id := strings.TrimSpace(pkg.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: aws.ResourceTypeOpenSearchPackage,
		Name:         strings.TrimSpace(pkg.Name),
		State:        strings.TrimSpace(pkg.Status),
		Attributes: map[string]any{
			"package_type":   strings.TrimSpace(pkg.Type),
			"description":    strings.TrimSpace(pkg.Description),
			"engine_version": strings.TrimSpace(pkg.EngineVersion),
			"owner":          strings.TrimSpace(pkg.Owner),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(pkg.Name)},
		SourceRecordID:     id,
	}
}

func collectionObservation(boundary aws.Boundary, collection Collection) aws.ResourceObservation {
	collectionARN := strings.TrimSpace(collection.ARN)
	id := strings.TrimSpace(collection.ID)
	resourceID := firstNonEmpty(collectionARN, id, strings.TrimSpace(collection.Name))
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          collectionARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeOpenSearchServerlessCollection,
		Name:         strings.TrimSpace(collection.Name),
		State:        strings.TrimSpace(collection.Status),
		Attributes: map[string]any{
			"collection_id":       id,
			"collection_type":     strings.TrimSpace(collection.Type),
			"description":         strings.TrimSpace(collection.Description),
			"kms_key_arn":         strings.TrimSpace(collection.KMSKeyARN),
			"standby_replicas":    strings.TrimSpace(collection.StandbyReplicas),
			"deletion_protection": strings.TrimSpace(collection.DeletionProtection),
		},
		CorrelationAnchors: []string{collectionARN, id, strings.TrimSpace(collection.Name)},
		SourceRecordID:     resourceID,
	}
}

func securityConfigObservation(boundary aws.Boundary, config SecurityConfig) aws.ResourceObservation {
	id := strings.TrimSpace(config.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: aws.ResourceTypeOpenSearchServerlessSecurityConfig,
		Name:         id,
		Attributes: map[string]any{
			"security_config_type": strings.TrimSpace(config.Type),
			"description":          strings.TrimSpace(config.Description),
			"config_version":       strings.TrimSpace(config.Version),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func vpcEndpointObservation(boundary aws.Boundary, endpoint VPCEndpoint) aws.ResourceObservation {
	id := strings.TrimSpace(endpoint.ID)
	resourceID := firstNonEmpty(id, strings.TrimSpace(endpoint.Name))
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeOpenSearchServerlessVPCEndpoint,
		Name:         strings.TrimSpace(endpoint.Name),
		State:        strings.TrimSpace(endpoint.Status),
		Attributes: map[string]any{
			"vpc_id":             strings.TrimSpace(endpoint.VPCID),
			"subnet_ids":         cloneStrings(endpoint.SubnetIDs),
			"security_group_ids": cloneStrings(endpoint.SecurityGroupIDs),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(endpoint.Name)},
		SourceRecordID:     resourceID,
	}
}

// domainARNIndex maps a domain name to its ARN so package associations can
// resolve to the domain's canonical ARN identity when AWS reports it.
func domainARNIndex(domains []Domain) map[string]string {
	index := make(map[string]string, len(domains))
	for _, domain := range domains {
		name := strings.TrimSpace(domain.Name)
		if name == "" {
			continue
		}
		index[name] = strings.TrimSpace(domain.ARN)
	}
	return index
}
