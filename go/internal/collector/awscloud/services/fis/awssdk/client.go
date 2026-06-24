// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfis "github.com/aws/aws-sdk-go-v2/service/fis"
	awsfistypes "github.com/aws/aws-sdk-go-v2/service/fis/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	fisservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/fis"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS FIS API the adapter calls.
// It is deliberately limited to the experiment-template list/get reads and the
// resource-tag read. It exposes no StartExperiment, no StopExperiment, no
// experiment run reads (GetExperiment, ListExperiments,
// ListExperimentResolvedTargets), and no Create/Update/Delete mutation, so the
// adapter cannot launch a fault-injection experiment, read experiment run
// results, or mutate FIS state. The exclusion_test reflects over this interface
// to enforce that contract at build time.
type apiClient interface {
	ListExperimentTemplates(
		context.Context,
		*awsfis.ListExperimentTemplatesInput,
		...func(*awsfis.Options),
	) (*awsfis.ListExperimentTemplatesOutput, error)
	GetExperimentTemplate(
		context.Context,
		*awsfis.GetExperimentTemplateInput,
		...func(*awsfis.Options),
	) (*awsfis.GetExperimentTemplateOutput, error)
	ListTagsForResource(
		context.Context,
		*awsfis.ListTagsForResourceInput,
		...func(*awsfis.Options),
	) (*awsfis.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK FIS control-plane calls into scanner-owned metadata. It
// never starts or stops an experiment, never reads experiment run results, and
// never calls a Create/Update/Delete mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an FIS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsfis.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns FIS experiment-template metadata visible to the configured
// AWS credentials. Each template summary is expanded through
// GetExperimentTemplate to read its action, target, logging, and stop-condition
// metadata. Experiment run results and resolved-target inventories are never
// read.
func (c *Client) Snapshot(ctx context.Context) (fisservice.Snapshot, error) {
	summaries, err := c.listTemplates(ctx)
	if err != nil {
		return fisservice.Snapshot{}, err
	}
	templates := make([]fisservice.ExperimentTemplate, 0, len(summaries))
	for _, summary := range summaries {
		id := strings.TrimSpace(aws.ToString(summary.Id))
		if id == "" {
			continue
		}
		template, err := c.getTemplate(ctx, id)
		if err != nil {
			return fisservice.Snapshot{}, err
		}
		if template != nil {
			templates = append(templates, *template)
		}
	}
	return fisservice.Snapshot{Templates: templates}, nil
}

func (c *Client) listTemplates(ctx context.Context) ([]awsfistypes.ExperimentTemplateSummary, error) {
	var summaries []awsfistypes.ExperimentTemplateSummary
	var nextToken *string
	for {
		var page *awsfis.ListExperimentTemplatesOutput
		err := c.recordAPICall(ctx, "ListExperimentTemplates", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListExperimentTemplates(callCtx, &awsfis.ListExperimentTemplatesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return summaries, nil
		}
		summaries = append(summaries, page.ExperimentTemplates...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return summaries, nil
		}
	}
}

func (c *Client) getTemplate(ctx context.Context, id string) (*fisservice.ExperimentTemplate, error) {
	var output *awsfis.GetExperimentTemplateOutput
	err := c.recordAPICall(ctx, "GetExperimentTemplate", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetExperimentTemplate(callCtx, &awsfis.GetExperimentTemplateInput{
			Id: aws.String(id),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.ExperimentTemplate == nil {
		return nil, nil
	}
	return c.mapTemplate(ctx, output.ExperimentTemplate)
}

func (c *Client) mapTemplate(
	ctx context.Context,
	template *awsfistypes.ExperimentTemplate,
) (*fisservice.ExperimentTemplate, error) {
	arn := strings.TrimSpace(aws.ToString(template.Arn))
	tags := cloneTags(template.Tags)
	if len(tags) == 0 {
		fetched, err := c.listTags(ctx, arn)
		if err != nil {
			return nil, err
		}
		tags = fetched
	}
	logGroupARN, s3Bucket, s3Prefix := logDestinations(template.LogConfiguration)
	mapped := &fisservice.ExperimentTemplate{
		ID:                     strings.TrimSpace(aws.ToString(template.Id)),
		ARN:                    arn,
		Name:                   strings.TrimSpace(tags["Name"]),
		Description:            strings.TrimSpace(aws.ToString(template.Description)),
		RoleARN:                strings.TrimSpace(aws.ToString(template.RoleArn)),
		Actions:                mapActions(template.Actions),
		Targets:                mapTargets(template.Targets),
		LogGroupARN:            logGroupARN,
		LogS3Bucket:            s3Bucket,
		LogS3Prefix:            s3Prefix,
		StopConditionAlarmARNs: stopConditionAlarmARNs(template.StopConditions),
		CreationTime:           aws.ToTime(template.CreationTime),
		LastUpdateTime:         aws.ToTime(template.LastUpdateTime),
		Tags:                   tags,
	}
	return mapped, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsfis.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListTagsForResource(callCtx, &awsfis.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return callErr
	})
	if err != nil || output == nil {
		return nil, err
	}
	return cloneTags(output.Tags), nil
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

var _ fisservice.Client = (*Client)(nil)

var _ apiClient = (*awsfis.Client)(nil)
