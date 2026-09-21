// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cognito

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestScannerEmitsUserPoolAndIdentityPoolMetadata(t *testing.T) {
	poolARN := "arn:aws:cognito-idp:us-east-1:123456789012:userpool/us-east-1_abc123"
	lambdaARN := "arn:aws:lambda:us-east-1:123456789012:function:pre-signup"
	identityPoolARN := "arn:aws:cognito-identity:us-east-1:123456789012:identitypool/us-east-1:11111111-2222-3333-4444-555555555555"
	samlARN := "arn:aws:iam::123456789012:saml-provider/corp"

	client := fakeClient{
		userPools: []UserPool{{
			ID:                 "us-east-1_abc123",
			ARN:                poolARN,
			Name:               "orders",
			MFAConfiguration:   "OPTIONAL",
			DeletionProtection: "ACTIVE",
			EstimatedNumUsers:  42,
			PasswordPolicy: &PasswordPolicy{
				MinimumLength:    12,
				RequireUppercase: true,
				RequireSymbols:   true,
			},
			LambdaTriggers: []LambdaTrigger{{Trigger: "PreSignUp", ARN: lambdaARN}},
			Tags:           map[string]string{"Environment": "prod"},
		}},
		clientsByPool: map[string][]UserPoolClient{
			"us-east-1_abc123": {{
				ID:                "client-1",
				Name:              "web",
				UserPoolID:        "us-east-1_abc123",
				AllowedOAuthFlows: []string{"code"},
				CallbackURLs:      []string{"https://app.example.com/callback"},
			}},
		},
		providersByPool: map[string][]IdentityProvider{
			"us-east-1_abc123": {{
				UserPoolID:   "us-east-1_abc123",
				ProviderName: "Google",
				ProviderType: "Google",
			}},
		},
		resourceServersByPool: map[string][]ResourceServer{
			"us-east-1_abc123": {{
				UserPoolID: "us-east-1_abc123",
				Identifier: "https://api.example.com",
				Name:       "orders-api",
				Scopes:     []string{"orders.read", "orders.write"},
			}},
		},
		groupsByPool: map[string][]Group{
			"us-east-1_abc123": {{
				UserPoolID:  "us-east-1_abc123",
				Name:        "admins",
				Description: "privileged operators",
				RoleARN:     "arn:aws:iam::123456789012:role/cognito-admins",
			}},
		},
		identityPools: []IdentityPool{{
			ID:                             "us-east-1:11111111-2222-3333-4444-555555555555",
			ARN:                            identityPoolARN,
			Name:                           "orders-identity",
			AllowUnauthenticatedIdentities: true,
			DeveloperProviderName:          "login.orders.example",
			UserPoolProviders: []IdentityPoolUserPoolProvider{{
				ProviderName: "cognito-idp.us-east-1.amazonaws.com/us-east-1_abc123",
				ClientID:     "client-1",
			}},
			SAMLProviderARNs: []string{samlARN},
			RolesSummary: map[string]string{
				"authenticated": "arn:aws:iam::123456789012:role/auth",
			},
		}},
	}

	envelopes, err := newScanner(t, client).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	pool := resourceByType(t, envelopes, awscloud.ResourceTypeCognitoUserPool)
	poolAttrs := attributesOf(t, pool)
	if got, want := poolAttrs["mfa_configuration"], "OPTIONAL"; got != want {
		t.Fatalf("mfa_configuration = %#v, want %q", got, want)
	}
	policy, ok := poolAttrs["password_policy"].(map[string]any)
	if !ok {
		t.Fatalf("password_policy = %#v, want map", poolAttrs["password_policy"])
	}
	if got, want := policy["minimum_length"], int32(12); got != want {
		t.Fatalf("password_policy.minimum_length = %#v, want %d", got, want)
	}
	if got, want := poolAttrs["deletion_protection"], "ACTIVE"; got != want {
		t.Fatalf("deletion_protection = %#v, want %q", got, want)
	}

	client1 := resourceByType(t, envelopes, awscloud.ResourceTypeCognitoUserPoolClient)
	clientAttrs := attributesOf(t, client1)
	for _, forbidden := range []string{"client_secret", "ClientSecret", "secret"} {
		if _, exists := clientAttrs[forbidden]; exists {
			t.Fatalf("user pool client attribute %q persisted; ClientSecret must never be stored", forbidden)
		}
	}
	if got, want := clientAttrs["client_id"], "client-1"; got != want {
		t.Fatalf("client_id = %#v, want %q", got, want)
	}

	provider := resourceByType(t, envelopes, awscloud.ResourceTypeCognitoIdentityProvider)
	providerAttrs := attributesOf(t, provider)
	for _, forbidden := range []string{"provider_details", "ProviderDetails", "client_secret", "google_client_secret"} {
		if _, exists := providerAttrs[forbidden]; exists {
			t.Fatalf("identity provider attribute %q persisted; ProviderDetails secrets must never be stored", forbidden)
		}
	}
	if got, want := providerAttrs["provider_type"], "Google"; got != want {
		t.Fatalf("provider_type = %#v, want %q", got, want)
	}

	resourceServer := resourceByType(t, envelopes, awscloud.ResourceTypeCognitoResourceServer)
	resourceServerAttrs := attributesOf(t, resourceServer)
	if got, want := resourceServerAttrs["identifier"], "https://api.example.com"; got != want {
		t.Fatalf("resource server identifier = %#v, want %q", got, want)
	}

	group := resourceByType(t, envelopes, awscloud.ResourceTypeCognitoUserPoolGroup)
	groupAttrs := attributesOf(t, group)
	description, ok := groupAttrs["description"].(map[string]any)
	if !ok {
		t.Fatalf("group description = %#v, want redaction map", groupAttrs["description"])
	}
	marker, _ := description["marker"].(string)
	if !strings.HasPrefix(marker, "redacted:") {
		t.Fatalf("group description marker = %q, want redacted prefix", marker)
	}

	identityPool := resourceByType(t, envelopes, awscloud.ResourceTypeCognitoIdentityPool)
	identityPoolAttrs := attributesOf(t, identityPool)
	developerName, ok := identityPoolAttrs["developer_provider_name"].(map[string]any)
	if !ok {
		t.Fatalf("developer_provider_name = %#v, want redaction map", identityPoolAttrs["developer_provider_name"])
	}
	if developerMarker, _ := developerName["marker"].(string); !strings.HasPrefix(developerMarker, "redacted:") {
		t.Fatalf("developer_provider_name marker = %q, want redacted prefix", developerMarker)
	}

	assertRelationship(t, envelopes, awscloud.RelationshipCognitoUserPoolClientUsesUserPool)
	assertRelationship(t, envelopes, awscloud.RelationshipCognitoUserPoolUsesLambdaTrigger)
	assertRelationship(t, envelopes, awscloud.RelationshipCognitoIdentityPoolUsesUserPool)
	assertRelationship(t, envelopes, awscloud.RelationshipCognitoIdentityPoolUsesIdentityProvider)

	// The identity-pool -> user-pool edge must target an identity that the user
	// pool resource fact actually publishes (its resource_id / correlation
	// anchors are the bare pool ID and ARN), not the raw
	// "cognito-idp.<region>.amazonaws.com/<poolId>" provider name string. AWS
	// returns the provider name in that compound form; emitting it verbatim
	// produces a dangling edge that never joins the user pool node.
	identityToUserPool := relationshipByType(t, envelopes, awscloud.RelationshipCognitoIdentityPoolUsesUserPool)
	if got, want := payloadString(t, identityToUserPool, "target_resource_id"), "us-east-1_abc123"; got != want {
		t.Fatalf("identity-pool -> user-pool target_resource_id = %q, want %q (user pool resource_id)", got, want)
	}
	// The Lambda trigger edge must target the function ARN the Lambda scanner
	// publishes as its resource_id/arn.
	lambdaEdge := relationshipByType(t, envelopes, awscloud.RelationshipCognitoUserPoolUsesLambdaTrigger)
	if got, want := payloadString(t, lambdaEdge, "target_resource_id"), lambdaARN; got != want {
		t.Fatalf("user-pool -> lambda target_resource_id = %q, want %q", got, want)
	}
	if got, want := payloadString(t, lambdaEdge, "target_type"), awscloud.ResourceTypeLambdaFunction; got != want {
		t.Fatalf("user-pool -> lambda target_type = %q, want %q", got, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECR

	_, err := newScanner(t, fakeClient{}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scan() error = %q, want redaction key", err)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{RedactionKey: testKey(t)}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing client")
	}
}

// TestUserPoolIDFromProviderName covers the compound AWS provider-name shape
// and the defensive fallbacks so the identity-pool -> user-pool join key is
// always the strongest available identity, never empty.
func TestUserPoolIDFromProviderName(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		want     string
	}{
		{"compound", "cognito-idp.us-east-1.amazonaws.com/us-east-1_abc123", "us-east-1_abc123"},
		{"compound padded", "  cognito-idp.eu-west-2.amazonaws.com/eu-west-2_XyZ  ", "eu-west-2_XyZ"},
		{"no slash", "us-east-1_abc123", "us-east-1_abc123"},
		{"trailing slash falls back to full name", "cognito-idp.us-east-1.amazonaws.com/", "cognito-idp.us-east-1.amazonaws.com/"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := userPoolIDFromProviderName(tc.provider); got != tc.want {
				t.Fatalf("userPoolIDFromProviderName(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

// TestClientInterfaceExcludesUserRecordAndMutationAPIs proves the scanner Client
// interface cannot reach Cognito user records (PII) or mutation APIs. Adding any
// of these methods fails the build's contract test before a scan can call them.
func TestClientInterfaceExcludesUserRecordAndMutationAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	forbidden := []string{
		// User-record reads (PII) - explicitly forbidden by issue #748.
		"ListUsers",
		"AdminGetUser",
		"AdminListGroupsForUser",
		"ListUsersInGroup",
		"GetUser",
		"AdminListUserAuthEvents",
		// Secret-bearing reads.
		"ListUserPoolClientSecrets",
		"DescribeUserPoolClient",
		"DescribeIdentityProvider",
		// Mutation APIs.
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
		"CreateIdentityPool",
		"DeleteIdentityPool",
		"UpdateIdentityPool",
	}
	for _, method := range forbidden {
		if _, ok := clientType.MethodByName(method); ok {
			t.Fatalf("Client declares forbidden Cognito API %q; the scanner must stay metadata-only and never reach user records", method)
		}
	}
}

func newScanner(t *testing.T, client Client) Scanner {
	t.Helper()
	return Scanner{Client: client, RedactionKey: testKey(t)}
}

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCognito,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:cognito:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	userPools             []UserPool
	clientsByPool         map[string][]UserPoolClient
	providersByPool       map[string][]IdentityProvider
	resourceServersByPool map[string][]ResourceServer
	groupsByPool          map[string][]Group
	identityPools         []IdentityPool
}

func (c fakeClient) ListUserPools(context.Context) ([]UserPool, error) {
	return c.userPools, nil
}

func (c fakeClient) ListUserPoolClients(_ context.Context, poolID string) ([]UserPoolClient, error) {
	return c.clientsByPool[poolID], nil
}

func (c fakeClient) ListIdentityProviders(_ context.Context, poolID string) ([]IdentityProvider, error) {
	return c.providersByPool[poolID], nil
}

func (c fakeClient) ListResourceServers(_ context.Context, poolID string) ([]ResourceServer, error) {
	return c.resourceServersByPool[poolID], nil
}

func (c fakeClient) ListGroups(_ context.Context, poolID string) ([]Group, error) {
	return c.groupsByPool[poolID], nil
}

func (c fakeClient) ListIdentityPools(context.Context) ([]IdentityPool, error) {
	return c.identityPools, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
	return facts.Envelope{}
}

func payloadString(t *testing.T, envelope facts.Envelope, key string) string {
	t.Helper()
	value, ok := envelope.Payload[key].(string)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want string", key, envelope.Payload[key])
	}
	return value
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
