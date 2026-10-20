// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsidentity "github.com/aws/aws-sdk-go-v2/service/cognitoidentity"
	awsidentitytypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentity/types"
	awsidp "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	awsidptypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestUserPoolAPINeverIncludesUserRecordOrSecretMethods asserts the narrow
// cognito-idp adapter interface excludes every API that would read Cognito user
// records (PII), list app-client secrets, or mutate user pool resources. Adding
// any of these methods to userPoolClientAPI fails this test before any code can
// call them.
func TestUserPoolAPINeverIncludesUserRecordOrSecretMethods(t *testing.T) {
	apiType := reflect.TypeOf((*userPoolClientAPI)(nil)).Elem()
	forbidden := []string{
		// User-record reads (PII) - explicitly forbidden by issue #748.
		"ListUsers",
		"AdminGetUser",
		"AdminListGroupsForUser",
		"ListUsersInGroup",
		"GetUser",
		"AdminListUserAuthEvents",
		// Secret-listing reads.
		"ListUserPoolClientSecrets",
		// Mutations.
		"CreateUserPool",
		"DeleteUserPool",
		"UpdateUserPool",
		"CreateUserPoolClient",
		"DeleteUserPoolClient",
		"UpdateUserPoolClient",
		"CreateIdentityProvider",
		"DeleteIdentityProvider",
		"UpdateIdentityProvider",
		"CreateResourceServer",
		"DeleteResourceServer",
		"UpdateResourceServer",
		"CreateGroup",
		"DeleteGroup",
		"UpdateGroup",
		"AdminCreateUser",
		"AdminDeleteUser",
		"AdminUpdateUserAttributes",
	}
	for _, method := range forbidden {
		if _, ok := apiType.MethodByName(method); ok {
			t.Fatalf("userPoolClientAPI declares forbidden Cognito API %q; the adapter must not gain access to that method", method)
		}
	}
}

// TestIdentityPoolAPINeverIncludesIdentityRecordOrMutationMethods asserts the
// cognito-identity adapter interface excludes identity-record reads, credential
// minting, and mutations.
func TestIdentityPoolAPINeverIncludesIdentityRecordOrMutationMethods(t *testing.T) {
	apiType := reflect.TypeOf((*identityPoolAPI)(nil)).Elem()
	forbidden := []string{
		"ListIdentities",
		"DescribeIdentity",
		"GetId",
		"GetCredentialsForIdentity",
		"GetOpenIdToken",
		"GetOpenIdTokenForDeveloperIdentity",
		"CreateIdentityPool",
		"DeleteIdentityPool",
		"UpdateIdentityPool",
		"SetIdentityPoolRoles",
	}
	for _, method := range forbidden {
		if _, ok := apiType.MethodByName(method); ok {
			t.Fatalf("identityPoolAPI declares forbidden Cognito Identity API %q; the adapter must not gain access to that method", method)
		}
	}
}

func TestClientListUserPoolsMapsMetadataAndDropsSecrets(t *testing.T) {
	now := time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC)
	fake := &fakeUserPoolClient{
		userPools: []awsidptypes.UserPoolDescriptionType{{Id: aws.String("us-east-1_abc")}},
		pool: &awsidptypes.UserPoolType{
			Id:               aws.String("us-east-1_abc"),
			Arn:              aws.String("arn:aws:cognito-idp:us-east-1:123456789012:userpool/us-east-1_abc"),
			Name:             aws.String("orders"),
			MfaConfiguration: awsidptypes.UserPoolMfaTypeOptional,
			Policies: &awsidptypes.UserPoolPolicyType{PasswordPolicy: &awsidptypes.PasswordPolicyType{
				MinimumLength:    aws.Int32(12),
				RequireUppercase: true,
			}},
			LambdaConfig: &awsidptypes.LambdaConfigType{
				PreSignUp: aws.String("arn:aws:lambda:us-east-1:123456789012:function:pre-signup"),
			},
			CreationDate: aws.Time(now),
		},
		clientIDs: []string{"client-1"},
		client: &awsidptypes.UserPoolClientType{
			ClientId:          aws.String("client-1"),
			ClientName:        aws.String("web"),
			UserPoolId:        aws.String("us-east-1_abc"),
			ClientSecret:      aws.String("super-secret-value"),
			AllowedOAuthFlows: []awsidptypes.OAuthFlowType{awsidptypes.OAuthFlowTypeCode},
			CallbackURLs:      []string{"https://app.example.com/callback"},
		},
		providers: []awsidptypes.ProviderDescription{{
			ProviderName: aws.String("Google"),
			ProviderType: awsidptypes.IdentityProviderTypeTypeGoogle,
		}},
		resourceServers: []awsidptypes.ResourceServerType{{
			UserPoolId: aws.String("us-east-1_abc"),
			Identifier: aws.String("https://api.example.com"),
			Name:       aws.String("orders-api"),
			Scopes:     []awsidptypes.ResourceServerScopeType{{ScopeName: aws.String("orders.read")}},
		}},
		groups: []awsidptypes.GroupType{{
			UserPoolId: aws.String("us-east-1_abc"),
			GroupName:  aws.String("admins"),
			RoleArn:    aws.String("arn:aws:iam::123456789012:role/cognito-admins"),
		}},
	}
	client := &Client{userPoolClient: fake, boundary: testBoundary()}

	pools, err := client.ListUserPools(context.Background())
	if err != nil {
		t.Fatalf("ListUserPools() error = %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("ListUserPools() len = %d, want 1", len(pools))
	}
	pool := pools[0]
	if pool.MFAConfiguration != "OPTIONAL" {
		t.Fatalf("MFAConfiguration = %q, want OPTIONAL", pool.MFAConfiguration)
	}
	if pool.PasswordPolicy == nil || pool.PasswordPolicy.MinimumLength != 12 {
		t.Fatalf("PasswordPolicy = %#v, want minimum length 12", pool.PasswordPolicy)
	}
	if len(pool.LambdaTriggers) != 1 || pool.LambdaTriggers[0].Trigger != "PreSignUp" {
		t.Fatalf("LambdaTriggers = %#v, want PreSignUp", pool.LambdaTriggers)
	}

	clients, err := client.ListUserPoolClients(context.Background(), "us-east-1_abc")
	if err != nil {
		t.Fatalf("ListUserPoolClients() error = %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("ListUserPoolClients() len = %d, want 1", len(clients))
	}
	mappedClient := clients[0]
	clientValue := reflect.ValueOf(mappedClient)
	for i := 0; i < clientValue.NumField(); i++ {
		if got, _ := clientValue.Field(i).Interface().(string); got == "super-secret-value" {
			t.Fatalf("UserPoolClient field %q leaked ClientSecret", clientValue.Type().Field(i).Name)
		}
	}
	if len(mappedClient.CallbackURLs) != 1 {
		t.Fatalf("CallbackURLs = %#v, want one entry", mappedClient.CallbackURLs)
	}

	providers, err := client.ListIdentityProviders(context.Background(), "us-east-1_abc")
	if err != nil {
		t.Fatalf("ListIdentityProviders() error = %v", err)
	}
	if len(providers) != 1 || providers[0].ProviderType != "Google" {
		t.Fatalf("ListIdentityProviders() = %#v, want Google", providers)
	}

	resourceServers, err := client.ListResourceServers(context.Background(), "us-east-1_abc")
	if err != nil {
		t.Fatalf("ListResourceServers() error = %v", err)
	}
	if len(resourceServers) != 1 || resourceServers[0].Scopes[0] != "orders.read" {
		t.Fatalf("ListResourceServers() = %#v, want orders.read scope", resourceServers)
	}

	groups, err := client.ListGroups(context.Background(), "us-east-1_abc")
	if err != nil {
		t.Fatalf("ListGroups() error = %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "admins" {
		t.Fatalf("ListGroups() = %#v, want admins", groups)
	}
}

func TestClientListIdentityPoolsSynthesizesARNAndRoleSummary(t *testing.T) {
	fake := &fakeIdentityPoolClient{
		summaries: []awsidentitytypes.IdentityPoolShortDescription{{
			IdentityPoolId: aws.String("us-east-1:pool-1"),
		}},
		pool: &awsidentity.DescribeIdentityPoolOutput{
			IdentityPoolId:                 aws.String("us-east-1:pool-1"),
			IdentityPoolName:               aws.String("orders-identity"),
			AllowUnauthenticatedIdentities: true,
			CognitoIdentityProviders: []awsidentitytypes.CognitoIdentityProvider{{
				ProviderName: aws.String("cognito-idp.us-east-1.amazonaws.com/us-east-1_abc"),
				ClientId:     aws.String("client-1"),
			}},
			SamlProviderARNs: []string{"arn:aws:iam::123456789012:saml-provider/corp"},
		},
		roles: map[string]string{"authenticated": "arn:aws:iam::123456789012:role/auth"},
	}
	client := &Client{identityClient: fake, boundary: testBoundary()}

	pools, err := client.ListIdentityPools(context.Background())
	if err != nil {
		t.Fatalf("ListIdentityPools() error = %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("ListIdentityPools() len = %d, want 1", len(pools))
	}
	pool := pools[0]
	want := "arn:aws:cognito-identity:us-east-1:123456789012:identitypool/us-east-1:pool-1"
	if pool.ARN != want {
		t.Fatalf("identity pool ARN = %q, want %q", pool.ARN, want)
	}
	if pool.RolesSummary["authenticated"] != "arn:aws:iam::123456789012:role/auth" {
		t.Fatalf("RolesSummary = %#v, want authenticated role", pool.RolesSummary)
	}
	if len(pool.UserPoolProviders) != 1 {
		t.Fatalf("UserPoolProviders = %#v, want one entry", pool.UserPoolProviders)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceCognito,
	}
}

type fakeUserPoolClient struct {
	userPools       []awsidptypes.UserPoolDescriptionType
	pool            *awsidptypes.UserPoolType
	clientIDs       []string
	client          *awsidptypes.UserPoolClientType
	providers       []awsidptypes.ProviderDescription
	resourceServers []awsidptypes.ResourceServerType
	groups          []awsidptypes.GroupType
}

func (c *fakeUserPoolClient) ListUserPools(context.Context, *awsidp.ListUserPoolsInput, ...func(*awsidp.Options)) (*awsidp.ListUserPoolsOutput, error) {
	return &awsidp.ListUserPoolsOutput{UserPools: c.userPools}, nil
}

func (c *fakeUserPoolClient) DescribeUserPool(context.Context, *awsidp.DescribeUserPoolInput, ...func(*awsidp.Options)) (*awsidp.DescribeUserPoolOutput, error) {
	return &awsidp.DescribeUserPoolOutput{UserPool: c.pool}, nil
}

func (c *fakeUserPoolClient) ListUserPoolClients(context.Context, *awsidp.ListUserPoolClientsInput, ...func(*awsidp.Options)) (*awsidp.ListUserPoolClientsOutput, error) {
	clients := make([]awsidptypes.UserPoolClientDescription, 0, len(c.clientIDs))
	for _, id := range c.clientIDs {
		clients = append(clients, awsidptypes.UserPoolClientDescription{ClientId: aws.String(id)})
	}
	return &awsidp.ListUserPoolClientsOutput{UserPoolClients: clients}, nil
}

func (c *fakeUserPoolClient) DescribeUserPoolClient(context.Context, *awsidp.DescribeUserPoolClientInput, ...func(*awsidp.Options)) (*awsidp.DescribeUserPoolClientOutput, error) {
	return &awsidp.DescribeUserPoolClientOutput{UserPoolClient: c.client}, nil
}

func (c *fakeUserPoolClient) ListIdentityProviders(context.Context, *awsidp.ListIdentityProvidersInput, ...func(*awsidp.Options)) (*awsidp.ListIdentityProvidersOutput, error) {
	return &awsidp.ListIdentityProvidersOutput{Providers: c.providers}, nil
}

func (c *fakeUserPoolClient) ListResourceServers(context.Context, *awsidp.ListResourceServersInput, ...func(*awsidp.Options)) (*awsidp.ListResourceServersOutput, error) {
	return &awsidp.ListResourceServersOutput{ResourceServers: c.resourceServers}, nil
}

func (c *fakeUserPoolClient) ListGroups(context.Context, *awsidp.ListGroupsInput, ...func(*awsidp.Options)) (*awsidp.ListGroupsOutput, error) {
	return &awsidp.ListGroupsOutput{Groups: c.groups}, nil
}

type fakeIdentityPoolClient struct {
	summaries []awsidentitytypes.IdentityPoolShortDescription
	pool      *awsidentity.DescribeIdentityPoolOutput
	roles     map[string]string
}

func (c *fakeIdentityPoolClient) ListIdentityPools(context.Context, *awsidentity.ListIdentityPoolsInput, ...func(*awsidentity.Options)) (*awsidentity.ListIdentityPoolsOutput, error) {
	return &awsidentity.ListIdentityPoolsOutput{IdentityPools: c.summaries}, nil
}

func (c *fakeIdentityPoolClient) DescribeIdentityPool(context.Context, *awsidentity.DescribeIdentityPoolInput, ...func(*awsidentity.Options)) (*awsidentity.DescribeIdentityPoolOutput, error) {
	return c.pool, nil
}

func (c *fakeIdentityPoolClient) GetIdentityPoolRoles(context.Context, *awsidentity.GetIdentityPoolRolesInput, ...func(*awsidentity.Options)) (*awsidentity.GetIdentityPoolRolesOutput, error) {
	return &awsidentity.GetIdentityPoolRolesOutput{Roles: c.roles}, nil
}
