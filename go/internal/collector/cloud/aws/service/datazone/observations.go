// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datazone

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// domainObservation builds the metadata-only resource observation for one
// DataZone domain. It records identity, status, the KMS key reference, the
// execution/service IAM role references, and lifecycle timestamps; it never
// records glossaries, asset content, or subscription data.
func domainObservation(boundary aws.Boundary, domain Domain) aws.ResourceObservation {
	arn := strings.TrimSpace(domain.ARN)
	id := strings.TrimSpace(domain.ID)
	name := strings.TrimSpace(domain.Name)
	resourceID := domainResourceID(domain)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeDatazoneDomain,
		Name:         name,
		State:        strings.TrimSpace(domain.Status),
		Tags:         cloneStringMap(domain.Tags),
		Attributes: map[string]any{
			"domain_id":             id,
			"kms_key_identifier":    strings.TrimSpace(domain.KMSKeyIdentifier),
			"domain_execution_role": strings.TrimSpace(domain.DomainExecutionRole),
			"service_role":          strings.TrimSpace(domain.ServiceRole),
			"portal_url":            strings.TrimSpace(domain.PortalURL),
			"description":           strings.TrimSpace(domain.Description),
			"created_at":            timeOrNil(domain.CreatedAt),
			"last_updated_at":       timeOrNil(domain.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, id, name},
		SourceRecordID:     resourceID,
	}
}

// projectObservation builds the metadata-only resource observation for one
// DataZone project.
func projectObservation(boundary aws.Boundary, project Project) aws.ResourceObservation {
	id := strings.TrimSpace(project.ID)
	name := strings.TrimSpace(project.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: aws.ResourceTypeDatazoneProject,
		Name:         name,
		State:        strings.TrimSpace(project.Status),
		Attributes: map[string]any{
			"project_id":       id,
			"domain_id":        strings.TrimSpace(project.DomainID),
			"project_category": strings.TrimSpace(project.ProjectCategory),
			"domain_unit_id":   strings.TrimSpace(project.DomainUnitID),
			"description":      strings.TrimSpace(project.Description),
			"created_at":       timeOrNil(project.CreatedAt),
			"updated_at":       timeOrNil(project.UpdatedAt),
		},
		CorrelationAnchors: []string{id, name},
		SourceRecordID:     id,
	}
}

// environmentObservation builds the metadata-only resource observation for one
// DataZone environment.
func environmentObservation(boundary aws.Boundary, environment Environment) aws.ResourceObservation {
	id := strings.TrimSpace(environment.ID)
	name := strings.TrimSpace(environment.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: aws.ResourceTypeDatazoneEnvironment,
		Name:         name,
		State:        strings.TrimSpace(environment.Status),
		Attributes: map[string]any{
			"environment_id":     id,
			"domain_id":          strings.TrimSpace(environment.DomainID),
			"project_id":         strings.TrimSpace(environment.ProjectID),
			"provider":           strings.TrimSpace(environment.Provider),
			"blueprint_id":       strings.TrimSpace(environment.BlueprintID),
			"profile_id":         strings.TrimSpace(environment.ProfileID),
			"aws_account_id":     strings.TrimSpace(environment.AWSAccountID),
			"aws_account_region": strings.TrimSpace(environment.AWSAccountRegion),
			"description":        strings.TrimSpace(environment.Description),
			"created_at":         timeOrNil(environment.CreatedAt),
			"updated_at":         timeOrNil(environment.UpdatedAt),
		},
		CorrelationAnchors: []string{id, name},
		SourceRecordID:     id,
	}
}

// dataSourceObservation builds the metadata-only resource observation for one
// DataZone data source. It records identity, parentage, source type, and
// enablement; it never records ingested asset content, relational filter
// expressions, or access credentials.
func dataSourceObservation(boundary aws.Boundary, dataSource DataSource) aws.ResourceObservation {
	id := strings.TrimSpace(dataSource.ID)
	name := strings.TrimSpace(dataSource.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: aws.ResourceTypeDatazoneDataSource,
		Name:         name,
		State:        strings.TrimSpace(dataSource.Status),
		Attributes: map[string]any{
			"data_source_id":        id,
			"domain_id":             strings.TrimSpace(dataSource.DomainID),
			"project_id":            strings.TrimSpace(dataSource.ProjectID),
			"environment_id":        strings.TrimSpace(dataSource.EnvironmentID),
			"type":                  strings.TrimSpace(dataSource.Type),
			"enabled":               dataSource.Enabled,
			"connection_id":         strings.TrimSpace(dataSource.ConnectionID),
			"glue_database_names":   cloneStrings(dataSource.GlueDatabaseNames),
			"redshift_cluster_name": strings.TrimSpace(dataSource.RedshiftClusterName),
			"description":           strings.TrimSpace(dataSource.Description),
			"created_at":            timeOrNil(dataSource.CreatedAt),
			"updated_at":            timeOrNil(dataSource.UpdatedAt),
		},
		CorrelationAnchors: []string{id, name},
		SourceRecordID:     id,
	}
}
