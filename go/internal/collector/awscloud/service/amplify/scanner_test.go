// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amplify

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAmplify,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, time.May, 28, 0, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	apps          []App
	branchesByApp map[string][]Branch
	domainsByApp  map[string][]DomainAssociation
}

func (f fakeClient) ListApps(context.Context) ([]App, error) { return f.apps, nil }

func (f fakeClient) ListBranches(_ context.Context, appID string) ([]Branch, error) {
	return f.branchesByApp[appID], nil
}

func (f fakeClient) ListDomainAssociations(_ context.Context, appID string) ([]DomainAssociation, error) {
	return f.domainsByApp[appID], nil
}

const (
	sampleAppARN         = "arn:aws:amplify:us-east-1:123456789012:apps/d111111111"
	sampleBranchARN      = "arn:aws:amplify:us-east-1:123456789012:apps/d111111111/branches/main"
	sampleRoleARN        = "arn:aws:iam::123456789012:role/AmplifyServiceRole"
	sampleComputeRoleARN = "arn:aws:iam::123456789012:role/AmplifyComputeRole"
)

func sampleClient() fakeClient {
	return fakeClient{
		apps: []App{{
			ID:                    "d111111111",
			ARN:                   sampleAppARN,
			Name:                  "storefront",
			Platform:              "WEB_COMPUTE",
			RepositoryURL:         "https://github.com/acme/storefront",
			RepositoryCloneMethod: "TOKEN",
			DefaultDomain:         "d111111111.amplifyapp.com",
			ServiceRoleARN:        sampleRoleARN,
			ComputeRoleARN:        sampleComputeRoleARN,
			ProductionBranchName:  "main",
			CreateTime:            time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
		}},
		branchesByApp: map[string][]Branch{
			"d111111111": {{
				AppID:           "d111111111",
				Name:            "main",
				ARN:             sampleBranchARN,
				DisplayName:     "main",
				Stage:           "PRODUCTION",
				Framework:       "Next.js - SSR",
				EnableAutoBuild: true,
			}},
		},
		domainsByApp: map[string][]DomainAssociation{
			"d111111111": {{
				AppID:      "d111111111",
				ARN:        "arn:aws:amplify:us-east-1:123456789012:apps/d111111111/domains/storefront.example.com",
				DomainName: "storefront.example.com",
				Status:     "AVAILABLE",
				SubDomains: []SubDomain{
					{
						Prefix:     "www",
						BranchName: "main",
						DNSRecord:  "CNAME d2example.cloudfront.net",
						Verified:   true,
					},
					{
						Prefix:     "blog",
						BranchName: "main",
						DNSRecord:  "CNAME d2example.cloudfront.net.",
						Verified:   true,
					},
				},
			}},
		},
	}
}

func resourcesByType(t *testing.T, envelopes []facts.Envelope, resourceType string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if env.Payload["resource_type"] == resourceType {
			matched = append(matched, env.Payload)
		}
	}
	return matched
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relType string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if env.Payload["relationship_type"] == relType {
			matched = append(matched, env.Payload)
		}
	}
	return matched
}

func TestScannerEmitsAppAndBranchResources(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	apps := resourcesByType(t, envelopes, awscloud.ResourceTypeAmplifyApp)
	if len(apps) != 1 {
		t.Fatalf("app resources = %d, want 1", len(apps))
	}
	if apps[0]["resource_id"] != sampleAppARN {
		t.Fatalf("app resource_id = %v, want %v", apps[0]["resource_id"], sampleAppARN)
	}
	appAttrs := apps[0]["attributes"].(map[string]any)
	if appAttrs["platform"] != "WEB_COMPUTE" {
		t.Fatalf("app platform = %v, want WEB_COMPUTE", appAttrs["platform"])
	}
	if appAttrs["repository_url"] != "https://github.com/acme/storefront" {
		t.Fatalf("app repository_url = %v", appAttrs["repository_url"])
	}

	branches := resourcesByType(t, envelopes, awscloud.ResourceTypeAmplifyBranch)
	if len(branches) != 1 {
		t.Fatalf("branch resources = %d, want 1", len(branches))
	}
	if branches[0]["resource_id"] != sampleBranchARN {
		t.Fatalf("branch resource_id = %v, want %v", branches[0]["resource_id"], sampleBranchARN)
	}
	if branches[0]["state"] != "PRODUCTION" {
		t.Fatalf("branch state = %v, want PRODUCTION", branches[0]["state"])
	}
}

func TestScannerEmitsRelationshipsWithJoinKeys(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	repo := relationshipsByType(t, envelopes, awscloud.RelationshipAmplifyAppDeploysFromRepository)
	if len(repo) != 1 || repo[0]["target_type"] != repositorySourceTargetType {
		t.Fatalf("app->repository = %#v", repo)
	}
	if repo[0]["target_resource_id"] != "https://github.com/acme/storefront" {
		t.Fatalf("app->repository target_resource_id = %v", repo[0]["target_resource_id"])
	}
	if repo[0]["source_resource_id"] != sampleAppARN {
		t.Fatalf("app->repository source_resource_id = %v, want app node id", repo[0]["source_resource_id"])
	}

	roles := relationshipsByType(t, envelopes, awscloud.RelationshipAmplifyAppUsesIAMRole)
	if len(roles) != 2 {
		t.Fatalf("app->IAM-role relationships = %d, want 2", len(roles))
	}
	for _, role := range roles {
		if role["target_type"] != awscloud.ResourceTypeIAMRole {
			t.Fatalf("app->IAM-role target_type = %v", role["target_type"])
		}
		if !strings.HasPrefix(role["target_resource_id"].(string), "arn:aws:iam::") {
			t.Fatalf("app->IAM-role target_resource_id = %v, want role ARN", role["target_resource_id"])
		}
		if role["target_arn"] != role["target_resource_id"] {
			t.Fatalf("app->IAM-role target_arn = %v, want = target_resource_id", role["target_arn"])
		}
	}

	zone := relationshipsByType(t, envelopes, awscloud.RelationshipAmplifyAppServesCustomDomainViaHostedZone)
	if len(zone) != 1 || zone[0]["target_type"] != awscloud.ResourceTypeRoute53HostedZone {
		t.Fatalf("app->Route53 = %#v", zone)
	}
	if zone[0]["target_resource_id"] != "storefront.example.com" {
		t.Fatalf("app->Route53 target_resource_id = %v, want normalized domain", zone[0]["target_resource_id"])
	}
	if zone[0]["target_arn"] != "" {
		t.Fatalf("app->Route53 target_arn = %v, want empty (Amplify reports no zone ARN)", zone[0]["target_arn"])
	}

	cf := relationshipsByType(t, envelopes, awscloud.RelationshipAmplifyAppServesCustomDomainViaCloudFront)
	if len(cf) != 1 || cf[0]["target_type"] != awscloud.ResourceTypeCloudFrontDistribution {
		t.Fatalf("app->CloudFront = %#v (want 1 deduped edge)", cf)
	}
	if cf[0]["target_resource_id"] != "d2example.cloudfront.net" {
		t.Fatalf("app->CloudFront target_resource_id = %v, want cloudfront domain", cf[0]["target_resource_id"])
	}

	branchToApp := relationshipsByType(t, envelopes, awscloud.RelationshipAmplifyBranchBelongsToApp)
	if len(branchToApp) != 1 || branchToApp[0]["target_type"] != awscloud.ResourceTypeAmplifyApp {
		t.Fatalf("branch->app = %#v", branchToApp)
	}
	if branchToApp[0]["target_resource_id"] != sampleAppARN {
		t.Fatalf("branch->app target_resource_id = %v, want app node id", branchToApp[0]["target_resource_id"])
	}
	if branchToApp[0]["source_resource_id"] != sampleBranchARN {
		t.Fatalf("branch->app source_resource_id = %v, want branch node id", branchToApp[0]["source_resource_id"])
	}
}

// TestRelationshipsSatisfyGraphJoinContract runs every emitted relationship
// through the shared relguard runtime guard so each edge's target_type is a
// known resource family (or an allowlisted non-AWS anchor) and any populated
// target ARN is ARN-shaped.
func TestRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	domains := sampleClient().domainsByApp["d111111111"]
	app := sampleClient().apps[0]
	observations := appRelationships(testBoundary(), app, domains)
	branch := sampleClient().branchesByApp["d111111111"][0]
	if rel, ok := branchAppRelationship(testBoundary(), branch, appResourceID(testBoundary(), app)); ok {
		observations = append(observations, rel)
	}
	if len(observations) == 0 {
		t.Fatalf("no relationship observations produced")
	}
	relguard.AssertObservations(t, observations...)
}

// TestScannerNeverEmitsEnvVarsOrTokens proves no fact payload carries an Amplify
// environment-variable value, a build-spec body, a basic-auth credential, or a
// repository access token. The scanner-owned types carry no such field, so this
// is a defense-in-depth scan over the full emitted payload set.
func TestScannerNeverEmitsEnvVarsOrTokens(t *testing.T) {
	client := sampleClient()
	// Inject a token-bearing repository URL and confirm the token never appears.
	client.apps[0].RepositoryURL = SanitizeRepositoryURL("https://x-access-token:ghp_SECRETTOKEN@github.com/acme/storefront.git")
	envelopes, err := Scanner{Client: client}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	forbidden := []string{
		"ghp_SECRETTOKEN", "x-access-token",
		"environment_variable", "env_var", "build_spec", "buildspec",
		"basic_auth", "BasicAuthCredentials", "access_token",
	}
	for _, env := range envelopes {
		rendered := renderPayload(env.Payload)
		for _, needle := range forbidden {
			if strings.Contains(rendered, needle) {
				t.Fatalf("fact payload leaked forbidden value %q: %s", needle, rendered)
			}
		}
	}
}

func renderPayload(payload map[string]any) string {
	var b strings.Builder
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, inner := range v {
				b.WriteString(key)
				b.WriteByte(' ')
				walk(inner)
			}
		case []map[string]any:
			for _, inner := range v {
				walk(inner)
			}
		case []any:
			for _, inner := range v {
				walk(inner)
			}
		case map[string]string:
			// tags lands in the payload as a native map[string]string.
			for key, inner := range v {
				b.WriteString(key)
				b.WriteByte(' ')
				b.WriteString(inner)
				b.WriteByte(' ')
			}
		case []string:
			// correlation_anchors lands as a native []string.
			for _, inner := range v {
				b.WriteString(inner)
				b.WriteByte(' ')
			}
		case string:
			b.WriteString(v)
			b.WriteByte(' ')
		}
	}
	walk(map[string]any(payload))
	return b.String()
}

// TestRenderPayloadScansStringContainers guards the defense-in-depth scan used
// by TestScannerNeverEmitsEnvVarsOrTokens. The emitted payload carries tags as a
// native map[string]string and correlation_anchors as a native []string, so a
// renderPayload walker that only recursed into map[string]any and []any would
// silently skip a token hiding in either container. This proves the walker
// surfaces values from both so the forbidden-string assertion can catch them.
func TestRenderPayloadScansStringContainers(t *testing.T) {
	payload := map[string]any{
		"tags":                map[string]string{"owner": "tag_secret_value"},
		"correlation_anchors": []string{"anchor_secret_value"},
	}
	rendered := renderPayload(payload)
	for _, needle := range []string{"tag_secret_value", "anchor_secret_value"} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("renderPayload did not scan %q in its string container: %q", needle, rendered)
		}
	}
}

func TestScannerUsesBoundaryPartitionForSynthesizedARNs(t *testing.T) {
	cases := []struct {
		name          string
		region        string
		wantPartition string
	}{
		{name: "govcloud", region: "us-gov-west-1", wantPartition: "aws-us-gov"},
		{name: "china", region: "cn-north-1", wantPartition: "aws-cn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := testBoundary()
			boundary.Region = tc.region
			// Drop API ARNs so the scanner must synthesize them partition-aware.
			client := sampleClient()
			client.apps[0].ARN = ""
			client.branchesByApp["d111111111"][0].ARN = ""

			envelopes, err := Scanner{Client: client}.Scan(context.Background(), boundary)
			if err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			apps := resourcesByType(t, envelopes, awscloud.ResourceTypeAmplifyApp)
			if len(apps) != 1 {
				t.Fatalf("app resources = %d, want 1", len(apps))
			}
			wantAppARN := "arn:" + tc.wantPartition + ":amplify:" + tc.region + ":123456789012:apps/d111111111"
			if apps[0]["resource_id"] != wantAppARN {
				t.Fatalf("app resource_id = %v, want %v", apps[0]["resource_id"], wantAppARN)
			}
			branchToApp := relationshipsByType(t, envelopes, awscloud.RelationshipAmplifyBranchBelongsToApp)
			if len(branchToApp) != 1 || branchToApp[0]["target_resource_id"] != wantAppARN {
				t.Fatalf("branch->app target = %#v, want %v", branchToApp, wantAppARN)
			}
		})
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceIAM
	_, err := Scanner{Client: sampleClient()}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service-kind mismatch error")
	}
}

func TestScannerDefaultsServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	envelopes, err := Scanner{Client: sampleClient()}.Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, env := range envelopes {
		if env.Payload["service_kind"] != awscloud.ServiceAmplify {
			t.Fatalf("service_kind = %v, want amplify", env.Payload["service_kind"])
		}
	}
}
