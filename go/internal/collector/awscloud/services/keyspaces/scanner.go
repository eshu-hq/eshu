// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package keyspaces

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Keyspaces (for Apache Cassandra) metadata facts for one
// claimed account and region. It never executes CQL, never reads table rows or
// cells, never runs ExecuteStatement/BatchStatement/Select, and never mutates
// keyspaces or tables. Schema column names and types are structural metadata and
// are the only schema information emitted.
type Scanner struct {
	Client Client
}

// Scan observes Amazon Keyspaces keyspaces and tables plus the table-in-keyspace
// and direct customer-managed KMS dependency edges through the configured
// client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("keyspaces scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceKeyspaces:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceKeyspaces
	default:
		return nil, fmt.Errorf("keyspaces scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Keyspaces keyspaces: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, keyspace := range snapshot.Keyspaces {
		envelope, err := awscloud.NewResourceEnvelope(keyspaceObservation(boundary, keyspace))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, table := range snapshot.Tables {
		tableEnvelopes, err := tableEnvelopes(boundary, table)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, tableEnvelopes...)
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

func tableEnvelopes(boundary awscloud.Boundary, table Table) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(tableObservation(boundary, table))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		tableKeyspaceRelationship(boundary, table),
		tableKMSRelationship(boundary, table),
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

func keyspaceObservation(boundary awscloud.Boundary, keyspace Keyspace) awscloud.ResourceObservation {
	keyspaceARN := strings.TrimSpace(keyspace.ARN)
	keyspaceName := strings.TrimSpace(keyspace.Name)
	resourceID := firstNonEmpty(keyspaceARN, keyspaceName)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          keyspaceARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeKeyspacesKeyspace,
		Name:         keyspaceName,
		Attributes: map[string]any{
			"keyspace_name":        keyspaceName,
			"replication_strategy": strings.TrimSpace(keyspace.ReplicationStrategy),
			"replication_regions":  cloneStrings(keyspace.ReplicationRegions),
		},
		CorrelationAnchors: []string{keyspaceARN, keyspaceName},
		SourceRecordID:     resourceID,
	}
}

func tableObservation(boundary awscloud.Boundary, table Table) awscloud.ResourceObservation {
	tableARN := strings.TrimSpace(table.ARN)
	tableName := strings.TrimSpace(table.Name)
	resourceID := firstNonEmpty(tableARN, tableName)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          tableARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeKeyspacesTable,
		Name:         tableName,
		State:        strings.TrimSpace(table.Status),
		Tags:         cloneStringMap(table.Tags),
		Attributes:   tableAttributes(table),
		CorrelationAnchors: []string{
			tableARN,
			tableName,
			strings.TrimSpace(table.KeyspaceARN),
		},
		SourceRecordID: resourceID,
	}
}

func tableAttributes(table Table) map[string]any {
	return map[string]any{
		"keyspace_name":          strings.TrimSpace(table.KeyspaceName),
		"keyspace_arn":           strings.TrimSpace(table.KeyspaceARN),
		"creation_time":          timeOrNil(table.CreationTime),
		"capacity_mode":          strings.TrimSpace(table.CapacityMode),
		"read_capacity_units":    table.ReadCapacityUnits,
		"write_capacity_units":   table.WriteCapacityUnits,
		"default_time_to_live":   table.DefaultTimeToLive,
		"ttl_status":             strings.TrimSpace(table.TimeToLiveStatus),
		"encryption_type":        strings.TrimSpace(table.Encryption.Type),
		"kms_key_identifier":     strings.TrimSpace(table.Encryption.KMSKeyIdentifier),
		"point_in_time_recovery": strings.TrimSpace(table.PointInTimeRecovery.Status),
		"client_side_timestamps": strings.TrimSpace(table.ClientSideTimestamps),
		"cdc_status":             strings.TrimSpace(table.CDCStatus),
		"comment":                strings.TrimSpace(table.Comment),
		"latest_stream_arn":      strings.TrimSpace(table.LatestStreamARN),
		"schema_columns":         columnMaps(table.Schema.Columns),
		"schema_partition_keys":  cloneStrings(table.Schema.PartitionKeys),
		"schema_clustering_keys": clusteringKeyMaps(table.Schema.ClusteringKeys),
		"schema_static_columns":  cloneStrings(table.Schema.StaticColumns),
	}
}
