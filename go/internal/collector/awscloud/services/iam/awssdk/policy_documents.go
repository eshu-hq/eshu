package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	awsiamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
)

// ListUsers returns IAM users visible to the configured AWS credentials, each
// with its normalized inline and attached managed policy statements.
func (c *Client) ListUsers(ctx context.Context) ([]iamservice.User, error) {
	paginator := awsiam.NewListUsersPaginator(c.client, &awsiam.ListUsersInput{})
	var users []iamservice.User
	for paginator.HasMorePages() {
		var page *awsiam.ListUsersOutput
		err := c.recordAPICall(ctx, "ListUsers", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, user := range page.Users {
			mapped, err := c.mapUser(ctx, user)
			if err != nil {
				return nil, err
			}
			users = append(users, mapped)
		}
	}
	return users, nil
}

func (c *Client) mapUser(ctx context.Context, user awsiamtypes.User) (iamservice.User, error) {
	userName := aws.ToString(user.UserName)
	attached, err := c.listAttachedUserPolicies(ctx, userName)
	if err != nil {
		return iamservice.User{}, err
	}
	inlineNames, err := c.listUserPolicies(ctx, userName)
	if err != nil {
		return iamservice.User{}, err
	}
	detail, err := c.getUserDetail(ctx, userName)
	if err != nil {
		return iamservice.User{}, err
	}
	userDetail := user
	if detail != nil {
		userDetail = *detail
	}
	boundary := permissionBoundary(userDetail.PermissionsBoundary)
	statements, err := c.userStatements(ctx, userName, boundary.PolicyARN, attached, inlineNames)
	if err != nil {
		return iamservice.User{}, err
	}
	return iamservice.User{
		ARN:                  firstNonBlank(aws.ToString(userDetail.Arn), aws.ToString(user.Arn)),
		Name:                 userName,
		Path:                 firstNonBlank(aws.ToString(userDetail.Path), aws.ToString(user.Path)),
		PermissionBoundary:   boundary,
		AttachedPolicyARNs:   attached,
		InlinePolicyNames:    inlineNames,
		PermissionStatements: statements,
	}, nil
}

func (c *Client) getUserDetail(ctx context.Context, userName string) (*awsiamtypes.User, error) {
	var out *awsiam.GetUserOutput
	err := c.recordAPICall(ctx, "GetUser", func(callCtx context.Context) error {
		var err error
		out, err = c.client.GetUser(callCtx, &awsiam.GetUserInput{
			UserName: aws.String(userName),
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get IAM user %q: %w", userName, err)
	}
	if out == nil {
		return nil, nil
	}
	return out.User, nil
}

// roleStatements assembles the normalized, metadata-only permission statements
// for one role: the trust policy, every inline policy document, the attached
// managed policy documents up to the per-principal fan-out cap, and — when a
// permission boundary is attached — the boundary policy document's statements
// (tagged PolicySourceBoundary so the reducer can intersect them as a ceiling).
func (c *Client) roleStatements(ctx context.Context, roleName, rawTrust, boundaryARN string, attached, inlineNames []string) ([]iamservice.PolicyStatement, error) {
	var statements []iamservice.PolicyStatement

	trust, err := normalizeTrustPolicyDocument(rawTrust)
	if err != nil {
		return nil, fmt.Errorf("normalize IAM trust policy for role %q: %w", roleName, err)
	}
	statements = append(statements, trust...)

	for _, name := range inlineNames {
		document, err := c.getRolePolicyDocument(ctx, roleName, name)
		if err != nil {
			return nil, err
		}
		normalized, err := normalizePolicyDocument(document, iamservice.PolicySourceInline, "", name)
		if err != nil {
			return nil, fmt.Errorf("normalize inline policy %q for role %q: %w", name, roleName, err)
		}
		statements = append(statements, normalized...)
	}

	managed, err := c.managedPolicyStatements(ctx, attached)
	if err != nil {
		return nil, err
	}
	statements = append(statements, managed...)

	boundary, err := boundaryPolicyStatements(boundaryARN, func(arn string) (string, error) {
		return c.getManagedPolicyDocument(ctx, arn)
	})
	if err != nil {
		return nil, err
	}
	return append(statements, boundary...), nil
}

// userStatements assembles the normalized inline and attached managed policy
// statements for one user, bounding the managed-policy fan-out, plus the user's
// permission boundary statements when a boundary is attached (PolicySourceBoundary).
func (c *Client) userStatements(ctx context.Context, userName, boundaryARN string, attached, inlineNames []string) ([]iamservice.PolicyStatement, error) {
	var statements []iamservice.PolicyStatement
	for _, name := range inlineNames {
		document, err := c.getUserPolicyDocument(ctx, userName, name)
		if err != nil {
			return nil, err
		}
		normalized, err := normalizePolicyDocument(document, iamservice.PolicySourceInline, "", name)
		if err != nil {
			return nil, fmt.Errorf("normalize inline policy %q for user %q: %w", name, userName, err)
		}
		statements = append(statements, normalized...)
	}
	managed, err := c.managedPolicyStatements(ctx, attached)
	if err != nil {
		return nil, err
	}
	statements = append(statements, managed...)

	boundary, err := boundaryPolicyStatements(boundaryARN, func(arn string) (string, error) {
		return c.getManagedPolicyDocument(ctx, arn)
	})
	if err != nil {
		return nil, err
	}
	return append(statements, boundary...), nil
}

func (c *Client) listAttachedUserPolicies(ctx context.Context, userName string) ([]string, error) {
	paginator := awsiam.NewListAttachedUserPoliciesPaginator(c.client, &awsiam.ListAttachedUserPoliciesInput{
		UserName: aws.String(userName),
	})
	var policyARNs []string
	for paginator.HasMorePages() {
		var page *awsiam.ListAttachedUserPoliciesOutput
		err := c.recordAPICall(ctx, "ListAttachedUserPolicies", func(callCtx context.Context) error {
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

func (c *Client) listUserPolicies(ctx context.Context, userName string) ([]string, error) {
	paginator := awsiam.NewListUserPoliciesPaginator(c.client, &awsiam.ListUserPoliciesInput{
		UserName: aws.String(userName),
	})
	var names []string
	for paginator.HasMorePages() {
		var page *awsiam.ListUserPoliciesOutput
		err := c.recordAPICall(ctx, "ListUserPolicies", func(callCtx context.Context) error {
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

// managedPolicyStatements reads and normalizes attached managed policy documents
// up to maxPolicyDocumentsPerPrincipal. Reading one document costs a GetPolicy +
// GetPolicyVersion pair, so the cap keeps the per-principal fan-out bounded.
func (c *Client) managedPolicyStatements(ctx context.Context, policyARNs []string) ([]iamservice.PolicyStatement, error) {
	return boundedManagedPolicyStatements(policyARNs, maxPolicyDocumentsPerPrincipal, func(policyARN string) (string, error) {
		return c.getManagedPolicyDocument(ctx, policyARN)
	})
}

// boundedManagedPolicyStatements fetches at most cap managed policy documents
// (via fetch) and normalizes them into derived statements. Splitting the cap and
// iteration out of the SDK call keeps the per-principal fan-out bound unit
// testable without standing up an AWS client: fetch is invoked at most cap times
// regardless of how many ARNs are attached.
func boundedManagedPolicyStatements(policyARNs []string, maxDocuments int, fetch func(policyARN string) (string, error)) ([]iamservice.PolicyStatement, error) {
	var statements []iamservice.PolicyStatement
	limit := len(policyARNs)
	if limit > maxDocuments {
		limit = maxDocuments
	}
	for i := 0; i < limit; i++ {
		policyARN := strings.TrimSpace(policyARNs[i])
		if policyARN == "" {
			continue
		}
		document, err := fetch(policyARN)
		if err != nil {
			return nil, err
		}
		normalized, err := normalizePolicyDocument(document, iamservice.PolicySourceAttachedManaged, policyARN, "")
		if err != nil {
			return nil, fmt.Errorf("normalize managed policy %q: %w", policyARN, err)
		}
		statements = append(statements, normalized...)
	}
	return statements, nil
}

// boundaryPolicyStatements fetches and normalizes one principal's permission
// boundary policy document into metadata-only statements tagged
// PolicySourceBoundary. A permission boundary is a managed policy referenced by
// ARN, so it reuses the SAME identity-policy normalization (effect, action/resource
// patterns, condition KEY names only) — no raw policy JSON or condition values are
// ever retained. A blank boundary ARN (no boundary attached) yields no fetch and no
// statements. fetch is the bounded document reader; splitting it out keeps the
// boundary read unit-testable without an AWS client, mirroring
// boundedManagedPolicyStatements.
func boundaryPolicyStatements(boundaryARN string, fetch func(policyARN string) (string, error)) ([]iamservice.PolicyStatement, error) {
	boundaryARN = strings.TrimSpace(boundaryARN)
	if boundaryARN == "" {
		return nil, nil
	}
	document, err := fetch(boundaryARN)
	if err != nil {
		return nil, fmt.Errorf("get permission boundary %q: %w", boundaryARN, err)
	}
	normalized, err := normalizePolicyDocument(document, iamservice.PolicySourceBoundary, boundaryARN, "")
	if err != nil {
		return nil, fmt.Errorf("normalize permission boundary %q: %w", boundaryARN, err)
	}
	return normalized, nil
}

func (c *Client) getRolePolicyDocument(ctx context.Context, roleName, policyName string) (string, error) {
	var out *awsiam.GetRolePolicyOutput
	err := c.recordAPICall(ctx, "GetRolePolicy", func(callCtx context.Context) error {
		var err error
		out, err = c.client.GetRolePolicy(callCtx, &awsiam.GetRolePolicyInput{
			RoleName:   aws.String(roleName),
			PolicyName: aws.String(policyName),
		})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("get inline policy %q for role %q: %w", policyName, roleName, err)
	}
	return aws.ToString(out.PolicyDocument), nil
}

func (c *Client) getUserPolicyDocument(ctx context.Context, userName, policyName string) (string, error) {
	var out *awsiam.GetUserPolicyOutput
	err := c.recordAPICall(ctx, "GetUserPolicy", func(callCtx context.Context) error {
		var err error
		out, err = c.client.GetUserPolicy(callCtx, &awsiam.GetUserPolicyInput{
			UserName:   aws.String(userName),
			PolicyName: aws.String(policyName),
		})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("get inline policy %q for user %q: %w", policyName, userName, err)
	}
	return aws.ToString(out.PolicyDocument), nil
}

// getManagedPolicyDocument reads the default version document of one attached
// managed policy. It is the GetPolicy + GetPolicyVersion pair the per-principal
// cap bounds.
func (c *Client) getManagedPolicyDocument(ctx context.Context, policyARN string) (string, error) {
	var policyOut *awsiam.GetPolicyOutput
	err := c.recordAPICall(ctx, "GetPolicy", func(callCtx context.Context) error {
		var err error
		policyOut, err = c.client.GetPolicy(callCtx, &awsiam.GetPolicyInput{
			PolicyArn: aws.String(policyARN),
		})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("get managed policy %q: %w", policyARN, err)
	}
	if policyOut.Policy == nil {
		return "", nil
	}
	versionID := aws.ToString(policyOut.Policy.DefaultVersionId)
	if strings.TrimSpace(versionID) == "" {
		return "", nil
	}
	var versionOut *awsiam.GetPolicyVersionOutput
	err = c.recordAPICall(ctx, "GetPolicyVersion", func(callCtx context.Context) error {
		var err error
		versionOut, err = c.client.GetPolicyVersion(callCtx, &awsiam.GetPolicyVersionInput{
			PolicyArn: aws.String(policyARN),
			VersionId: aws.String(versionID),
		})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("get managed policy version %q for %q: %w", versionID, policyARN, err)
	}
	if versionOut.PolicyVersion == nil {
		return "", nil
	}
	return aws.ToString(versionOut.PolicyVersion.Document), nil
}
