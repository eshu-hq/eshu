// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cttypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesEventPayloadAndMutationAPIs is the adapter
// side of the same security contract enforced on the scanner-owned `Client`
// interface. It proves the AWS SDK surface this adapter accepts never lists
// `LookupEvents`, Lake query APIs, or any mutation API as a callable method.
//
// Combined with the scanner-side guard test, this gives two layers of
// reflective protection so a maintainer cannot quietly widen the contract.
func TestAPIClientInterfaceExcludesEventPayloadAndMutationAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Event payload extraction.
		"LookupEvents",
		// Lake query data plane.
		"StartQuery",
		"GetQueryResults",
		"CancelQuery",
		"DescribeQuery",
		"GenerateQuery",
		"ListQueries",
		// Mutation APIs.
		"CreateTrail",
		"UpdateTrail",
		"DeleteTrail",
		"StartLogging",
		"StopLogging",
		"PutEventSelectors",
		"PutInsightSelectors",
		"PutEventConfiguration",
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
		// Tag mutation.
		"AddTags",
		"RemoveTags",
		// Resource policy + federation mutation.
		"PutResourcePolicy",
		"DeleteResourcePolicy",
		"EnableFederation",
		"DisableFederation",
		// Import/insights data plane.
		"StartImport",
		"StopImport",
		"ListInsightsData",
		"ListInsightsMetricData",
	}
	for _, name := range forbidden {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("apiClient exposes forbidden method %q; CloudTrail SDK adapter must stay metadata-only", name)
		}
	}
}

func TestClientListTrailsReadsMetadataAndNeverFetchesEventPayloads(t *testing.T) {
	trailARN := "arn:aws:cloudtrail:us-east-1:123456789012:trail/audit-trail"
	bucket := "audit-trail-logs"
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:audit-trail:*"
	snsARN := "arn:aws:sns:us-east-1:123456789012:audit-trail-alerts"
	kmsKey := "arn:aws:kms:us-east-1:123456789012:key/audit-1234"

	api := &fakeCloudTrailAPI{
		trailsPages: []*awscloudtrail.ListTrailsOutput{{
			Trails: []cttypes.TrailInfo{{
				TrailARN:   aws.String(trailARN),
				Name:       aws.String("audit-trail"),
				HomeRegion: aws.String("us-east-1"),
			}},
		}},
		trail: map[string]*awscloudtrail.GetTrailOutput{
			trailARN: {
				Trail: &cttypes.Trail{
					Name:                       aws.String("audit-trail"),
					HomeRegion:                 aws.String("us-east-1"),
					S3BucketName:               aws.String(bucket),
					S3KeyPrefix:                aws.String("logs/"),
					SnsTopicARN:                aws.String(snsARN),
					CloudWatchLogsLogGroupArn:  aws.String(logGroupARN),
					CloudWatchLogsRoleArn:      aws.String("arn:aws:iam::123456789012:role/CT-CW"),
					KmsKeyId:                   aws.String(kmsKey),
					IncludeGlobalServiceEvents: aws.Bool(true),
					IsMultiRegionTrail:         aws.Bool(true),
					IsOrganizationTrail:        aws.Bool(true),
					LogFileValidationEnabled:   aws.Bool(true),
					HasCustomEventSelectors:    aws.Bool(true),
					HasInsightSelectors:        aws.Bool(true),
				},
			},
		},
		trailStatus: map[string]*awscloudtrail.GetTrailStatusOutput{
			trailARN: {
				IsLogging:               aws.Bool(true),
				LatestDeliveryError:     aws.String(""),
				LatestNotificationError: aws.String(""),
			},
		},
		eventSelectors: map[string]*awscloudtrail.GetEventSelectorsOutput{
			trailARN: {
				EventSelectors: []cttypes.EventSelector{{
					DataResources: []cttypes.DataResource{{
						Type: aws.String("AWS::S3::Object"),
					}, {
						Type: aws.String("AWS::Lambda::Function"),
					}},
				}},
				AdvancedEventSelectors: []cttypes.AdvancedEventSelector{{
					Name: aws.String("Log Lambda data events"),
					FieldSelectors: []cttypes.AdvancedFieldSelector{{
						Field:  aws.String("resources.type"),
						Equals: []string{"AWS::DynamoDB::Table"},
					}},
				}},
			},
		},
		insightSelectors: map[string]*awscloudtrail.GetInsightSelectorsOutput{
			trailARN: {
				InsightSelectors: []cttypes.InsightSelector{{
					InsightType: cttypes.InsightTypeApiCallRateInsight,
				}, {
					InsightType: cttypes.InsightTypeApiErrorRateInsight,
				}},
			},
		},
		tags: map[string]*awscloudtrail.ListTagsOutput{
			trailARN: {
				ResourceTagList: []cttypes.ResourceTag{{
					ResourceId: aws.String(trailARN),
					TagsList: []cttypes.Tag{{
						Key:   aws.String("Environment"),
						Value: aws.String("prod"),
					}},
				}},
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceCloudTrail},
	}

	trails, err := adapter.ListTrails(context.Background())
	if err != nil {
		t.Fatalf("ListTrails() error = %v, want nil", err)
	}
	if got, want := len(trails), 1; got != want {
		t.Fatalf("len(trails) = %d, want %d", got, want)
	}
	trail := trails[0]
	if trail.ARN != trailARN {
		t.Fatalf("trail.ARN = %q, want %q", trail.ARN, trailARN)
	}
	if trail.S3BucketName != bucket {
		t.Fatalf("trail.S3BucketName = %q, want %q", trail.S3BucketName, bucket)
	}
	if !trail.LoggingEnabled {
		t.Fatalf("trail.LoggingEnabled = false, want true")
	}
	if got, want := trail.EventSelectorSummary.EventSelectorCount, 1; got != want {
		t.Fatalf("EventSelectorCount = %d, want %d", got, want)
	}
	if got, want := trail.EventSelectorSummary.AdvancedEventSelectorCount, 1; got != want {
		t.Fatalf("AdvancedEventSelectorCount = %d, want %d", got, want)
	}
	if trail.EventSelectorSummary.ResourceTypeCounts["AWS::S3::Object"] != 1 {
		t.Fatalf("ResourceTypeCounts S3=1 missing: %#v", trail.EventSelectorSummary.ResourceTypeCounts)
	}
	if trail.EventSelectorSummary.ResourceTypeCounts["AWS::DynamoDB::Table"] != 1 {
		t.Fatalf("ResourceTypeCounts DynamoDB=1 missing: %#v", trail.EventSelectorSummary.ResourceTypeCounts)
	}
	if got, want := len(trail.InsightSelectors), 2; got != want {
		t.Fatalf("InsightSelectors length = %d, want %d", got, want)
	}
	if trail.Tags["Environment"] != "prod" {
		t.Fatalf("trail.Tags = %#v, want Environment=prod", trail.Tags)
	}

	for _, forbidden := range []string{
		"LookupEvents",
		"StartQuery",
		"GetQueryResults",
		"CancelQuery",
		"DescribeQuery",
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
		"StartEventDataStoreIngestion",
		"StopEventDataStoreIngestion",
		"StartDashboardRefresh",
		"AddTags",
		"RemoveTags",
	} {
		if slices.Contains(api.calls, forbidden) {
			t.Fatalf("forbidden CloudTrail call %s was made; calls=%v", forbidden, api.calls)
		}
	}
}

func TestClientListEventDataStoresReadsMetadataOnly(t *testing.T) {
	storeARN := "arn:aws:cloudtrail:us-east-1:123456789012:eventdatastore/aaaa-bbbb"
	kmsKey := "arn:aws:kms:us-east-1:123456789012:key/store-1111"

	api := &fakeCloudTrailAPI{
		eventDataStoresPages: []*awscloudtrail.ListEventDataStoresOutput{{
			EventDataStores: []cttypes.EventDataStore{{
				EventDataStoreArn: aws.String(storeARN),
			}},
		}},
		eventDataStoreDetails: map[string]*awscloudtrail.GetEventDataStoreOutput{
			storeARN: {
				Name:                         aws.String("security-lake"),
				Status:                       cttypes.EventDataStoreStatusEnabled,
				RetentionPeriod:              aws.Int32(2555),
				MultiRegionEnabled:           aws.Bool(true),
				OrganizationEnabled:          aws.Bool(true),
				TerminationProtectionEnabled: aws.Bool(true),
				BillingMode:                  cttypes.BillingModeExtendableRetentionPricing,
				KmsKeyId:                     aws.String(kmsKey),
				CreatedTimestamp:             aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
				UpdatedTimestamp:             aws.Time(time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)),
				AdvancedEventSelectors: []cttypes.AdvancedEventSelector{
					{Name: aws.String("selector-1")},
					{Name: aws.String("selector-2")},
				},
			},
		},
		tags: map[string]*awscloudtrail.ListTagsOutput{
			storeARN: {
				ResourceTagList: []cttypes.ResourceTag{{
					ResourceId: aws.String(storeARN),
					TagsList: []cttypes.Tag{{
						Key:   aws.String("Team"),
						Value: aws.String("security"),
					}},
				}},
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceCloudTrail},
	}

	stores, err := adapter.ListEventDataStores(context.Background())
	if err != nil {
		t.Fatalf("ListEventDataStores() error = %v, want nil", err)
	}
	if got, want := len(stores), 1; got != want {
		t.Fatalf("len(stores) = %d, want %d", got, want)
	}
	store := stores[0]
	if store.ARN != storeARN {
		t.Fatalf("store.ARN = %q, want %q", store.ARN, storeARN)
	}
	if store.RetentionPeriod != 2555 {
		t.Fatalf("store.RetentionPeriod = %d, want 2555", store.RetentionPeriod)
	}
	if store.BillingMode != string(cttypes.BillingModeExtendableRetentionPricing) {
		t.Fatalf("store.BillingMode = %q, want %q", store.BillingMode, cttypes.BillingModeExtendableRetentionPricing)
	}
	if store.AdvancedEventSelectorCount != 2 {
		t.Fatalf("store.AdvancedEventSelectorCount = %d, want 2", store.AdvancedEventSelectorCount)
	}
	if store.Tags["Team"] != "security" {
		t.Fatalf("store.Tags = %#v, want Team=security", store.Tags)
	}

	for _, forbidden := range []string{"StartQuery", "GetQueryResults", "CreateEventDataStore", "DeleteEventDataStore"} {
		if slices.Contains(api.calls, forbidden) {
			t.Fatalf("forbidden CloudTrail call %s was made; calls=%v", forbidden, api.calls)
		}
	}
}

func TestClientListChannelsAndDashboardsReadsMetadataOnly(t *testing.T) {
	channelARN := "arn:aws:cloudtrail:us-east-1:123456789012:channel/ch-1"
	storeARN := "arn:aws:cloudtrail:us-east-1:123456789012:eventdatastore/store-1"
	dashboardARN := "arn:aws:cloudtrail:us-east-1:123456789012:dashboard/db-1"

	api := &fakeCloudTrailAPI{
		channelsPages: []*awscloudtrail.ListChannelsOutput{{
			Channels: []cttypes.Channel{{
				ChannelArn: aws.String(channelARN),
			}},
		}},
		channelDetails: map[string]*awscloudtrail.GetChannelOutput{
			channelARN: {
				Name:   aws.String("external-events"),
				Source: aws.String("Custom"),
				Destinations: []cttypes.Destination{{
					Type:     cttypes.DestinationTypeEventDataStore,
					Location: aws.String(storeARN),
				}},
			},
		},
		dashboardsPages: []*awscloudtrail.ListDashboardsOutput{{
			Dashboards: []cttypes.DashboardDetail{{
				DashboardArn: aws.String(dashboardARN),
				Type:         cttypes.DashboardTypeCustom,
			}},
		}},
		dashboardDetails: map[string]*awscloudtrail.GetDashboardOutput{
			dashboardARN: {
				Status: cttypes.DashboardStatusCreated,
				Type:   cttypes.DashboardTypeCustom,
				Widgets: []cttypes.Widget{
					{
						QueryAlias:     aws.String("widget-1"),
						QueryStatement: aws.String("SELECT * FROM events"),
					},
					{
						QueryAlias:     aws.String("widget-2"),
						QueryStatement: aws.String("SELECT count(*) FROM events"),
					},
				},
				RefreshSchedule: &cttypes.RefreshSchedule{
					Frequency: &cttypes.RefreshScheduleFrequency{
						Unit: cttypes.RefreshScheduleFrequencyUnitDays,
					},
				},
			},
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceCloudTrail},
	}

	channels, err := adapter.ListChannels(context.Background())
	if err != nil {
		t.Fatalf("ListChannels() error = %v, want nil", err)
	}
	if got, want := len(channels), 1; got != want {
		t.Fatalf("len(channels) = %d, want %d", got, want)
	}
	channel := channels[0]
	if channel.DestinationARN != storeARN {
		t.Fatalf("channel.DestinationARN = %q, want %q", channel.DestinationARN, storeARN)
	}
	if channel.DestinationType != string(cttypes.DestinationTypeEventDataStore) {
		t.Fatalf("channel.DestinationType = %q, want EVENT_DATA_STORE", channel.DestinationType)
	}

	dashboards, err := adapter.ListDashboards(context.Background())
	if err != nil {
		t.Fatalf("ListDashboards() error = %v, want nil", err)
	}
	if got, want := len(dashboards), 1; got != want {
		t.Fatalf("len(dashboards) = %d, want %d", got, want)
	}
	dashboard := dashboards[0]
	if dashboard.WidgetCount != 2 {
		t.Fatalf("dashboard.WidgetCount = %d, want 2", dashboard.WidgetCount)
	}
	if dashboard.Status != string(cttypes.DashboardStatusCreated) {
		t.Fatalf("dashboard.Status = %q, want CREATED", dashboard.Status)
	}
	if dashboard.RefreshSchedule != string(cttypes.RefreshScheduleFrequencyUnitDays) {
		t.Fatalf("dashboard.RefreshSchedule = %q, want DAYS", dashboard.RefreshSchedule)
	}

	for _, forbidden := range []string{
		"CreateChannel", "UpdateChannel", "DeleteChannel",
		"CreateDashboard", "UpdateDashboard", "DeleteDashboard",
		"StartDashboardRefresh",
	} {
		if slices.Contains(api.calls, forbidden) {
			t.Fatalf("forbidden CloudTrail call %s was made; calls=%v", forbidden, api.calls)
		}
	}
}

// fakeCloudTrailAPI lives in fake_client_test.go to keep this file under the
// repo-wide 500-line cap.
