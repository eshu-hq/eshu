// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datazone

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon DataZone metadata-only facts for one claimed account and
// region. It never reads or persists business glossaries, glossary terms,
// catalog asset content, subscription data, or any data-plane payload, and never
// mutates DataZone state. It reports domains, projects, environments, and data
// sources plus the domain-to-KMS-key, domain-to-IAM-role, child-in-domain, and
// data-source-to-backing-store (Glue database / provisioned Redshift cluster)
// relationships.
type Scanner struct {
	// Client is the metadata-only DataZone snapshot source.
	Client Client
}

// Scan observes DataZone domains, their projects, environments, and data
// sources, plus the direct KMS, IAM, and backing-store dependency metadata
// through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("datazone scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDatazone:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceDatazone
	default:
		return nil, fmt.Errorf("datazone scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DataZone domains: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, domain := range snapshot.Domains {
		next, err := domainEnvelopes(boundary, domain)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func domainEnvelopes(boundary awscloud.Boundary, domain Domain) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(domainObservation(boundary, domain))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	domainID := domainResourceID(domain)
	for _, relationship := range domainRelationships(boundary, domain) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	for _, project := range domain.Projects {
		next, err := projectEnvelopes(boundary, domainID, project)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, environment := range domain.Environments {
		next, err := environmentEnvelopes(boundary, domainID, environment)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, dataSource := range domain.DataSources {
		next, err := dataSourceEnvelopes(boundary, domainID, dataSource)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

// domainRelationships builds the KMS-key and IAM-role dependency edges a domain
// reports. Each edge resolves to a scanned target node or is omitted.
func domainRelationships(boundary awscloud.Boundary, domain Domain) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	candidates := []*awscloud.RelationshipObservation{
		domainKMSRelationship(boundary, domain),
		domainIAMRoleRelationship(boundary, domain, domain.DomainExecutionRole, "domain_execution_role"),
		domainIAMRoleRelationship(boundary, domain, domain.ServiceRole, "service_role"),
	}
	for _, candidate := range candidates {
		if candidate != nil {
			observations = append(observations, *candidate)
		}
	}
	return observations
}

func projectEnvelopes(boundary awscloud.Boundary, domainID string, project Project) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(projectObservation(boundary, project))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := childInDomainRelationship(
		boundary,
		awscloud.RelationshipDatazoneProjectInDomain,
		strings.TrimSpace(project.ID),
		"",
		domainID,
	); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func environmentEnvelopes(boundary awscloud.Boundary, domainID string, environment Environment) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(environmentObservation(boundary, environment))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := childInDomainRelationship(
		boundary,
		awscloud.RelationshipDatazoneEnvironmentInDomain,
		strings.TrimSpace(environment.ID),
		"",
		domainID,
	); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func dataSourceEnvelopes(boundary awscloud.Boundary, domainID string, dataSource DataSource) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(dataSourceObservation(boundary, dataSource))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	var relationships []awscloud.RelationshipObservation
	if relationship := childInDomainRelationship(
		boundary,
		awscloud.RelationshipDatazoneDataSourceInDomain,
		strings.TrimSpace(dataSource.ID),
		"",
		domainID,
	); relationship != nil {
		relationships = append(relationships, *relationship)
	}
	relationships = append(relationships, dataSourceGlueRelationships(boundary, dataSource)...)
	if relationship := dataSourceRedshiftRelationship(boundary, dataSource); relationship != nil {
		relationships = append(relationships, *relationship)
	}
	for _, relationship := range relationships {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}
