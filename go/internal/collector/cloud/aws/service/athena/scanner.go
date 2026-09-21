// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package athena

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Athena metadata facts for one claimed account and region.
// It never starts, stops, or mutates Athena queries, never reads query result
// rows, query history strings, or named-query / prepared-statement SQL bodies,
// and never persists query result location object contents.
type Scanner struct {
	Client Client
}

// Scan observes Athena workgroups, data catalogs, prepared-statement names, and
// named-query identities through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("athena scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceAthena:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceAthena
	default:
		return nil, fmt.Errorf("athena scanner received service_kind %q", boundary.ServiceKind)
	}

	workGroups, err := s.Client.ListWorkGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Athena workgroups: %w", err)
	}
	var envelopes []facts.Envelope
	workGroupNames := make([]string, 0, len(workGroups))
	for _, workGroup := range workGroups {
		name := strings.TrimSpace(workGroup.Name)
		if name == "" {
			continue
		}
		workGroupNames = append(workGroupNames, name)
		envelope, err := aws.NewResourceEnvelope(workGroupObservation(boundary, workGroup))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		for _, relationship := range []*aws.RelationshipObservation{
			workGroupResultBucketRelationship(boundary, workGroup),
			workGroupKMSRelationship(boundary, workGroup),
		} {
			if relationship == nil {
				continue
			}
			relationshipEnvelope, err := aws.NewRelationshipEnvelope(*relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationshipEnvelope)
		}
	}

	catalogs, err := s.Client.ListDataCatalogs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Athena data catalogs: %w", err)
	}
	for _, catalog := range catalogs {
		if strings.TrimSpace(catalog.Name) == "" {
			continue
		}
		envelope, err := aws.NewResourceEnvelope(dataCatalogObservation(boundary, catalog))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	statements, err := s.Client.ListPreparedStatements(ctx, workGroupNames)
	if err != nil {
		return nil, fmt.Errorf("list Athena prepared statements: %w", err)
	}
	for _, statement := range statements {
		envelope, err := aws.NewResourceEnvelope(preparedStatementObservation(boundary, statement))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		if relationship := preparedStatementWorkGroupRelationship(boundary, statement); relationship != nil {
			relationshipEnvelope, err := aws.NewRelationshipEnvelope(*relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationshipEnvelope)
		}
	}

	queries, err := s.Client.ListNamedQueries(ctx, workGroupNames)
	if err != nil {
		return nil, fmt.Errorf("list Athena named queries: %w", err)
	}
	for _, query := range queries {
		envelope, err := aws.NewResourceEnvelope(namedQueryObservation(boundary, query))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		if relationship := namedQueryWorkGroupRelationship(boundary, query); relationship != nil {
			relationshipEnvelope, err := aws.NewRelationshipEnvelope(*relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationshipEnvelope)
		}
	}

	return envelopes, nil
}

func workGroupObservation(boundary aws.Boundary, workGroup WorkGroup) aws.ResourceObservation {
	name := strings.TrimSpace(workGroup.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: aws.ResourceTypeAthenaWorkGroup,
		Name:         name,
		State:        strings.TrimSpace(workGroup.State),
		Tags:         cloneStringMap(workGroup.Tags),
		Attributes: map[string]any{
			"description":                        strings.TrimSpace(workGroup.Description),
			"creation_time":                      timeOrNil(workGroup.CreationTime),
			"output_location":                    strings.TrimSpace(workGroup.OutputLocation),
			"encryption_option":                  strings.TrimSpace(workGroup.EncryptionOption),
			"kms_key":                            strings.TrimSpace(workGroup.KMSKey),
			"enforce_workgroup_configuration":    workGroup.EnforceWorkGroupConfiguration,
			"publish_cloudwatch_metrics_enabled": workGroup.PublishCloudWatchMetricsEnabled,
			"requester_pays_enabled":             workGroup.RequesterPaysEnabled,
			"engine_version":                     strings.TrimSpace(workGroup.EngineVersion),
			"effective_engine_version":           strings.TrimSpace(workGroup.EffectiveEngineVersion),
			"bytes_scanned_cutoff_per_query":     workGroup.BytesScannedCutoffPerQuery,
			"expected_bucket_owner":              strings.TrimSpace(workGroup.ExpectedBucketOwner),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func dataCatalogObservation(boundary aws.Boundary, catalog DataCatalog) aws.ResourceObservation {
	name := strings.TrimSpace(catalog.Name)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: aws.ResourceTypeAthenaDataCatalog,
		Name:         name,
		Tags:         cloneStringMap(catalog.Tags),
		Attributes: map[string]any{
			"catalog_type": strings.TrimSpace(catalog.Type),
			"description":  strings.TrimSpace(catalog.Description),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func preparedStatementObservation(boundary aws.Boundary, statement PreparedStatement) aws.ResourceObservation {
	resourceID := preparedStatementResourceID(statement)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAthenaPreparedStatement,
		Name:         strings.TrimSpace(statement.StatementName),
		Attributes: map[string]any{
			"statement_name":     strings.TrimSpace(statement.StatementName),
			"workgroup_name":     strings.TrimSpace(statement.WorkGroupName),
			"last_modified_time": timeOrNil(statement.LastModifiedTime),
		},
		CorrelationAnchors: []string{resourceID, strings.TrimSpace(statement.StatementName)},
		SourceRecordID:     resourceID,
	}
}

func namedQueryObservation(boundary aws.Boundary, query NamedQuery) aws.ResourceObservation {
	resourceID := namedQueryResourceID(query)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeAthenaNamedQuery,
		Name:         strings.TrimSpace(query.Name),
		Attributes: map[string]any{
			"named_query_id": strings.TrimSpace(query.NamedQueryID),
			"query_name":     strings.TrimSpace(query.Name),
			"description":    strings.TrimSpace(query.Description),
			"database":       strings.TrimSpace(query.Database),
			"workgroup_name": strings.TrimSpace(query.WorkGroupName),
		},
		CorrelationAnchors: []string{resourceID, strings.TrimSpace(query.Name)},
		SourceRecordID:     resourceID,
	}
}
