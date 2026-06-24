// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsvp "github.com/aws/aws-sdk-go-v2/service/verifiedpermissions"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	vpservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/verifiedpermissions"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Verified Permissions API the
// adapter calls. It is deliberately limited to the policy store, policy, and
// identity source list reads plus the per-store Get and resource-tag reads. It
// exposes no GetPolicy (which returns the Cedar policy statement body), no
// GetSchema, no GetPolicyTemplate, and no IsAuthorized/BatchIsAuthorized
// authorization-evaluation call, so the adapter cannot read Cedar source,
// schema bodies, or authorization payloads, and no Create/Update/Delete/Put
// mutation, so it cannot mutate Verified Permissions state. The exclusion_test
// reflects over this interface to enforce that contract at build time.
type apiClient interface {
	ListPolicyStores(
		context.Context,
		*awsvp.ListPolicyStoresInput,
		...func(*awsvp.Options),
	) (*awsvp.ListPolicyStoresOutput, error)
	GetPolicyStore(
		context.Context,
		*awsvp.GetPolicyStoreInput,
		...func(*awsvp.Options),
	) (*awsvp.GetPolicyStoreOutput, error)
	ListPolicies(
		context.Context,
		*awsvp.ListPoliciesInput,
		...func(*awsvp.Options),
	) (*awsvp.ListPoliciesOutput, error)
	ListIdentitySources(
		context.Context,
		*awsvp.ListIdentitySourcesInput,
		...func(*awsvp.Options),
	) (*awsvp.ListIdentitySourcesOutput, error)
}

// Client adapts AWS SDK Verified Permissions control-plane calls into
// scanner-owned metadata. It never reads Cedar policy statement bodies, schema
// bodies, or policy template bodies, never evaluates authorization requests,
// and never calls a Create/Update/Delete/Put mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Verified Permissions SDK adapter for one claimed AWS
// boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsvp.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Verified Permissions policy store metadata and the policies
// and identity sources under each store visible to the configured AWS
// credentials. Cedar policy statement bodies, schema bodies, policy template
// bodies, and authorization payloads are never read.
func (c *Client) Snapshot(ctx context.Context) (vpservice.Snapshot, error) {
	stores, err := c.listPolicyStores(ctx)
	if err != nil {
		return vpservice.Snapshot{}, err
	}
	for i := range stores {
		detail, err := c.getPolicyStore(ctx, stores[i].ID)
		if err != nil {
			return vpservice.Snapshot{}, err
		}
		applyPolicyStoreDetail(&stores[i], detail)

		policies, err := c.listPolicies(ctx, stores[i].ID)
		if err != nil {
			return vpservice.Snapshot{}, err
		}
		stores[i].Policies = policies

		sources, err := c.listIdentitySources(ctx, stores[i].ID)
		if err != nil {
			return vpservice.Snapshot{}, err
		}
		stores[i].IdentitySources = sources
	}
	return vpservice.Snapshot{PolicyStores: stores}, nil
}

func (c *Client) listPolicyStores(ctx context.Context) ([]vpservice.PolicyStore, error) {
	var stores []vpservice.PolicyStore
	var nextToken *string
	for {
		var page *awsvp.ListPolicyStoresOutput
		err := c.recordAPICall(ctx, "ListPolicyStores", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListPolicyStores(callCtx, &awsvp.ListPolicyStoresInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return stores, nil
		}
		for _, item := range page.PolicyStores {
			stores = append(stores, mapPolicyStoreItem(item))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return stores, nil
		}
	}
}

func (c *Client) getPolicyStore(ctx context.Context, storeID string) (*awsvp.GetPolicyStoreOutput, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, nil
	}
	var output *awsvp.GetPolicyStoreOutput
	err := c.recordAPICall(ctx, "GetPolicyStore", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetPolicyStore(callCtx, &awsvp.GetPolicyStoreInput{
			PolicyStoreId: aws.String(storeID),
			Tags:          true,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

func (c *Client) listPolicies(ctx context.Context, storeID string) ([]vpservice.Policy, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, nil
	}
	var policies []vpservice.Policy
	var nextToken *string
	for {
		var page *awsvp.ListPoliciesOutput
		err := c.recordAPICall(ctx, "ListPolicies", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListPolicies(callCtx, &awsvp.ListPoliciesInput{
				PolicyStoreId: aws.String(storeID),
				NextToken:     nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return policies, nil
		}
		for _, item := range page.Policies {
			policies = append(policies, mapPolicyItem(item))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return policies, nil
		}
	}
}

func (c *Client) listIdentitySources(ctx context.Context, storeID string) ([]vpservice.IdentitySource, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, nil
	}
	var sources []vpservice.IdentitySource
	var nextToken *string
	for {
		var page *awsvp.ListIdentitySourcesOutput
		err := c.recordAPICall(ctx, "ListIdentitySources", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListIdentitySources(callCtx, &awsvp.ListIdentitySourcesInput{
				PolicyStoreId: aws.String(storeID),
				NextToken:     nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return sources, nil
		}
		for _, item := range page.IdentitySources {
			sources = append(sources, mapIdentitySourceItem(item))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return sources, nil
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

var _ vpservice.Client = (*Client)(nil)

var _ apiClient = (*awsvp.Client)(nil)
