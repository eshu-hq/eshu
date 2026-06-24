// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsamplify "github.com/aws/aws-sdk-go-v2/service/amplify"
	amplifytypes "github.com/aws/aws-sdk-go-v2/service/amplify/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	amplifyservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/amplify"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listPageSize bounds each Amplify list page so the scan stays metadata-sized
// regardless of how many apps, branches, or domain associations exist.
const listPageSize = 50

// apiClient is the metadata-only Amplify SDK surface the adapter consumes. It
// intentionally omits every Create, Update, Delete, Start (job/deployment),
// Generate (access logs), and webhook API. The reflection guard test asserts the
// omission, so an app's environment variables, build-spec secrets, repository
// access tokens, and basic-auth credentials cannot be mutated or read through
// this surface. ListApps, ListBranches, and ListDomainAssociations return the
// app/branch/domain structs whose secret-bearing fields the mappers below drop.
type apiClient interface {
	ListApps(context.Context, *awsamplify.ListAppsInput, ...func(*awsamplify.Options)) (*awsamplify.ListAppsOutput, error)
	ListBranches(context.Context, *awsamplify.ListBranchesInput, ...func(*awsamplify.Options)) (*awsamplify.ListBranchesOutput, error)
	ListDomainAssociations(context.Context, *awsamplify.ListDomainAssociationsInput, ...func(*awsamplify.Options)) (*awsamplify.ListDomainAssociationsOutput, error)
}

// Client adapts AWS SDK Amplify pagination into scanner-owned metadata. It drops
// every app and branch environment-variable map, build-spec body, and basic-auth
// credential, and reduces repository URLs to host and path so an embedded token
// cannot leak.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Amplify SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsamplify.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListApps reads Amplify app metadata. Environment variables, build-spec bodies,
// and basic-auth credentials are dropped by mapApp; only identity, platform,
// sanitized repository URL, IAM role ARNs, and the default domain survive.
func (c *Client) ListApps(ctx context.Context) ([]amplifyservice.App, error) {
	var apps []amplifyservice.App
	var nextToken *string
	for {
		var page *awsamplify.ListAppsOutput
		err := c.recordAPICall(ctx, "ListApps", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListApps(callCtx, &awsamplify.ListAppsInput{
				MaxResults: listPageSize,
				NextToken:  nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return apps, nil
		}
		for _, app := range page.Apps {
			apps = append(apps, mapApp(app))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return apps, nil
		}
	}
}

// ListBranches reads branch metadata for one app. Branch environment variables,
// build-spec bodies, and basic-auth credentials are dropped by mapBranch.
func (c *Client) ListBranches(ctx context.Context, appID string) ([]amplifyservice.Branch, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, nil
	}
	var branches []amplifyservice.Branch
	var nextToken *string
	for {
		var page *awsamplify.ListBranchesOutput
		err := c.recordAPICall(ctx, "ListBranches", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListBranches(callCtx, &awsamplify.ListBranchesInput{
				AppId:      aws.String(appID),
				MaxResults: listPageSize,
				NextToken:  nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return branches, nil
		}
		for _, branch := range page.Branches {
			branches = append(branches, mapBranch(appID, branch))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return branches, nil
		}
	}
}

// ListDomainAssociations reads custom-domain association metadata for one app,
// including subdomain DNS records. Certificate bodies and verification secret
// material are dropped by mapDomainAssociation.
func (c *Client) ListDomainAssociations(ctx context.Context, appID string) ([]amplifyservice.DomainAssociation, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, nil
	}
	var domains []amplifyservice.DomainAssociation
	var nextToken *string
	for {
		var page *awsamplify.ListDomainAssociationsOutput
		err := c.recordAPICall(ctx, "ListDomainAssociations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListDomainAssociations(callCtx, &awsamplify.ListDomainAssociationsInput{
				AppId:      aws.String(appID),
				MaxResults: listPageSize,
				NextToken:  nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return domains, nil
		}
		for _, domain := range page.DomainAssociations {
			domains = append(domains, mapDomainAssociation(appID, domain))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return domains, nil
		}
	}
}

func mapApp(app amplifytypes.App) amplifyservice.App {
	mapped := amplifyservice.App{
		ID:                    strings.TrimSpace(aws.ToString(app.AppId)),
		ARN:                   strings.TrimSpace(aws.ToString(app.AppArn)),
		Name:                  strings.TrimSpace(aws.ToString(app.Name)),
		Platform:              strings.TrimSpace(string(app.Platform)),
		RepositoryURL:         amplifyservice.SanitizeRepositoryURL(aws.ToString(app.Repository)),
		RepositoryCloneMethod: strings.TrimSpace(string(app.RepositoryCloneMethod)),
		DefaultDomain:         strings.TrimSpace(aws.ToString(app.DefaultDomain)),
		ServiceRoleARN:        strings.TrimSpace(aws.ToString(app.IamServiceRoleArn)),
		ComputeRoleARN:        strings.TrimSpace(aws.ToString(app.ComputeRoleArn)),
		CreateTime:            aws.ToTime(app.CreateTime),
		UpdateTime:            aws.ToTime(app.UpdateTime),
		Tags:                  cloneStringMap(app.Tags),
	}
	if app.ProductionBranch != nil {
		mapped.ProductionBranchName = strings.TrimSpace(aws.ToString(app.ProductionBranch.BranchName))
	}
	return mapped
}

func mapBranch(appID string, branch amplifytypes.Branch) amplifyservice.Branch {
	return amplifyservice.Branch{
		AppID:             strings.TrimSpace(appID),
		Name:              strings.TrimSpace(aws.ToString(branch.BranchName)),
		ARN:               strings.TrimSpace(aws.ToString(branch.BranchArn)),
		DisplayName:       strings.TrimSpace(aws.ToString(branch.DisplayName)),
		Stage:             strings.TrimSpace(string(branch.Stage)),
		Framework:         strings.TrimSpace(aws.ToString(branch.Framework)),
		EnableAutoBuild:   aws.ToBool(branch.EnableAutoBuild),
		ComputeRoleARN:    strings.TrimSpace(aws.ToString(branch.ComputeRoleArn)),
		CustomDomainCount: len(branch.CustomDomains),
		CreateTime:        aws.ToTime(branch.CreateTime),
		UpdateTime:        aws.ToTime(branch.UpdateTime),
		Tags:              cloneStringMap(branch.Tags),
	}
}

func mapDomainAssociation(appID string, domain amplifytypes.DomainAssociation) amplifyservice.DomainAssociation {
	mapped := amplifyservice.DomainAssociation{
		AppID:      strings.TrimSpace(appID),
		ARN:        strings.TrimSpace(aws.ToString(domain.DomainAssociationArn)),
		DomainName: strings.TrimSpace(aws.ToString(domain.DomainName)),
		Status:     strings.TrimSpace(string(domain.DomainStatus)),
	}
	for _, sub := range domain.SubDomains {
		entry := amplifyservice.SubDomain{
			DNSRecord: strings.TrimSpace(aws.ToString(sub.DnsRecord)),
			Verified:  aws.ToBool(sub.Verified),
		}
		if sub.SubDomainSetting != nil {
			entry.Prefix = strings.TrimSpace(aws.ToString(sub.SubDomainSetting.Prefix))
			entry.BranchName = strings.TrimSpace(aws.ToString(sub.SubDomainSetting.BranchName))
		}
		mapped.SubDomains = append(mapped.SubDomains, entry)
	}
	return mapped
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
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

var _ amplifyservice.Client = (*Client)(nil)

var _ apiClient = (*awsamplify.Client)(nil)
