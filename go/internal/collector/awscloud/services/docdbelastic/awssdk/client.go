// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdocdbelastic "github.com/aws/aws-sdk-go-v2/service/docdbelastic"
	awsdocdbelastictypes "github.com/aws/aws-sdk-go-v2/service/docdbelastic/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	docdbelasticservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/docdbelastic"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS DocumentDB Elastic Clusters
// API the adapter calls. It is deliberately limited to the cluster list/get
// control-plane reads and resource-tag reads. It exposes no Create/Delete/
// Update/Start/Stop/Copy/Restore/Apply mutation and no document, collection,
// index, or query read, so the adapter cannot read database contents or mutate
// Elastic Cluster state. The exclusion_test reflects over this interface to
// enforce that contract at build time.
type apiClient interface {
	ListClusters(
		context.Context,
		*awsdocdbelastic.ListClustersInput,
		...func(*awsdocdbelastic.Options),
	) (*awsdocdbelastic.ListClustersOutput, error)
	GetCluster(
		context.Context,
		*awsdocdbelastic.GetClusterInput,
		...func(*awsdocdbelastic.Options),
	) (*awsdocdbelastic.GetClusterOutput, error)
	ListTagsForResource(
		context.Context,
		*awsdocdbelastic.ListTagsForResourceInput,
		...func(*awsdocdbelastic.Options),
	) (*awsdocdbelastic.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK DocumentDB Elastic Clusters control-plane calls into
// scanner-owned metadata. It never reads document contents, collections,
// indexes, or query results, never reads the admin password, and never calls a
// mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a DocumentDB Elastic Clusters SDK adapter for one claimed
// AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdocdbelastic.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns DocumentDB Elastic cluster metadata visible to the
// configured AWS credentials. ListClusters returns identity-only summaries, so
// the adapter calls GetCluster per cluster for the full control-plane metadata;
// document contents, collections, indexes, query results, and the admin
// password are never read.
func (c *Client) Snapshot(ctx context.Context) (docdbelasticservice.Snapshot, error) {
	summaries, err := c.listClusters(ctx)
	if err != nil {
		return docdbelasticservice.Snapshot{}, err
	}
	clusters := make([]docdbelasticservice.Cluster, 0, len(summaries))
	for _, summary := range summaries {
		arn := strings.TrimSpace(aws.ToString(summary.ClusterArn))
		if arn == "" {
			continue
		}
		cluster, err := c.getCluster(ctx, arn)
		if err != nil {
			return docdbelasticservice.Snapshot{}, err
		}
		clusters = append(clusters, cluster)
	}
	return docdbelasticservice.Snapshot{Clusters: clusters}, nil
}

func (c *Client) listClusters(ctx context.Context) ([]awsdocdbelastictypes.ClusterInList, error) {
	var summaries []awsdocdbelastictypes.ClusterInList
	var nextToken *string
	for {
		var page *awsdocdbelastic.ListClustersOutput
		err := c.recordAPICall(ctx, "ListClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListClusters(callCtx, &awsdocdbelastic.ListClustersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return summaries, nil
		}
		summaries = append(summaries, page.Clusters...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return summaries, nil
		}
	}
}

func (c *Client) getCluster(ctx context.Context, arn string) (docdbelasticservice.Cluster, error) {
	var output *awsdocdbelastic.GetClusterOutput
	err := c.recordAPICall(ctx, "GetCluster", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetCluster(callCtx, &awsdocdbelastic.GetClusterInput{
			ClusterArn: aws.String(arn),
		})
		return err
	})
	if err != nil {
		return docdbelasticservice.Cluster{}, err
	}
	if output == nil || output.Cluster == nil {
		return docdbelasticservice.Cluster{ARN: arn}, nil
	}
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return docdbelasticservice.Cluster{}, err
	}
	return mapCluster(output.Cluster, tags), nil
}

// mapCluster copies control-plane metadata from the SDK cluster into the
// scanner-owned model. It never copies the cluster endpoint connection string
// or the admin password. Under SECRET_ARN auth, AWS reports the Secrets Manager
// secret ARN in the AdminUserName field; that ARN is captured only as the
// admin-secret reference. Under PLAIN_TEXT auth the admin user name is dropped
// entirely (it is not a graph-resolvable reference and is treated as
// credential-adjacent identity).
func mapCluster(cluster *awsdocdbelastictypes.Cluster, tags map[string]string) docdbelasticservice.Cluster {
	authType := strings.TrimSpace(string(cluster.AuthType))
	mapped := docdbelasticservice.Cluster{
		ARN:                        strings.TrimSpace(aws.ToString(cluster.ClusterArn)),
		Name:                       strings.TrimSpace(aws.ToString(cluster.ClusterName)),
		Status:                     strings.TrimSpace(string(cluster.Status)),
		AuthType:                   authType,
		KMSKeyID:                   strings.TrimSpace(aws.ToString(cluster.KmsKeyId)),
		ShardCapacity:              aws.ToInt32(cluster.ShardCapacity),
		ShardCount:                 aws.ToInt32(cluster.ShardCount),
		ShardInstanceCount:         aws.ToInt32(cluster.ShardInstanceCount),
		BackupRetentionPeriod:      aws.ToInt32(cluster.BackupRetentionPeriod),
		PreferredBackupWindow:      strings.TrimSpace(aws.ToString(cluster.PreferredBackupWindow)),
		PreferredMaintenanceWindow: strings.TrimSpace(aws.ToString(cluster.PreferredMaintenanceWindow)),
		SubnetIDs:                  trimmedStrings(cluster.SubnetIds),
		SecurityGroupIDs:           trimmedStrings(cluster.VpcSecurityGroupIds),
		CreateTime:                 parseTime(aws.ToString(cluster.CreateTime)),
		Tags:                       tags,
	}
	if authType == string(awsdocdbelastictypes.AuthSecretArn) {
		mapped.AdminSecretARN = secretARN(aws.ToString(cluster.AdminUserName))
	}
	return mapped
}

// secretARN returns value only when it is an ARN-shaped admin-secret reference,
// so a plaintext admin user name accidentally seen under SECRET_ARN auth is
// never persisted as a credential.
func secretARN(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "arn:") {
		return value
	}
	return ""
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsdocdbelastic.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsdocdbelastic.ListTagsForResourceInput{
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

// trimmedStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func trimmedStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// parseTime parses the RFC 3339 cluster create-time string AWS reports, or
// returns the zero time when it is empty or unparseable so the scanner omits an
// unknown timestamp instead of emitting an epoch.
func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}

var _ docdbelasticservice.Client = (*Client)(nil)

var _ apiClient = (*awsdocdbelastic.Client)(nil)
