// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudtrail

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS CloudTrail metadata facts for one claimed account and
// region.
//
// CloudTrail is the audit-config service. The scanner never reads audit event
// payloads (LookupEvents), never executes Lake queries (StartQuery,
// GetQueryResults), and never mutates trails, event data stores, channels, or
// dashboards. The forbidden APIs are excluded from the Client interface by
// construction; see TestClientInterfaceExcludesEventPayloadAndMutationAPIs.
type Scanner struct {
	Client Client
}

// Scan observes CloudTrail trails, event data stores, channels, and Lake
// dashboards through the configured client and returns reported-confidence
// AWS facts. The scan calls four read-only list-and-describe paths and never
// reaches event-extraction, Lake query, or mutation surfaces.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("cloudtrail scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCloudTrail:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCloudTrail
	default:
		return nil, fmt.Errorf("cloudtrail scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	trails, err := s.Client.ListTrails(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudTrail trails: %w", err)
	}
	for _, trail := range trails {
		envelopes, err = appendTrail(envelopes, boundary, trail)
		if err != nil {
			return nil, err
		}
	}

	stores, err := s.Client.ListEventDataStores(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudTrail event data stores: %w", err)
	}
	for _, store := range stores {
		envelopes, err = appendEventDataStore(envelopes, boundary, store)
		if err != nil {
			return nil, err
		}
	}

	channels, err := s.Client.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudTrail channels: %w", err)
	}
	for _, channel := range channels {
		envelope, err := awscloud.NewResourceEnvelope(channelObservation(boundary, channel))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	dashboards, err := s.Client.ListDashboards(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudTrail dashboards: %w", err)
	}
	for _, dashboard := range dashboards {
		envelope, err := awscloud.NewResourceEnvelope(dashboardObservation(boundary, dashboard))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

func appendTrail(envelopes []facts.Envelope, boundary awscloud.Boundary, trail Trail) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(trailObservation(boundary, trail))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, resource)
	for _, relationship := range trailRelationships(boundary, trail) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func appendEventDataStore(envelopes []facts.Envelope, boundary awscloud.Boundary, store EventDataStore) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(eventDataStoreObservation(boundary, store))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, resource)
	if relationship, ok := eventDataStoreKMSRelationship(boundary, store); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func trailObservation(boundary awscloud.Boundary, trail Trail) awscloud.ResourceObservation {
	trailARN := strings.TrimSpace(trail.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          trailARN,
		ResourceID:   firstNonEmpty(trailARN, trail.Name),
		ResourceType: awscloud.ResourceTypeCloudTrailTrail,
		Name:         strings.TrimSpace(trail.Name),
		State:        loggingState(trail.LoggingEnabled),
		Tags:         cloneStringMap(trail.Tags),
		Attributes: map[string]any{
			"home_region":                         strings.TrimSpace(trail.HomeRegion),
			"s3_bucket_name":                      strings.TrimSpace(trail.S3BucketName),
			"s3_key_prefix":                       strings.TrimSpace(trail.S3KeyPrefix),
			"sns_topic_arn":                       strings.TrimSpace(trail.SNSTopicARN),
			"cloudwatch_logs_log_group_arn":       strings.TrimSpace(trail.CloudWatchLogsLogGroupARN),
			"cloudwatch_logs_role_arn":            strings.TrimSpace(trail.CloudWatchLogsRoleARN),
			"kms_key_id":                          strings.TrimSpace(trail.KMSKeyID),
			"include_global_service_events":       trail.IncludeGlobalServiceEvents,
			"is_multi_region_trail":               trail.IsMultiRegionTrail,
			"is_organization_trail":               trail.IsOrganizationTrail,
			"log_file_validation_enabled":         trail.LogFileValidationEnabled,
			"has_custom_event_selectors":          trail.HasCustomEventSelectors,
			"has_insight_selectors":               trail.HasInsightSelectors,
			"logging_enabled":                     trail.LoggingEnabled,
			"latest_delivery_error":               strings.TrimSpace(trail.LatestDeliveryError),
			"latest_notification_error":           strings.TrimSpace(trail.LatestNotificationError),
			"event_selector_count":                trail.EventSelectorSummary.EventSelectorCount,
			"advanced_event_selector_count":       trail.EventSelectorSummary.AdvancedEventSelectorCount,
			"event_selector_resource_type_counts": cloneIntMap(trail.EventSelectorSummary.ResourceTypeCounts),
			"insight_selectors":                   cloneStrings(trail.InsightSelectors),
		},
		CorrelationAnchors: []string{trailARN, trail.Name},
		SourceRecordID:     firstNonEmpty(trailARN, trail.Name),
	}
}

func eventDataStoreObservation(boundary awscloud.Boundary, store EventDataStore) awscloud.ResourceObservation {
	storeARN := strings.TrimSpace(store.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          storeARN,
		ResourceID:   firstNonEmpty(storeARN, store.Name),
		ResourceType: awscloud.ResourceTypeCloudTrailEventDataStore,
		Name:         strings.TrimSpace(store.Name),
		State:        strings.TrimSpace(store.Status),
		Tags:         cloneStringMap(store.Tags),
		Attributes: map[string]any{
			"retention_period":               store.RetentionPeriod,
			"multi_region_enabled":           store.MultiRegionEnabled,
			"organization_enabled":           store.OrganizationEnabled,
			"termination_protection_enabled": store.TerminationProtectionEnabled,
			"billing_mode":                   strings.TrimSpace(store.BillingMode),
			"kms_key_id":                     strings.TrimSpace(store.KMSKeyID),
			"created_timestamp":              strings.TrimSpace(store.CreatedTimestamp),
			"updated_timestamp":              strings.TrimSpace(store.UpdatedTimestamp),
			"advanced_event_selector_count":  store.AdvancedEventSelectorCount,
		},
		CorrelationAnchors: []string{storeARN, store.Name},
		SourceRecordID:     firstNonEmpty(storeARN, store.Name),
	}
}

func channelObservation(boundary awscloud.Boundary, channel Channel) awscloud.ResourceObservation {
	channelARN := strings.TrimSpace(channel.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          channelARN,
		ResourceID:   firstNonEmpty(channelARN, channel.Name),
		ResourceType: awscloud.ResourceTypeCloudTrailChannel,
		Name:         strings.TrimSpace(channel.Name),
		Tags:         cloneStringMap(channel.Tags),
		Attributes: map[string]any{
			"source":           strings.TrimSpace(channel.Source),
			"destination_type": strings.TrimSpace(channel.DestinationType),
			"destination_arn":  strings.TrimSpace(channel.DestinationARN),
		},
		CorrelationAnchors: []string{channelARN, channel.Name},
		SourceRecordID:     firstNonEmpty(channelARN, channel.Name),
	}
}

func dashboardObservation(boundary awscloud.Boundary, dashboard Dashboard) awscloud.ResourceObservation {
	dashboardARN := strings.TrimSpace(dashboard.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          dashboardARN,
		ResourceID:   firstNonEmpty(dashboardARN, dashboard.Name),
		ResourceType: awscloud.ResourceTypeCloudTrailDashboardConfig,
		Name:         strings.TrimSpace(dashboard.Name),
		State:        strings.TrimSpace(dashboard.Status),
		Tags:         cloneStringMap(dashboard.Tags),
		Attributes: map[string]any{
			"type":              strings.TrimSpace(dashboard.Type),
			"refresh_schedule":  strings.TrimSpace(dashboard.RefreshSchedule),
			"widget_count":      dashboard.WidgetCount,
			"created_timestamp": strings.TrimSpace(dashboard.CreatedTimestamp),
			"updated_timestamp": strings.TrimSpace(dashboard.UpdatedTimestamp),
		},
		CorrelationAnchors: []string{dashboardARN, dashboard.Name},
		SourceRecordID:     firstNonEmpty(dashboardARN, dashboard.Name),
	}
}

func trailRelationships(boundary awscloud.Boundary, trail Trail) []awscloud.RelationshipObservation {
	trailARN := strings.TrimSpace(trail.ARN)
	if trailARN == "" {
		return nil
	}
	sourceID := firstNonEmpty(trailARN, trail.Name)
	var out []awscloud.RelationshipObservation
	if bucket := strings.TrimSpace(trail.S3BucketName); bucket != "" {
		out = append(out, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCloudTrailTrailLogsToS3Bucket,
			SourceResourceID: sourceID,
			SourceARN:        trailARN,
			TargetResourceID: bucket,
			TargetType:       awscloud.ResourceTypeS3Bucket,
			Attributes: map[string]any{
				"s3_key_prefix": strings.TrimSpace(trail.S3KeyPrefix),
			},
			SourceRecordID: trailARN + "->s3:" + bucket,
		})
	}
	if logGroup := strings.TrimSpace(trail.CloudWatchLogsLogGroupARN); logGroup != "" {
		out = append(out, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCloudTrailTrailLogsToCloudWatchLogs,
			SourceResourceID: sourceID,
			SourceARN:        trailARN,
			TargetResourceID: logGroup,
			TargetARN:        logGroup,
			TargetType:       awscloud.ResourceTypeCloudWatchLogsLogGroup,
			SourceRecordID:   trailARN + "->logs:" + logGroup,
		})
	}
	if topic := strings.TrimSpace(trail.SNSTopicARN); topic != "" {
		out = append(out, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCloudTrailTrailNotifiesSNSTopic,
			SourceResourceID: sourceID,
			SourceARN:        trailARN,
			TargetResourceID: topic,
			TargetARN:        topic,
			TargetType:       awscloud.ResourceTypeSNSTopic,
			SourceRecordID:   trailARN + "->sns:" + topic,
		})
	}
	if kms := strings.TrimSpace(trail.KMSKeyID); kms != "" {
		out = append(out, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCloudTrailTrailUsesKMSKey,
			SourceResourceID: sourceID,
			SourceARN:        trailARN,
			TargetResourceID: kms,
			TargetARN:        kmsTargetARN(kms),
			TargetType:       resourceTypeKMSKey,
			SourceRecordID:   trailARN + "->kms:" + kms,
		})
	}
	return out
}

func eventDataStoreKMSRelationship(
	boundary awscloud.Boundary,
	store EventDataStore,
) (awscloud.RelationshipObservation, bool) {
	storeARN := strings.TrimSpace(store.ARN)
	kms := strings.TrimSpace(store.KMSKeyID)
	if storeARN == "" || kms == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudTrailEventDataStoreUsesKMSKey,
		SourceResourceID: storeARN,
		SourceARN:        storeARN,
		TargetResourceID: kms,
		TargetARN:        kmsTargetARN(kms),
		TargetType:       resourceTypeKMSKey,
		SourceRecordID:   storeARN + "->kms:" + kms,
	}, true
}

// resourceTypeKMSKey is the relationship target type CloudTrail emits for KMS
// key references, matching the convention shared by every other awscloud
// service scanner that points at a KMS key.
const resourceTypeKMSKey = "aws_kms_key"

// kmsTargetARN returns key as the relationship TargetARN only when it is an
// ARN-shaped value. CloudTrail's KmsKeyId may be a bare key id or an alias
// (e.g. "alias/foo"), neither of which is an ARN; returning "" for those keeps
// non-ARN identifiers out of target_arn while TargetResourceID still carries
// the raw value. The caller is responsible for trimming whitespace.
func kmsTargetARN(key string) string {
	if strings.HasPrefix(key, "arn:") {
		return key
	}
	return ""
}

func loggingState(enabled bool) string {
	if enabled {
		return "logging"
	}
	return "not_logging"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneIntMap(input map[string]int) map[string]int {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]int, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
