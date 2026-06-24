// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quicksight

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon QuickSight metadata-only facts for one claimed account
// and region. It never reads data-source credentials, connection passwords,
// secret connection parameters, SQL query bodies, or visual definitions, and
// never mutates QuickSight state. It reports data sources, datasets, dashboards,
// and analyses plus the data-source-to-backing-store (Redshift cluster, RDS
// instance, Athena workgroup, S3 bucket), data-source-to-VPC-connection
// (security group, subnet), dataset-to-data-source, and dashboard/analysis-to-
// dataset relationships.
type Scanner struct {
	// Client is the metadata-only QuickSight snapshot source.
	Client Client
}

// Scan observes QuickSight data sources, datasets, dashboards, and analyses plus
// their backing-store, VPC-connection, and internal read relationships through
// the configured client. A not-subscribed account yields an empty result, not a
// failed scan.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("quicksight scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceQuickSight:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceQuickSight
	default:
		return nil, fmt.Errorf("quicksight scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot QuickSight metadata: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}

	for _, dataSource := range snapshot.DataSources {
		next, err := dataSourceEnvelopes(boundary, dataSource, snapshot.VPCConnections)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, dataSet := range snapshot.DataSets {
		next, err := dataSetEnvelopes(boundary, dataSet)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, dashboard := range snapshot.Dashboards {
		next, err := dashboardEnvelopes(boundary, dashboard)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, analysis := range snapshot.Analyses {
		next, err := analysisEnvelopes(boundary, analysis)
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

func appendRelationships(
	envelopes *[]facts.Envelope,
	relationships []awscloud.RelationshipObservation,
) error {
	for i := range relationships {
		envelope, err := awscloud.NewRelationshipEnvelope(relationships[i])
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func dataSourceEnvelopes(
	boundary awscloud.Boundary,
	dataSource DataSource,
	connections map[string]VPCConnection,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(dataSourceObservation(boundary, dataSource))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := dataSourceBackingRelationship(boundary, dataSource); relationship != nil {
		if err := appendRelationships(&envelopes, []awscloud.RelationshipObservation{*relationship}); err != nil {
			return nil, err
		}
	}
	if err := appendRelationships(&envelopes, dataSourceVPCRelationships(boundary, dataSource, connections)); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func dataSetEnvelopes(boundary awscloud.Boundary, dataSet DataSet) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(dataSetObservation(boundary, dataSet))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if err := appendRelationships(&envelopes, dataSetDataSourceRelationships(boundary, dataSet)); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func dashboardEnvelopes(boundary awscloud.Boundary, dashboard Dashboard) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(dashboardObservation(boundary, dashboard))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if err := appendRelationships(&envelopes, dashboardDataSetRelationships(boundary, dashboard)); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func analysisEnvelopes(boundary awscloud.Boundary, analysis Analysis) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(analysisObservation(boundary, analysis))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if err := appendRelationships(&envelopes, analysisDataSetRelationships(boundary, analysis)); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func dataSourceObservation(boundary awscloud.Boundary, dataSource DataSource) awscloud.ResourceObservation {
	arn := strings.TrimSpace(dataSource.ARN)
	name := strings.TrimSpace(dataSource.Name)
	resourceID := dataSourceResourceID(dataSource)
	attributes := map[string]any{
		"data_source_id":    strings.TrimSpace(dataSource.ID),
		"connector_type":    strings.TrimSpace(dataSource.Type),
		"secret_configured": dataSource.SecretConfigured,
		"created_time":      timeOrNil(dataSource.CreatedTime),
		"last_updated_time": timeOrNil(dataSource.LastUpdatedTime),
	}
	if vpcARN := strings.TrimSpace(dataSource.VPCConnectionARN); vpcARN != "" {
		attributes["vpc_connection_arn"] = vpcARN
	}
	if kind := dataSource.Backing.Kind; kind != BackingStoreNone {
		attributes["backing_store_kind"] = string(kind)
		attributes["backing_store_identifier"] = strings.TrimSpace(dataSource.Backing.Identifier)
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeQuickSightDataSource,
		Name:               name,
		State:              strings.TrimSpace(dataSource.Status),
		Tags:               cloneStringMap(dataSource.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func dataSetObservation(boundary awscloud.Boundary, dataSet DataSet) awscloud.ResourceObservation {
	arn := strings.TrimSpace(dataSet.ARN)
	name := strings.TrimSpace(dataSet.Name)
	resourceID := dataSetResourceID(dataSet)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeQuickSightDataSet,
		Name:         name,
		Tags:         cloneStringMap(dataSet.Tags),
		Attributes: map[string]any{
			"data_set_id":       strings.TrimSpace(dataSet.ID),
			"import_mode":       strings.TrimSpace(dataSet.ImportMode),
			"data_source_arns":  dedupeStrings(dataSet.DataSourceARNs),
			"created_time":      timeOrNil(dataSet.CreatedTime),
			"last_updated_time": timeOrNil(dataSet.LastUpdatedTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func dashboardObservation(boundary awscloud.Boundary, dashboard Dashboard) awscloud.ResourceObservation {
	arn := strings.TrimSpace(dashboard.ARN)
	name := strings.TrimSpace(dashboard.Name)
	resourceID := dashboardResourceID(dashboard)
	attributes := map[string]any{
		"dashboard_id":      strings.TrimSpace(dashboard.ID),
		"data_set_arns":     dedupeStrings(dashboard.DataSetARNs),
		"created_time":      timeOrNil(dashboard.CreatedTime),
		"last_updated_time": timeOrNil(dashboard.LastUpdatedTime),
	}
	if dashboard.PublishedVersionNumber > 0 {
		attributes["published_version_number"] = dashboard.PublishedVersionNumber
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeQuickSightDashboard,
		Name:               name,
		Tags:               cloneStringMap(dashboard.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func analysisObservation(boundary awscloud.Boundary, analysis Analysis) awscloud.ResourceObservation {
	arn := strings.TrimSpace(analysis.ARN)
	name := strings.TrimSpace(analysis.Name)
	resourceID := analysisResourceID(analysis)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeQuickSightAnalysis,
		Name:         name,
		State:        strings.TrimSpace(analysis.Status),
		Tags:         cloneStringMap(analysis.Tags),
		Attributes: map[string]any{
			"analysis_id":       strings.TrimSpace(analysis.ID),
			"data_set_arns":     dedupeStrings(analysis.DataSetARNs),
			"created_time":      timeOrNil(analysis.CreatedTime),
			"last_updated_time": timeOrNil(analysis.LastUpdatedTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
