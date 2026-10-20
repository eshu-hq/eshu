// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awspinpoint "github.com/aws/aws-sdk-go-v2/service/pinpoint"
	awspinpointtypes "github.com/aws/aws-sdk-go-v2/service/pinpoint/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

type stubAPI struct {
	appsPages    []*awspinpoint.GetAppsOutput
	segmentPages map[string][]*awspinpoint.GetSegmentsOutput
	channels     map[string]*awspinpoint.GetChannelsOutput
	emailChannel map[string]*awspinpoint.GetEmailChannelOutput

	appsCalls    int
	segmentCalls int
}

func (s *stubAPI) GetApps(_ context.Context, _ *awspinpoint.GetAppsInput, _ ...func(*awspinpoint.Options)) (*awspinpoint.GetAppsOutput, error) {
	page := s.appsPages[s.appsCalls]
	s.appsCalls++
	return page, nil
}

func (s *stubAPI) GetSegments(_ context.Context, in *awspinpoint.GetSegmentsInput, _ ...func(*awspinpoint.Options)) (*awspinpoint.GetSegmentsOutput, error) {
	pages := s.segmentPages[aws.ToString(in.ApplicationId)]
	idx := s.segmentCalls
	s.segmentCalls++
	if idx >= len(pages) {
		return &awspinpoint.GetSegmentsOutput{SegmentsResponse: &awspinpointtypes.SegmentsResponse{}}, nil
	}
	return pages[idx], nil
}

func (s *stubAPI) GetChannels(_ context.Context, in *awspinpoint.GetChannelsInput, _ ...func(*awspinpoint.Options)) (*awspinpoint.GetChannelsOutput, error) {
	return s.channels[aws.ToString(in.ApplicationId)], nil
}

func (s *stubAPI) GetEmailChannel(_ context.Context, in *awspinpoint.GetEmailChannelInput, _ ...func(*awspinpoint.Options)) (*awspinpoint.GetEmailChannelOutput, error) {
	return s.emailChannel[aws.ToString(in.ApplicationId)], nil
}

func TestSnapshotMapsApplicationsSegmentsChannels(t *testing.T) {
	const appID = "app-1"
	stub := &stubAPI{
		appsPages: []*awspinpoint.GetAppsOutput{{
			ApplicationsResponse: &awspinpointtypes.ApplicationsResponse{
				Item: []awspinpointtypes.ApplicationResponse{{
					Id:           aws.String(appID),
					Arn:          aws.String("arn:aws:mobiletargeting:us-east-1:123456789012:apps/app-1"),
					Name:         aws.String("marketing"),
					CreationDate: aws.String("2026-05-14T12:00:00.000Z"),
					Tags:         map[string]string{"Env": "prod"},
				}},
			},
		}},
		segmentPages: map[string][]*awspinpoint.GetSegmentsOutput{
			appID: {{
				SegmentsResponse: &awspinpointtypes.SegmentsResponse{
					Item: []awspinpointtypes.SegmentResponse{{
						Id:            aws.String("seg-1"),
						Arn:           aws.String("arn:aws:mobiletargeting:us-east-1:123456789012:apps/app-1/segments/seg-1"),
						Name:          aws.String("active"),
						ApplicationId: aws.String(appID),
						SegmentType:   awspinpointtypes.SegmentTypeImport,
						Version:       aws.Int32(2),
						ImportDefinition: &awspinpointtypes.SegmentImportResource{
							Format:     awspinpointtypes.FormatCsv,
							Size:       aws.Int32(99),
							S3Url:      aws.String("s3://secret-bucket/endpoints.csv"),
							ExternalId: aws.String("super-secret-external-id"),
							RoleArn:    aws.String("arn:aws:iam::123456789012:role/import"),
						},
					}},
				},
			}},
		},
		channels: map[string]*awspinpoint.GetChannelsOutput{
			appID: {ChannelsResponse: &awspinpointtypes.ChannelsResponse{
				Channels: map[string]awspinpointtypes.ChannelResponse{
					"EMAIL": {Enabled: aws.Bool(true), Version: aws.Int32(3)},
					"SMS":   {Enabled: aws.Bool(false), Version: aws.Int32(1)},
				},
			}},
		},
		emailChannel: map[string]*awspinpoint.GetEmailChannelOutput{
			appID: {EmailChannelResponse: &awspinpointtypes.EmailChannelResponse{
				ConfigurationSet: aws.String("marketing-config-set"),
				Identity:         aws.String("arn:aws:ses:us-east-1:123456789012:identity/example.com"),
				FromAddress:      aws.String("noreply@example.com"),
			}},
		},
	}

	client := &Client{client: stub, boundary: awscloud.Boundary{ServiceKind: awscloud.ServicePinpoint}}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Applications) != 1 {
		t.Fatalf("got %d applications, want 1", len(snapshot.Applications))
	}
	app := snapshot.Applications[0]
	if app.Name != "marketing" {
		t.Fatalf("application name = %q, want marketing", app.Name)
	}
	if len(app.Segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(app.Segments))
	}
	seg := app.Segments[0]
	if !seg.ImportedFromS3 || seg.ImportFormat != "CSV" || seg.ImportSize != 99 {
		t.Fatalf("segment import metadata = %+v, want imported CSV size 99", seg)
	}

	// EMAIL channel must carry the SES references; SMS must not.
	var email, sms *struct {
		configSet string
		identity  string
	}
	for i := range app.Channels {
		ch := app.Channels[i]
		switch ch.ChannelType {
		case "EMAIL":
			email = &struct {
				configSet string
				identity  string
			}{ch.SESConfigurationSet, ch.SESIdentityARN}
		case "SMS":
			sms = &struct {
				configSet string
				identity  string
			}{ch.SESConfigurationSet, ch.SESIdentityARN}
		}
	}
	if email == nil || email.configSet != "marketing-config-set" || email.identity != "arn:aws:ses:us-east-1:123456789012:identity/example.com" {
		t.Fatalf("email channel SES refs = %+v, want config set + identity ARN", email)
	}
	if sms == nil || sms.configSet != "" || sms.identity != "" {
		t.Fatalf("SMS channel must carry no SES refs, got %+v", sms)
	}
}

// TestSnapshotNeverCopiesEmailFromAddressOrImportSecrets is the focused PII
// gate: even though GetEmailChannel returns a from-address and the import
// definition returns an S3 URL, external id, and role ARN, none of them may
// appear on the scanner-owned model.
func TestSnapshotNeverCopiesEmailFromAddressOrImportSecrets(t *testing.T) {
	const appID = "app-1"
	stub := &stubAPI{
		appsPages: []*awspinpoint.GetAppsOutput{{
			ApplicationsResponse: &awspinpointtypes.ApplicationsResponse{
				Item: []awspinpointtypes.ApplicationResponse{{Id: aws.String(appID), Name: aws.String("m")}},
			},
		}},
		segmentPages: map[string][]*awspinpoint.GetSegmentsOutput{
			appID: {{SegmentsResponse: &awspinpointtypes.SegmentsResponse{
				Item: []awspinpointtypes.SegmentResponse{{
					Id:            aws.String("seg-1"),
					ApplicationId: aws.String(appID),
					SegmentType:   awspinpointtypes.SegmentTypeImport,
					ImportDefinition: &awspinpointtypes.SegmentImportResource{
						S3Url:      aws.String("s3://secret-bucket/endpoints.csv"),
						ExternalId: aws.String("super-secret"),
						RoleArn:    aws.String("arn:aws:iam::123456789012:role/import"),
					},
				}},
			}}},
		},
		channels: map[string]*awspinpoint.GetChannelsOutput{
			appID: {ChannelsResponse: &awspinpointtypes.ChannelsResponse{
				Channels: map[string]awspinpointtypes.ChannelResponse{"EMAIL": {Enabled: aws.Bool(true)}},
			}},
		},
		emailChannel: map[string]*awspinpoint.GetEmailChannelOutput{
			appID: {EmailChannelResponse: &awspinpointtypes.EmailChannelResponse{
				FromAddress: aws.String("noreply@example.com"),
			}},
		},
	}
	client := &Client{client: stub, boundary: awscloud.Boundary{ServiceKind: awscloud.ServicePinpoint}}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	app := snapshot.Applications[0]
	seg := app.Segments[0]
	// Only presence/format/size are kept; no URL / external id / role.
	if !seg.ImportedFromS3 {
		t.Fatalf("imported_from_s3 should be true")
	}
	for _, ch := range app.Channels {
		// The email channel has no SES config set / identity in this fixture, and
		// the from-address must never leak onto any field.
		if ch.SESIdentityARN != "" || ch.SESConfigurationSet != "" {
			t.Fatalf("email channel must carry no SES refs in this fixture, got %+v", ch)
		}
	}
}

func TestListApplicationsPaginates(t *testing.T) {
	stub := &stubAPI{
		appsPages: []*awspinpoint.GetAppsOutput{
			{ApplicationsResponse: &awspinpointtypes.ApplicationsResponse{
				Item:      []awspinpointtypes.ApplicationResponse{{Id: aws.String("app-1"), Name: aws.String("one")}},
				NextToken: aws.String("page2"),
			}},
			{ApplicationsResponse: &awspinpointtypes.ApplicationsResponse{
				Item: []awspinpointtypes.ApplicationResponse{{Id: aws.String("app-2"), Name: aws.String("two")}},
			}},
		},
		channels: map[string]*awspinpoint.GetChannelsOutput{},
	}
	client := &Client{client: stub, boundary: awscloud.Boundary{ServiceKind: awscloud.ServicePinpoint}}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Applications) != 2 {
		t.Fatalf("got %d applications across pages, want 2", len(snapshot.Applications))
	}
	if stub.appsCalls != 2 {
		t.Fatalf("GetApps called %d times, want 2 (paginated)", stub.appsCalls)
	}
}
