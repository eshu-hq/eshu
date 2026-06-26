// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"testing"
)

func TestNormalizeBitbucketPushFromFixture(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "testdata/bitbucket/push.json")
	expected := loadExpected(t, "testdata/bitbucket/push_expected.json")

	trigger, err := NormalizeBitbucket("repo:push", expected.DeliveryID, payload, "")
	if err != nil {
		t.Fatalf("NormalizeBitbucket() error = %v, want nil", err)
	}

	assertTriggerFromExpected(t, trigger, expected)
}

func TestNormalizeBitbucketPullRequestFulfilledFromFixture(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "testdata/bitbucket/pull_request_fulfilled.json")
	expected := loadExpected(t, "testdata/bitbucket/pull_request_fulfilled_expected.json")

	trigger, err := NormalizeBitbucket("pullrequest:fulfilled", expected.DeliveryID, payload, "")
	if err != nil {
		t.Fatalf("NormalizeBitbucket() error = %v, want nil", err)
	}

	assertTriggerFromExpected(t, trigger, expected)
}
