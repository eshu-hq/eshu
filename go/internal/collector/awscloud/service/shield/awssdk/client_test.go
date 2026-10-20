// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsshield "github.com/aws/aws-sdk-go-v2/service/shield"
	awsshieldtypes "github.com/aws/aws-sdk-go-v2/service/shield/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

const (
	protectionARN = "arn:aws:shield::123456789012:protection/abcd1234"
	resourceARN   = "arn:aws:cloudfront::123456789012:distribution/E2EXAMPLE"
)

func testClient(api apiClient) *Client {
	return &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceShield},
	}
}

func TestClientListsProtectionsAcrossPages(t *testing.T) {
	api := &fakeAPI{
		protectionPages: []*awsshield.ListProtectionsOutput{
			{
				Protections: []awsshieldtypes.Protection{{
					ProtectionArn: aws.String(protectionARN),
					Id:            aws.String("abcd1234"),
					Name:          aws.String("cf-protection"),
					ResourceArn:   aws.String(resourceARN),
				}},
				NextToken: aws.String("page-2"),
			},
			{
				Protections: []awsshieldtypes.Protection{{
					ProtectionArn: aws.String("arn:aws:shield::123456789012:protection/second"),
					Id:            aws.String("second"),
					ResourceArn:   aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc"),
				}},
			},
		},
	}
	protections, err := testClient(api).ListProtections(context.Background())
	if err != nil {
		t.Fatalf("ListProtections() error = %v, want nil", err)
	}
	if got, want := len(protections), 2; got != want {
		t.Fatalf("protection count = %d, want %d", got, want)
	}
	if got := protections[0].ARN; got != protectionARN {
		t.Fatalf("protection[0].ARN = %q, want %q", got, protectionARN)
	}
	if got := protections[0].ResourceARN; got != resourceARN {
		t.Fatalf("protection[0].ResourceARN = %q, want %q", got, resourceARN)
	}
	if api.listCalls != 2 {
		t.Fatalf("ListProtections called %d times, want 2", api.listCalls)
	}
}

func TestClientDescribesSubscriptionMetadataOnly(t *testing.T) {
	api := &fakeAPI{
		subscription: &awsshield.DescribeSubscriptionOutput{
			Subscription: &awsshieldtypes.Subscription{
				SubscriptionArn: aws.String("arn:aws:shield::123456789012:subscription"),
				AutoRenew:       awsshieldtypes.AutoRenewEnabled,
				// Billing detail present on the API response that must not survive.
				SubscriptionLimits:      &awsshieldtypes.SubscriptionLimits{},
				TimeCommitmentInSeconds: 31536000,
			},
		},
		state: &awsshield.GetSubscriptionStateOutput{
			SubscriptionState: awsshieldtypes.SubscriptionStateActive,
		},
	}
	subscription, err := testClient(api).DescribeSubscription(context.Background())
	if err != nil {
		t.Fatalf("DescribeSubscription() error = %v, want nil", err)
	}
	if subscription == nil {
		t.Fatalf("DescribeSubscription() = nil, want subscription")
	}
	if got := subscription.State; got != "ACTIVE" {
		t.Fatalf("subscription state = %q, want ACTIVE", got)
	}
	if got := subscription.AutoRenew; got != "ENABLED" {
		t.Fatalf("subscription auto_renew = %q, want ENABLED", got)
	}
	if api.subscriptionState != 1 {
		t.Fatalf("GetSubscriptionState called %d times, want 1", api.subscriptionState)
	}
}

func TestClientDescribeSubscriptionNilWhenAbsent(t *testing.T) {
	api := &fakeAPI{
		subscriptionErr: &awsshieldtypes.ResourceNotFoundException{},
	}
	subscription, err := testClient(api).DescribeSubscription(context.Background())
	if err != nil {
		t.Fatalf("DescribeSubscription() error = %v, want nil", err)
	}
	if subscription != nil {
		t.Fatalf("DescribeSubscription() = %#v, want nil", subscription)
	}
	if api.subscriptionState != 0 {
		t.Fatalf("GetSubscriptionState called %d times, want 0 (no subscription)", api.subscriptionState)
	}
}
