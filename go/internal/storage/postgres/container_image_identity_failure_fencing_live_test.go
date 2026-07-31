// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestContainerImageIdentityFailureStatusAuthorizationLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name        string
		suffix      string
		cause       error
		maxAttempts int
		wantStatus  string
		wantClass   string
	}{
		{
			name: "retry", suffix: "retry",
			cause:       containerImageIdentityRetryableFailure{},
			maxAttempts: 3, wantStatus: "retrying",
			wantClass: "reducer_retryable",
		},
		{
			name: "dead letter", suffix: "dead-letter",
			cause:       errors.New("synthetic terminal failure"),
			maxAttempts: 1, wantStatus: "dead_letter",
			wantClass: "projection_bug",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			scopeID := "repository:5854-failure-" + test.suffix
			generationID := "generation:5854-failure-" + test.suffix
			workItemID := "work-5854-failure-" + test.suffix
			owner := "reducer-5854-failure-" + test.suffix
			seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
			seedContainerImageIdentityAckGeneration(
				t, ctx, db, scopeID, generationID,
			)
			seedContainerImageIdentityAckWorkItem(
				t, ctx, db, workItemID, scopeID, generationID,
				owner, now.Add(time.Minute), now,
			)
			insertContainerImageIdentityCutoverMarker(
				t, ctx, db, scopeID, generationID,
			)
			queue := ReducerQueue{
				db: SQLDB{DB: db}, LeaseOwner: owner,
				LeaseDuration: time.Minute, RetryDelay: time.Second,
				MaxAttempts: test.maxAttempts, JitterFraction: 0,
				Now: func() time.Time { return now },
			}
			intent := reducer.Intent{
				IntentID:     workItemID,
				Domain:       reducer.DomainContainerImageIdentity,
				AttemptCount: 1,
				ClaimEpoch:   1,
			}
			if err := queue.Fail(ctx, intent, test.cause); err != nil {
				t.Fatalf("Fail() error = %v", err)
			}
			assertContainerImageIdentityFailureOutcome(
				t, ctx, db, workItemID, test.wantStatus,
				test.wantClass, 1, test.wantStatus,
			)
		})
	}

	t.Run("stale epoch rejects terminal mutation", func(t *testing.T) {
		const (
			scopeID      = "repository:5854-failure-stale"
			generationID = "generation:5854-failure-stale"
			workItemID   = "work-5854-failure-stale"
			owner        = "reducer-5854-failure-stale"
		)
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
		seedContainerImageIdentityAckWorkItem(
			t, ctx, db, workItemID, scopeID, generationID,
			owner, now.Add(time.Minute), now,
		)
		insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeID, generationID)
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch = 2
WHERE work_item_id = $1
`, workItemID); err != nil {
			t.Fatalf("advance stale failure epoch: %v", err)
		}
		queue := ReducerQueue{
			db: SQLDB{DB: db}, LeaseOwner: owner,
			LeaseDuration: time.Minute, MaxAttempts: 1,
			Now: func() time.Time { return now },
		}
		err := queue.Fail(
			ctx,
			reducer.Intent{
				IntentID:     workItemID,
				Domain:       reducer.DomainContainerImageIdentity,
				AttemptCount: 1,
				ClaimEpoch:   1,
			},
			errors.New("synthetic stale terminal failure"),
		)
		if !errors.Is(err, ErrReducerClaimRejected) {
			t.Fatalf("stale Fail() error = %v, want claim rejection", err)
		}
		assertContainerImageIdentityFailureOutcome(
			t, ctx, db, workItemID, "running", "", 2, "running",
		)
	})
}

func assertContainerImageIdentityFailureOutcome(
	t *testing.T,
	ctx context.Context,
	db interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	workItemID string,
	wantStatus string,
	wantClass string,
	wantClaimEpoch int64,
	wantAuthorizedStatus string,
) {
	t.Helper()
	var (
		status           string
		failureClass     string
		claimEpoch       int64
		authorizedStatus string
	)
	if err := db.QueryRowContext(ctx, `
SELECT
    status,
    COALESCE(failure_class, ''),
    container_image_identity_claim_epoch,
    container_image_identity_v2_authorized_status
FROM fact_work_items
WHERE work_item_id = $1
`, workItemID).Scan(
		&status,
		&failureClass,
		&claimEpoch,
		&authorizedStatus,
	); err != nil {
		t.Fatalf("read failure outcome %s: %v", workItemID, err)
	}
	if status != wantStatus ||
		failureClass != wantClass ||
		claimEpoch != wantClaimEpoch ||
		authorizedStatus != wantAuthorizedStatus {
		t.Fatalf(
			"failure outcome %s = status %q class %q claim %d authorized %q, want %q/%q/%d/%q",
			workItemID,
			status,
			failureClass,
			claimEpoch,
			authorizedStatus,
			wantStatus,
			wantClass,
			wantClaimEpoch,
			wantAuthorizedStatus,
		)
	}
}
