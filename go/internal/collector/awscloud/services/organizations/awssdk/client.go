// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsorg "github.com/aws/aws-sdk-go-v2/service/organizations"
	awsorgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	organizationsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/organizations"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// OrganizationsEndpointRegion is the AWS Organizations control-plane endpoint
// region used for commercial AWS accounts.
const OrganizationsEndpointRegion = "us-east-1"

type apiClient interface {
	DescribeOrganization(context.Context, *awsorg.DescribeOrganizationInput, ...func(*awsorg.Options)) (*awsorg.DescribeOrganizationOutput, error)
	ListRoots(context.Context, *awsorg.ListRootsInput, ...func(*awsorg.Options)) (*awsorg.ListRootsOutput, error)
	ListOrganizationalUnitsForParent(context.Context, *awsorg.ListOrganizationalUnitsForParentInput, ...func(*awsorg.Options)) (*awsorg.ListOrganizationalUnitsForParentOutput, error)
	ListAccountsForParent(context.Context, *awsorg.ListAccountsForParentInput, ...func(*awsorg.Options)) (*awsorg.ListAccountsForParentOutput, error)
	ListPolicies(context.Context, *awsorg.ListPoliciesInput, ...func(*awsorg.Options)) (*awsorg.ListPoliciesOutput, error)
	ListTargetsForPolicy(context.Context, *awsorg.ListTargetsForPolicyInput, ...func(*awsorg.Options)) (*awsorg.ListTargetsForPolicyOutput, error)
	ListDelegatedAdministrators(context.Context, *awsorg.ListDelegatedAdministratorsInput, ...func(*awsorg.Options)) (*awsorg.ListDelegatedAdministratorsOutput, error)
	ListDelegatedServicesForAccount(context.Context, *awsorg.ListDelegatedServicesForAccountInput, ...func(*awsorg.Options)) (*awsorg.ListDelegatedServicesForAccountOutput, error)
	ListTagsForResource(context.Context, *awsorg.ListTagsForResourceInput, ...func(*awsorg.Options)) (*awsorg.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Organizations pagination into scanner-owned metadata.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
	region      string
}

// NewClient builds an Organizations SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	config.Region = OrganizationsEndpointRegion
	return &Client{
		client:      awsorg.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
		region:      config.Region,
	}
}

// Snapshot returns Organizations metadata visible to management or
// delegated-administrator credentials. Policy document bodies are never read.
func (c *Client) Snapshot(ctx context.Context) (organizationsservice.Snapshot, error) {
	organization, err := c.describeOrganization(ctx)
	if err != nil {
		if isOrgAccessSkipError(err) {
			return c.skippedSnapshot(skipReason(err)), nil
		}
		return organizationsservice.Snapshot{}, err
	}
	roots, err := c.listRoots(ctx)
	if err != nil {
		if isOrgAccessSkipError(err) {
			return c.skippedSnapshot(skipReason(err)), nil
		}
		return organizationsservice.Snapshot{}, err
	}
	snapshot := organizationsservice.Snapshot{
		Organization: organization,
		Roots:        roots,
	}
	for _, root := range roots {
		ous, accounts, err := c.childrenForParent(ctx, root.ID)
		if err != nil {
			return organizationsservice.Snapshot{}, err
		}
		snapshot.OrganizationalUnits = append(snapshot.OrganizationalUnits, ous...)
		snapshot.Accounts = append(snapshot.Accounts, accounts...)
	}
	policies, err := c.listPolicySummaries(ctx)
	if err != nil {
		return organizationsservice.Snapshot{}, err
	}
	snapshot.Policies = policies
	admins, err := c.listDelegatedAdministrators(ctx)
	if err != nil {
		return organizationsservice.Snapshot{}, err
	}
	snapshot.DelegatedAdministrators = admins
	return snapshot, nil
}

func (c *Client) describeOrganization(ctx context.Context) (organizationsservice.Organization, error) {
	var output *awsorg.DescribeOrganizationOutput
	err := c.recordAPICall(ctx, "DescribeOrganization", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeOrganization(callCtx, &awsorg.DescribeOrganizationInput{})
		return err
	})
	if err != nil {
		return organizationsservice.Organization{}, err
	}
	if output == nil || output.Organization == nil {
		return organizationsservice.Organization{}, nil
	}
	org := output.Organization
	return organizationsservice.Organization{
		ARN:               strings.TrimSpace(aws.ToString(org.Arn)),
		ID:                strings.TrimSpace(aws.ToString(org.Id)),
		ManagementAccount: strings.TrimSpace(aws.ToString(org.MasterAccountId)),
		FeatureSet:        strings.TrimSpace(string(org.FeatureSet)),
	}, nil
}

func (c *Client) listRoots(ctx context.Context) ([]organizationsservice.Root, error) {
	var roots []organizationsservice.Root
	var nextToken *string
	for {
		var output *awsorg.ListRootsOutput
		err := c.recordAPICall(ctx, "ListRoots", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListRoots(callCtx, &awsorg.ListRootsInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return roots, nil
		}
		for _, root := range output.Roots {
			mapped, err := c.mapRoot(ctx, root)
			if err != nil {
				return nil, err
			}
			roots = append(roots, mapped)
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return roots, nil
		}
	}
}

func (c *Client) mapRoot(ctx context.Context, root awsorgtypes.Root) (organizationsservice.Root, error) {
	rootID := aws.ToString(root.Id)
	tags, err := c.listTags(ctx, rootID)
	if err != nil {
		return organizationsservice.Root{}, err
	}
	return organizationsservice.Root{
		ARN:         strings.TrimSpace(aws.ToString(root.Arn)),
		ID:          strings.TrimSpace(rootID),
		Name:        strings.TrimSpace(aws.ToString(root.Name)),
		PolicyTypes: policyTypeSummaries(root.PolicyTypes),
		Tags:        tags,
	}, nil
}

func (c *Client) childrenForParent(
	ctx context.Context,
	parentID string,
) ([]organizationsservice.OrganizationalUnit, []organizationsservice.Account, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil, nil, nil
	}
	ous, err := c.listOUsForParent(ctx, parentID)
	if err != nil {
		return nil, nil, err
	}
	accounts, err := c.listAccountsForParent(ctx, parentID)
	if err != nil {
		return nil, nil, err
	}
	for _, ou := range ous {
		childOUs, childAccounts, err := c.childrenForParent(ctx, ou.ID)
		if err != nil {
			return nil, nil, err
		}
		ous = append(ous, childOUs...)
		accounts = append(accounts, childAccounts...)
	}
	return ous, accounts, nil
}

func (c *Client) listOUsForParent(
	ctx context.Context,
	parentID string,
) ([]organizationsservice.OrganizationalUnit, error) {
	var ous []organizationsservice.OrganizationalUnit
	var nextToken *string
	for {
		var output *awsorg.ListOrganizationalUnitsForParentOutput
		err := c.recordAPICall(ctx, "ListOrganizationalUnitsForParent", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListOrganizationalUnitsForParent(callCtx, &awsorg.ListOrganizationalUnitsForParentInput{
				NextToken: nextToken,
				ParentId:  aws.String(parentID),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return ous, nil
		}
		for _, ou := range output.OrganizationalUnits {
			mapped, err := c.mapOU(ctx, parentID, ou)
			if err != nil {
				return nil, err
			}
			ous = append(ous, mapped)
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return ous, nil
		}
	}
}

func (c *Client) mapOU(
	ctx context.Context,
	parentID string,
	ou awsorgtypes.OrganizationalUnit,
) (organizationsservice.OrganizationalUnit, error) {
	ouID := aws.ToString(ou.Id)
	tags, err := c.listTags(ctx, ouID)
	if err != nil {
		return organizationsservice.OrganizationalUnit{}, err
	}
	return organizationsservice.OrganizationalUnit{
		ARN:      strings.TrimSpace(aws.ToString(ou.Arn)),
		ID:       strings.TrimSpace(ouID),
		Name:     strings.TrimSpace(aws.ToString(ou.Name)),
		ParentID: strings.TrimSpace(parentID),
		Tags:     tags,
	}, nil
}

func (c *Client) listAccountsForParent(ctx context.Context, parentID string) ([]organizationsservice.Account, error) {
	var accounts []organizationsservice.Account
	var nextToken *string
	for {
		var output *awsorg.ListAccountsForParentOutput
		err := c.recordAPICall(ctx, "ListAccountsForParent", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListAccountsForParent(callCtx, &awsorg.ListAccountsForParentInput{
				NextToken: nextToken,
				ParentId:  aws.String(parentID),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return accounts, nil
		}
		for _, account := range output.Accounts {
			mapped, err := c.mapAccount(ctx, parentID, account)
			if err != nil {
				return nil, err
			}
			accounts = append(accounts, mapped)
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return accounts, nil
		}
	}
}

func (c *Client) mapAccount(
	ctx context.Context,
	parentID string,
	account awsorgtypes.Account,
) (organizationsservice.Account, error) {
	accountID := aws.ToString(account.Id)
	tags, err := c.listTags(ctx, accountID)
	if err != nil {
		return organizationsservice.Account{}, err
	}
	return organizationsservice.Account{
		ARN:       strings.TrimSpace(aws.ToString(account.Arn)),
		Email:     strings.TrimSpace(aws.ToString(account.Email)),
		ID:        strings.TrimSpace(accountID),
		JoinedAt:  aws.ToTime(account.JoinedTimestamp),
		JoinedVia: strings.TrimSpace(string(account.JoinedMethod)),
		Name:      strings.TrimSpace(aws.ToString(account.Name)),
		ParentID:  strings.TrimSpace(parentID),
		State:     strings.TrimSpace(string(account.State)),
		Status:    strings.TrimSpace(string(account.Status)),
		Tags:      tags,
	}, nil
}

var _ organizationsservice.Client = (*Client)(nil)

var _ apiClient = (*awsorg.Client)(nil)
