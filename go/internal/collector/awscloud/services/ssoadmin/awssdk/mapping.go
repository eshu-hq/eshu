// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssoadmin "github.com/aws/aws-sdk-go-v2/service/ssoadmin"
	awsssoadmintypes "github.com/aws/aws-sdk-go-v2/service/ssoadmin/types"

	ssoadminservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ssoadmin"
)

func (c *Client) listInstances(ctx context.Context) ([]ssoadminservice.Instance, error) {
	paginator := awsssoadmin.NewListInstancesPaginator(c.ssoAdmin, &awsssoadmin.ListInstancesInput{})
	var instances []ssoadminservice.Instance
	for paginator.HasMorePages() {
		var page *awsssoadmin.ListInstancesOutput
		err := c.recordAPICall(ctx, "ListInstances", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, instance := range page.Instances {
			arn := strings.TrimSpace(aws.ToString(instance.InstanceArn))
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			instances = append(instances, ssoadminservice.Instance{
				ARN:             arn,
				IdentityStoreID: strings.TrimSpace(aws.ToString(instance.IdentityStoreId)),
				Name:            strings.TrimSpace(aws.ToString(instance.Name)),
				OwnerAccountID:  strings.TrimSpace(aws.ToString(instance.OwnerAccountId)),
				Status:          strings.TrimSpace(string(instance.Status)),
				CreatedAt:       aws.ToTime(instance.CreatedDate),
				Tags:            tags,
			})
		}
	}
	return instances, nil
}

func (c *Client) listPermissionSets(ctx context.Context, instanceARN string) ([]ssoadminservice.PermissionSet, error) {
	paginator := awsssoadmin.NewListPermissionSetsPaginator(c.ssoAdmin, &awsssoadmin.ListPermissionSetsInput{
		InstanceArn: aws.String(instanceARN),
	})
	var permSets []ssoadminservice.PermissionSet
	for paginator.HasMorePages() {
		var page *awsssoadmin.ListPermissionSetsOutput
		err := c.recordAPICall(ctx, "ListPermissionSets", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, permSetARN := range page.PermissionSets {
			permSet, err := c.describePermissionSet(ctx, instanceARN, permSetARN)
			if err != nil {
				return nil, err
			}
			permSets = append(permSets, permSet)
		}
	}
	return permSets, nil
}

func (c *Client) describePermissionSet(
	ctx context.Context,
	instanceARN string,
	permSetARN string,
) (ssoadminservice.PermissionSet, error) {
	var output *awsssoadmin.DescribePermissionSetOutput
	err := c.recordAPICall(ctx, "DescribePermissionSet", func(callCtx context.Context) error {
		var err error
		output, err = c.ssoAdmin.DescribePermissionSet(callCtx, &awsssoadmin.DescribePermissionSetInput{
			InstanceArn:      aws.String(instanceARN),
			PermissionSetArn: aws.String(permSetARN),
		})
		return err
	})
	if err != nil {
		return ssoadminservice.PermissionSet{}, err
	}
	permSet := ssoadminservice.PermissionSet{
		ARN:         strings.TrimSpace(permSetARN),
		InstanceARN: strings.TrimSpace(instanceARN),
	}
	if output != nil && output.PermissionSet != nil {
		ps := output.PermissionSet
		permSet.Name = strings.TrimSpace(aws.ToString(ps.Name))
		permSet.Description = strings.TrimSpace(aws.ToString(ps.Description))
		permSet.SessionDuration = strings.TrimSpace(aws.ToString(ps.SessionDuration))
		permSet.RelayState = strings.TrimSpace(aws.ToString(ps.RelayState))
		permSet.CreatedAt = aws.ToTime(ps.CreatedDate)
	}
	managed, err := c.listManagedPolicies(ctx, instanceARN, permSetARN)
	if err != nil {
		return ssoadminservice.PermissionSet{}, err
	}
	permSet.ManagedPolicies = managed
	customer, err := c.listCustomerManagedPolicies(ctx, instanceARN, permSetARN)
	if err != nil {
		return ssoadminservice.PermissionSet{}, err
	}
	permSet.CustomerManagedPolicies = customer
	return permSet, nil
}

func (c *Client) listManagedPolicies(
	ctx context.Context,
	instanceARN string,
	permSetARN string,
) ([]ssoadminservice.ManagedPolicyReference, error) {
	paginator := awsssoadmin.NewListManagedPoliciesInPermissionSetPaginator(c.ssoAdmin, &awsssoadmin.ListManagedPoliciesInPermissionSetInput{
		InstanceArn:      aws.String(instanceARN),
		PermissionSetArn: aws.String(permSetARN),
	})
	var managed []ssoadminservice.ManagedPolicyReference
	for paginator.HasMorePages() {
		var page *awsssoadmin.ListManagedPoliciesInPermissionSetOutput
		err := c.recordAPICall(ctx, "ListManagedPoliciesInPermissionSet", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, policy := range page.AttachedManagedPolicies {
			managed = append(managed, ssoadminservice.ManagedPolicyReference{
				ARN:  strings.TrimSpace(aws.ToString(policy.Arn)),
				Name: strings.TrimSpace(aws.ToString(policy.Name)),
			})
		}
	}
	return managed, nil
}

func (c *Client) listCustomerManagedPolicies(
	ctx context.Context,
	instanceARN string,
	permSetARN string,
) ([]ssoadminservice.CustomerManagedPolicyReference, error) {
	paginator := awsssoadmin.NewListCustomerManagedPolicyReferencesInPermissionSetPaginator(c.ssoAdmin, &awsssoadmin.ListCustomerManagedPolicyReferencesInPermissionSetInput{
		InstanceArn:      aws.String(instanceARN),
		PermissionSetArn: aws.String(permSetARN),
	})
	var references []ssoadminservice.CustomerManagedPolicyReference
	for paginator.HasMorePages() {
		var page *awsssoadmin.ListCustomerManagedPolicyReferencesInPermissionSetOutput
		err := c.recordAPICall(ctx, "ListCustomerManagedPolicyReferencesInPermissionSet", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		// Only the policy name and path are read here. The IAM policy body lives
		// in IAM and is never fetched.
		for _, reference := range page.CustomerManagedPolicyReferences {
			references = append(references, ssoadminservice.CustomerManagedPolicyReference{
				Name: strings.TrimSpace(aws.ToString(reference.Name)),
				Path: strings.TrimSpace(aws.ToString(reference.Path)),
			})
		}
	}
	return references, nil
}

func (c *Client) listAssignments(
	ctx context.Context,
	instanceARN string,
	permSets []ssoadminservice.PermissionSet,
) ([]ssoadminservice.AccountAssignment, error) {
	var assignments []ssoadminservice.AccountAssignment
	for _, permSet := range permSets {
		accounts, err := c.listProvisionedAccounts(ctx, instanceARN, permSet.ARN)
		if err != nil {
			return nil, err
		}
		for _, accountID := range accounts {
			accountAssignments, err := c.listAccountAssignments(ctx, instanceARN, permSet.ARN, accountID)
			if err != nil {
				return nil, err
			}
			assignments = append(assignments, accountAssignments...)
		}
	}
	return assignments, nil
}

func (c *Client) listProvisionedAccounts(
	ctx context.Context,
	instanceARN string,
	permSetARN string,
) ([]string, error) {
	var accounts []string
	var nextToken *string
	for {
		var output *awsssoadmin.ListAccountsForProvisionedPermissionSetOutput
		err := c.recordAPICall(ctx, "ListAccountsForProvisionedPermissionSet", func(callCtx context.Context) error {
			var err error
			output, err = c.ssoAdmin.ListAccountsForProvisionedPermissionSet(callCtx, &awsssoadmin.ListAccountsForProvisionedPermissionSetInput{
				InstanceArn:      aws.String(instanceARN),
				PermissionSetArn: aws.String(permSetARN),
				NextToken:        nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return accounts, nil
		}
		for _, accountID := range output.AccountIds {
			accounts = append(accounts, strings.TrimSpace(accountID))
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return accounts, nil
		}
	}
}

func (c *Client) listAccountAssignments(
	ctx context.Context,
	instanceARN string,
	permSetARN string,
	accountID string,
) ([]ssoadminservice.AccountAssignment, error) {
	paginator := awsssoadmin.NewListAccountAssignmentsPaginator(c.ssoAdmin, &awsssoadmin.ListAccountAssignmentsInput{
		InstanceArn:      aws.String(instanceARN),
		PermissionSetArn: aws.String(permSetARN),
		AccountId:        aws.String(accountID),
	})
	var assignments []ssoadminservice.AccountAssignment
	for paginator.HasMorePages() {
		var page *awsssoadmin.ListAccountAssignmentsOutput
		err := c.recordAPICall(ctx, "ListAccountAssignments", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, assignment := range page.AccountAssignments {
			assignments = append(assignments, ssoadminservice.AccountAssignment{
				InstanceARN:      strings.TrimSpace(instanceARN),
				PermissionSetARN: strings.TrimSpace(aws.ToString(assignment.PermissionSetArn)),
				AccountID:        strings.TrimSpace(aws.ToString(assignment.AccountId)),
				PrincipalID:      strings.TrimSpace(aws.ToString(assignment.PrincipalId)),
				PrincipalType:    strings.TrimSpace(string(assignment.PrincipalType)),
			})
		}
	}
	return assignments, nil
}

func (c *Client) listTrustedTokenIssuers(
	ctx context.Context,
	instanceARN string,
) ([]ssoadminservice.TrustedTokenIssuer, error) {
	paginator := awsssoadmin.NewListTrustedTokenIssuersPaginator(c.ssoAdmin, &awsssoadmin.ListTrustedTokenIssuersInput{
		InstanceArn: aws.String(instanceARN),
	})
	var issuers []ssoadminservice.TrustedTokenIssuer
	for paginator.HasMorePages() {
		var page *awsssoadmin.ListTrustedTokenIssuersOutput
		err := c.recordAPICall(ctx, "ListTrustedTokenIssuers", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, issuer := range page.TrustedTokenIssuers {
			issuers = append(issuers, ssoadminservice.TrustedTokenIssuer{
				ARN:         strings.TrimSpace(aws.ToString(issuer.TrustedTokenIssuerArn)),
				InstanceARN: strings.TrimSpace(instanceARN),
				Name:        strings.TrimSpace(aws.ToString(issuer.Name)),
				Type:        strings.TrimSpace(string(issuer.TrustedTokenIssuerType)),
			})
		}
	}
	return issuers, nil
}

func (c *Client) listApplications(
	ctx context.Context,
	instanceARN string,
) ([]ssoadminservice.Application, error) {
	paginator := awsssoadmin.NewListApplicationsPaginator(c.ssoAdmin, &awsssoadmin.ListApplicationsInput{
		InstanceArn: aws.String(instanceARN),
	})
	var applications []ssoadminservice.Application
	for paginator.HasMorePages() {
		var page *awsssoadmin.ListApplicationsOutput
		err := c.recordAPICall(ctx, "ListApplications", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, application := range page.Applications {
			applications = append(applications, mapApplication(instanceARN, application))
		}
	}
	return applications, nil
}

func mapApplication(instanceARN string, application awsssoadmintypes.Application) ssoadminservice.Application {
	// Application access-scope attributes (group filters) are intentionally not
	// read or mapped. Only inventory metadata is captured.
	return ssoadminservice.Application{
		ARN:                    strings.TrimSpace(aws.ToString(application.ApplicationArn)),
		InstanceARN:            strings.TrimSpace(instanceARN),
		Name:                   strings.TrimSpace(aws.ToString(application.Name)),
		Description:            strings.TrimSpace(aws.ToString(application.Description)),
		ApplicationAccountID:   strings.TrimSpace(aws.ToString(application.ApplicationAccount)),
		ApplicationProviderARN: strings.TrimSpace(aws.ToString(application.ApplicationProviderArn)),
		IdentityStoreARN:       strings.TrimSpace(aws.ToString(application.IdentityStoreArn)),
		Status:                 strings.TrimSpace(string(application.Status)),
		PortalVisibility:       portalVisibility(application.PortalOptions),
		CreatedAt:              aws.ToTime(application.CreatedDate),
	}
}

func portalVisibility(options *awsssoadmintypes.PortalOptions) string {
	if options == nil {
		return ""
	}
	return strings.TrimSpace(string(options.Visibility))
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsssoadmin.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.ssoAdmin.ListTagsForResource(callCtx, &awsssoadmin.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil {
		// Tags are best-effort metadata. A tag access error must not fail the
		// whole snapshot; the caller proceeds without tags.
		if isAccessSkipError(err) {
			return nil, nil
		}
		return nil, err
	}
	if output == nil || len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for _, tag := range output.Tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}
