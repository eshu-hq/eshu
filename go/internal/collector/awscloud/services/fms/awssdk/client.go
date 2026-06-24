// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfms "github.com/aws/aws-sdk-go-v2/service/fms"
	awsfmstypes "github.com/aws/aws-sdk-go-v2/service/fms/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	fmsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/fms"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listLimit bounds each Firewall Manager list page. The FMS list APIs are not
// standard smithy paginators, so the adapter loops on NextToken explicitly.
const listLimit int32 = 100

// apiClient is the read-only Firewall Manager surface the adapter consumes. It
// exposes only List operations that return policy and member-account metadata.
// A reflection gate in exclusion_test.go fails the build path if any mutation
// or rule-payload read method (PutPolicy, DeletePolicy, GetPolicy, and the
// other rule-body or mutation operations) is added here.
type apiClient interface {
	ListPolicies(context.Context, *awsfms.ListPoliciesInput, ...func(*awsfms.Options)) (*awsfms.ListPoliciesOutput, error)
	ListComplianceStatus(context.Context, *awsfms.ListComplianceStatusInput, ...func(*awsfms.Options)) (*awsfms.ListComplianceStatusOutput, error)
}

// Client adapts AWS SDK for Go v2 Firewall Manager reads into scanner-owned
// metadata. It never reads or persists policy rule payloads (the
// SecurityServicePolicyData managed service data document), and it never calls
// an FMS mutation API. GetPolicy is intentionally not on the read surface so
// the rule payload is unreachable by construction; ListPolicies already returns
// every policy field the scanner records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Firewall Manager SDK adapter for one claimed FMS
// administrator boundary. FMS is an organization-wide control plane, so the
// claim region selects the FMS endpoint; the adapter does not rebind the
// region.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsfms.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListPolicies returns the administrator account's Firewall Manager policy
// summaries. Each summary carries the governing security service type, the
// in-scope resource type label, and the remediation flag; the policy rule body
// is never read.
func (c *Client) ListPolicies(ctx context.Context) ([]fmsservice.Policy, error) {
	var policies []fmsservice.Policy
	var token *string
	for {
		var page *awsfms.ListPoliciesOutput
		err := c.recordAPICall(ctx, "ListPolicies", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListPolicies(callCtx, &awsfms.ListPoliciesInput{
				MaxResults: aws.Int32(listLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return policies, nil
		}
		for _, summary := range page.PolicyList {
			policies = append(policies, mapPolicy(summary))
		}
		if token = nextToken(page.NextToken); token == nil {
			return policies, nil
		}
	}
}

// ListPolicyMemberAccounts returns the bare 12-digit Organizations member
// account ids a policy is evaluated against, resolved from the policy
// compliance status. The account ids are deduplicated; the scanner sorts them
// so the relationship identity is stable across generations.
func (c *Client) ListPolicyMemberAccounts(ctx context.Context, policyID string) ([]string, error) {
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	var accounts []string
	var token *string
	for {
		var page *awsfms.ListComplianceStatusOutput
		err := c.recordAPICall(ctx, "ListComplianceStatus", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListComplianceStatus(callCtx, &awsfms.ListComplianceStatusInput{
				PolicyId:   aws.String(policyID),
				MaxResults: aws.Int32(listLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return accounts, nil
		}
		for _, status := range page.PolicyComplianceStatusList {
			account := strings.TrimSpace(aws.ToString(status.MemberAccount))
			if account == "" {
				continue
			}
			if _, ok := seen[account]; ok {
				continue
			}
			seen[account] = struct{}{}
			accounts = append(accounts, account)
		}
		if token = nextToken(page.NextToken); token == nil {
			return accounts, nil
		}
	}
}

// mapPolicy converts one FMS PolicySummary into the scanner-owned Policy. Only
// metadata is copied; the policy rule payload is never part of PolicySummary
// and is never requested.
func mapPolicy(summary awsfmstypes.PolicySummary) fmsservice.Policy {
	return fmsservice.Policy{
		ARN:                            strings.TrimSpace(aws.ToString(summary.PolicyArn)),
		ID:                             strings.TrimSpace(aws.ToString(summary.PolicyId)),
		Name:                           strings.TrimSpace(aws.ToString(summary.PolicyName)),
		SecurityServiceType:            string(summary.SecurityServiceType),
		ResourceType:                   strings.TrimSpace(aws.ToString(summary.ResourceType)),
		RemediationEnabled:             summary.RemediationEnabled,
		DeleteUnusedFMManagedResources: summary.DeleteUnusedFMManagedResources,
		PolicyStatus:                   string(summary.PolicyStatus),
	}
}

func nextToken(token *string) *string {
	if aws.ToString(token) == "" {
		return nil
	}
	return token
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
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := strings.ToLower(apiErr.ErrorCode())
	return strings.Contains(code, "throttl") || strings.Contains(code, "rate")
}

var _ fmsservice.Client = (*Client)(nil)
