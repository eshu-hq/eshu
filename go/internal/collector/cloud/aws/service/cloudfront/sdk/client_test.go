// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awscloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	awscloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientListsCloudFrontDistributionMetadataOnly(t *testing.T) {
	distributionARN := "arn:aws:cloudfront::123456789012:distribution/EDFDVBD632BHDS5"
	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/cert-1"
	webACLARN := "arn:aws:wafv2:us-east-1:123456789012:global/webacl/orders/a1b2c3"
	lastModified := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	api := &fakeCloudFrontAPI{
		distributionPages: []*awscloudfront.ListDistributionsOutput{{
			DistributionList: &awscloudfronttypes.DistributionList{
				Items: []awscloudfronttypes.DistributionSummary{{
					ARN:              awsv2.String(distributionARN),
					Id:               awsv2.String("EDFDVBD632BHDS5"),
					DomainName:       awsv2.String("d111111abcdef8.cloudfront.net"),
					Status:           awsv2.String("Deployed"),
					Enabled:          awsv2.Bool(true),
					Comment:          awsv2.String("orders edge"),
					HttpVersion:      awscloudfronttypes.HttpVersionHttp2and3,
					IsIPV6Enabled:    awsv2.Bool(true),
					LastModifiedTime: awsv2.Time(lastModified),
					PriceClass:       awscloudfronttypes.PriceClassPriceClass100,
					Staging:          awsv2.Bool(false),
					WebACLId:         awsv2.String(webACLARN),
					Aliases: &awscloudfronttypes.Aliases{
						Items: []string{"orders.example.com"},
					},
					Origins: &awscloudfronttypes.Origins{
						Items: []awscloudfronttypes.Origin{{
							Id:                    awsv2.String("orders-origin"),
							DomainName:            awsv2.String("orders.s3.us-east-1.amazonaws.com"),
							OriginPath:            awsv2.String("/public"),
							OriginAccessControlId: awsv2.String("oac-123"),
							CustomHeaders: &awscloudfronttypes.CustomHeaders{
								Items: []awscloudfronttypes.OriginCustomHeader{{
									HeaderName:  awsv2.String("X-Origin-Auth"),
									HeaderValue: awsv2.String("secret-value"),
								}},
							},
						}},
					},
					DefaultCacheBehavior: &awscloudfronttypes.DefaultCacheBehavior{
						TargetOriginId:          awsv2.String("orders-origin"),
						ViewerProtocolPolicy:    awscloudfronttypes.ViewerProtocolPolicyRedirectToHttps,
						AllowedMethods:          methodList(awscloudfronttypes.MethodGet, awscloudfronttypes.MethodHead),
						CachePolicyId:           awsv2.String("cache-policy-1"),
						OriginRequestPolicyId:   awsv2.String("origin-request-policy-1"),
						ResponseHeadersPolicyId: awsv2.String("response-headers-policy-1"),
						Compress:                awsv2.Bool(true),
					},
					CacheBehaviors: &awscloudfronttypes.CacheBehaviors{
						Items: []awscloudfronttypes.CacheBehavior{{
							PathPattern:             awsv2.String("/api/*"),
							TargetOriginId:          awsv2.String("orders-origin"),
							ViewerProtocolPolicy:    awscloudfronttypes.ViewerProtocolPolicyHttpsOnly,
							AllowedMethods:          methodList(awscloudfronttypes.MethodGet, awscloudfronttypes.MethodHead, awscloudfronttypes.MethodOptions),
							CachePolicyId:           awsv2.String("cache-policy-2"),
							OriginRequestPolicyId:   awsv2.String("origin-request-policy-2"),
							ResponseHeadersPolicyId: awsv2.String("response-headers-policy-2"),
							Compress:                awsv2.Bool(false),
						}},
					},
					ViewerCertificate: &awscloudfronttypes.ViewerCertificate{
						ACMCertificateArn:            awsv2.String(certificateARN),
						CloudFrontDefaultCertificate: awsv2.Bool(false),
						IAMCertificateId:             awsv2.String("iam-cert-1"),
						MinimumProtocolVersion:       awscloudfronttypes.MinimumProtocolVersionTLSv122021,
						SSLSupportMethod:             awscloudfronttypes.SSLSupportMethodSniOnly,
					},
				}},
				IsTruncated: awsv2.Bool(true),
				NextMarker:  awsv2.String("next-page"),
			},
		}, {
			DistributionList: &awscloudfronttypes.DistributionList{
				Items: []awscloudfronttypes.DistributionSummary{{
					ARN:        awsv2.String("arn:aws:cloudfront::123456789012:distribution/ESECOND"),
					Id:         awsv2.String("ESECOND"),
					DomainName: awsv2.String("d222222abcdef8.cloudfront.net"),
					Status:     awsv2.String("InProgress"),
				}},
			},
		}},
		tags: map[string]*awscloudfront.ListTagsForResourceOutput{
			distributionARN: {
				Tags: &awscloudfronttypes.Tags{Items: []awscloudfronttypes.Tag{{
					Key:   awsv2.String("Environment"),
					Value: awsv2.String("prod"),
				}}},
			},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	distributions, err := adapter.ListDistributions(context.Background())
	if err != nil {
		t.Fatalf("ListDistributions() error = %v, want nil", err)
	}

	if got, want := len(distributions), 2; got != want {
		t.Fatalf("len(distributions) = %d, want %d", got, want)
	}
	if got, want := api.maxItems, []int32{100, 100}; !int32SlicesEqual(got, want) {
		t.Fatalf("ListDistributions MaxItems = %#v, want %#v", got, want)
	}
	if got, want := api.markers, []string{"", "next-page"}; !stringSlicesEqual(got, want) {
		t.Fatalf("ListDistributions Marker = %#v, want %#v", got, want)
	}
	if got, want := api.tagResources, []string{distributionARN, "arn:aws:cloudfront::123456789012:distribution/ESECOND"}; !stringSlicesEqual(got, want) {
		t.Fatalf("ListTagsForResource Resource = %#v, want %#v", got, want)
	}
	first := distributions[0]
	if first.ARN != distributionARN || first.ID != "EDFDVBD632BHDS5" || first.DomainName != "d111111abcdef8.cloudfront.net" {
		t.Fatalf("distribution identity = %#v, want mapped ARN/id/domain", first)
	}
	if first.ViewerCertificate.ACMCertificateARN != certificateARN || first.ViewerCertificate.IAMCertificateID != "iam-cert-1" {
		t.Fatalf("viewer certificate = %#v, want mapped metadata", first.ViewerCertificate)
	}
	if first.Origins[0].CustomHeaderNames[0] != "X-Origin-Auth" {
		t.Fatalf("origin custom headers = %#v, want names only", first.Origins[0].CustomHeaderNames)
	}
	if first.Tags["Environment"] != "prod" {
		t.Fatalf("tags = %#v, want Environment tag", first.Tags)
	}
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "aws-global",
		ServiceKind: aws.ServiceCloudFront,
	}
}

type fakeCloudFrontAPI struct {
	distributionPages []*awscloudfront.ListDistributionsOutput
	distributionCalls int
	maxItems          []int32
	markers           []string
	tags              map[string]*awscloudfront.ListTagsForResourceOutput
	tagResources      []string
}

func (f *fakeCloudFrontAPI) ListDistributions(
	_ context.Context,
	input *awscloudfront.ListDistributionsInput,
	_ ...func(*awscloudfront.Options),
) (*awscloudfront.ListDistributionsOutput, error) {
	f.maxItems = append(f.maxItems, awsv2.ToInt32(input.MaxItems))
	f.markers = append(f.markers, awsv2.ToString(input.Marker))
	if f.distributionCalls >= len(f.distributionPages) {
		return &awscloudfront.ListDistributionsOutput{}, nil
	}
	page := f.distributionPages[f.distributionCalls]
	f.distributionCalls++
	return page, nil
}

func (f *fakeCloudFrontAPI) ListTagsForResource(
	_ context.Context,
	input *awscloudfront.ListTagsForResourceInput,
	_ ...func(*awscloudfront.Options),
) (*awscloudfront.ListTagsForResourceOutput, error) {
	resource := awsv2.ToString(input.Resource)
	f.tagResources = append(f.tagResources, resource)
	if output := f.tags[resource]; output != nil {
		return output, nil
	}
	return &awscloudfront.ListTagsForResourceOutput{}, nil
}

var _ apiClient = (*fakeCloudFrontAPI)(nil)

func methodList(methods ...awscloudfronttypes.Method) *awscloudfronttypes.AllowedMethods {
	return &awscloudfronttypes.AllowedMethods{
		Items: methods,
		CachedMethods: &awscloudfronttypes.CachedMethods{
			Items: []awscloudfronttypes.Method{awscloudfronttypes.MethodGet, awscloudfronttypes.MethodHead},
		},
	}
}

func int32SlicesEqual(got []int32, want []int32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func stringSlicesEqual(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
