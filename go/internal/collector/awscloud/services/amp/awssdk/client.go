// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsamp "github.com/aws/aws-sdk-go-v2/service/amp"
	awsamptypes "github.com/aws/aws-sdk-go-v2/service/amp/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	ampservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/amp"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Amazon Managed Service for
// Prometheus (aps) API the adapter calls. It is deliberately limited to the
// workspace/namespace/scraper list reads and resource-tag reads. It exposes no
// Describe of rule-group bodies, no DescribeWorkspaceConfiguration, no
// alert-manager or scrape-configuration reads, and no Create/Update/Delete/Put
// mutation, so the adapter cannot read ingested samples, rule definitions, or
// scrape-configuration bodies, and cannot mutate AMP state. The exclusion_test
// reflects over this interface to enforce that contract at build time.
type apiClient interface {
	ListWorkspaces(
		context.Context,
		*awsamp.ListWorkspacesInput,
		...func(*awsamp.Options),
	) (*awsamp.ListWorkspacesOutput, error)
	ListRuleGroupsNamespaces(
		context.Context,
		*awsamp.ListRuleGroupsNamespacesInput,
		...func(*awsamp.Options),
	) (*awsamp.ListRuleGroupsNamespacesOutput, error)
	ListScrapers(
		context.Context,
		*awsamp.ListScrapersInput,
		...func(*awsamp.Options),
	) (*awsamp.ListScrapersOutput, error)
}

// Client adapts AWS SDK Amazon Managed Service for Prometheus control-plane
// calls into scanner-owned metadata. It never reads ingested time-series
// samples, query results, alert-manager definitions, rule-group definition
// bodies, or scrape-configuration bodies, and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an AMP SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsamp.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns AMP workspace metadata, the rule-groups namespaces (names
// only) under each workspace, and the account's managed-collector scrapers
// visible to the configured AWS credentials. Ingested samples, query results,
// rule-group definition bodies, and scrape-configuration bodies are never read.
func (c *Client) Snapshot(ctx context.Context) (ampservice.Snapshot, error) {
	workspaces, err := c.listWorkspaces(ctx)
	if err != nil {
		return ampservice.Snapshot{}, fmt.Errorf("list AMP workspaces: %w", err)
	}
	for i := range workspaces {
		namespaces, err := c.listNamespaces(ctx, workspaces[i].WorkspaceID)
		if err != nil {
			return ampservice.Snapshot{}, fmt.Errorf("list AMP rule-groups namespaces: %w", err)
		}
		workspaces[i].RuleGroupsNamespaces = namespaces
	}
	scrapers, err := c.listScrapers(ctx)
	if err != nil {
		return ampservice.Snapshot{}, fmt.Errorf("list AMP scrapers: %w", err)
	}
	return ampservice.Snapshot{Workspaces: workspaces, Scrapers: scrapers}, nil
}

func (c *Client) listWorkspaces(ctx context.Context) ([]ampservice.Workspace, error) {
	var workspaces []ampservice.Workspace
	var nextToken *string
	for {
		var page *awsamp.ListWorkspacesOutput
		err := c.recordAPICall(ctx, "ListWorkspaces", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListWorkspaces(callCtx, &awsamp.ListWorkspacesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return workspaces, nil
		}
		for _, summary := range page.Workspaces {
			workspaces = append(workspaces, mapWorkspace(summary))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return workspaces, nil
		}
	}
}

func mapWorkspace(summary awsamptypes.WorkspaceSummary) ampservice.Workspace {
	workspace := ampservice.Workspace{
		ARN:         strings.TrimSpace(aws.ToString(summary.Arn)),
		WorkspaceID: strings.TrimSpace(aws.ToString(summary.WorkspaceId)),
		Alias:       strings.TrimSpace(aws.ToString(summary.Alias)),
		KMSKeyARN:   strings.TrimSpace(aws.ToString(summary.KmsKeyArn)),
		CreatedAt:   aws.ToTime(summary.CreatedAt),
		Tags:        cloneTags(summary.Tags),
	}
	if summary.Status != nil {
		workspace.Status = strings.TrimSpace(string(summary.Status.StatusCode))
	}
	return workspace
}

func (c *Client) listNamespaces(ctx context.Context, workspaceID string) ([]ampservice.RuleGroupsNamespace, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, nil
	}
	var namespaces []ampservice.RuleGroupsNamespace
	var nextToken *string
	for {
		var page *awsamp.ListRuleGroupsNamespacesOutput
		err := c.recordAPICall(ctx, "ListRuleGroupsNamespaces", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListRuleGroupsNamespaces(callCtx, &awsamp.ListRuleGroupsNamespacesInput{
				WorkspaceId: aws.String(workspaceID),
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return namespaces, nil
		}
		for _, summary := range page.RuleGroupsNamespaces {
			namespaces = append(namespaces, mapNamespace(summary))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return namespaces, nil
		}
	}
}

func mapNamespace(summary awsamptypes.RuleGroupsNamespaceSummary) ampservice.RuleGroupsNamespace {
	namespace := ampservice.RuleGroupsNamespace{
		ARN:        strings.TrimSpace(aws.ToString(summary.Arn)),
		Name:       strings.TrimSpace(aws.ToString(summary.Name)),
		CreatedAt:  aws.ToTime(summary.CreatedAt),
		ModifiedAt: aws.ToTime(summary.ModifiedAt),
		Tags:       cloneTags(summary.Tags),
	}
	if summary.Status != nil {
		namespace.Status = strings.TrimSpace(string(summary.Status.StatusCode))
	}
	return namespace
}

func (c *Client) listScrapers(ctx context.Context) ([]ampservice.Scraper, error) {
	var scrapers []ampservice.Scraper
	var nextToken *string
	for {
		var page *awsamp.ListScrapersOutput
		err := c.recordAPICall(ctx, "ListScrapers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListScrapers(callCtx, &awsamp.ListScrapersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return scrapers, nil
		}
		for _, summary := range page.Scrapers {
			scrapers = append(scrapers, mapScraper(summary))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return scrapers, nil
		}
	}
}

func mapScraper(summary awsamptypes.ScraperSummary) ampservice.Scraper {
	scraper := ampservice.Scraper{
		ARN:                     strings.TrimSpace(aws.ToString(summary.Arn)),
		ScraperID:               strings.TrimSpace(aws.ToString(summary.ScraperId)),
		Alias:                   strings.TrimSpace(aws.ToString(summary.Alias)),
		RoleARN:                 strings.TrimSpace(aws.ToString(summary.RoleArn)),
		SourceEKSClusterARN:     sourceEKSClusterARN(summary.Source),
		DestinationWorkspaceARN: destinationWorkspaceARN(summary.Destination),
		CreatedAt:               aws.ToTime(summary.CreatedAt),
		Tags:                    cloneTags(summary.Tags),
	}
	if summary.Status != nil {
		scraper.Status = strings.TrimSpace(string(summary.Status.StatusCode))
	}
	scraper.SubnetIDs, scraper.SecurityGroupIDs = scraperVPCConfig(summary.Source)
	return scraper
}

// sourceEKSClusterARN extracts the EKS cluster ARN from a scraper source union.
// Only the EKS configuration variant carries a cluster ARN; the MSK/VPC source
// variant carries no EKS cluster, so it yields an empty string.
func sourceEKSClusterARN(source awsamptypes.Source) string {
	eks, ok := source.(*awsamptypes.SourceMemberEksConfiguration)
	if !ok {
		return ""
	}
	return strings.TrimSpace(aws.ToString(eks.Value.ClusterArn))
}

// scraperVPCConfig extracts the bare subnet and security-group ids from a
// scraper EKS source configuration. The MSK/VPC source variant is not an EKS
// scrape source, so it yields no ids.
func scraperVPCConfig(source awsamptypes.Source) (subnets, securityGroups []string) {
	eks, ok := source.(*awsamptypes.SourceMemberEksConfiguration)
	if !ok {
		return nil, nil
	}
	return trimAll(eks.Value.SubnetIds), trimAll(eks.Value.SecurityGroupIds)
}

// destinationWorkspaceARN extracts the destination workspace ARN from a scraper
// destination union. Only the AMP workspace destination variant is defined.
func destinationWorkspaceARN(destination awsamptypes.Destination) string {
	amp, ok := destination.(*awsamptypes.DestinationMemberAmpConfiguration)
	if !ok {
		return ""
	}
	return strings.TrimSpace(aws.ToString(amp.Value.WorkspaceArn))
}

func trimAll(input []string) []string {
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

func cloneTags(input map[string]string) map[string]string {
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

var _ ampservice.Client = (*Client)(nil)

var _ apiClient = (*awsamp.Client)(nil)
