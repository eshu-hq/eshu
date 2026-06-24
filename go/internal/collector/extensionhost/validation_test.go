// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestSourceRejectsUnclaimedWorkItemAsTerminalInvalidClaim(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	item.Status = workflow.WorkItemStatusPending
	source := mustNewSource(t, &recordingRunner{result: completeResult(item)}, nil)

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want invalid claim error")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	assertFailure(t, err, FailureClassInvalidClaim, true)
}

func TestSourceRejectsUnboundedStatusFailureClass(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	result := baseResult(item, sdkcollector.ResultRetryable)
	result.Statuses = []sdkcollector.Status{{
		Class:             sdkcollector.StatusFailure,
		FailureClass:      " rate_limited ",
		RetryAfterSeconds: 30,
	}}
	source := mustNewSource(t, &recordingRunner{result: result}, nil)

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want bounded status validation error")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	assertFailure(t, err, FailureClassInvalidResult, true)
}
