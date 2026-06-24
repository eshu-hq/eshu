// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsopensearch "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awsserverless "github.com/aws/aws-sdk-go-v2/service/opensearchserverless"
	awsserverlesstypes "github.com/aws/aws-sdk-go-v2/service/opensearchserverless/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	opensearchservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/opensearch"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const listMaxResults int32 = 100

// domainAPI is the OpenSearch Service control-plane read surface this adapter
// reaches. It is deliberately limited to metadata reads: no mutation, no
// inbound-connection acceptance, and no OpenSearch HTTP API (_search, _index,
// _doc, _bulk, and similar). The OpenSearch HTTP API is not part of the AWS
// SDK client at all, so it is unreachable from this interface by construction.
type domainAPI interface {
	ListDomainNames(
		context.Context,
		*awsopensearch.ListDomainNamesInput,
		...func(*awsopensearch.Options),
	) (*awsopensearch.ListDomainNamesOutput, error)
	DescribeDomains(
		context.Context,
		*awsopensearch.DescribeDomainsInput,
		...func(*awsopensearch.Options),
	) (*awsopensearch.DescribeDomainsOutput, error)
	DescribePackages(
		context.Context,
		*awsopensearch.DescribePackagesInput,
		...func(*awsopensearch.Options),
	) (*awsopensearch.DescribePackagesOutput, error)
	ListDomainsForPackage(
		context.Context,
		*awsopensearch.ListDomainsForPackageInput,
		...func(*awsopensearch.Options),
	) (*awsopensearch.ListDomainsForPackageOutput, error)
	ListTags(
		context.Context,
		*awsopensearch.ListTagsInput,
		...func(*awsopensearch.Options),
	) (*awsopensearch.ListTagsOutput, error)
}

// serverlessAPI is the OpenSearch Serverless control-plane read surface this
// adapter reaches. It excludes GetIndex and every mutation, policy-body, and
// data API so collection contents stay outside the scan slice.
type serverlessAPI interface {
	ListCollections(
		context.Context,
		*awsserverless.ListCollectionsInput,
		...func(*awsserverless.Options),
	) (*awsserverless.ListCollectionsOutput, error)
	BatchGetCollection(
		context.Context,
		*awsserverless.BatchGetCollectionInput,
		...func(*awsserverless.Options),
	) (*awsserverless.BatchGetCollectionOutput, error)
	ListSecurityConfigs(
		context.Context,
		*awsserverless.ListSecurityConfigsInput,
		...func(*awsserverless.Options),
	) (*awsserverless.ListSecurityConfigsOutput, error)
	ListVpcEndpoints(
		context.Context,
		*awsserverless.ListVpcEndpointsInput,
		...func(*awsserverless.Options),
	) (*awsserverless.ListVpcEndpointsOutput, error)
	BatchGetVpcEndpoint(
		context.Context,
		*awsserverless.BatchGetVpcEndpointInput,
		...func(*awsserverless.Options),
	) (*awsserverless.BatchGetVpcEndpointOutput, error)
}

// Client adapts AWS SDK OpenSearch and OpenSearch Serverless control-plane
// calls into scanner-owned metadata. It never reaches the OpenSearch HTTP API
// (_search, _index, _doc, _bulk, and similar), never calls a mutation or
// inbound-connection API, and never persists master user passwords, domain
// endpoint contents, access policy bodies, custom package bodies, or
// serverless saved-object bodies.
type Client struct {
	domain      domainAPI
	serverless  serverlessAPI
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an OpenSearch SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		domain:      awsopensearch.NewFromConfig(config),
		serverless:  awsserverless.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDomains returns OpenSearch Service provisioned domain metadata. It lists
// domain names, batches DescribeDomains for full status, and joins tags per
// domain ARN. The master user password is never returned by DescribeDomains and
// is never persisted; the adapter resolves only IAM role ARNs referenced by the
// domain access policy.
func (c *Client) ListDomains(ctx context.Context) ([]opensearchservice.Domain, error) {
	var names *awsopensearch.ListDomainNamesOutput
	err := c.recordAPICall(ctx, "ListDomainNames", func(callCtx context.Context) error {
		var err error
		names, err = c.domain.ListDomainNames(callCtx, &awsopensearch.ListDomainNamesInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	if names == nil || len(names.DomainNames) == 0 {
		return nil, nil
	}
	domainNames := make([]string, 0, len(names.DomainNames))
	for _, info := range names.DomainNames {
		if name := strings.TrimSpace(aws.ToString(info.DomainName)); name != "" {
			domainNames = append(domainNames, name)
		}
	}
	if len(domainNames) == 0 {
		return nil, nil
	}

	var statuses *awsopensearch.DescribeDomainsOutput
	err = c.recordAPICall(ctx, "DescribeDomains", func(callCtx context.Context) error {
		var err error
		statuses, err = c.domain.DescribeDomains(callCtx, &awsopensearch.DescribeDomainsInput{
			DomainNames: domainNames,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if statuses == nil {
		return nil, nil
	}
	domains := make([]opensearchservice.Domain, 0, len(statuses.DomainStatusList))
	for _, status := range statuses.DomainStatusList {
		tags, err := c.listTags(ctx, aws.ToString(status.ARN))
		if err != nil {
			return nil, err
		}
		domains = append(domains, mapDomain(status, tags))
	}
	return domains, nil
}

// ListPackages returns OpenSearch custom package metadata visible to the
// configured AWS credentials. The adapter projects name, type, status, and
// owning identity only; it never reads the package body.
func (c *Client) ListPackages(ctx context.Context) ([]opensearchservice.Package, error) {
	var packages []opensearchservice.Package
	var token *string
	for {
		var page *awsopensearch.DescribePackagesOutput
		err := c.recordAPICall(ctx, "DescribePackages", func(callCtx context.Context) error {
			var err error
			page, err = c.domain.DescribePackages(callCtx, &awsopensearch.DescribePackagesInput{
				MaxResults: listMaxResults,
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return packages, nil
		}
		for _, raw := range page.PackageDetailsList {
			packages = append(packages, mapPackage(raw))
		}
		next := strings.TrimSpace(aws.ToString(page.NextToken))
		if next == "" {
			return packages, nil
		}
		token = aws.String(next)
	}
}

// ListPackageAssociations returns the domains associated with one package. The
// scanner emits package-to-domain relationships from this list.
func (c *Client) ListPackageAssociations(ctx context.Context, packageID string) ([]opensearchservice.PackageAssociation, error) {
	packageID = strings.TrimSpace(packageID)
	if packageID == "" {
		return nil, nil
	}
	var associations []opensearchservice.PackageAssociation
	var token *string
	for {
		var page *awsopensearch.ListDomainsForPackageOutput
		err := c.recordAPICall(ctx, "ListDomainsForPackage", func(callCtx context.Context) error {
			var err error
			page, err = c.domain.ListDomainsForPackage(callCtx, &awsopensearch.ListDomainsForPackageInput{
				PackageID:  aws.String(packageID),
				MaxResults: listMaxResults,
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return associations, nil
		}
		for _, raw := range page.DomainPackageDetailsList {
			associations = append(associations, opensearchservice.PackageAssociation{
				PackageID:         strings.TrimSpace(aws.ToString(raw.PackageID)),
				DomainName:        strings.TrimSpace(aws.ToString(raw.DomainName)),
				DomainPackageStat: string(raw.DomainPackageStatus),
				ReferencePath:     strings.TrimSpace(aws.ToString(raw.ReferencePath)),
			})
		}
		next := strings.TrimSpace(aws.ToString(page.NextToken))
		if next == "" {
			return associations, nil
		}
		token = aws.String(next)
	}
}

// ListCollections returns OpenSearch Serverless collection metadata. It lists
// collection summaries, then batches BatchGetCollection for full detail.
// Collection endpoints, dashboard endpoints, and indexed data are never read.
func (c *Client) ListCollections(ctx context.Context) ([]opensearchservice.Collection, error) {
	var ids []string
	var token *string
	for {
		var page *awsserverless.ListCollectionsOutput
		err := c.recordAPICall(ctx, "ListCollections", func(callCtx context.Context) error {
			var err error
			page, err = c.serverless.ListCollections(callCtx, &awsserverless.ListCollectionsInput{
				MaxResults: aws.Int32(listMaxResults),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, summary := range page.CollectionSummaries {
			if id := strings.TrimSpace(aws.ToString(summary.Id)); id != "" {
				ids = append(ids, id)
			}
		}
		next := strings.TrimSpace(aws.ToString(page.NextToken))
		if next == "" {
			break
		}
		token = aws.String(next)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	var collections []opensearchservice.Collection
	for _, batch := range chunkStrings(ids, batchGetCollectionLimit) {
		var detail *awsserverless.BatchGetCollectionOutput
		err := c.recordAPICall(ctx, "BatchGetCollection", func(callCtx context.Context) error {
			var err error
			detail, err = c.serverless.BatchGetCollection(callCtx, &awsserverless.BatchGetCollectionInput{
				Ids: batch,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if detail == nil {
			continue
		}
		for _, raw := range detail.CollectionDetails {
			collections = append(collections, mapCollection(raw))
		}
	}
	return collections, nil
}

// ListSecurityConfigs returns OpenSearch Serverless security configuration
// summaries across every security config type. SAML metadata XML and IAM
// Identity Center secrets stay outside the result.
func (c *Client) ListSecurityConfigs(ctx context.Context) ([]opensearchservice.SecurityConfig, error) {
	var configs []opensearchservice.SecurityConfig
	for _, configType := range awsserverlesstypes.SecurityConfigType("").Values() {
		var token *string
		for {
			var page *awsserverless.ListSecurityConfigsOutput
			err := c.recordAPICall(ctx, "ListSecurityConfigs", func(callCtx context.Context) error {
				var err error
				page, err = c.serverless.ListSecurityConfigs(callCtx, &awsserverless.ListSecurityConfigsInput{
					Type:       configType,
					MaxResults: aws.Int32(listMaxResults),
					NextToken:  token,
				})
				return err
			})
			if err != nil {
				return nil, err
			}
			if page == nil {
				break
			}
			for _, raw := range page.SecurityConfigSummaries {
				configs = append(configs, opensearchservice.SecurityConfig{
					ID:          strings.TrimSpace(aws.ToString(raw.Id)),
					Type:        string(raw.Type),
					Description: strings.TrimSpace(aws.ToString(raw.Description)),
					Version:     strings.TrimSpace(aws.ToString(raw.ConfigVersion)),
				})
			}
			next := strings.TrimSpace(aws.ToString(page.NextToken))
			if next == "" {
				break
			}
			token = aws.String(next)
		}
	}
	return configs, nil
}

// ListVPCEndpoints returns OpenSearch Serverless managed VPC endpoint metadata.
// It lists endpoint summaries, then batches BatchGetVpcEndpoint for full VPC,
// subnet, and security group detail.
func (c *Client) ListVPCEndpoints(ctx context.Context) ([]opensearchservice.VPCEndpoint, error) {
	var ids []string
	var token *string
	for {
		var page *awsserverless.ListVpcEndpointsOutput
		err := c.recordAPICall(ctx, "ListVpcEndpoints", func(callCtx context.Context) error {
			var err error
			page, err = c.serverless.ListVpcEndpoints(callCtx, &awsserverless.ListVpcEndpointsInput{
				MaxResults: aws.Int32(listMaxResults),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, summary := range page.VpcEndpointSummaries {
			if id := strings.TrimSpace(aws.ToString(summary.Id)); id != "" {
				ids = append(ids, id)
			}
		}
		next := strings.TrimSpace(aws.ToString(page.NextToken))
		if next == "" {
			break
		}
		token = aws.String(next)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	var endpoints []opensearchservice.VPCEndpoint
	for _, batch := range chunkStrings(ids, batchGetVPCEndpointLimit) {
		var detail *awsserverless.BatchGetVpcEndpointOutput
		err := c.recordAPICall(ctx, "BatchGetVpcEndpoint", func(callCtx context.Context) error {
			var err error
			detail, err = c.serverless.BatchGetVpcEndpoint(callCtx, &awsserverless.BatchGetVpcEndpointInput{
				Ids: batch,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if detail == nil {
			continue
		}
		for _, raw := range detail.VpcEndpointDetails {
			endpoints = append(endpoints, mapVPCEndpoint(raw))
		}
	}
	return endpoints, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsopensearch.ListTagsOutput
	err := c.recordAPICall(ctx, "ListTags", func(callCtx context.Context) error {
		var err error
		output, err = c.domain.ListTags(callCtx, &awsopensearch.ListTagsInput{
			ARN: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	return mapTags(output.TagList), nil
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

var _ opensearchservice.Client = (*Client)(nil)

var (
	_ domainAPI     = (*awsopensearch.Client)(nil)
	_ serverlessAPI = (*awsserverless.Client)(nil)
)
