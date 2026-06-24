// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudtrail

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestClientInterfaceExcludesEventPayloadAndMutationAPIs proves the Client
// surface this scanner consumes never carries CloudTrail event-extraction or
// mutation methods. The forbidden APIs are LookupEvents (event payload
// extraction), StartQuery and GetQueryResults (Lake query data plane), and
// the trail/store/channel/dashboard mutation surface (CreateTrail,
// UpdateTrail, DeleteTrail, StartLogging, StopLogging, PutEventSelectors,
// PutInsightSelectors, CreateEventDataStore, UpdateEventDataStore,
// DeleteEventDataStore, CreateChannel, UpdateChannel, DeleteChannel,
// CreateDashboard, UpdateDashboard, DeleteDashboard).
//
// The interface MUST stay metadata-only. A maintainer adding any of these
// methods to Client breaks the security contract for the entire scanner.
func TestClientInterfaceExcludesEventPayloadAndMutationAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	forbidden := []string{
		// Event payload extraction.
		"LookupEvents",
		// Lake query data plane.
		"StartQuery",
		"GetQueryResults",
		"CancelQuery",
		"DescribeQuery",
		// Mutation APIs.
		"CreateTrail",
		"UpdateTrail",
		"DeleteTrail",
		"StartLogging",
		"StopLogging",
		"PutEventSelectors",
		"PutInsightSelectors",
		"CreateEventDataStore",
		"UpdateEventDataStore",
		"DeleteEventDataStore",
		"RestoreEventDataStore",
		"CreateChannel",
		"UpdateChannel",
		"DeleteChannel",
		"CreateDashboard",
		"UpdateDashboard",
		"DeleteDashboard",
		"StartEventDataStoreIngestion",
		"StopEventDataStoreIngestion",
		"StartDashboardRefresh",
	}
	for _, name := range forbidden {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("Client exposes forbidden method %q; CloudTrail scanner must stay metadata-only and never reach event payloads or mutate resources", name)
		}
	}
}

func TestScannerEmitsTrailEventStoreChannelAndDashboardMetadata(t *testing.T) {
	trailARN := "arn:aws:cloudtrail:us-east-1:123456789012:trail/org-trail"
	bucket := "org-trail-logs"
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:org-trail:*"
	kmsKey := "arn:aws:kms:us-east-1:123456789012:key/abcd-1234"
	snsARN := "arn:aws:sns:us-east-1:123456789012:org-trail-alerts"
	storeARN := "arn:aws:cloudtrail:us-east-1:123456789012:eventdatastore/aaaa-bbbb"
	storeKMSKey := "arn:aws:kms:us-east-1:123456789012:key/store-1111"
	channelARN := "arn:aws:cloudtrail:us-east-1:123456789012:channel/ch-1"
	dashboardARN := "arn:aws:cloudtrail:us-east-1:123456789012:dashboard/db-1"

	client := fakeClient{
		trails: []Trail{{
			ARN:                        trailARN,
			Name:                       "org-trail",
			HomeRegion:                 "us-east-1",
			S3BucketName:               bucket,
			S3KeyPrefix:                "audit/",
			SNSTopicARN:                snsARN,
			CloudWatchLogsLogGroupARN:  logGroupARN,
			CloudWatchLogsRoleARN:      "arn:aws:iam::123456789012:role/CloudTrailToCW",
			KMSKeyID:                   kmsKey,
			IncludeGlobalServiceEvents: true,
			IsMultiRegionTrail:         true,
			IsOrganizationTrail:        true,
			LogFileValidationEnabled:   true,
			HasCustomEventSelectors:    true,
			HasInsightSelectors:        true,
			LoggingEnabled:             true,
			EventSelectorSummary: EventSelectorSummary{
				EventSelectorCount:         2,
				AdvancedEventSelectorCount: 1,
				ResourceTypeCounts: map[string]int{
					"AWS::S3::Object":       1,
					"AWS::Lambda::Function": 1,
					"AWS::DynamoDB::Table":  1,
				},
			},
			InsightSelectors: []string{"ApiCallRateInsight", "ApiErrorRateInsight"},
			Tags:             map[string]string{"Environment": "prod"},
		}},
		eventDataStores: []EventDataStore{{
			ARN:                          storeARN,
			Name:                         "security-lake",
			Status:                       "ENABLED",
			RetentionPeriod:              2555,
			MultiRegionEnabled:           true,
			OrganizationEnabled:          true,
			TerminationProtectionEnabled: true,
			BillingMode:                  "EXTENDABLE_RETENTION_PRICING",
			KMSKeyID:                     storeKMSKey,
			AdvancedEventSelectorCount:   3,
			Tags:                         map[string]string{"Team": "security"},
		}},
		channels: []Channel{{
			ARN:             channelARN,
			Name:            "external-events",
			Source:          "Custom",
			DestinationType: "EVENT_DATA_STORE",
			DestinationARN:  storeARN,
			Tags:            map[string]string{"Team": "security"},
		}},
		dashboards: []Dashboard{{
			ARN:             dashboardARN,
			Name:            "security-overview",
			Status:          "CREATED",
			Type:            "CUSTOM",
			RefreshSchedule: "DAILY",
			WidgetCount:     4,
			Tags:            map[string]string{"Team": "security"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	trail := resourceByType(t, envelopes, awscloud.ResourceTypeCloudTrailTrail)
	if got, want := trail.Payload["arn"], trailARN; got != want {
		t.Fatalf("trail arn = %#v, want %q", got, want)
	}
	trailAttrs := attributesOf(t, trail)
	if got, want := trailAttrs["s3_bucket_name"], bucket; got != want {
		t.Fatalf("s3_bucket_name = %#v, want %q", got, want)
	}
	if got, want := trailAttrs["s3_key_prefix"], "audit/"; got != want {
		t.Fatalf("s3_key_prefix = %#v, want %q", got, want)
	}
	if got, want := trailAttrs["is_multi_region_trail"], true; got != want {
		t.Fatalf("is_multi_region_trail = %#v, want %v", got, want)
	}
	if got, want := trailAttrs["is_organization_trail"], true; got != want {
		t.Fatalf("is_organization_trail = %#v, want %v", got, want)
	}
	if got, want := trailAttrs["logging_enabled"], true; got != want {
		t.Fatalf("logging_enabled = %#v, want %v", got, want)
	}
	if got, want := trailAttrs["log_file_validation_enabled"], true; got != want {
		t.Fatalf("log_file_validation_enabled = %#v, want %v", got, want)
	}
	if got, want := trailAttrs["include_global_service_events"], true; got != want {
		t.Fatalf("include_global_service_events = %#v, want %v", got, want)
	}
	if got, want := trailAttrs["kms_key_id"], kmsKey; got != want {
		t.Fatalf("kms_key_id = %#v, want %q", got, want)
	}
	insightSelectors, ok := trailAttrs["insight_selectors"].([]string)
	if !ok {
		t.Fatalf("insight_selectors = %#v, want []string", trailAttrs["insight_selectors"])
	}
	if got, want := insightSelectors, []string{"ApiCallRateInsight", "ApiErrorRateInsight"}; !equalStringSlice(got, want) {
		t.Fatalf("insight_selectors = %#v, want %#v", got, want)
	}
	if got, want := trailAttrs["event_selector_count"], 2; got != want {
		t.Fatalf("event_selector_count = %#v, want %d", got, want)
	}
	if got, want := trailAttrs["advanced_event_selector_count"], 1; got != want {
		t.Fatalf("advanced_event_selector_count = %#v, want %d", got, want)
	}
	resourceTypeCounts, ok := trailAttrs["event_selector_resource_type_counts"].(map[string]int)
	if !ok {
		t.Fatalf("event_selector_resource_type_counts = %#v, want map[string]int", trailAttrs["event_selector_resource_type_counts"])
	}
	if resourceTypeCounts["AWS::S3::Object"] != 1 {
		t.Fatalf("resource_type_counts = %#v, want AWS::S3::Object=1", resourceTypeCounts)
	}

	// Event payload, selector body, and Lake query bodies must never appear on
	// the trail attribute map.
	for _, forbidden := range []string{
		"events", "event_payload", "event_data", "lookup_events",
		"event_selectors_body", "event_selector_bodies",
		"advanced_event_selectors_body", "advanced_event_selector_bodies",
		"insight_selectors_body",
		"query", "query_string", "query_result", "query_results",
	} {
		if _, exists := trailAttrs[forbidden]; exists {
			t.Fatalf("trail attribute %q persisted; CloudTrail scanner must never store event payloads or query bodies", forbidden)
		}
	}

	assertRelationship(t, envelopes, awscloud.RelationshipCloudTrailTrailLogsToS3Bucket)
	assertRelationship(t, envelopes, awscloud.RelationshipCloudTrailTrailLogsToCloudWatchLogs)
	assertRelationship(t, envelopes, awscloud.RelationshipCloudTrailTrailNotifiesSNSTopic)
	assertRelationship(t, envelopes, awscloud.RelationshipCloudTrailTrailUsesKMSKey)

	store := resourceByType(t, envelopes, awscloud.ResourceTypeCloudTrailEventDataStore)
	storeAttrs := attributesOf(t, store)
	if got, want := storeAttrs["retention_period"], int32(2555); got != want {
		t.Fatalf("retention_period = %#v, want %d", got, want)
	}
	if got, want := storeAttrs["multi_region_enabled"], true; got != want {
		t.Fatalf("multi_region_enabled = %#v, want %v", got, want)
	}
	if got, want := storeAttrs["organization_enabled"], true; got != want {
		t.Fatalf("organization_enabled = %#v, want %v", got, want)
	}
	if got, want := storeAttrs["advanced_event_selector_count"], 3; got != want {
		t.Fatalf("advanced_event_selector_count = %#v, want %d", got, want)
	}
	for _, forbidden := range []string{
		"events", "event_payload", "advanced_event_selectors_body",
		"query", "query_string", "query_result", "query_results",
	} {
		if _, exists := storeAttrs[forbidden]; exists {
			t.Fatalf("event data store attribute %q persisted; selector bodies and query results must never leak", forbidden)
		}
	}
	assertRelationship(t, envelopes, awscloud.RelationshipCloudTrailEventDataStoreUsesKMSKey)

	channel := resourceByType(t, envelopes, awscloud.ResourceTypeCloudTrailChannel)
	channelAttrs := attributesOf(t, channel)
	if got, want := channelAttrs["destination_type"], "EVENT_DATA_STORE"; got != want {
		t.Fatalf("channel destination_type = %#v, want %q", got, want)
	}
	if got, want := channelAttrs["destination_arn"], storeARN; got != want {
		t.Fatalf("channel destination_arn = %#v, want %q", got, want)
	}

	dashboard := resourceByType(t, envelopes, awscloud.ResourceTypeCloudTrailDashboardConfig)
	if got, want := dashboard.Payload["state"], "CREATED"; got != want {
		t.Fatalf("dashboard state = %#v, want %q", got, want)
	}
	dashboardAttrs := attributesOf(t, dashboard)
	if got, want := dashboardAttrs["widget_count"], 4; got != want {
		t.Fatalf("dashboard widget_count = %#v, want %d", got, want)
	}
	for _, forbidden := range []string{
		"widgets", "widget_definitions", "widget_query", "widget_queries",
		"query_results",
	} {
		if _, exists := dashboardAttrs[forbidden]; exists {
			t.Fatalf("dashboard attribute %q persisted; widget bodies and query results must never leak", forbidden)
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECR

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCloudTrail,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:cloudtrail:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	trails          []Trail
	eventDataStores []EventDataStore
	channels        []Channel
	dashboards      []Dashboard
}

func (c fakeClient) ListTrails(context.Context) ([]Trail, error) {
	return c.trails, nil
}

func (c fakeClient) ListEventDataStores(context.Context) ([]EventDataStore, error) {
	return c.eventDataStores, nil
}

func (c fakeClient) ListChannels(context.Context) ([]Channel, error) {
	return c.channels, nil
}

func (c fakeClient) ListDashboards(context.Context) ([]Dashboard, error) {
	return c.dashboards, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in envelopes (n=%d)", resourceType, len(envelopes))
	return facts.Envelope{}
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q in envelopes (n=%d)", relationshipType, len(envelopes))
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func equalStringSlice(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
