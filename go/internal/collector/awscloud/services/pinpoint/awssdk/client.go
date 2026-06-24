// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awspinpoint "github.com/aws/aws-sdk-go-v2/service/pinpoint"
	awspinpointtypes "github.com/aws/aws-sdk-go-v2/service/pinpoint/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	pinpointservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/pinpoint"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Pinpoint API the adapter
// calls. It is deliberately limited to the application/segment list reads and
// channel-settings get reads. It exposes no SendMessages, no
// GetEndpoint/GetUserEndpoints, no message/template/export reads, and no
// Create/Update/Delete mutation, so the adapter cannot read endpoint records,
// addresses, or message content, and cannot mutate Pinpoint state. The
// exclusion_test reflects over this interface to enforce that contract at build
// time.
type apiClient interface {
	GetApps(
		context.Context,
		*awspinpoint.GetAppsInput,
		...func(*awspinpoint.Options),
	) (*awspinpoint.GetAppsOutput, error)
	GetSegments(
		context.Context,
		*awspinpoint.GetSegmentsInput,
		...func(*awspinpoint.Options),
	) (*awspinpoint.GetSegmentsOutput, error)
	GetChannels(
		context.Context,
		*awspinpoint.GetChannelsInput,
		...func(*awspinpoint.Options),
	) (*awspinpoint.GetChannelsOutput, error)
	GetEmailChannel(
		context.Context,
		*awspinpoint.GetEmailChannelInput,
		...func(*awspinpoint.Options),
	) (*awspinpoint.GetEmailChannelOutput, error)
}

// Client adapts AWS SDK Pinpoint control-plane calls into scanner-owned
// metadata. It never reads endpoint records, addresses, or message content, and
// never calls a Send or mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Pinpoint SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awspinpoint.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Pinpoint application metadata and the segments and channel
// settings under each application visible to the configured AWS credentials.
// Endpoint records, addresses, and message content are never read.
func (c *Client) Snapshot(ctx context.Context) (pinpointservice.Snapshot, error) {
	applications, err := c.listApplications(ctx)
	if err != nil {
		return pinpointservice.Snapshot{}, err
	}
	for i := range applications {
		segments, err := c.listSegments(ctx, applications[i].ID)
		if err != nil {
			return pinpointservice.Snapshot{}, err
		}
		applications[i].Segments = segments
		channels, err := c.listChannels(ctx, applications[i].ID)
		if err != nil {
			return pinpointservice.Snapshot{}, err
		}
		applications[i].Channels = channels
	}
	return pinpointservice.Snapshot{Applications: applications}, nil
}

func (c *Client) listApplications(ctx context.Context) ([]pinpointservice.Application, error) {
	var applications []pinpointservice.Application
	var token *string
	for {
		var page *awspinpoint.GetAppsOutput
		err := c.recordAPICall(ctx, "GetApps", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetApps(callCtx, &awspinpoint.GetAppsInput{
				Token: token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil || page.ApplicationsResponse == nil {
			return applications, nil
		}
		for _, app := range page.ApplicationsResponse.Item {
			applications = append(applications, mapApplication(app))
		}
		token = page.ApplicationsResponse.NextToken
		if aws.ToString(token) == "" {
			return applications, nil
		}
	}
}

func (c *Client) listSegments(ctx context.Context, applicationID string) ([]pinpointservice.Segment, error) {
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		return nil, nil
	}
	var segments []pinpointservice.Segment
	var token *string
	for {
		var page *awspinpoint.GetSegmentsOutput
		err := c.recordAPICall(ctx, "GetSegments", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetSegments(callCtx, &awspinpoint.GetSegmentsInput{
				ApplicationId: aws.String(applicationID),
				Token:         token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil || page.SegmentsResponse == nil {
			return segments, nil
		}
		for _, segment := range page.SegmentsResponse.Item {
			segments = append(segments, mapSegment(segment, applicationID))
		}
		token = page.SegmentsResponse.NextToken
		if aws.ToString(token) == "" {
			return segments, nil
		}
	}
}

func (c *Client) listChannels(ctx context.Context, applicationID string) ([]pinpointservice.Channel, error) {
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		return nil, nil
	}
	var output *awspinpoint.GetChannelsOutput
	err := c.recordAPICall(ctx, "GetChannels", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetChannels(callCtx, &awspinpoint.GetChannelsInput{
			ApplicationId: aws.String(applicationID),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.ChannelsResponse == nil {
		return nil, nil
	}
	channels := mapChannels(applicationID, output.ChannelsResponse.Channels)
	if hasEmailChannel(output.ChannelsResponse.Channels) {
		if err := c.enrichEmailChannel(ctx, applicationID, channels); err != nil {
			return nil, err
		}
	}
	return channels, nil
}

// enrichEmailChannel fetches the email channel's SES configuration set and
// identity reference through GetEmailChannel and copies only those references
// onto the matching channel entry. The verified from-address (an email address)
// is intentionally never read off the response.
func (c *Client) enrichEmailChannel(
	ctx context.Context,
	applicationID string,
	channels []pinpointservice.Channel,
) error {
	var output *awspinpoint.GetEmailChannelOutput
	err := c.recordAPICall(ctx, "GetEmailChannel", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetEmailChannel(callCtx, &awspinpoint.GetEmailChannelInput{
			ApplicationId: aws.String(applicationID),
		})
		return callErr
	})
	if err != nil {
		return err
	}
	if output == nil || output.EmailChannelResponse == nil {
		return nil
	}
	configSet := strings.TrimSpace(aws.ToString(output.EmailChannelResponse.ConfigurationSet))
	identity := strings.TrimSpace(aws.ToString(output.EmailChannelResponse.Identity))
	for i := range channels {
		if !strings.EqualFold(channels[i].ChannelType, channelTypeEmail) {
			continue
		}
		channels[i].SESConfigurationSet = configSet
		channels[i].SESIdentityARN = identity
	}
	return nil
}

func mapApplication(app awspinpointtypes.ApplicationResponse) pinpointservice.Application {
	return pinpointservice.Application{
		ID:           strings.TrimSpace(aws.ToString(app.Id)),
		ARN:          strings.TrimSpace(aws.ToString(app.Arn)),
		Name:         strings.TrimSpace(aws.ToString(app.Name)),
		CreationTime: parseISO8601(aws.ToString(app.CreationDate)),
		Tags:         cloneTags(app.Tags),
	}
}

func mapSegment(segment awspinpointtypes.SegmentResponse, applicationID string) pinpointservice.Segment {
	mapped := pinpointservice.Segment{
		ID:               strings.TrimSpace(aws.ToString(segment.Id)),
		ARN:              strings.TrimSpace(aws.ToString(segment.Arn)),
		Name:             strings.TrimSpace(aws.ToString(segment.Name)),
		ApplicationID:    firstNonEmpty(strings.TrimSpace(aws.ToString(segment.ApplicationId)), applicationID),
		SegmentType:      strings.TrimSpace(string(segment.SegmentType)),
		Version:          aws.ToInt32(segment.Version),
		CreationTime:     parseISO8601(aws.ToString(segment.CreationDate)),
		LastModifiedTime: parseISO8601(aws.ToString(segment.LastModifiedDate)),
		Tags:             cloneTags(segment.Tags),
	}
	// Record only the presence, format, and aggregate size of an S3 import.
	// The import S3 URL, external id, and role ARN are endpoint-adjacent and are
	// never copied onto the scanner model.
	if imported := segment.ImportDefinition; imported != nil {
		mapped.ImportedFromS3 = true
		mapped.ImportFormat = strings.TrimSpace(string(imported.Format))
		mapped.ImportSize = aws.ToInt32(imported.Size)
	}
	return mapped
}

func mapChannels(applicationID string, channels map[string]awspinpointtypes.ChannelResponse) []pinpointservice.Channel {
	if len(channels) == 0 {
		return nil
	}
	kinds := make([]string, 0, len(channels))
	for kind := range channels {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	mapped := make([]pinpointservice.Channel, 0, len(kinds))
	for _, kind := range kinds {
		channel := channels[kind]
		mapped = append(mapped, pinpointservice.Channel{
			ApplicationID:    applicationID,
			ChannelType:      strings.TrimSpace(kind),
			Enabled:          aws.ToBool(channel.Enabled),
			Archived:         aws.ToBool(channel.IsArchived),
			Version:          aws.ToInt32(channel.Version),
			CreationTime:     parseISO8601(aws.ToString(channel.CreationDate)),
			LastModifiedTime: parseISO8601(aws.ToString(channel.LastModifiedDate)),
		})
	}
	return mapped
}

func hasEmailChannel(channels map[string]awspinpointtypes.ChannelResponse) bool {
	for kind := range channels {
		if strings.EqualFold(strings.TrimSpace(kind), channelTypeEmail) {
			return true
		}
	}
	return false
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

var _ pinpointservice.Client = (*Client)(nil)

var _ apiClient = (*awspinpoint.Client)(nil)
