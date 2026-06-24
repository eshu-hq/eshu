// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsca "github.com/aws/aws-sdk-go-v2/service/codeartifact"
	awscatypes "github.com/aws/aws-sdk-go-v2/service/codeartifact/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	codeartifactservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codeartifact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK CodeArtifact surface the adapter consumes.
// It is deliberately limited to the four metadata reads ListDomains,
// DescribeDomain, ListRepositories, and DescribeRepository. It exposes no
// GetPackageVersionAsset, GetPackageVersionReadme, ListPackages,
// ListPackageVersions, PublishPackageVersion, CopyPackageVersions, or any
// Create/Update/Delete/Dispose/Put mutation, so package payloads and resource
// mutation are unreachable by construction. A reflection guard test enforces
// this.
type apiClient interface {
	ListDomains(context.Context, *awsca.ListDomainsInput, ...func(*awsca.Options)) (*awsca.ListDomainsOutput, error)
	DescribeDomain(context.Context, *awsca.DescribeDomainInput, ...func(*awsca.Options)) (*awsca.DescribeDomainOutput, error)
	ListRepositories(context.Context, *awsca.ListRepositoriesInput, ...func(*awsca.Options)) (*awsca.ListRepositoriesOutput, error)
	DescribeRepository(context.Context, *awsca.DescribeRepositoryInput, ...func(*awsca.Options)) (*awsca.DescribeRepositoryOutput, error)
}

// Client adapts AWS SDK CodeArtifact pagination into scanner-owned metadata. The
// adapter never reads, downloads, publishes, copies, or deletes a package
// version or asset; it reads only domain and repository metadata.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a CodeArtifact SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsca.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDomains reads CodeArtifact domains with ListDomains and enriches each with
// DescribeDomain metadata (encryption-key ARN, owner, repository count, asset
// size, S3 bucket ARN). No package assets stored in the domain are read.
func (c *Client) ListDomains(ctx context.Context) ([]codeartifactservice.Domain, error) {
	summaries, err := c.listDomainSummaries(ctx)
	if err != nil {
		return nil, err
	}
	domains := make([]codeartifactservice.Domain, 0, len(summaries))
	for _, summary := range summaries {
		name := strings.TrimSpace(aws.ToString(summary.Name))
		if name == "" {
			continue
		}
		domain, err := c.describeDomain(ctx, name, aws.ToString(summary.Owner), summary)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func (c *Client) listDomainSummaries(ctx context.Context) ([]awscatypes.DomainSummary, error) {
	var summaries []awscatypes.DomainSummary
	var nextToken *string
	for {
		var page *awsca.ListDomainsOutput
		err := c.recordAPICall(ctx, "ListDomains", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDomains(callCtx, &awsca.ListDomainsInput{
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
		summaries = append(summaries, page.Domains...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return summaries, nil
		}
	}
}

func (c *Client) describeDomain(
	ctx context.Context,
	name string,
	owner string,
	summary awscatypes.DomainSummary,
) (codeartifactservice.Domain, error) {
	var output *awsca.DescribeDomainOutput
	err := c.recordAPICall(ctx, "DescribeDomain", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeDomain(callCtx, &awsca.DescribeDomainInput{
			Domain:      aws.String(name),
			DomainOwner: optionalString(owner),
		})
		return err
	})
	if err != nil {
		return codeartifactservice.Domain{}, err
	}
	domain := codeartifactservice.Domain{
		Name:          name,
		ARN:           strings.TrimSpace(aws.ToString(summary.Arn)),
		Owner:         strings.TrimSpace(aws.ToString(summary.Owner)),
		EncryptionKey: strings.TrimSpace(aws.ToString(summary.EncryptionKey)),
		Status:        strings.TrimSpace(string(summary.Status)),
		CreatedTime:   aws.ToTime(summary.CreatedTime),
	}
	if output != nil && output.Domain != nil {
		description := output.Domain
		domain.ARN = firstNonEmpty(strings.TrimSpace(aws.ToString(description.Arn)), domain.ARN)
		domain.Owner = firstNonEmpty(strings.TrimSpace(aws.ToString(description.Owner)), domain.Owner)
		domain.EncryptionKey = firstNonEmpty(strings.TrimSpace(aws.ToString(description.EncryptionKey)), domain.EncryptionKey)
		domain.S3BucketARN = strings.TrimSpace(aws.ToString(description.S3BucketArn))
		domain.RepositoryCount = description.RepositoryCount
		domain.AssetSizeBytes = description.AssetSizeBytes
		domain.Status = firstNonEmpty(strings.TrimSpace(string(description.Status)), domain.Status)
		if created := aws.ToTime(description.CreatedTime); !created.IsZero() {
			domain.CreatedTime = created
		}
	}
	return domain, nil
}

// ListRepositories reads CodeArtifact repositories with ListRepositories and
// enriches each with DescribeRepository metadata (external connections and
// upstream repositories). No package versions or assets are read.
func (c *Client) ListRepositories(ctx context.Context) ([]codeartifactservice.Repository, error) {
	summaries, err := c.listRepositorySummaries(ctx)
	if err != nil {
		return nil, err
	}
	repositories := make([]codeartifactservice.Repository, 0, len(summaries))
	for _, summary := range summaries {
		name := strings.TrimSpace(aws.ToString(summary.Name))
		domainName := strings.TrimSpace(aws.ToString(summary.DomainName))
		if name == "" || domainName == "" {
			continue
		}
		repository, err := c.describeRepository(ctx, domainName, name, aws.ToString(summary.DomainOwner), summary)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, repository)
	}
	return repositories, nil
}

func (c *Client) listRepositorySummaries(ctx context.Context) ([]awscatypes.RepositorySummary, error) {
	var summaries []awscatypes.RepositorySummary
	var nextToken *string
	for {
		var page *awsca.ListRepositoriesOutput
		err := c.recordAPICall(ctx, "ListRepositories", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListRepositories(callCtx, &awsca.ListRepositoriesInput{
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
		summaries = append(summaries, page.Repositories...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return summaries, nil
		}
	}
}

func (c *Client) describeRepository(
	ctx context.Context,
	domainName string,
	name string,
	domainOwner string,
	summary awscatypes.RepositorySummary,
) (codeartifactservice.Repository, error) {
	var output *awsca.DescribeRepositoryOutput
	err := c.recordAPICall(ctx, "DescribeRepository", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeRepository(callCtx, &awsca.DescribeRepositoryInput{
			Domain:      aws.String(domainName),
			Repository:  aws.String(name),
			DomainOwner: optionalString(domainOwner),
		})
		return err
	})
	if err != nil {
		return codeartifactservice.Repository{}, err
	}
	repository := codeartifactservice.Repository{
		Name:                 name,
		ARN:                  strings.TrimSpace(aws.ToString(summary.Arn)),
		DomainName:           domainName,
		DomainOwner:          strings.TrimSpace(aws.ToString(summary.DomainOwner)),
		AdministratorAccount: strings.TrimSpace(aws.ToString(summary.AdministratorAccount)),
		Description:          strings.TrimSpace(aws.ToString(summary.Description)),
		CreatedTime:          aws.ToTime(summary.CreatedTime),
	}
	if output != nil && output.Repository != nil {
		description := output.Repository
		repository.ARN = firstNonEmpty(strings.TrimSpace(aws.ToString(description.Arn)), repository.ARN)
		repository.DomainName = firstNonEmpty(strings.TrimSpace(aws.ToString(description.DomainName)), repository.DomainName)
		repository.DomainOwner = firstNonEmpty(strings.TrimSpace(aws.ToString(description.DomainOwner)), repository.DomainOwner)
		repository.AdministratorAccount = firstNonEmpty(strings.TrimSpace(aws.ToString(description.AdministratorAccount)), repository.AdministratorAccount)
		repository.Description = firstNonEmpty(strings.TrimSpace(aws.ToString(description.Description)), repository.Description)
		if created := aws.ToTime(description.CreatedTime); !created.IsZero() {
			repository.CreatedTime = created
		}
		repository.ExternalConnections = mapExternalConnections(description.ExternalConnections)
		repository.Upstreams = mapUpstreams(description.Upstreams)
	}
	return repository, nil
}

func mapExternalConnections(connections []awscatypes.RepositoryExternalConnectionInfo) []codeartifactservice.ExternalConnection {
	if len(connections) == 0 {
		return nil
	}
	mapped := make([]codeartifactservice.ExternalConnection, 0, len(connections))
	for _, connection := range connections {
		name := strings.TrimSpace(aws.ToString(connection.ExternalConnectionName))
		if name == "" {
			continue
		}
		mapped = append(mapped, codeartifactservice.ExternalConnection{
			Name:          name,
			PackageFormat: strings.TrimSpace(string(connection.PackageFormat)),
			Status:        strings.TrimSpace(string(connection.Status)),
		})
	}
	if len(mapped) == 0 {
		return nil
	}
	return mapped
}

func mapUpstreams(upstreams []awscatypes.UpstreamRepositoryInfo) []string {
	if len(upstreams) == 0 {
		return nil
	}
	names := make([]string, 0, len(upstreams))
	for _, upstream := range upstreams {
		if name := strings.TrimSpace(aws.ToString(upstream.RepositoryName)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return aws.String(value)
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

var _ codeartifactservice.Client = (*Client)(nil)

var _ apiClient = (*awsca.Client)(nil)
