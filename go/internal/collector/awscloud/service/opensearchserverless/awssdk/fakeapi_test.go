// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsaoss "github.com/aws/aws-sdk-go-v2/service/opensearchserverless"
	awsaosstypes "github.com/aws/aws-sdk-go-v2/service/opensearchserverless/types"
)

// fakeAPI is an in-memory apiClient used by the adapter tests. It returns the
// configured summaries and details and records nothing it should never call,
// since the apiClient interface itself excludes the mutation and data-plane
// surface (enforced by the exclusion test).
type fakeAPI struct {
	collectionSummaries  []awsaosstypes.CollectionSummary
	collectionDetails    map[string]awsaosstypes.CollectionDetail
	encryptionPolicies   []awsaosstypes.SecurityPolicySummary
	networkPolicies      []awsaosstypes.SecurityPolicySummary
	encryptionDetail     map[string]awsaosstypes.SecurityPolicyDetail
	vpcEndpointSummaries []awsaosstypes.VpcEndpointSummary
	vpcEndpointDetails   map[string]awsaosstypes.VpcEndpointDetail
	tags                 map[string][]awsaosstypes.Tag
}

func (f *fakeAPI) ListCollections(
	_ context.Context,
	_ *awsaoss.ListCollectionsInput,
	_ ...func(*awsaoss.Options),
) (*awsaoss.ListCollectionsOutput, error) {
	return &awsaoss.ListCollectionsOutput{CollectionSummaries: f.collectionSummaries}, nil
}

func (f *fakeAPI) BatchGetCollection(
	_ context.Context,
	input *awsaoss.BatchGetCollectionInput,
	_ ...func(*awsaoss.Options),
) (*awsaoss.BatchGetCollectionOutput, error) {
	var details []awsaosstypes.CollectionDetail
	for _, id := range input.Ids {
		if detail, ok := f.collectionDetails[id]; ok {
			details = append(details, detail)
		}
	}
	return &awsaoss.BatchGetCollectionOutput{CollectionDetails: details}, nil
}

func (f *fakeAPI) ListSecurityPolicies(
	_ context.Context,
	input *awsaoss.ListSecurityPoliciesInput,
	_ ...func(*awsaoss.Options),
) (*awsaoss.ListSecurityPoliciesOutput, error) {
	switch input.Type {
	case awsaosstypes.SecurityPolicyTypeEncryption:
		return &awsaoss.ListSecurityPoliciesOutput{SecurityPolicySummaries: f.encryptionPolicies}, nil
	case awsaosstypes.SecurityPolicyTypeNetwork:
		return &awsaoss.ListSecurityPoliciesOutput{SecurityPolicySummaries: f.networkPolicies}, nil
	default:
		return &awsaoss.ListSecurityPoliciesOutput{}, nil
	}
}

func (f *fakeAPI) GetSecurityPolicy(
	_ context.Context,
	input *awsaoss.GetSecurityPolicyInput,
	_ ...func(*awsaoss.Options),
) (*awsaoss.GetSecurityPolicyOutput, error) {
	detail, ok := f.encryptionDetail[aws.ToString(input.Name)]
	if !ok {
		return &awsaoss.GetSecurityPolicyOutput{}, nil
	}
	return &awsaoss.GetSecurityPolicyOutput{SecurityPolicyDetail: &detail}, nil
}

func (f *fakeAPI) ListVpcEndpoints(
	_ context.Context,
	_ *awsaoss.ListVpcEndpointsInput,
	_ ...func(*awsaoss.Options),
) (*awsaoss.ListVpcEndpointsOutput, error) {
	return &awsaoss.ListVpcEndpointsOutput{VpcEndpointSummaries: f.vpcEndpointSummaries}, nil
}

func (f *fakeAPI) BatchGetVpcEndpoint(
	_ context.Context,
	input *awsaoss.BatchGetVpcEndpointInput,
	_ ...func(*awsaoss.Options),
) (*awsaoss.BatchGetVpcEndpointOutput, error) {
	var details []awsaosstypes.VpcEndpointDetail
	for _, id := range input.Ids {
		if detail, ok := f.vpcEndpointDetails[id]; ok {
			details = append(details, detail)
		}
	}
	return &awsaoss.BatchGetVpcEndpointOutput{VpcEndpointDetails: details}, nil
}

func (f *fakeAPI) ListTagsForResource(
	_ context.Context,
	input *awsaoss.ListTagsForResourceInput,
	_ ...func(*awsaoss.Options),
) (*awsaoss.ListTagsForResourceOutput, error) {
	return &awsaoss.ListTagsForResourceOutput{Tags: f.tags[aws.ToString(input.ResourceArn)]}, nil
}
