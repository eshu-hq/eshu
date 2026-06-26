// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"testing"
)

func TestNormalizeGitLabPushFromFixture(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "testdata/gitlab/push.json")
	expected := loadExpected(t, "testdata/gitlab/push_expected.json")

	trigger, err := NormalizeGitLab("Push Hook", expected.DeliveryID, payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitLab() error = %v, want nil", err)
	}

	assertTriggerFromExpected(t, trigger, expected)
}

func TestNormalizeGitLabPushFromFixtureByObjectKind(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "testdata/gitlab/push.json")
	expected := loadExpected(t, "testdata/gitlab/push_expected.json")

	trigger, err := NormalizeGitLab("", expected.DeliveryID, payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitLab() error = %v, want nil", err)
	}

	assertTriggerFromExpected(t, trigger, expected)
}

func TestNormalizeGitLabMergeRequestMergedFromFixture(t *testing.T) {
	t.Parallel()

	payload := loadFixture(t, "testdata/gitlab/merge_request_merged.json")
	expected := loadExpected(t, "testdata/gitlab/merge_request_merged_expected.json")

	trigger, err := NormalizeGitLab("Merge Request Hook", expected.DeliveryID, payload, "")
	if err != nil {
		t.Fatalf("NormalizeGitLab() error = %v, want nil", err)
	}

	assertTriggerFromExpected(t, trigger, expected)
}
