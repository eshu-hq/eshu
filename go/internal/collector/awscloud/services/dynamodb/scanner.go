package dynamodb

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS DynamoDB metadata facts for one claimed account and region.
// It never reads table items, stream records, exports, backup payloads,
// resource policies, or PartiQL/query/scan results.
type Scanner struct {
	Client Client
}

// Scan observes DynamoDB tables and direct KMS dependency metadata through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("dynamodb scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceDynamoDB
	case awscloud.ServiceDynamoDB:
	default:
		return nil, fmt.Errorf("dynamodb scanner received service_kind %q", boundary.ServiceKind)
	}

	tables, err := s.Client.ListTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DynamoDB tables: %w", err)
	}
	var envelopes []facts.Envelope
	for _, table := range tables {
		tableEnvelopes, err := tableEnvelopes(boundary, table)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, tableEnvelopes...)
	}
	return envelopes, nil
}

func tableEnvelopes(boundary awscloud.Boundary, table Table) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(tableObservation(boundary, table))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := kmsRelationship(boundary, table); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func tableObservation(boundary awscloud.Boundary, table Table) awscloud.ResourceObservation {
	tableARN := strings.TrimSpace(table.ARN)
	tableName := strings.TrimSpace(table.Name)
	resourceID := firstNonEmpty(tableARN, table.ID, tableName)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          tableARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDynamoDBTable,
		Name:         tableName,
		State:        strings.TrimSpace(table.Status),
		Tags:         cloneStringMap(table.Tags),
		Attributes:   tableAttributes(table),
		CorrelationAnchors: []string{
			tableARN,
			tableName,
			strings.TrimSpace(table.ID),
			strings.TrimSpace(table.Stream.LatestStreamARN),
		},
		SourceRecordID: resourceID,
	}
}

func tableAttributes(table Table) map[string]any {
	return map[string]any{
		"table_id":                      strings.TrimSpace(table.ID),
		"creation_time":                 timeOrNil(table.CreationTime),
		"billing_mode":                  strings.TrimSpace(table.BillingMode),
		"table_class":                   strings.TrimSpace(table.TableClass),
		"item_count":                    table.ItemCount,
		"table_size_bytes":              table.TableSizeBytes,
		"deletion_protection_enabled":   table.DeletionProtectionEnabled,
		"key_schema":                    keySchemaMaps(table.KeySchema),
		"attribute_definitions":         attributeDefinitionMaps(table.AttributeDefinitions),
		"provisioned_throughput":        throughputMap(table.ProvisionedThroughput),
		"on_demand_throughput":          onDemandThroughputMap(table.OnDemandThroughput),
		"sse_status":                    strings.TrimSpace(table.SSE.Status),
		"sse_type":                      strings.TrimSpace(table.SSE.Type),
		"kms_master_key_arn":            strings.TrimSpace(table.SSE.KMSMasterKeyARN),
		"ttl_status":                    strings.TrimSpace(table.TTL.Status),
		"ttl_attribute_name":            strings.TrimSpace(table.TTL.AttributeName),
		"continuous_backups_status":     strings.TrimSpace(table.ContinuousBackups.Status),
		"point_in_time_recovery_status": strings.TrimSpace(table.ContinuousBackups.PointInTimeRecoveryStatus),
		"recovery_period_in_days":       table.ContinuousBackups.RecoveryPeriodInDays,
		"stream_enabled":                table.Stream.Enabled,
		"stream_view_type":              strings.TrimSpace(table.Stream.ViewType),
		"latest_stream_arn":             strings.TrimSpace(table.Stream.LatestStreamARN),
		"latest_stream_label":           strings.TrimSpace(table.Stream.LatestLabel),
		"global_secondary_indexes":      secondaryIndexMaps(table.GlobalSecondaryIndexes),
		"local_secondary_indexes":       secondaryIndexMaps(table.LocalSecondaryIndexes),
		"replicas":                      replicaMaps(table.Replicas),
	}
}
