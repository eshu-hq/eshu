// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsemr "github.com/aws/aws-sdk-go-v2/service/emr"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
	awsemrserverless "github.com/aws/aws-sdk-go-v2/service/emrserverless"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	emrservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/emr"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recentlyTerminatedWindow bounds the "recently terminated" cluster slice. EMR
// retains terminated cluster metadata for a limited period; this window keeps
// the scan to clusters created within the last 30 days so terminated-cluster
// evidence stays current without an unbounded historical scan.
const recentlyTerminatedWindow = 30 * 24 * time.Hour

// emrAPIClient is the metadata-only EMR read surface this adapter depends on.
// It intentionally lists no mutation API, no step/bootstrap body reader, and no
// security-configuration policy-body reader; the package exclusion tests
// enforce that boundary by reflection.
type emrAPIClient interface {
	ListClusters(context.Context, *awsemr.ListClustersInput, ...func(*awsemr.Options)) (*awsemr.ListClustersOutput, error)
	DescribeCluster(context.Context, *awsemr.DescribeClusterInput, ...func(*awsemr.Options)) (*awsemr.DescribeClusterOutput, error)
	ListInstanceGroups(context.Context, *awsemr.ListInstanceGroupsInput, ...func(*awsemr.Options)) (*awsemr.ListInstanceGroupsOutput, error)
	ListInstanceFleets(context.Context, *awsemr.ListInstanceFleetsInput, ...func(*awsemr.Options)) (*awsemr.ListInstanceFleetsOutput, error)
	ListSecurityConfigurations(context.Context, *awsemr.ListSecurityConfigurationsInput, ...func(*awsemr.Options)) (*awsemr.ListSecurityConfigurationsOutput, error)
	ListStudios(context.Context, *awsemr.ListStudiosInput, ...func(*awsemr.Options)) (*awsemr.ListStudiosOutput, error)
	DescribeStudio(context.Context, *awsemr.DescribeStudioInput, ...func(*awsemr.Options)) (*awsemr.DescribeStudioOutput, error)
	ListStudioSessionMappings(context.Context, *awsemr.ListStudioSessionMappingsInput, ...func(*awsemr.Options)) (*awsemr.ListStudioSessionMappingsOutput, error)
}

// emrServerlessAPIClient is the metadata-only EMR Serverless read surface. It
// lists no application/job mutation API and no job-run reader (which carries
// SparkSubmit entry-point arguments); the package exclusion tests enforce that.
type emrServerlessAPIClient interface {
	ListApplications(context.Context, *awsemrserverless.ListApplicationsInput, ...func(*awsemrserverless.Options)) (*awsemrserverless.ListApplicationsOutput, error)
	GetApplication(context.Context, *awsemrserverless.GetApplicationInput, ...func(*awsemrserverless.Options)) (*awsemrserverless.GetApplicationOutput, error)
}

// Client adapts AWS SDK EMR and EMR Serverless reads into scanner-owned EMR
// records for one claimed AWS boundary.
type Client struct {
	emr         emrAPIClient
	serverless  emrServerlessAPIClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an EMR SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		emr:         awsemr.NewFromConfig(config),
		serverless:  awsemrserverless.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListClusters returns running and recently terminated EMR on EC2 clusters,
// each enriched with its uniform instance groups or instance fleets.
func (c *Client) ListClusters(ctx context.Context) ([]emrservice.Cluster, error) {
	createdAfter := c.now().Add(-recentlyTerminatedWindow)
	input := &awsemr.ListClustersInput{
		ClusterStates: clusterStates(),
		CreatedAfter:  aws.Time(createdAfter),
	}
	var clusters []emrservice.Cluster
	for {
		var page *awsemr.ListClustersOutput
		err := c.recordEMRCall(ctx, "ListClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.emr.ListClusters(callCtx, input)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, summary := range page.Clusters {
			cluster, err := c.describeCluster(ctx, aws.ToString(summary.Id))
			if err != nil {
				return nil, err
			}
			if cluster == nil {
				continue
			}
			clusters = append(clusters, *cluster)
		}
		if !advance(&input.Marker, page.Marker) {
			break
		}
	}
	return clusters, nil
}

func (c *Client) describeCluster(ctx context.Context, id string) (*emrservice.Cluster, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	var output *awsemr.DescribeClusterOutput
	err := c.recordEMRCall(ctx, "DescribeCluster", func(callCtx context.Context) error {
		var err error
		output, err = c.emr.DescribeCluster(callCtx, &awsemr.DescribeClusterInput{ClusterId: aws.String(id)})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("describe EMR cluster %q: %w", id, err)
	}
	if output == nil || output.Cluster == nil {
		return nil, nil
	}
	cluster := mapCluster(*output.Cluster)
	if isInstanceFleetCluster(output.Cluster.InstanceCollectionType) {
		fleets, err := c.listInstanceFleets(ctx, id)
		if err != nil {
			return nil, err
		}
		cluster.InstanceFleets = fleets
	} else {
		groups, err := c.listInstanceGroups(ctx, id)
		if err != nil {
			return nil, err
		}
		cluster.InstanceGroups = groups
	}
	return &cluster, nil
}

func (c *Client) listInstanceGroups(ctx context.Context, clusterID string) ([]emrservice.InstanceGroup, error) {
	input := &awsemr.ListInstanceGroupsInput{ClusterId: aws.String(clusterID)}
	var groups []emrservice.InstanceGroup
	for {
		var page *awsemr.ListInstanceGroupsOutput
		err := c.recordEMRCall(ctx, "ListInstanceGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.emr.ListInstanceGroups(callCtx, input)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("list EMR instance groups for cluster %q: %w", clusterID, err)
		}
		if page == nil {
			break
		}
		for _, group := range page.InstanceGroups {
			groups = append(groups, mapInstanceGroup(group))
		}
		if !advance(&input.Marker, page.Marker) {
			break
		}
	}
	return groups, nil
}

func (c *Client) listInstanceFleets(ctx context.Context, clusterID string) ([]emrservice.InstanceFleet, error) {
	input := &awsemr.ListInstanceFleetsInput{ClusterId: aws.String(clusterID)}
	var fleets []emrservice.InstanceFleet
	for {
		var page *awsemr.ListInstanceFleetsOutput
		err := c.recordEMRCall(ctx, "ListInstanceFleets", func(callCtx context.Context) error {
			var err error
			page, err = c.emr.ListInstanceFleets(callCtx, input)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("list EMR instance fleets for cluster %q: %w", clusterID, err)
		}
		if page == nil {
			break
		}
		for _, fleet := range page.InstanceFleets {
			fleets = append(fleets, mapInstanceFleet(fleet))
		}
		if !advance(&input.Marker, page.Marker) {
			break
		}
	}
	return fleets, nil
}

// ListSecurityConfigurations returns EMR security configuration metadata. Only
// the name and creation time are read; DescribeSecurityConfiguration (which
// returns the policy body) is never called.
func (c *Client) ListSecurityConfigurations(ctx context.Context) ([]emrservice.SecurityConfiguration, error) {
	input := &awsemr.ListSecurityConfigurationsInput{}
	var configs []emrservice.SecurityConfiguration
	for {
		var page *awsemr.ListSecurityConfigurationsOutput
		err := c.recordEMRCall(ctx, "ListSecurityConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = c.emr.ListSecurityConfigurations(callCtx, input)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, summary := range page.SecurityConfigurations {
			configs = append(configs, emrservice.SecurityConfiguration{
				Name:      aws.ToString(summary.Name),
				CreatedAt: aws.ToTime(summary.CreationDateTime),
			})
		}
		if !advance(&input.Marker, page.Marker) {
			break
		}
	}
	return configs, nil
}

// ListStudios returns EMR Studios, each enriched with its session mappings.
func (c *Client) ListStudios(ctx context.Context) ([]emrservice.Studio, error) {
	input := &awsemr.ListStudiosInput{}
	var studios []emrservice.Studio
	for {
		var page *awsemr.ListStudiosOutput
		err := c.recordEMRCall(ctx, "ListStudios", func(callCtx context.Context) error {
			var err error
			page, err = c.emr.ListStudios(callCtx, input)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, summary := range page.Studios {
			studio, err := c.describeStudio(ctx, aws.ToString(summary.StudioId))
			if err != nil {
				return nil, err
			}
			if studio == nil {
				continue
			}
			studios = append(studios, *studio)
		}
		if !advance(&input.Marker, page.Marker) {
			break
		}
	}
	return studios, nil
}

func (c *Client) describeStudio(ctx context.Context, id string) (*emrservice.Studio, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	var output *awsemr.DescribeStudioOutput
	err := c.recordEMRCall(ctx, "DescribeStudio", func(callCtx context.Context) error {
		var err error
		output, err = c.emr.DescribeStudio(callCtx, &awsemr.DescribeStudioInput{StudioId: aws.String(id)})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("describe EMR studio %q: %w", id, err)
	}
	if output == nil || output.Studio == nil {
		return nil, nil
	}
	studio := mapStudio(*output.Studio)
	mappings, err := c.listSessionMappings(ctx, id)
	if err != nil {
		return nil, err
	}
	studio.SessionMappings = mappings
	return &studio, nil
}

func (c *Client) listSessionMappings(ctx context.Context, studioID string) ([]emrservice.StudioSessionMapping, error) {
	input := &awsemr.ListStudioSessionMappingsInput{StudioId: aws.String(studioID)}
	var mappings []emrservice.StudioSessionMapping
	for {
		var page *awsemr.ListStudioSessionMappingsOutput
		err := c.recordEMRCall(ctx, "ListStudioSessionMappings", func(callCtx context.Context) error {
			var err error
			page, err = c.emr.ListStudioSessionMappings(callCtx, input)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("list EMR studio session mappings for studio %q: %w", studioID, err)
		}
		if page == nil {
			break
		}
		for _, summary := range page.SessionMappings {
			mappings = append(mappings, mapSessionMapping(summary))
		}
		if !advance(&input.Marker, page.Marker) {
			break
		}
	}
	return mappings, nil
}

// ListServerlessApplications returns EMR Serverless applications enriched with
// their network and disk-encryption configuration.
func (c *Client) ListServerlessApplications(ctx context.Context) ([]emrservice.ServerlessApplication, error) {
	input := &awsemrserverless.ListApplicationsInput{}
	var applications []emrservice.ServerlessApplication
	for {
		var page *awsemrserverless.ListApplicationsOutput
		err := c.recordServerlessCall(ctx, "ListApplications", func(callCtx context.Context) error {
			var err error
			page, err = c.serverless.ListApplications(callCtx, input)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, summary := range page.Applications {
			application, err := c.getServerlessApplication(ctx, aws.ToString(summary.Id))
			if err != nil {
				return nil, err
			}
			if application == nil {
				continue
			}
			applications = append(applications, *application)
		}
		if !advanceToken(&input.NextToken, page.NextToken) {
			break
		}
	}
	return applications, nil
}

func (c *Client) getServerlessApplication(ctx context.Context, id string) (*emrservice.ServerlessApplication, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	var output *awsemrserverless.GetApplicationOutput
	err := c.recordServerlessCall(ctx, "GetApplication", func(callCtx context.Context) error {
		var err error
		output, err = c.serverless.GetApplication(callCtx, &awsemrserverless.GetApplicationInput{ApplicationId: aws.String(id)})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get EMR Serverless application %q: %w", id, err)
	}
	if output == nil || output.Application == nil {
		return nil, nil
	}
	application := mapServerlessApplication(*output.Application)
	return &application, nil
}

func (c *Client) now() time.Time {
	if !c.boundary.ObservedAt.IsZero() {
		return c.boundary.ObservedAt
	}
	return time.Now()
}

func clusterStates() []emrtypes.ClusterState {
	return []emrtypes.ClusterState{
		emrtypes.ClusterStateStarting,
		emrtypes.ClusterStateBootstrapping,
		emrtypes.ClusterStateRunning,
		emrtypes.ClusterStateWaiting,
		emrtypes.ClusterStateTerminating,
		emrtypes.ClusterStateTerminated,
		emrtypes.ClusterStateTerminatedWithErrors,
	}
}

func isInstanceFleetCluster(collectionType emrtypes.InstanceCollectionType) bool {
	return collectionType == emrtypes.InstanceCollectionTypeInstanceFleet
}

// advance updates the EMR Marker continuation token and reports whether to keep
// paginating. It stops on an empty token and on an unchanging token so a buggy
// server response cannot loop forever.
func advance(current **string, next *string) bool {
	token := strings.TrimSpace(aws.ToString(next))
	if token == "" {
		return false
	}
	if *current != nil && strings.TrimSpace(**current) == token {
		return false
	}
	*current = aws.String(token)
	return true
}

// advanceToken is advance for EMR Serverless NextToken pagination.
func advanceToken(current **string, next *string) bool {
	return advance(current, next)
}

func (c *Client) recordEMRCall(ctx context.Context, operation string, call func(context.Context) error) error {
	return c.recordAPICall(ctx, operation, call)
}

func (c *Client) recordServerlessCall(ctx context.Context, operation string, call func(context.Context) error) error {
	return c.recordAPICall(ctx, operation, call)
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

var (
	_ emrservice.Client      = (*Client)(nil)
	_ emrAPIClient           = (*awsemr.Client)(nil)
	_ emrServerlessAPIClient = (*awsemrserverless.Client)(nil)
)
