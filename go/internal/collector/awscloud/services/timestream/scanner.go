// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package timestream

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Timestream for LiveAnalytics metadata-only facts for one
// claimed account and region. It never reads time-series records, measure
// values, or query results, and never writes records or mutates Timestream
// state. It reports databases and tables plus the table-in-database,
// database-to-KMS-key, and table-to-S3 (magnetic-store rejected-data report
// location) relationships.
type Scanner struct {
	// Client is the metadata-only Timestream snapshot source.
	Client Client
}

// Scan observes Timestream databases, their tables, and the direct KMS and S3
// dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("timestream scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceTimestream:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceTimestream
	default:
		return nil, fmt.Errorf("timestream scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Timestream databases: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, database := range snapshot.Databases {
		next, err := databaseEnvelopes(boundary, database)
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

func databaseEnvelopes(boundary awscloud.Boundary, database Database) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(databaseObservation(boundary, database))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := databaseKMSRelationship(boundary, database); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	databaseID := databaseResourceID(database)
	for _, table := range database.Tables {
		next, err := tableEnvelopes(boundary, databaseID, table)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func tableEnvelopes(boundary awscloud.Boundary, databaseID string, table Table) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(tableObservation(boundary, table))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		tableInDatabaseRelationship(boundary, databaseID, table),
		tableRejectedDataS3Relationship(boundary, table),
	} {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func databaseObservation(boundary awscloud.Boundary, database Database) awscloud.ResourceObservation {
	databaseARN := strings.TrimSpace(database.ARN)
	name := strings.TrimSpace(database.Name)
	resourceID := databaseResourceID(database)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          databaseARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeTimestreamDatabase,
		Name:         name,
		Tags:         cloneStringMap(database.Tags),
		Attributes: map[string]any{
			"database_name":     name,
			"kms_key_id":        strings.TrimSpace(database.KMSKeyID),
			"table_count":       database.TableCount,
			"creation_time":     timeOrNil(database.CreationTime),
			"last_updated_time": timeOrNil(database.LastUpdatedTime),
		},
		CorrelationAnchors: []string{databaseARN, name},
		SourceRecordID:     resourceID,
	}
}

func tableObservation(boundary awscloud.Boundary, table Table) awscloud.ResourceObservation {
	tableARN := strings.TrimSpace(table.ARN)
	name := strings.TrimSpace(table.Name)
	resourceID := tableResourceID(table)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          tableARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeTimestreamTable,
		Name:         name,
		State:        strings.TrimSpace(table.State),
		Tags:         cloneStringMap(table.Tags),
		Attributes: map[string]any{
			"database_name":                           strings.TrimSpace(table.DatabaseName),
			"memory_store_retention_period_in_hours":  table.MemoryStoreRetentionPeriodInHours,
			"magnetic_store_retention_period_in_days": table.MagneticStoreRetentionPeriodInDays,
			"magnetic_store_writes_enabled":           table.MagneticStoreWritesEnabled,
			"rejected_data_s3_bucket":                 strings.TrimSpace(table.RejectedDataS3Bucket),
			"rejected_data_s3_prefix":                 strings.TrimSpace(table.RejectedDataS3Prefix),
			"rejected_data_s3_encryption_option":      strings.TrimSpace(table.RejectedDataS3EncryptionOption),
			"partition_key_names":                     cloneStrings(table.PartitionKeyNames),
			"creation_time":                           timeOrNil(table.CreationTime),
			"last_updated_time":                       timeOrNil(table.LastUpdatedTime),
		},
		CorrelationAnchors: []string{tableARN, name},
		SourceRecordID:     resourceID,
	}
}
