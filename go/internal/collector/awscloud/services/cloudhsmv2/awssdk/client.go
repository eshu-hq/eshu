// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudhsmv2 "github.com/aws/aws-sdk-go-v2/service/cloudhsmv2"
	awscloudhsmv2types "github.com/aws/aws-sdk-go-v2/service/cloudhsmv2/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cloudhsmv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudhsmv2"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS CloudHSM v2 API the adapter
// calls. It is deliberately limited to the two control-plane Describe reads,
// both of which return resource tags inline. It exposes no cluster/HSM
// create/delete/modify mutation, no backup restore/delete/copy, no
// InitializeCluster, and no GetResourcePolicy, so the adapter can neither
// initialize a cluster (which would expose the PRECO password flow) nor mutate
// CloudHSM state. The exclusion_test reflects over this interface to enforce
// that contract at build time.
type apiClient interface {
	DescribeClusters(
		context.Context,
		*awscloudhsmv2.DescribeClustersInput,
		...func(*awscloudhsmv2.Options),
	) (*awscloudhsmv2.DescribeClustersOutput, error)
	DescribeBackups(
		context.Context,
		*awscloudhsmv2.DescribeBackupsInput,
		...func(*awscloudhsmv2.Options),
	) (*awscloudhsmv2.DescribeBackupsOutput, error)
}

// Client adapts AWS SDK CloudHSM v2 control-plane calls into scanner-owned
// metadata. It never reads or persists key material, certificate PEM bodies,
// the cluster certificate signing request body, or the Pre-Crypto Officer
// password, and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a CloudHSM v2 SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awscloudhsmv2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns CloudHSM v2 cluster and backup metadata visible to the
// configured AWS credentials. Key material, certificate bodies, CSR bodies, and
// the PRECO password are never read.
func (c *Client) Snapshot(ctx context.Context) (cloudhsmv2service.Snapshot, error) {
	clusters, err := c.describeClusters(ctx)
	if err != nil {
		return cloudhsmv2service.Snapshot{}, err
	}
	backups, err := c.describeBackups(ctx)
	if err != nil {
		return cloudhsmv2service.Snapshot{}, err
	}
	return cloudhsmv2service.Snapshot{Clusters: clusters, Backups: backups}, nil
}

func (c *Client) describeClusters(ctx context.Context) ([]cloudhsmv2service.Cluster, error) {
	var clusters []cloudhsmv2service.Cluster
	var nextToken *string
	for {
		var page *awscloudhsmv2.DescribeClustersOutput
		err := c.recordAPICall(ctx, "DescribeClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeClusters(callCtx, &awscloudhsmv2.DescribeClustersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return clusters, nil
		}
		for _, cluster := range page.Clusters {
			clusters = append(clusters, mapCluster(cluster))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return clusters, nil
		}
	}
}

func (c *Client) describeBackups(ctx context.Context) ([]cloudhsmv2service.Backup, error) {
	var backups []cloudhsmv2service.Backup
	var nextToken *string
	for {
		var page *awscloudhsmv2.DescribeBackupsOutput
		err := c.recordAPICall(ctx, "DescribeBackups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeBackups(callCtx, &awscloudhsmv2.DescribeBackupsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return backups, nil
		}
		for _, backup := range page.Backups {
			backups = append(backups, mapBackup(backup))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return backups, nil
		}
	}
}

func mapCluster(cluster awscloudhsmv2types.Cluster) cloudhsmv2service.Cluster {
	mapped := cloudhsmv2service.Cluster{
		ID:              strings.TrimSpace(aws.ToString(cluster.ClusterId)),
		State:           string(cluster.State),
		StateMessage:    strings.TrimSpace(aws.ToString(cluster.StateMessage)),
		HsmType:         strings.TrimSpace(aws.ToString(cluster.HsmType)),
		Mode:            string(cluster.Mode),
		NetworkType:     string(cluster.NetworkType),
		VPCID:           strings.TrimSpace(aws.ToString(cluster.VpcId)),
		SecurityGroupID: strings.TrimSpace(aws.ToString(cluster.SecurityGroup)),
		SourceBackupID:  strings.TrimSpace(aws.ToString(cluster.SourceBackupId)),
		BackupPolicy:    string(cluster.BackupPolicy),
		SubnetMappings:  subnetMappings(cluster.SubnetMapping),
		HSMs:            mapHSMs(cluster.Hsms),
		CreateTimestamp: aws.ToTime(cluster.CreateTimestamp),
		Tags:            mapTags(cluster.TagList),
	}
	if retention := cluster.BackupRetentionPolicy; retention != nil {
		mapped.BackupRetentionType = string(retention.Type)
		mapped.BackupRetentionValue = strings.TrimSpace(aws.ToString(retention.Value))
	}
	mapped.CertificatePresence = certificatePresence(cluster.Certificates)
	return mapped
}

// certificatePresence records which certificate fields AWS returned without ever
// copying a body. Each flag is the presence of the corresponding PEM/CSR string;
// the string itself is read here only to test for emptiness and is then dropped.
func certificatePresence(certs *awscloudhsmv2types.Certificates) cloudhsmv2service.CertificatePresence {
	if certs == nil {
		return cloudhsmv2service.CertificatePresence{}
	}
	return cloudhsmv2service.CertificatePresence{
		ClusterCertificate:              nonEmpty(certs.ClusterCertificate),
		ClusterCSR:                      nonEmpty(certs.ClusterCsr),
		HSMCertificate:                  nonEmpty(certs.HsmCertificate),
		AWSHardwareCertificate:          nonEmpty(certs.AwsHardwareCertificate),
		ManufacturerHardwareCertificate: nonEmpty(certs.ManufacturerHardwareCertificate),
	}
}

func mapHSMs(hsms []awscloudhsmv2types.Hsm) []cloudhsmv2service.HSM {
	if len(hsms) == 0 {
		return nil
	}
	out := make([]cloudhsmv2service.HSM, 0, len(hsms))
	for _, hsm := range hsms {
		out = append(out, cloudhsmv2service.HSM{
			ID:               strings.TrimSpace(aws.ToString(hsm.HsmId)),
			State:            string(hsm.State),
			AvailabilityZone: strings.TrimSpace(aws.ToString(hsm.AvailabilityZone)),
			SubnetID:         strings.TrimSpace(aws.ToString(hsm.SubnetId)),
			ENIID:            strings.TrimSpace(aws.ToString(hsm.EniId)),
			ENIIP:            strings.TrimSpace(aws.ToString(hsm.EniIp)),
			ENIIPV6:          strings.TrimSpace(aws.ToString(hsm.EniIpV6)),
		})
	}
	return out
}

func mapBackup(backup awscloudhsmv2types.Backup) cloudhsmv2service.Backup {
	return cloudhsmv2service.Backup{
		ID:              strings.TrimSpace(aws.ToString(backup.BackupId)),
		ARN:             strings.TrimSpace(aws.ToString(backup.BackupArn)),
		State:           string(backup.BackupState),
		ClusterID:       strings.TrimSpace(aws.ToString(backup.ClusterId)),
		HsmType:         strings.TrimSpace(aws.ToString(backup.HsmType)),
		Mode:            string(backup.Mode),
		NeverExpires:    aws.ToBool(backup.NeverExpires),
		SourceBackup:    strings.TrimSpace(aws.ToString(backup.SourceBackup)),
		SourceCluster:   strings.TrimSpace(aws.ToString(backup.SourceCluster)),
		SourceRegion:    strings.TrimSpace(aws.ToString(backup.SourceRegion)),
		CreateTimestamp: aws.ToTime(backup.CreateTimestamp),
		CopyTimestamp:   aws.ToTime(backup.CopyTimestamp),
		DeleteTimestamp: aws.ToTime(backup.DeleteTimestamp),
		Tags:            mapTags(backup.TagList),
	}
}

// subnetMappings flattens the CloudHSM availability-zone-to-subnet map into a
// deterministic slice the scanner can iterate, dropping blank entries.
func subnetMappings(mapping map[string]string) []cloudhsmv2service.SubnetMapping {
	if len(mapping) == 0 {
		return nil
	}
	out := make([]cloudhsmv2service.SubnetMapping, 0, len(mapping))
	for zone, subnet := range mapping {
		subnetID := strings.TrimSpace(subnet)
		if subnetID == "" {
			continue
		}
		out = append(out, cloudhsmv2service.SubnetMapping{
			AvailabilityZone: strings.TrimSpace(zone),
			SubnetID:         subnetID,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mapTags(tagList []awscloudhsmv2types.Tag) map[string]string {
	if len(tagList) == 0 {
		return nil
	}
	tags := make(map[string]string, len(tagList))
	for _, tag := range tagList {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

// nonEmpty reports whether a CloudHSM certificate/CSR pointer holds a non-blank
// body. It is the only inspection the adapter performs on certificate material;
// the body is never copied out.
func nonEmpty(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
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

var _ cloudhsmv2service.Client = (*Client)(nil)

var _ apiClient = (*awscloudhsmv2.Client)(nil)
