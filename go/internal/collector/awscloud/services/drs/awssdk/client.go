// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdrs "github.com/aws/aws-sdk-go-v2/service/drs"
	awsdrstypes "github.com/aws/aws-sdk-go-v2/service/drs/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	drsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/drs"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Elastic Disaster Recovery API
// the adapter calls. It is deliberately limited to the three control-plane
// describe reads (source servers, recovery instances, replication configuration
// templates). It exposes no replication-agent/secret read, no replicated-disk or
// snapshot data read, no job-log read, and no Create/Update/Delete/Start/Stop/
// Recover mutation, so the adapter cannot read DRS data-plane content or mutate
// DRS state. The exclusion_test reflects over this interface to enforce that
// contract at build time.
type apiClient interface {
	DescribeSourceServers(
		context.Context,
		*awsdrs.DescribeSourceServersInput,
		...func(*awsdrs.Options),
	) (*awsdrs.DescribeSourceServersOutput, error)
	DescribeRecoveryInstances(
		context.Context,
		*awsdrs.DescribeRecoveryInstancesInput,
		...func(*awsdrs.Options),
	) (*awsdrs.DescribeRecoveryInstancesOutput, error)
	DescribeReplicationConfigurationTemplates(
		context.Context,
		*awsdrs.DescribeReplicationConfigurationTemplatesInput,
		...func(*awsdrs.Options),
	) (*awsdrs.DescribeReplicationConfigurationTemplatesOutput, error)
}

// Client adapts AWS SDK Elastic Disaster Recovery control-plane calls into
// scanner-owned metadata. It never reads replication agent secrets, replicated
// disk data, or point-in-time snapshot contents, never reads job logs, and never
// calls a recover, start, stop, or mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a DRS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdrs.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns DRS source server, recovery instance, and replication
// configuration template metadata visible to the configured AWS credentials.
// Replication agent secrets, replicated disk data, and snapshot contents are
// never read.
func (c *Client) Snapshot(ctx context.Context) (drsservice.Snapshot, error) {
	servers, err := c.describeSourceServers(ctx)
	if err != nil {
		return drsservice.Snapshot{}, err
	}
	instances, err := c.describeRecoveryInstances(ctx)
	if err != nil {
		return drsservice.Snapshot{}, err
	}
	templates, err := c.describeReplicationConfigurationTemplates(ctx)
	if err != nil {
		return drsservice.Snapshot{}, err
	}
	return drsservice.Snapshot{
		SourceServers:                     servers,
		RecoveryInstances:                 instances,
		ReplicationConfigurationTemplates: templates,
	}, nil
}

func (c *Client) describeSourceServers(ctx context.Context) ([]drsservice.SourceServer, error) {
	var servers []drsservice.SourceServer
	var nextToken *string
	for {
		var page *awsdrs.DescribeSourceServersOutput
		err := c.recordAPICall(ctx, "DescribeSourceServers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeSourceServers(callCtx, &awsdrs.DescribeSourceServersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return servers, nil
		}
		for _, server := range page.Items {
			servers = append(servers, mapSourceServer(server))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return servers, nil
		}
	}
}

func (c *Client) describeRecoveryInstances(ctx context.Context) ([]drsservice.RecoveryInstance, error) {
	var instances []drsservice.RecoveryInstance
	var nextToken *string
	for {
		var page *awsdrs.DescribeRecoveryInstancesOutput
		err := c.recordAPICall(ctx, "DescribeRecoveryInstances", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeRecoveryInstances(callCtx, &awsdrs.DescribeRecoveryInstancesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return instances, nil
		}
		for _, instance := range page.Items {
			instances = append(instances, mapRecoveryInstance(instance))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return instances, nil
		}
	}
}

func (c *Client) describeReplicationConfigurationTemplates(
	ctx context.Context,
) ([]drsservice.ReplicationConfigurationTemplate, error) {
	var templates []drsservice.ReplicationConfigurationTemplate
	var nextToken *string
	for {
		var page *awsdrs.DescribeReplicationConfigurationTemplatesOutput
		err := c.recordAPICall(ctx, "DescribeReplicationConfigurationTemplates", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeReplicationConfigurationTemplates(
				callCtx,
				&awsdrs.DescribeReplicationConfigurationTemplatesInput{NextToken: nextToken},
			)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return templates, nil
		}
		for _, template := range page.Items {
			templates = append(templates, mapTemplate(template))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return templates, nil
		}
	}
}

func mapSourceServer(server awsdrstypes.SourceServer) drsservice.SourceServer {
	mapped := drsservice.SourceServer{
		SourceServerID:       strings.TrimSpace(aws.ToString(server.SourceServerID)),
		ARN:                  strings.TrimSpace(aws.ToString(server.Arn)),
		RecoveryInstanceID:   strings.TrimSpace(aws.ToString(server.RecoveryInstanceId)),
		ReplicationDirection: strings.TrimSpace(string(server.ReplicationDirection)),
		LastLaunchResult:     strings.TrimSpace(string(server.LastLaunchResult)),
		Tags:                 trimTags(server.Tags),
	}
	if info := server.DataReplicationInfo; info != nil {
		mapped.DataReplicationState = strings.TrimSpace(string(info.DataReplicationState))
	}
	applySourceProperties(&mapped, server.SourceProperties)
	if cloud := server.SourceCloudProperties; cloud != nil {
		mapped.OriginAccountID = strings.TrimSpace(aws.ToString(cloud.OriginAccountID))
		mapped.OriginRegion = strings.TrimSpace(aws.ToString(cloud.OriginRegion))
		mapped.OriginAvailabilityZone = strings.TrimSpace(aws.ToString(cloud.OriginAvailabilityZone))
	}
	return mapped
}

// applySourceProperties copies the reported identification hints, operating
// system description, and recommended recovery instance type onto the source
// server. It never copies disk contents, CPU/RAM inventory beyond presence, or
// network interface payloads.
func applySourceProperties(server *drsservice.SourceServer, props *awsdrstypes.SourceProperties) {
	if props == nil {
		return
	}
	server.RecommendedInstanceType = strings.TrimSpace(aws.ToString(props.RecommendedInstanceType))
	if os := props.Os; os != nil {
		server.OperatingSystem = strings.TrimSpace(aws.ToString(os.FullString))
	}
	if hints := props.IdentificationHints; hints != nil {
		server.Hostname = strings.TrimSpace(aws.ToString(hints.Hostname))
		server.FQDN = strings.TrimSpace(aws.ToString(hints.Fqdn))
	}
}

func mapRecoveryInstance(instance awsdrstypes.RecoveryInstance) drsservice.RecoveryInstance {
	return drsservice.RecoveryInstance{
		RecoveryInstanceID: strings.TrimSpace(aws.ToString(instance.RecoveryInstanceID)),
		ARN:                strings.TrimSpace(aws.ToString(instance.Arn)),
		EC2InstanceID:      strings.TrimSpace(aws.ToString(instance.Ec2InstanceID)),
		EC2InstanceState:   strings.TrimSpace(string(instance.Ec2InstanceState)),
		SourceServerID:     strings.TrimSpace(aws.ToString(instance.SourceServerID)),
		IsDrill:            aws.ToBool(instance.IsDrill),
		OriginEnvironment:  strings.TrimSpace(string(instance.OriginEnvironment)),
		Tags:               trimTags(instance.Tags),
	}
}

func mapTemplate(template awsdrstypes.ReplicationConfigurationTemplate) drsservice.ReplicationConfigurationTemplate {
	return drsservice.ReplicationConfigurationTemplate{
		TemplateID:                    strings.TrimSpace(aws.ToString(template.ReplicationConfigurationTemplateID)),
		ARN:                           strings.TrimSpace(aws.ToString(template.Arn)),
		EBSEncryption:                 strings.TrimSpace(string(template.EbsEncryption)),
		StagingAreaSubnetID:           strings.TrimSpace(aws.ToString(template.StagingAreaSubnetId)),
		ReplicationServerInstanceType: strings.TrimSpace(aws.ToString(template.ReplicationServerInstanceType)),
		UseDedicatedReplicationServer: aws.ToBool(template.UseDedicatedReplicationServer),
		AssociateDefaultSecurityGroup: aws.ToBool(template.AssociateDefaultSecurityGroup),
		Tags:                          trimTags(template.Tags),
	}
}

// trimTags returns a trimmed-key copy of input, or nil when nothing survives.
func trimTags(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	tags := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		tags[key] = value
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
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

var _ drsservice.Client = (*Client)(nil)

var _ apiClient = (*awsdrs.Client)(nil)
