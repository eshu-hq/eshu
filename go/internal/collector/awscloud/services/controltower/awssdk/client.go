// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscontroltower "github.com/aws/aws-sdk-go-v2/service/controltower"
	awscontroltowertypes "github.com/aws/aws-sdk-go-v2/service/controltower/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	controltowerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/controltower"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Control Tower API the adapter
// calls. It is deliberately limited to landing-zone, enabled-control, and
// enabled-baseline list/get reads plus resource-tag reads. It exposes no
// CreateLandingZone, no EnableControl/DisableControl, no EnableBaseline/
// ResetEnabledBaseline, and no Update/Delete mutation, so the adapter can never
// change Control Tower state. The exclusion_test reflects over this interface to
// enforce that contract at build time.
type apiClient interface {
	ListLandingZones(
		context.Context,
		*awscontroltower.ListLandingZonesInput,
		...func(*awscontroltower.Options),
	) (*awscontroltower.ListLandingZonesOutput, error)
	GetLandingZone(
		context.Context,
		*awscontroltower.GetLandingZoneInput,
		...func(*awscontroltower.Options),
	) (*awscontroltower.GetLandingZoneOutput, error)
	ListEnabledControls(
		context.Context,
		*awscontroltower.ListEnabledControlsInput,
		...func(*awscontroltower.Options),
	) (*awscontroltower.ListEnabledControlsOutput, error)
	ListEnabledBaselines(
		context.Context,
		*awscontroltower.ListEnabledBaselinesInput,
		...func(*awscontroltower.Options),
	) (*awscontroltower.ListEnabledBaselinesOutput, error)
	ListTagsForResource(
		context.Context,
		*awscontroltower.ListTagsForResourceInput,
		...func(*awscontroltower.Options),
	) (*awscontroltower.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Control Tower control-plane calls into scanner-owned
// metadata. It never reads or persists the landing-zone manifest body, control
// or baseline parameter values, or any governance payload, and never calls an
// enable, disable, reset, create, update, or delete API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Control Tower SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awscontroltower.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Control Tower landing-zone, enabled-baseline, and
// enabled-control metadata visible to the configured AWS credentials. The
// landing-zone manifest body, control parameter values, and baseline parameter
// values are never read.
func (c *Client) Snapshot(ctx context.Context) (controltowerservice.Snapshot, error) {
	var snapshot controltowerservice.Snapshot

	landingZone, err := c.landingZone(ctx)
	if err != nil {
		return controltowerservice.Snapshot{}, err
	}
	snapshot.LandingZone = landingZone

	baselines, err := c.listEnabledBaselines(ctx)
	if err != nil {
		return controltowerservice.Snapshot{}, err
	}
	snapshot.EnabledBaselines = baselines

	controls, err := c.listEnabledControls(ctx, baselineTargets(baselines))
	if err != nil {
		return controltowerservice.Snapshot{}, err
	}
	snapshot.EnabledControls = controls

	return snapshot, nil
}

// landingZone resolves the single landing zone for the boundary, or nil when
// Control Tower is not set up. ListLandingZones returns at most one landing zone
// ARN; GetLandingZone fills version, status, and drift. The manifest body is
// never read.
func (c *Client) landingZone(ctx context.Context) (*controltowerservice.LandingZone, error) {
	arns, err := c.listLandingZoneARNs(ctx)
	if err != nil {
		return nil, err
	}
	if len(arns) == 0 {
		return nil, nil
	}
	arn := arns[0]
	var output *awscontroltower.GetLandingZoneOutput
	err = c.recordAPICall(ctx, "GetLandingZone", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetLandingZone(callCtx, &awscontroltower.GetLandingZoneInput{
			LandingZoneIdentifier: aws.String(arn),
		})
		return callErr
	})
	if err != nil || output == nil || output.LandingZone == nil {
		if err != nil {
			return nil, err
		}
		return &controltowerservice.LandingZone{ARN: arn}, nil
	}
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return nil, err
	}
	return mapLandingZone(arn, output.LandingZone, tags), nil
}

func (c *Client) listLandingZoneARNs(ctx context.Context) ([]string, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awscontroltower.ListLandingZonesOutput
		err := c.recordAPICall(ctx, "ListLandingZones", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListLandingZones(callCtx, &awscontroltower.ListLandingZonesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return arns, nil
		}
		for _, summary := range page.LandingZones {
			if arn := strings.TrimSpace(aws.ToString(summary.Arn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return arns, nil
		}
	}
}

func (c *Client) listEnabledBaselines(ctx context.Context) ([]controltowerservice.EnabledBaseline, error) {
	var baselines []controltowerservice.EnabledBaseline
	var nextToken *string
	for {
		var page *awscontroltower.ListEnabledBaselinesOutput
		err := c.recordAPICall(ctx, "ListEnabledBaselines", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListEnabledBaselines(callCtx, &awscontroltower.ListEnabledBaselinesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return baselines, nil
		}
		for _, summary := range page.EnabledBaselines {
			baselines = append(baselines, mapEnabledBaseline(summary))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return baselines, nil
		}
	}
}

// listEnabledControls enumerates enabled controls for each organizational-unit
// target. ListEnabledControls requires a TargetIdentifier, so the adapter
// queries the distinct OU targets the enabled baselines report (the baseline
// fleet covers every Control Tower governed OU). Duplicate enabled-control ARNs
// across targets are de-duplicated so the scanner emits one node per control.
func (c *Client) listEnabledControls(
	ctx context.Context,
	targets []string,
) ([]controltowerservice.EnabledControl, error) {
	var controls []controltowerservice.EnabledControl
	seen := make(map[string]struct{})
	for _, target := range targets {
		next, err := c.listEnabledControlsForTarget(ctx, target)
		if err != nil {
			return nil, err
		}
		for _, control := range next {
			key := strings.TrimSpace(control.ARN)
			if key == "" {
				continue
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			controls = append(controls, control)
		}
	}
	return controls, nil
}

func (c *Client) listEnabledControlsForTarget(
	ctx context.Context,
	target string,
) ([]controltowerservice.EnabledControl, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, nil
	}
	var controls []controltowerservice.EnabledControl
	var nextToken *string
	for {
		var page *awscontroltower.ListEnabledControlsOutput
		err := c.recordAPICall(ctx, "ListEnabledControls", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListEnabledControls(callCtx, &awscontroltower.ListEnabledControlsInput{
				TargetIdentifier: aws.String(target),
				NextToken:        nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return controls, nil
		}
		for _, summary := range page.EnabledControls {
			controls = append(controls, mapEnabledControl(summary, target))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return controls, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awscontroltower.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListTagsForResource(callCtx, &awscontroltower.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return callErr
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for key, value := range output.Tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		tags[key] = value
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

// baselineTargets returns the distinct, trimmed, non-empty OU target ARNs the
// enabled baselines report, in first-seen order. These seed the per-target
// ListEnabledControls queries.
func baselineTargets(baselines []controltowerservice.EnabledBaseline) []string {
	var targets []string
	seen := make(map[string]struct{})
	for _, baseline := range baselines {
		target := strings.TrimSpace(baseline.TargetIdentifier)
		if target == "" {
			continue
		}
		if _, dup := seen[target]; dup {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	return targets
}

func mapLandingZone(
	arn string,
	detail *awscontroltowertypes.LandingZoneDetail,
	tags map[string]string,
) *controltowerservice.LandingZone {
	landingZone := &controltowerservice.LandingZone{
		ARN:                    strings.TrimSpace(arn),
		Version:                strings.TrimSpace(aws.ToString(detail.Version)),
		LatestAvailableVersion: strings.TrimSpace(aws.ToString(detail.LatestAvailableVersion)),
		Status:                 strings.TrimSpace(string(detail.Status)),
		Tags:                   tags,
	}
	if detail.Arn != nil {
		if detailARN := strings.TrimSpace(aws.ToString(detail.Arn)); detailARN != "" {
			landingZone.ARN = detailARN
		}
	}
	if detail.DriftStatus != nil {
		landingZone.DriftStatus = strings.TrimSpace(string(detail.DriftStatus.Status))
	}
	return landingZone
}

func mapEnabledControl(
	summary awscontroltowertypes.EnabledControlSummary,
	target string,
) controltowerservice.EnabledControl {
	control := controltowerservice.EnabledControl{
		ARN:               strings.TrimSpace(aws.ToString(summary.Arn)),
		ControlIdentifier: strings.TrimSpace(aws.ToString(summary.ControlIdentifier)),
		TargetIdentifier:  strings.TrimSpace(aws.ToString(summary.TargetIdentifier)),
		ParentIdentifier:  strings.TrimSpace(aws.ToString(summary.ParentIdentifier)),
	}
	if control.TargetIdentifier == "" {
		control.TargetIdentifier = strings.TrimSpace(target)
	}
	if summary.StatusSummary != nil {
		control.Status = strings.TrimSpace(string(summary.StatusSummary.Status))
	}
	if summary.DriftStatusSummary != nil {
		control.DriftStatus = strings.TrimSpace(string(summary.DriftStatusSummary.DriftStatus))
	}
	return control
}

func mapEnabledBaseline(summary awscontroltowertypes.EnabledBaselineSummary) controltowerservice.EnabledBaseline {
	baseline := controltowerservice.EnabledBaseline{
		ARN:                strings.TrimSpace(aws.ToString(summary.Arn)),
		BaselineIdentifier: strings.TrimSpace(aws.ToString(summary.BaselineIdentifier)),
		BaselineVersion:    strings.TrimSpace(aws.ToString(summary.BaselineVersion)),
		TargetIdentifier:   strings.TrimSpace(aws.ToString(summary.TargetIdentifier)),
		ParentIdentifier:   strings.TrimSpace(aws.ToString(summary.ParentIdentifier)),
	}
	if summary.StatusSummary != nil {
		baseline.Status = strings.TrimSpace(string(summary.StatusSummary.Status))
	}
	return baseline
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

var _ controltowerservice.Client = (*Client)(nil)

var _ apiClient = (*awscontroltower.Client)(nil)
