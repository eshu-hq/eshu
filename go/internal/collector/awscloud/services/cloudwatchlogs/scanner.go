package cloudwatchlogs

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS CloudWatch Logs log group metadata facts for one claimed
// account and region. It never reads log events, log stream payloads, Insights
// query results, export payloads, resource policies, or subscription payloads.
type Scanner struct {
	Client Client
}

// Scan observes CloudWatch Logs log groups and direct KMS dependency metadata
// through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("cloudwatchlogs scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceCloudWatchLogs
	case awscloud.ServiceCloudWatchLogs:
	default:
		return nil, fmt.Errorf("cloudwatchlogs scanner received service_kind %q", boundary.ServiceKind)
	}

	logGroups, err := s.Client.ListLogGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudWatch Logs log groups: %w", err)
	}
	var envelopes []facts.Envelope
	for _, logGroup := range logGroups {
		groupEnvelopes, err := logGroupEnvelopes(boundary, logGroup)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, groupEnvelopes...)
	}
	return envelopes, nil
}

func logGroupEnvelopes(boundary awscloud.Boundary, logGroup LogGroup) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(logGroupObservation(boundary, logGroup))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := kmsRelationship(boundary, logGroup); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func logGroupObservation(boundary awscloud.Boundary, logGroup LogGroup) awscloud.ResourceObservation {
	logGroupARN := strings.TrimSpace(logGroup.ARN)
	name := strings.TrimSpace(logGroup.Name)
	resourceID := firstNonEmpty(logGroupARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          logGroupARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCloudWatchLogsLogGroup,
		Name:         name,
		Tags:         cloneStringMap(logGroup.Tags),
		Attributes: map[string]any{
			"creation_time":                       timeOrNil(logGroup.CreationTime),
			"retention_in_days":                   logGroup.RetentionInDays,
			"stored_bytes":                        logGroup.StoredBytes,
			"metric_filter_count":                 logGroup.MetricFilterCount,
			"log_group_class":                     strings.TrimSpace(logGroup.LogGroupClass),
			"data_protection_status":              strings.TrimSpace(logGroup.DataProtectionStatus),
			"inherited_properties":                cloneStrings(logGroup.InheritedProperties),
			"kms_key_id":                          strings.TrimSpace(logGroup.KMSKeyID),
			"deletion_protected":                  logGroup.DeletionProtected,
			"bearer_token_authentication_enabled": logGroup.BearerTokenAuth,
		},
		CorrelationAnchors: []string{logGroupARN, name},
		SourceRecordID:     resourceID,
	}
}
