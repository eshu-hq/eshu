// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// fakeKMSAPI is a page-driven stand-in for the bounded apiClient surface. Each
// List* method walks its configured pages and returns an empty page once they
// are exhausted, so tests can exercise pagination termination without a real
// AWS client.
type fakeKMSAPI struct {
	listKeysPages    []*awskms.ListKeysOutput
	listKeysCalls    int
	describeKey      map[string]*kmstypes.KeyMetadata
	listAliasesPages []*awskms.ListAliasesOutput
	listAliasesCalls int

	listGrantsByKey   map[string][]*awskms.ListGrantsOutput
	listGrantsCounter map[string]int

	listPoliciesByKey   map[string][]*awskms.ListKeyPoliciesOutput
	listPoliciesCounter map[string]int
	// listPoliciesTotalCalls counts every ListKeyPolicies invocation,
	// including those past the configured pages, so a test can detect an
	// unwanted extra page request after a non-truncated page.
	listPoliciesTotalCalls map[string]int

	rotationByKey             map[string]*awskms.GetKeyRotationStatusOutput
	rotationErr               error
	getKeyRotationStatusCalls int

	listTagsByKey   map[string][]*awskms.ListResourceTagsOutput
	listTagsCounter map[string]int

	keyPolicyByKey    map[string]string
	getKeyPolicyCalls int
}

func (f *fakeKMSAPI) ListKeys(_ context.Context, _ *awskms.ListKeysInput, _ ...func(*awskms.Options)) (*awskms.ListKeysOutput, error) {
	if f.listKeysCalls >= len(f.listKeysPages) {
		return &awskms.ListKeysOutput{}, nil
	}
	page := f.listKeysPages[f.listKeysCalls]
	f.listKeysCalls++
	return page, nil
}

func (f *fakeKMSAPI) DescribeKey(_ context.Context, input *awskms.DescribeKeyInput, _ ...func(*awskms.Options)) (*awskms.DescribeKeyOutput, error) {
	id := aws.ToString(input.KeyId)
	metadata, ok := f.describeKey[id]
	if !ok {
		return &awskms.DescribeKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{KeyId: aws.String(id)}}, nil
	}
	return &awskms.DescribeKeyOutput{KeyMetadata: metadata}, nil
}

func (f *fakeKMSAPI) ListAliases(_ context.Context, _ *awskms.ListAliasesInput, _ ...func(*awskms.Options)) (*awskms.ListAliasesOutput, error) {
	if f.listAliasesCalls >= len(f.listAliasesPages) {
		return &awskms.ListAliasesOutput{}, nil
	}
	page := f.listAliasesPages[f.listAliasesCalls]
	f.listAliasesCalls++
	return page, nil
}

func (f *fakeKMSAPI) ListGrants(_ context.Context, input *awskms.ListGrantsInput, _ ...func(*awskms.Options)) (*awskms.ListGrantsOutput, error) {
	id := aws.ToString(input.KeyId)
	if f.listGrantsCounter == nil {
		f.listGrantsCounter = map[string]int{}
	}
	pages := f.listGrantsByKey[id]
	index := f.listGrantsCounter[id]
	if index >= len(pages) {
		return &awskms.ListGrantsOutput{}, nil
	}
	f.listGrantsCounter[id] = index + 1
	return pages[index], nil
}

func (f *fakeKMSAPI) ListKeyPolicies(_ context.Context, input *awskms.ListKeyPoliciesInput, _ ...func(*awskms.Options)) (*awskms.ListKeyPoliciesOutput, error) {
	id := aws.ToString(input.KeyId)
	if f.listPoliciesCounter == nil {
		f.listPoliciesCounter = map[string]int{}
	}
	if f.listPoliciesTotalCalls == nil {
		f.listPoliciesTotalCalls = map[string]int{}
	}
	f.listPoliciesTotalCalls[id]++
	pages := f.listPoliciesByKey[id]
	index := f.listPoliciesCounter[id]
	if index >= len(pages) {
		return &awskms.ListKeyPoliciesOutput{}, nil
	}
	f.listPoliciesCounter[id] = index + 1
	return pages[index], nil
}

func (f *fakeKMSAPI) GetKeyRotationStatus(_ context.Context, input *awskms.GetKeyRotationStatusInput, _ ...func(*awskms.Options)) (*awskms.GetKeyRotationStatusOutput, error) {
	f.getKeyRotationStatusCalls++
	if f.rotationErr != nil {
		return nil, f.rotationErr
	}
	id := aws.ToString(input.KeyId)
	if output, ok := f.rotationByKey[id]; ok {
		return output, nil
	}
	return &awskms.GetKeyRotationStatusOutput{}, nil
}

func (f *fakeKMSAPI) ListResourceTags(_ context.Context, input *awskms.ListResourceTagsInput, _ ...func(*awskms.Options)) (*awskms.ListResourceTagsOutput, error) {
	id := aws.ToString(input.KeyId)
	if f.listTagsCounter == nil {
		f.listTagsCounter = map[string]int{}
	}
	pages := f.listTagsByKey[id]
	index := f.listTagsCounter[id]
	if index >= len(pages) {
		return &awskms.ListResourceTagsOutput{}, nil
	}
	f.listTagsCounter[id] = index + 1
	return pages[index], nil
}

func (f *fakeKMSAPI) GetKeyPolicy(_ context.Context, input *awskms.GetKeyPolicyInput, _ ...func(*awskms.Options)) (*awskms.GetKeyPolicyOutput, error) {
	f.getKeyPolicyCalls++
	id := aws.ToString(input.KeyId)
	policy, ok := f.keyPolicyByKey[id]
	if !ok {
		return &awskms.GetKeyPolicyOutput{}, nil
	}
	return &awskms.GetKeyPolicyOutput{
		Policy:     aws.String(policy),
		PolicyName: input.PolicyName,
	}, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

var _ apiClient = (*fakeKMSAPI)(nil)
