// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	awsshield "github.com/aws/aws-sdk-go-v2/service/shield"
)

// fakeAPI is a hand-rolled apiClient stand-in for adapter tests. It serves the
// preconfigured ListProtections pages, DescribeSubscription output, and
// GetSubscriptionState output, and records each call for assertion.
type fakeAPI struct {
	protectionPages []*awsshield.ListProtectionsOutput
	subscription    *awsshield.DescribeSubscriptionOutput
	subscriptionErr error
	state           *awsshield.GetSubscriptionStateOutput

	listCalls         int
	describeCalls     int
	subscriptionState int
}

func (f *fakeAPI) ListProtections(
	_ context.Context,
	_ *awsshield.ListProtectionsInput,
	_ ...func(*awsshield.Options),
) (*awsshield.ListProtectionsOutput, error) {
	idx := f.listCalls
	f.listCalls++
	if idx >= len(f.protectionPages) {
		return &awsshield.ListProtectionsOutput{}, nil
	}
	return f.protectionPages[idx], nil
}

func (f *fakeAPI) DescribeSubscription(
	_ context.Context,
	_ *awsshield.DescribeSubscriptionInput,
	_ ...func(*awsshield.Options),
) (*awsshield.DescribeSubscriptionOutput, error) {
	f.describeCalls++
	if f.subscriptionErr != nil {
		return nil, f.subscriptionErr
	}
	return f.subscription, nil
}

func (f *fakeAPI) GetSubscriptionState(
	_ context.Context,
	_ *awsshield.GetSubscriptionStateInput,
	_ ...func(*awsshield.Options),
) (*awsshield.GetSubscriptionStateOutput, error) {
	f.subscriptionState++
	return f.state, nil
}
