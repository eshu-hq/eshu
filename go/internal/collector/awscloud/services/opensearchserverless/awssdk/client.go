// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsaoss "github.com/aws/aws-sdk-go-v2/service/opensearchserverless"
	awsaosstypes "github.com/aws/aws-sdk-go-v2/service/opensearchserverless/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	aossservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/opensearchserverless"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS OpenSearch Serverless API the
// adapter calls. It is deliberately limited to collection, security-policy, and
// VPC-endpoint reads plus resource-tag reads. It exposes no CreateIndex, no
// BatchGetCollectionGroup data path, and no Create/Update/Delete mutation, and it
// never reaches the OpenSearch HTTP data plane (index, search, bulk, document
// APIs), which lives on the collection endpoint this package never constructs.
// The exclusion_test reflects over this interface to enforce that contract at
// build time.
type apiClient interface {
	ListCollections(
		context.Context,
		*awsaoss.ListCollectionsInput,
		...func(*awsaoss.Options),
	) (*awsaoss.ListCollectionsOutput, error)
	BatchGetCollection(
		context.Context,
		*awsaoss.BatchGetCollectionInput,
		...func(*awsaoss.Options),
	) (*awsaoss.BatchGetCollectionOutput, error)
	ListSecurityPolicies(
		context.Context,
		*awsaoss.ListSecurityPoliciesInput,
		...func(*awsaoss.Options),
	) (*awsaoss.ListSecurityPoliciesOutput, error)
	GetSecurityPolicy(
		context.Context,
		*awsaoss.GetSecurityPolicyInput,
		...func(*awsaoss.Options),
	) (*awsaoss.GetSecurityPolicyOutput, error)
	ListVpcEndpoints(
		context.Context,
		*awsaoss.ListVpcEndpointsInput,
		...func(*awsaoss.Options),
	) (*awsaoss.ListVpcEndpointsOutput, error)
	BatchGetVpcEndpoint(
		context.Context,
		*awsaoss.BatchGetVpcEndpointInput,
		...func(*awsaoss.Options),
	) (*awsaoss.BatchGetVpcEndpointOutput, error)
	ListTagsForResource(
		context.Context,
		*awsaoss.ListTagsForResourceInput,
		...func(*awsaoss.Options),
	) (*awsaoss.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK OpenSearch Serverless control-plane calls into
// scanner-owned metadata. It never reads the OpenSearch HTTP data plane, never
// persists access-policy or security-policy document bodies, and never calls a
// mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an OpenSearch Serverless SDK adapter for one claimed AWS
// boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsaoss.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns OpenSearch Serverless collection, security-policy, and managed
// VPC-endpoint metadata visible to the configured AWS credentials, plus the
// encryption-policy-to-KMS bindings used for collection encryption edges. The
// OpenSearch HTTP data plane is never read and no policy document body is
// persisted.
func (c *Client) Snapshot(ctx context.Context) (aossservice.Snapshot, error) {
	collections, err := c.listCollections(ctx)
	if err != nil {
		return aossservice.Snapshot{}, err
	}
	policies, bindings, err := c.listSecurityPolicies(ctx)
	if err != nil {
		return aossservice.Snapshot{}, err
	}
	endpoints, err := c.listVPCEndpoints(ctx)
	if err != nil {
		return aossservice.Snapshot{}, err
	}
	return aossservice.Snapshot{
		Collections:           collections,
		SecurityPolicies:      policies,
		VPCEndpoints:          endpoints,
		EncryptionKeyBindings: bindings,
	}, nil
}

func (c *Client) listCollections(ctx context.Context) ([]aossservice.Collection, error) {
	var summaries []awsaosstypes.CollectionSummary
	var nextToken *string
	for {
		var page *awsaoss.ListCollectionsOutput
		err := c.recordAPICall(ctx, "ListCollections", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListCollections(callCtx, &awsaoss.ListCollectionsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		summaries = append(summaries, page.CollectionSummaries...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	return c.describeCollections(ctx, summaries)
}

func (c *Client) describeCollections(
	ctx context.Context,
	summaries []awsaosstypes.CollectionSummary,
) ([]aossservice.Collection, error) {
	ids := collectionIDs(summaries)
	if len(ids) == 0 {
		return nil, nil
	}
	var collections []aossservice.Collection
	for _, batch := range chunk(ids, batchGetCollectionLimit) {
		var page *awsaoss.BatchGetCollectionOutput
		err := c.recordAPICall(ctx, "BatchGetCollection", func(callCtx context.Context) error {
			var err error
			page, err = c.client.BatchGetCollection(callCtx, &awsaoss.BatchGetCollectionInput{
				Ids: batch,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, detail := range page.CollectionDetails {
			mapped, err := c.mapCollection(ctx, detail)
			if err != nil {
				return nil, err
			}
			collections = append(collections, mapped)
		}
	}
	return collections, nil
}

func (c *Client) listVPCEndpoints(ctx context.Context) ([]aossservice.VPCEndpoint, error) {
	var ids []string
	var nextToken *string
	for {
		var page *awsaoss.ListVpcEndpointsOutput
		err := c.recordAPICall(ctx, "ListVpcEndpoints", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListVpcEndpoints(callCtx, &awsaoss.ListVpcEndpointsInput{
				NextToken: nextToken,
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
			if id := aws.ToString(summary.Id); id != "" {
				ids = append(ids, id)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	return c.describeVPCEndpoints(ctx, ids)
}

func (c *Client) describeVPCEndpoints(ctx context.Context, ids []string) ([]aossservice.VPCEndpoint, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var endpoints []aossservice.VPCEndpoint
	for _, batch := range chunk(ids, batchGetVPCEndpointLimit) {
		var page *awsaoss.BatchGetVpcEndpointOutput
		err := c.recordAPICall(ctx, "BatchGetVpcEndpoint", func(callCtx context.Context) error {
			var err error
			page, err = c.client.BatchGetVpcEndpoint(callCtx, &awsaoss.BatchGetVpcEndpointInput{
				Ids: batch,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, detail := range page.VpcEndpointDetails {
			endpoints = append(endpoints, mapVPCEndpoint(detail))
		}
	}
	return endpoints, nil
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
	c.recordInstruments(ctx, operation, result, throttled)
	return err
}

var _ aossservice.Client = (*Client)(nil)

var _ apiClient = (*awsaoss.Client)(nil)
