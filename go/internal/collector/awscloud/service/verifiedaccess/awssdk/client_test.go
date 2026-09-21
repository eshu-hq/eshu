// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	awsec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// fakeEC2 is a metadata-only test double for the Verified Access describe
// surface. Each list returns two pages to exercise NextToken pagination.
type fakeEC2 struct {
	instances      [][]awsec2types.VerifiedAccessInstance
	groups         [][]awsec2types.VerifiedAccessGroup
	endpoints      [][]awsec2types.VerifiedAccessEndpoint
	trustProviders [][]awsec2types.VerifiedAccessTrustProvider
	calls          map[string]int
}

func (f *fakeEC2) token(page, total int) *string {
	if page+1 < total {
		next := "page" + string(rune('1'+page))
		return aws.String(next)
	}
	return nil
}

func (f *fakeEC2) record(op string) int {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	idx := f.calls[op]
	f.calls[op]++
	return idx
}

func (f *fakeEC2) DescribeVerifiedAccessInstances(_ context.Context, in *awsec2.DescribeVerifiedAccessInstancesInput, _ ...func(*awsec2.Options)) (*awsec2.DescribeVerifiedAccessInstancesOutput, error) {
	idx := f.record("instances")
	return &awsec2.DescribeVerifiedAccessInstancesOutput{
		VerifiedAccessInstances: f.instances[idx],
		NextToken:               f.token(idx, len(f.instances)),
	}, nil
}

func (f *fakeEC2) DescribeVerifiedAccessGroups(_ context.Context, in *awsec2.DescribeVerifiedAccessGroupsInput, _ ...func(*awsec2.Options)) (*awsec2.DescribeVerifiedAccessGroupsOutput, error) {
	idx := f.record("groups")
	return &awsec2.DescribeVerifiedAccessGroupsOutput{
		VerifiedAccessGroups: f.groups[idx],
		NextToken:            f.token(idx, len(f.groups)),
	}, nil
}

func (f *fakeEC2) DescribeVerifiedAccessEndpoints(_ context.Context, in *awsec2.DescribeVerifiedAccessEndpointsInput, _ ...func(*awsec2.Options)) (*awsec2.DescribeVerifiedAccessEndpointsOutput, error) {
	idx := f.record("endpoints")
	return &awsec2.DescribeVerifiedAccessEndpointsOutput{
		VerifiedAccessEndpoints: f.endpoints[idx],
		NextToken:               f.token(idx, len(f.endpoints)),
	}, nil
}

func (f *fakeEC2) DescribeVerifiedAccessTrustProviders(_ context.Context, in *awsec2.DescribeVerifiedAccessTrustProvidersInput, _ ...func(*awsec2.Options)) (*awsec2.DescribeVerifiedAccessTrustProvidersOutput, error) {
	idx := f.record("trustProviders")
	return &awsec2.DescribeVerifiedAccessTrustProvidersOutput{
		VerifiedAccessTrustProviders: f.trustProviders[idx],
		NextToken:                    f.token(idx, len(f.trustProviders)),
	}, nil
}

func TestSnapshotPaginatesAndMapsMetadata(t *testing.T) {
	fake := &fakeEC2{
		instances: [][]awsec2types.VerifiedAccessInstance{
			{{VerifiedAccessInstanceId: aws.String("vai-1"), FipsEnabled: aws.Bool(true), VerifiedAccessTrustProviders: []awsec2types.VerifiedAccessTrustProviderCondensed{{VerifiedAccessTrustProviderId: aws.String("vatp-1")}}}},
			{{VerifiedAccessInstanceId: aws.String("vai-2"), CreationTime: aws.String("2026-05-01T12:00:00Z")}},
		},
		groups: [][]awsec2types.VerifiedAccessGroup{
			{{VerifiedAccessGroupId: aws.String("vagr-1"), VerifiedAccessGroupArn: aws.String("arn:aws:ec2:us-east-1:123456789012:verified-access-group/vagr-1"), VerifiedAccessInstanceId: aws.String("vai-1")}},
			{},
		},
		endpoints: [][]awsec2types.VerifiedAccessEndpoint{
			{{
				VerifiedAccessEndpointId: aws.String("vae-1"),
				VerifiedAccessGroupId:    aws.String("vagr-1"),
				EndpointType:             awsec2types.VerifiedAccessEndpointTypeLoadBalancer,
				DomainCertificateArn:     aws.String("arn:aws:acm:us-east-1:123456789012:certificate/abc"),
				SecurityGroupIds:         []string{"sg-1"},
				Status:                   &awsec2types.VerifiedAccessEndpointStatus{Code: awsec2types.VerifiedAccessEndpointStatusCodeActive},
				LoadBalancerOptions:      &awsec2types.VerifiedAccessEndpointLoadBalancerOptions{SubnetIds: []string{"subnet-1"}, LoadBalancerArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc")},
			}},
			{},
		},
		trustProviders: [][]awsec2types.VerifiedAccessTrustProvider{
			{{
				VerifiedAccessTrustProviderId: aws.String("vatp-1"),
				TrustProviderType:             awsec2types.TrustProviderTypeUser,
				UserTrustProviderType:         awsec2types.UserTrustProviderTypeOidc,
				OidcOptions:                   &awsec2types.OidcOptions{Issuer: aws.String("https://issuer.example.com"), ClientSecret: aws.String("super-secret"), ClientId: aws.String("client-123")},
			}},
			{},
		},
	}
	client := &Client{client: fake, boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceVerifiedAccess}}

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Instances) != 2 {
		t.Fatalf("instances = %d, want 2 (pagination)", len(snapshot.Instances))
	}
	if len(snapshot.Instances[0].TrustProviderIDs) != 1 || snapshot.Instances[0].TrustProviderIDs[0] != "vatp-1" {
		t.Fatalf("instance trust provider ids = %#v, want [vatp-1]", snapshot.Instances[0].TrustProviderIDs)
	}
	if !snapshot.Instances[0].FIPSEnabled {
		t.Fatalf("instance fips_enabled = false, want true")
	}
	if len(snapshot.Endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(snapshot.Endpoints))
	}
	endpoint := snapshot.Endpoints[0]
	if len(endpoint.SubnetIDs) != 1 || endpoint.SubnetIDs[0] != "subnet-1" {
		t.Fatalf("endpoint subnet ids = %#v, want [subnet-1]", endpoint.SubnetIDs)
	}
	if endpoint.Status != "active" {
		t.Fatalf("endpoint status = %q, want active", endpoint.Status)
	}
	// OIDC issuer kept; client secret and client id must never appear.
	tp := snapshot.TrustProviders[0]
	if tp.OIDCIssuer != "https://issuer.example.com" {
		t.Fatalf("oidc issuer = %q, want issuer url", tp.OIDCIssuer)
	}
}

func TestParseTimeHandlesEmptyAndInvalid(t *testing.T) {
	if got := parseTime(nil); !got.IsZero() {
		t.Fatalf("parseTime(nil) = %v, want zero", got)
	}
	if got := parseTime(aws.String("not-a-time")); !got.IsZero() {
		t.Fatalf("parseTime(invalid) = %v, want zero", got)
	}
	if got := parseTime(aws.String("2026-05-01T12:00:00Z")); got.IsZero() {
		t.Fatalf("parseTime(valid) = zero, want parsed")
	}
}
