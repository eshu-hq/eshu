// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecuritylake "github.com/aws/aws-sdk-go-v2/service/securitylake"
	awssecuritylaketypes "github.com/aws/aws-sdk-go-v2/service/securitylake/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	securitylakeservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/securitylake"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Security Lake API the adapter
// calls. It is deliberately limited to the data lake, log source, and subscriber
// list reads. It exposes no Create/Update/Delete mutation, no GetSubscriber
// secret read, no subscriber-notification read, and no exception read, so the
// adapter cannot mutate Security Lake state or read subscriber credentials. The
// exclusion_test reflects over this interface to enforce that contract at build
// time.
type apiClient interface {
	ListDataLakes(
		context.Context,
		*awssecuritylake.ListDataLakesInput,
		...func(*awssecuritylake.Options),
	) (*awssecuritylake.ListDataLakesOutput, error)
	ListLogSources(
		context.Context,
		*awssecuritylake.ListLogSourcesInput,
		...func(*awssecuritylake.Options),
	) (*awssecuritylake.ListLogSourcesOutput, error)
	ListSubscribers(
		context.Context,
		*awssecuritylake.ListSubscribersInput,
		...func(*awssecuritylake.Options),
	) (*awssecuritylake.ListSubscribersOutput, error)
}

// Client adapts AWS SDK Security Lake control-plane calls into scanner-owned
// metadata. It never reads ingested security log records, object contents,
// subscriber credentials (external id, endpoint), and never calls a mutation
// API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Security Lake SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssecuritylake.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Security Lake data lake, log source, and subscriber metadata
// visible to the configured AWS credentials in the boundary Region. Ingested log
// records, object contents, and subscriber credentials are never read.
func (c *Client) Snapshot(ctx context.Context) (securitylakeservice.Snapshot, error) {
	dataLakes, err := c.listDataLakes(ctx)
	if err != nil {
		return securitylakeservice.Snapshot{}, err
	}
	logSources, err := c.listLogSources(ctx)
	if err != nil {
		return securitylakeservice.Snapshot{}, err
	}
	subscribers, err := c.listSubscribers(ctx)
	if err != nil {
		return securitylakeservice.Snapshot{}, err
	}
	return securitylakeservice.Snapshot{
		DataLakes:   dataLakes,
		LogSources:  logSources,
		Subscribers: subscribers,
	}, nil
}

func (c *Client) listDataLakes(ctx context.Context) ([]securitylakeservice.DataLake, error) {
	var output *awssecuritylake.ListDataLakesOutput
	err := c.recordAPICall(ctx, "ListDataLakes", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListDataLakes(callCtx, &awssecuritylake.ListDataLakesInput{
			Regions: []string{c.boundary.Region},
		})
		return callErr
	})
	if err != nil || output == nil {
		return nil, err
	}
	lakes := make([]securitylakeservice.DataLake, 0, len(output.DataLakes))
	for _, lake := range output.DataLakes {
		lakes = append(lakes, mapDataLake(lake))
	}
	return lakes, nil
}

func mapDataLake(lake awssecuritylaketypes.DataLakeResource) securitylakeservice.DataLake {
	mapped := securitylakeservice.DataLake{
		ARN:          strings.TrimSpace(aws.ToString(lake.DataLakeArn)),
		Region:       strings.TrimSpace(aws.ToString(lake.Region)),
		S3BucketARN:  strings.TrimSpace(aws.ToString(lake.S3BucketArn)),
		CreateStatus: strings.TrimSpace(string(lake.CreateStatus)),
	}
	if lake.EncryptionConfiguration != nil {
		mapped.KMSKeyID = strings.TrimSpace(aws.ToString(lake.EncryptionConfiguration.KmsKeyId))
	}
	if lake.UpdateStatus != nil {
		mapped.UpdateStatus = strings.TrimSpace(string(lake.UpdateStatus.Status))
	}
	if lifecycle := lake.LifecycleConfiguration; lifecycle != nil {
		if lifecycle.Expiration != nil {
			mapped.ExpirationDays = aws.ToInt32(lifecycle.Expiration.Days)
		}
		mapped.TransitionCount = len(lifecycle.Transitions)
	}
	if replication := lake.ReplicationConfiguration; replication != nil {
		mapped.ReplicationRegions = trimmedSlice(replication.Regions)
	}
	return mapped
}

func (c *Client) listLogSources(ctx context.Context) ([]securitylakeservice.LogSource, error) {
	var sources []securitylakeservice.LogSource
	var nextToken *string
	for {
		var page *awssecuritylake.ListLogSourcesOutput
		err := c.recordAPICall(ctx, "ListLogSources", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListLogSources(callCtx, &awssecuritylake.ListLogSourcesInput{
				Regions:   []string{c.boundary.Region},
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return sources, nil
		}
		for _, logSource := range page.Sources {
			sources = append(sources, mapLogSources(logSource)...)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return sources, nil
		}
	}
}

// mapLogSources expands one AWS LogSource (an account/region scope carrying a
// set of source resources) into one scanner LogSource per source resource.
func mapLogSources(logSource awssecuritylaketypes.LogSource) []securitylakeservice.LogSource {
	account := strings.TrimSpace(aws.ToString(logSource.Account))
	region := strings.TrimSpace(aws.ToString(logSource.Region))
	out := make([]securitylakeservice.LogSource, 0, len(logSource.Sources))
	for _, resource := range logSource.Sources {
		if mapped, ok := mapLogSourceResource(account, region, resource); ok {
			out = append(out, mapped)
		}
	}
	return out
}

func mapLogSourceResource(
	account, region string,
	resource awssecuritylaketypes.LogSourceResource,
) (securitylakeservice.LogSource, bool) {
	switch typed := resource.(type) {
	case *awssecuritylaketypes.LogSourceResourceMemberAwsLogSource:
		return securitylakeservice.LogSource{
			Account:       account,
			Region:        region,
			SourceName:    strings.TrimSpace(string(typed.Value.SourceName)),
			SourceVersion: strings.TrimSpace(aws.ToString(typed.Value.SourceVersion)),
		}, true
	case *awssecuritylaketypes.LogSourceResourceMemberCustomLogSource:
		mapped := securitylakeservice.LogSource{
			Account:       account,
			Region:        region,
			SourceName:    strings.TrimSpace(aws.ToString(typed.Value.SourceName)),
			SourceVersion: strings.TrimSpace(aws.ToString(typed.Value.SourceVersion)),
			Custom:        true,
		}
		if provider := typed.Value.Provider; provider != nil {
			mapped.ProviderRoleARN = strings.TrimSpace(aws.ToString(provider.RoleArn))
		}
		return mapped, true
	default:
		return securitylakeservice.LogSource{}, false
	}
}

func (c *Client) listSubscribers(ctx context.Context) ([]securitylakeservice.Subscriber, error) {
	var subscribers []securitylakeservice.Subscriber
	var nextToken *string
	for {
		var page *awssecuritylake.ListSubscribersOutput
		err := c.recordAPICall(ctx, "ListSubscribers", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListSubscribers(callCtx, &awssecuritylake.ListSubscribersInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return subscribers, nil
		}
		for _, subscriber := range page.Subscribers {
			subscribers = append(subscribers, mapSubscriber(subscriber))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return subscribers, nil
		}
	}
}

// mapSubscriber maps a Security Lake subscriber into scanner metadata. It keeps
// the principal account identity but NEVER the external id (trust-establishment
// credential) or the subscriber endpoint (a private notification destination).
func mapSubscriber(subscriber awssecuritylaketypes.SubscriberResource) securitylakeservice.Subscriber {
	mapped := securitylakeservice.Subscriber{
		ARN:         strings.TrimSpace(aws.ToString(subscriber.SubscriberArn)),
		ID:          strings.TrimSpace(aws.ToString(subscriber.SubscriberId)),
		Name:        strings.TrimSpace(aws.ToString(subscriber.SubscriberName)),
		Status:      strings.TrimSpace(string(subscriber.SubscriberStatus)),
		AccessTypes: accessTypeStrings(subscriber.AccessTypes),
		RoleARN:     strings.TrimSpace(aws.ToString(subscriber.RoleArn)),
		S3BucketARN: strings.TrimSpace(aws.ToString(subscriber.S3BucketArn)),
		SourceNames: subscriberSourceNames(subscriber.Sources),
		CreatedAt:   aws.ToTime(subscriber.CreatedAt),
		UpdatedAt:   aws.ToTime(subscriber.UpdatedAt),
	}
	if subscriber.SubscriberIdentity != nil {
		mapped.PrincipalAccount = strings.TrimSpace(aws.ToString(subscriber.SubscriberIdentity.Principal))
	}
	return mapped
}

func subscriberSourceNames(sources []awssecuritylaketypes.LogSourceResource) []string {
	names := make([]string, 0, len(sources))
	for _, resource := range sources {
		if mapped, ok := mapLogSourceResource("", "", resource); ok && mapped.SourceName != "" {
			names = append(names, mapped.SourceName)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func accessTypeStrings(accessTypes []awssecuritylaketypes.AccessType) []string {
	if len(accessTypes) == 0 {
		return nil
	}
	out := make([]string, 0, len(accessTypes))
	for _, accessType := range accessTypes {
		if value := strings.TrimSpace(string(accessType)); value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func trimmedSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

var _ securitylakeservice.Client = (*Client)(nil)

var _ apiClient = (*awssecuritylake.Client)(nil)
