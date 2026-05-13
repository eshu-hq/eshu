package awssdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	awsiamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Client adapts AWS SDK IAM pagination into scanner-owned IAM records.
type Client struct {
	client      *awsiam.Client
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an IAM SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsiam.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListRoles returns all IAM roles visible to the configured AWS credentials.
func (c *Client) ListRoles(ctx context.Context) ([]iamservice.Role, error) {
	paginator := awsiam.NewListRolesPaginator(c.client, &awsiam.ListRolesInput{})
	var roles []iamservice.Role
	for paginator.HasMorePages() {
		var page *awsiam.ListRolesOutput
		err := c.recordAPICall(ctx, "ListRoles", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, role := range page.Roles {
			mapped, err := c.mapRole(ctx, role)
			if err != nil {
				return nil, err
			}
			roles = append(roles, mapped)
		}
	}
	return roles, nil
}

// ListPolicies returns customer-managed IAM policies visible to the configured
// AWS credentials.
func (c *Client) ListPolicies(ctx context.Context) ([]iamservice.Policy, error) {
	paginator := awsiam.NewListPoliciesPaginator(c.client, &awsiam.ListPoliciesInput{
		Scope: awsiamtypes.PolicyScopeTypeLocal,
	})
	var policies []iamservice.Policy
	for paginator.HasMorePages() {
		var page *awsiam.ListPoliciesOutput
		err := c.recordAPICall(ctx, "ListPolicies", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, policy := range page.Policies {
			policies = append(policies, iamservice.Policy{
				ARN:              aws.ToString(policy.Arn),
				Name:             aws.ToString(policy.PolicyName),
				Path:             aws.ToString(policy.Path),
				DefaultVersionID: aws.ToString(policy.DefaultVersionId),
				AttachmentCount:  aws.ToInt32(policy.AttachmentCount),
			})
		}
	}
	return policies, nil
}

// ListInstanceProfiles returns IAM instance profiles visible to the configured
// AWS credentials.
func (c *Client) ListInstanceProfiles(ctx context.Context) ([]iamservice.InstanceProfile, error) {
	paginator := awsiam.NewListInstanceProfilesPaginator(c.client, &awsiam.ListInstanceProfilesInput{})
	var profiles []iamservice.InstanceProfile
	for paginator.HasMorePages() {
		var page *awsiam.ListInstanceProfilesOutput
		err := c.recordAPICall(ctx, "ListInstanceProfiles", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, profile := range page.InstanceProfiles {
			roleARNs := make([]string, 0, len(profile.Roles))
			for _, role := range profile.Roles {
				roleARNs = append(roleARNs, aws.ToString(role.Arn))
			}
			profiles = append(profiles, iamservice.InstanceProfile{
				ARN:      aws.ToString(profile.Arn),
				Name:     aws.ToString(profile.InstanceProfileName),
				Path:     aws.ToString(profile.Path),
				RoleARNs: roleARNs,
			})
		}
	}
	return profiles, nil
}

func (c *Client) mapRole(ctx context.Context, role awsiamtypes.Role) (iamservice.Role, error) {
	roleName := aws.ToString(role.RoleName)
	trustPolicy, trustPrincipals, err := parseTrustPolicy(aws.ToString(role.AssumeRolePolicyDocument))
	if err != nil {
		return iamservice.Role{}, fmt.Errorf("parse IAM trust policy for role %q: %w", roleName, err)
	}
	attached, err := c.listAttachedRolePolicies(ctx, roleName)
	if err != nil {
		return iamservice.Role{}, err
	}
	inline, err := c.listRolePolicies(ctx, roleName)
	if err != nil {
		return iamservice.Role{}, err
	}
	return iamservice.Role{
		ARN:                aws.ToString(role.Arn),
		Name:               roleName,
		Path:               aws.ToString(role.Path),
		AssumeRolePolicy:   trustPolicy,
		TrustPrincipals:    trustPrincipals,
		AttachedPolicyARNs: attached,
		InlinePolicyNames:  inline,
	}, nil
}

func (c *Client) listAttachedRolePolicies(ctx context.Context, roleName string) ([]string, error) {
	paginator := awsiam.NewListAttachedRolePoliciesPaginator(c.client, &awsiam.ListAttachedRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	var policyARNs []string
	for paginator.HasMorePages() {
		var page *awsiam.ListAttachedRolePoliciesOutput
		err := c.recordAPICall(ctx, "ListAttachedRolePolicies", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, policy := range page.AttachedPolicies {
			policyARNs = append(policyARNs, aws.ToString(policy.PolicyArn))
		}
	}
	return policyARNs, nil
}

func (c *Client) listRolePolicies(ctx context.Context, roleName string) ([]string, error) {
	paginator := awsiam.NewListRolePoliciesPaginator(c.client, &awsiam.ListRolePoliciesInput{
		RoleName: aws.String(roleName),
	})
	var names []string
	for paginator.HasMorePages() {
		var page *awsiam.ListRolePoliciesOutput
		err := c.recordAPICall(ctx, "ListRolePolicies", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		names = append(names, page.PolicyNames...)
	}
	return names, nil
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
		attrs := metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		)
		c.instruments.AWSAPICalls.Add(ctx, 1, attrs)
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

func parseTrustPolicy(raw string) (map[string]any, []iamservice.TrustPrincipal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, nil
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return nil, nil, err
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(decoded), &document); err != nil {
		return nil, nil, err
	}
	return document, trustPrincipals(document), nil
}

func trustPrincipals(document map[string]any) []iamservice.TrustPrincipal {
	statements, ok := document["Statement"].([]any)
	if !ok {
		if single, ok := document["Statement"].(map[string]any); ok {
			statements = []any{single}
		}
	}
	var principals []iamservice.TrustPrincipal
	for _, statement := range statements {
		statementMap, ok := statement.(map[string]any)
		if !ok {
			continue
		}
		principals = append(principals, trustPrincipalEntries(statementMap["Principal"])...)
	}
	return principals
}

func trustPrincipalEntries(value any) []iamservice.TrustPrincipal {
	switch typed := value.(type) {
	case string:
		identifier := strings.TrimSpace(typed)
		if identifier == "" {
			return nil
		}
		return []iamservice.TrustPrincipal{{Type: "AWS", Identifier: identifier}}
	case []any:
		var principals []iamservice.TrustPrincipal
		for _, item := range typed {
			principals = append(principals, trustPrincipalEntries(item)...)
		}
		return principals
	case map[string]any:
		var principals []iamservice.TrustPrincipal
		for principalType, candidate := range typed {
			for _, identifier := range principalIdentifiers(candidate) {
				principals = append(principals, iamservice.TrustPrincipal{
					Type:       principalType,
					Identifier: identifier,
				})
			}
		}
		return principals
	default:
		return nil
	}
}

func principalIdentifiers(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	case []any:
		output := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				output = append(output, strings.TrimSpace(value))
			}
		}
		return output
	default:
		return nil
	}
}
