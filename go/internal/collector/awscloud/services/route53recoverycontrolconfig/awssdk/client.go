// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarc "github.com/aws/aws-sdk-go-v2/service/route53recoverycontrolconfig"
	awsarctypes "github.com/aws/aws-sdk-go-v2/service/route53recoverycontrolconfig/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	arcservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53recoverycontrolconfig"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Route 53 Application Recovery
// Controller recovery-control configuration API the adapter calls. It is
// deliberately limited to the cluster/control-panel/routing-control/safety-rule
// list reads and resource-tag reads. It exposes no Create/Update/Delete, no
// UpdateRoutingControlState (which lives in the separate route53recoverycluster
// data-plane module this package never imports), and no other mutation, so the
// adapter cannot change routing control state or mutate configuration. The
// exclusion_test reflects over this interface to enforce that contract at build
// time.
type apiClient interface {
	ListClusters(
		context.Context,
		*awsarc.ListClustersInput,
		...func(*awsarc.Options),
	) (*awsarc.ListClustersOutput, error)
	ListControlPanels(
		context.Context,
		*awsarc.ListControlPanelsInput,
		...func(*awsarc.Options),
	) (*awsarc.ListControlPanelsOutput, error)
	ListRoutingControls(
		context.Context,
		*awsarc.ListRoutingControlsInput,
		...func(*awsarc.Options),
	) (*awsarc.ListRoutingControlsOutput, error)
	ListSafetyRules(
		context.Context,
		*awsarc.ListSafetyRulesInput,
		...func(*awsarc.Options),
	) (*awsarc.ListSafetyRulesOutput, error)
	ListTagsForResource(
		context.Context,
		*awsarc.ListTagsForResourceInput,
		...func(*awsarc.Options),
	) (*awsarc.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Route 53 ARC recovery-control configuration control-plane
// calls into scanner-owned metadata. It never reads or sets routing control
// state and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Route 53 ARC recovery-control configuration SDK adapter for
// one claimed AWS boundary. The route53recoverycontrolconfig endpoint is global
// (the SDK pins it to us-west-2), so a single claim observes every cluster
// regardless of the boundary Region.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsarc.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns recovery-control cluster metadata plus the control panels
// under each cluster and the routing controls and safety rules under each
// control panel. Routing control state is never read.
func (c *Client) Snapshot(ctx context.Context) (arcservice.Snapshot, error) {
	clusters, err := c.listClusters(ctx)
	if err != nil {
		return arcservice.Snapshot{}, err
	}
	for i := range clusters {
		panels, err := c.listControlPanels(ctx, clusters[i].ARN)
		if err != nil {
			return arcservice.Snapshot{}, err
		}
		for j := range panels {
			controls, err := c.listRoutingControls(ctx, panels[j].ARN)
			if err != nil {
				return arcservice.Snapshot{}, err
			}
			panels[j].RoutingControls = controls
			rules, err := c.listSafetyRules(ctx, panels[j].ARN)
			if err != nil {
				return arcservice.Snapshot{}, err
			}
			panels[j].SafetyRules = rules
		}
		clusters[i].ControlPanels = panels
	}
	return arcservice.Snapshot{Clusters: clusters}, nil
}

func (c *Client) listClusters(ctx context.Context) ([]arcservice.Cluster, error) {
	var clusters []arcservice.Cluster
	var nextToken *string
	for {
		var page *awsarc.ListClustersOutput
		err := c.recordAPICall(ctx, "ListClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListClusters(callCtx, &awsarc.ListClustersInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return clusters, nil
		}
		for _, cluster := range page.Clusters {
			mapped, err := c.mapCluster(ctx, cluster)
			if err != nil {
				return nil, err
			}
			clusters = append(clusters, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return clusters, nil
		}
	}
}

func (c *Client) mapCluster(
	ctx context.Context,
	cluster awsarctypes.Cluster,
) (arcservice.Cluster, error) {
	arn := strings.TrimSpace(aws.ToString(cluster.ClusterArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return arcservice.Cluster{}, err
	}
	return arcservice.Cluster{
		ARN:             arn,
		Name:            strings.TrimSpace(aws.ToString(cluster.Name)),
		Status:          strings.TrimSpace(string(cluster.Status)),
		NetworkType:     strings.TrimSpace(string(cluster.NetworkType)),
		Owner:           strings.TrimSpace(aws.ToString(cluster.Owner)),
		EndpointRegions: endpointRegions(cluster.ClusterEndpoints),
		Tags:            tags,
	}, nil
}

func (c *Client) listControlPanels(ctx context.Context, clusterARN string) ([]arcservice.ControlPanel, error) {
	clusterARN = strings.TrimSpace(clusterARN)
	if clusterARN == "" {
		return nil, nil
	}
	var panels []arcservice.ControlPanel
	var nextToken *string
	for {
		var page *awsarc.ListControlPanelsOutput
		err := c.recordAPICall(ctx, "ListControlPanels", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListControlPanels(callCtx, &awsarc.ListControlPanelsInput{
				ClusterArn: aws.String(clusterARN),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return panels, nil
		}
		for _, panel := range page.ControlPanels {
			mapped, err := c.mapControlPanel(ctx, panel)
			if err != nil {
				return nil, err
			}
			panels = append(panels, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return panels, nil
		}
	}
}

func (c *Client) mapControlPanel(
	ctx context.Context,
	panel awsarctypes.ControlPanel,
) (arcservice.ControlPanel, error) {
	arn := strings.TrimSpace(aws.ToString(panel.ControlPanelArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return arcservice.ControlPanel{}, err
	}
	return arcservice.ControlPanel{
		ARN:                 arn,
		ClusterARN:          strings.TrimSpace(aws.ToString(panel.ClusterArn)),
		Name:                strings.TrimSpace(aws.ToString(panel.Name)),
		Status:              strings.TrimSpace(string(panel.Status)),
		DefaultControlPanel: aws.ToBool(panel.DefaultControlPanel),
		RoutingControlCount: aws.ToInt32(panel.RoutingControlCount),
		Owner:               strings.TrimSpace(aws.ToString(panel.Owner)),
		Tags:                tags,
	}, nil
}

func (c *Client) listRoutingControls(ctx context.Context, panelARN string) ([]arcservice.RoutingControl, error) {
	panelARN = strings.TrimSpace(panelARN)
	if panelARN == "" {
		return nil, nil
	}
	var controls []arcservice.RoutingControl
	var nextToken *string
	for {
		var page *awsarc.ListRoutingControlsOutput
		err := c.recordAPICall(ctx, "ListRoutingControls", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListRoutingControls(callCtx, &awsarc.ListRoutingControlsInput{
				ControlPanelArn: aws.String(panelARN),
				NextToken:       nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return controls, nil
		}
		for _, control := range page.RoutingControls {
			mapped, err := c.mapRoutingControl(ctx, control)
			if err != nil {
				return nil, err
			}
			controls = append(controls, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return controls, nil
		}
	}
}

func (c *Client) mapRoutingControl(
	ctx context.Context,
	control awsarctypes.RoutingControl,
) (arcservice.RoutingControl, error) {
	arn := strings.TrimSpace(aws.ToString(control.RoutingControlArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return arcservice.RoutingControl{}, err
	}
	return arcservice.RoutingControl{
		ARN:             arn,
		ControlPanelARN: strings.TrimSpace(aws.ToString(control.ControlPanelArn)),
		Name:            strings.TrimSpace(aws.ToString(control.Name)),
		Status:          strings.TrimSpace(string(control.Status)),
		Owner:           strings.TrimSpace(aws.ToString(control.Owner)),
		Tags:            tags,
	}, nil
}

func (c *Client) listSafetyRules(ctx context.Context, panelARN string) ([]arcservice.SafetyRule, error) {
	panelARN = strings.TrimSpace(panelARN)
	if panelARN == "" {
		return nil, nil
	}
	var rules []arcservice.SafetyRule
	var nextToken *string
	for {
		var page *awsarc.ListSafetyRulesOutput
		err := c.recordAPICall(ctx, "ListSafetyRules", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListSafetyRules(callCtx, &awsarc.ListSafetyRulesInput{
				ControlPanelArn: aws.String(panelARN),
				NextToken:       nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return rules, nil
		}
		for _, rule := range page.SafetyRules {
			mapped, err := c.mapSafetyRule(ctx, rule)
			if err != nil {
				return nil, err
			}
			if mapped != nil {
				rules = append(rules, *mapped)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return rules, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsarc.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsarc.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
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

var _ arcservice.Client = (*Client)(nil)

var _ apiClient = (*awsarc.Client)(nil)
