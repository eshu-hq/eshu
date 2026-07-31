// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueFailContainerImageIdentityBindsTerminalAttempt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	intent := reducer.Intent{
		IntentID:     "container-image-identity-fail-terminal",
		Domain:       reducer.DomainContainerImageIdentity,
		AttemptCount: 7,
		ClaimEpoch:   71,
	}

	if err := queue.Fail(context.Background(), intent, errors.New("synthetic terminal failure")); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("Fail() statements = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	for _, fragment := range []string{
		"status = 'dead_letter'",
		"container_image_identity_v2_authorized_status = CASE",
		"THEN 'dead_letter'",
		"container_image_identity_claim_epoch = $7",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("container identity terminal Fail query missing %q:\n%s", fragment, query)
		}
	}
	if got := db.execs[0].args[6]; got != int64(71) {
		t.Fatalf("container identity terminal Fail epoch arg = %v, want 71", got)
	}
}

func TestReducerQueueFailContainerImageIdentityBindsRetryAttempt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 30, 19, 1, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:             db,
		LeaseOwner:     "reducer",
		LeaseDuration:  time.Minute,
		RetryDelay:     time.Second,
		MaxAttempts:    3,
		JitterFraction: 0,
		Now:            func() time.Time { return now },
	}
	intent := reducer.Intent{
		IntentID:     "container-image-identity-fail-retry",
		Domain:       reducer.DomainContainerImageIdentity,
		AttemptCount: 1,
		ClaimEpoch:   11,
	}

	if err := queue.Fail(
		context.Background(),
		intent,
		containerImageIdentityRetryableFailure{},
	); err != nil {
		t.Fatalf("Fail() error = %v, want nil", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("Fail() statements = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	for _, fragment := range []string{
		"status = 'retrying'",
		"container_image_identity_v2_authorized_status = CASE",
		"THEN 'retrying'",
		"container_image_identity_claim_epoch = $8",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("container identity retry Fail query missing %q:\n%s", fragment, query)
		}
	}
	if got := db.execs[0].args[7]; got != int64(11) {
		t.Fatalf("container identity retry Fail epoch arg = %v, want 11", got)
	}
}

func TestContainerImageIdentityClaimQueriesDispatchEpochTrigger(t *testing.T) {
	t.Parallel()

	for _, query := range []string{claimReducerWorkQuery, claimReducerWorkBatchQuery} {
		for _, want := range []string{
			"SET status = 'claimed'",
			"container_image_identity_claim_epoch =\n            work.container_image_identity_claim_epoch",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("claim query missing %q:\n%s", want, query)
			}
		}
		for _, forbidden := range []string{
			"container_image_identity_claim_epoch = CASE",
		} {
			if strings.Contains(query, forbidden) {
				t.Fatalf("claim query retained per-row branching %q:\n%s", forbidden, query)
			}
		}
	}
}

type containerImageIdentityRetryableFailure struct{}

func (containerImageIdentityRetryableFailure) Error() string {
	return "synthetic retryable failure"
}

func (containerImageIdentityRetryableFailure) Retryable() bool {
	return true
}
