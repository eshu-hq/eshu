// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslightsail "github.com/aws/aws-sdk-go-v2/service/lightsail"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	lightsailservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lightsail"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only Lightsail read surface the adapter depends on.
// It exposes only the paginated Get* readers the scanner needs and deliberately
// excludes every create, delete, reboot, start, stop, snapshot, attach,
// detach, and key/access reader so a mutation or secret read can never be
// reached. The exclusion is enforced by a reflective guard test.
type apiClient interface {
	GetInstances(context.Context, *awslightsail.GetInstancesInput, ...func(*awslightsail.Options)) (*awslightsail.GetInstancesOutput, error)
	GetRelationalDatabases(context.Context, *awslightsail.GetRelationalDatabasesInput, ...func(*awslightsail.Options)) (*awslightsail.GetRelationalDatabasesOutput, error)
	GetLoadBalancers(context.Context, *awslightsail.GetLoadBalancersInput, ...func(*awslightsail.Options)) (*awslightsail.GetLoadBalancersOutput, error)
	GetDisks(context.Context, *awslightsail.GetDisksInput, ...func(*awslightsail.Options)) (*awslightsail.GetDisksOutput, error)
	GetStaticIps(context.Context, *awslightsail.GetStaticIpsInput, ...func(*awslightsail.Options)) (*awslightsail.GetStaticIpsOutput, error)
}

// Client adapts AWS SDK Lightsail pagination into scanner-owned metadata. The
// adapter only calls GetInstances, GetRelationalDatabases, GetLoadBalancers,
// GetDisks, and GetStaticIps. It never calls any Create/Delete/Reboot/Start/
// Stop/Snapshot/Attach/Detach API, never calls GetInstanceAccessDetails, and
// never reads default key-pair private material or database master passwords.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Lightsail SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awslightsail.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListInstances reads Lightsail instance metadata. Instance access details,
// default key-pair private keys, and user data stay outside the contract.
func (c *Client) ListInstances(ctx context.Context) ([]lightsailservice.Instance, error) {
	var instances []lightsailservice.Instance
	var pageToken *string
	for {
		var page *awslightsail.GetInstancesOutput
		err := c.recordAPICall(ctx, "GetInstances", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetInstances(callCtx, &awslightsail.GetInstancesInput{
				PageToken: pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return instances, nil
		}
		for _, instance := range page.Instances {
			instances = append(instances, mapInstance(instance))
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return instances, nil
		}
	}
}

// ListDatabases reads Lightsail managed relational database metadata. Master
// user passwords and certificate bodies stay outside the contract.
func (c *Client) ListDatabases(ctx context.Context) ([]lightsailservice.Database, error) {
	var databases []lightsailservice.Database
	var pageToken *string
	for {
		var page *awslightsail.GetRelationalDatabasesOutput
		err := c.recordAPICall(ctx, "GetRelationalDatabases", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetRelationalDatabases(callCtx, &awslightsail.GetRelationalDatabasesInput{
				PageToken: pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return databases, nil
		}
		for _, database := range page.RelationalDatabases {
			databases = append(databases, mapDatabase(database))
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return databases, nil
		}
	}
}

// ListLoadBalancers reads Lightsail load balancer metadata including the bare
// names of attached instances reported in the instance-health summary.
func (c *Client) ListLoadBalancers(ctx context.Context) ([]lightsailservice.LoadBalancer, error) {
	var loadBalancers []lightsailservice.LoadBalancer
	var pageToken *string
	for {
		var page *awslightsail.GetLoadBalancersOutput
		err := c.recordAPICall(ctx, "GetLoadBalancers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetLoadBalancers(callCtx, &awslightsail.GetLoadBalancersInput{
				PageToken: pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return loadBalancers, nil
		}
		for _, loadBalancer := range page.LoadBalancers {
			loadBalancers = append(loadBalancers, mapLoadBalancer(loadBalancer))
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return loadBalancers, nil
		}
	}
}

// ListDisks reads Lightsail block-storage disk metadata including the bare name
// of the instance each disk is attached to.
func (c *Client) ListDisks(ctx context.Context) ([]lightsailservice.Disk, error) {
	var disks []lightsailservice.Disk
	var pageToken *string
	for {
		var page *awslightsail.GetDisksOutput
		err := c.recordAPICall(ctx, "GetDisks", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetDisks(callCtx, &awslightsail.GetDisksInput{
				PageToken: pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return disks, nil
		}
		for _, disk := range page.Disks {
			disks = append(disks, mapDisk(disk))
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return disks, nil
		}
	}
}

// ListStaticIPs reads Lightsail static IP metadata including the bare name of
// the instance each static IP is attached to.
func (c *Client) ListStaticIPs(ctx context.Context) ([]lightsailservice.StaticIP, error) {
	var staticIPs []lightsailservice.StaticIP
	var pageToken *string
	for {
		var page *awslightsail.GetStaticIpsOutput
		err := c.recordAPICall(ctx, "GetStaticIps", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetStaticIps(callCtx, &awslightsail.GetStaticIpsInput{
				PageToken: pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return staticIPs, nil
		}
		for _, staticIP := range page.StaticIps {
			staticIPs = append(staticIPs, mapStaticIP(staticIP))
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return staticIPs, nil
		}
	}
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

var _ lightsailservice.Client = (*Client)(nil)

var _ apiClient = (*awslightsail.Client)(nil)
