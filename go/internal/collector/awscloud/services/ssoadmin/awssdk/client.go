// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsidentitystore "github.com/aws/aws-sdk-go-v2/service/identitystore"
	awsssoadmin "github.com/aws/aws-sdk-go-v2/service/ssoadmin"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	ssoadminservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ssoadmin"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SSOAdminEndpointRegion is the AWS region used for the sso-admin and
// identitystore control planes. Identity Center is org-scoped and runs against
// the management or a delegated-administrator account in the home region.
const SSOAdminEndpointRegion = "us-east-1"

// ssoAdminAPI is the metadata-only sso-admin read surface the adapter consumes.
// It deliberately omits GetInlinePolicyForPermissionSet,
// GetPermissionsBoundaryForPermissionSet, GetApplicationAccessScope,
// ListApplicationAccessScopes, and every mutation API.
type ssoAdminAPI interface {
	ListInstances(context.Context, *awsssoadmin.ListInstancesInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.ListInstancesOutput, error)
	ListPermissionSets(context.Context, *awsssoadmin.ListPermissionSetsInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.ListPermissionSetsOutput, error)
	DescribePermissionSet(context.Context, *awsssoadmin.DescribePermissionSetInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.DescribePermissionSetOutput, error)
	ListManagedPoliciesInPermissionSet(context.Context, *awsssoadmin.ListManagedPoliciesInPermissionSetInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.ListManagedPoliciesInPermissionSetOutput, error)
	ListCustomerManagedPolicyReferencesInPermissionSet(context.Context, *awsssoadmin.ListCustomerManagedPolicyReferencesInPermissionSetInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.ListCustomerManagedPolicyReferencesInPermissionSetOutput, error)
	ListAccountsForProvisionedPermissionSet(context.Context, *awsssoadmin.ListAccountsForProvisionedPermissionSetInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.ListAccountsForProvisionedPermissionSetOutput, error)
	ListAccountAssignments(context.Context, *awsssoadmin.ListAccountAssignmentsInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.ListAccountAssignmentsOutput, error)
	ListApplications(context.Context, *awsssoadmin.ListApplicationsInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.ListApplicationsOutput, error)
	ListTrustedTokenIssuers(context.Context, *awsssoadmin.ListTrustedTokenIssuersInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.ListTrustedTokenIssuersOutput, error)
	ListTagsForResource(context.Context, *awsssoadmin.ListTagsForResourceInput, ...func(*awsssoadmin.Options)) (*awsssoadmin.ListTagsForResourceOutput, error)
}

// identityStoreAPI is the metadata-only identitystore read surface the adapter
// consumes. It resolves principals to a display name only; it never lists
// memberships or reads structured identity attributes.
type identityStoreAPI interface {
	DescribeGroup(context.Context, *awsidentitystore.DescribeGroupInput, ...func(*awsidentitystore.Options)) (*awsidentitystore.DescribeGroupOutput, error)
	DescribeUser(context.Context, *awsidentitystore.DescribeUserInput, ...func(*awsidentitystore.Options)) (*awsidentitystore.DescribeUserOutput, error)
}

// Client adapts AWS sso-admin and identitystore reads into the scanner-owned
// Identity Center metadata snapshot.
type Client struct {
	ssoAdmin      ssoAdminAPI
	identityStore identityStoreAPI
	boundary      awscloud.Boundary
	tracer        trace.Tracer
	instruments   *telemetry.Instruments
}

// NewClient builds an Identity Center SDK adapter for one claimed AWS boundary.
// The sso-admin and identitystore clients use the org control-plane region.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	config.Region = SSOAdminEndpointRegion
	return &Client{
		ssoAdmin:      awsssoadmin.NewFromConfig(config),
		identityStore: awsidentitystore.NewFromConfig(config),
		boundary:      boundary,
		tracer:        tracer,
		instruments:   instruments,
	}
}

// Snapshot returns Identity Center metadata visible to org-aware credentials.
// Permission set inline policy bodies, permissions boundary bodies, and
// application access-scope filters are never read. An account with no Identity
// Center instance emits a warning rather than an error.
func (c *Client) Snapshot(ctx context.Context) (ssoadminservice.Snapshot, error) {
	instances, err := c.listInstances(ctx)
	if err != nil {
		if isAccessSkipError(err) {
			return skippedSnapshot(c.boundary, "identitycenter_access_skipped", skipReason(err)), nil
		}
		return ssoadminservice.Snapshot{}, err
	}
	if len(instances) == 0 {
		return skippedSnapshot(c.boundary, "identitycenter_no_instance", "no IAM Identity Center instance in this account/region"), nil
	}

	var snapshot ssoadminservice.Snapshot
	principals := newPrincipalSet()
	for i := range instances {
		instance := &instances[i]
		if err := c.populateInstance(ctx, instance, principals); err != nil {
			return ssoadminservice.Snapshot{}, err
		}
		apps, err := c.listApplications(ctx, instance.ARN)
		if err != nil {
			return ssoadminservice.Snapshot{}, err
		}
		snapshot.Applications = append(snapshot.Applications, apps...)
	}
	snapshot.Instances = instances
	resolved, err := c.resolvePrincipals(ctx, instances, principals)
	if err != nil {
		return ssoadminservice.Snapshot{}, err
	}
	snapshot.Principals = resolved
	return snapshot, nil
}

func (c *Client) populateInstance(
	ctx context.Context,
	instance *ssoadminservice.Instance,
	principals *principalSet,
) error {
	permSets, err := c.listPermissionSets(ctx, instance.ARN)
	if err != nil {
		return err
	}
	instance.PermissionSets = permSets
	assignments, err := c.listAssignments(ctx, instance.ARN, permSets)
	if err != nil {
		return err
	}
	instance.AccountAssignments = assignments
	for _, assignment := range assignments {
		principals.add(assignment.PrincipalType, assignment.PrincipalID)
	}
	issuers, err := c.listTrustedTokenIssuers(ctx, instance.ARN)
	if err != nil {
		return err
	}
	instance.TrustedTokenIssuers = issuers
	return nil
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

// isAccessSkipError reports whether the error is an org-access or
// not-enabled condition that should produce a warning rather than fail the
// whole claim.
func isAccessSkipError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "AccessDeniedException", "AccessDenied", "UnauthorizedException", "ForbiddenException":
		return true
	default:
		return false
	}
}

func skipReason(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return strings.TrimSpace(apiErr.ErrorCode())
	}
	return strings.TrimSpace(err.Error())
}

func skippedSnapshot(boundary awscloud.Boundary, warningKind, message string) ssoadminservice.Snapshot {
	return ssoadminservice.Snapshot{
		Warnings: []awscloud.WarningObservation{{
			Boundary:    boundary,
			WarningKind: warningKind,
			ErrorClass:  "skip",
			Message:     message,
		}},
	}
}

var (
	_ ssoadminservice.Client = (*Client)(nil)
	_ ssoAdminAPI            = (*awsssoadmin.Client)(nil)
	_ identityStoreAPI       = (*awsidentitystore.Client)(nil)
)
