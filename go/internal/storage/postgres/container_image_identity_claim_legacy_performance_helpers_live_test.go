// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_ack

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"
)

var legacyContainerImageIdentityClaimQuery = strings.NewReplacer(
	"        container_image_identity_v2_authorized_status = 'superseded',\n",
	"",
	`    SET status = CASE
            WHEN work.domain = 'container_image_identity'
                AND work.container_image_identity_v2_required
                THEN 'running'
            ELSE 'claimed'
        END,
`,
	"    SET status = 'claimed',\n",
	`        container_image_identity_claim_epoch = CASE
            WHEN work.domain = 'container_image_identity'
                THEN work.container_image_identity_claim_epoch + 1
            ELSE work.container_image_identity_claim_epoch
        END,
`,
	"",
	`        container_image_identity_claim_epoch =
            work.container_image_identity_claim_epoch,
`,
	"",
	`        container_image_identity_v2_authorized_status = CASE
            WHEN work.domain = 'container_image_identity'
                AND work.container_image_identity_v2_required
                THEN 'running'
            ELSE ''
        END,
`,
	"",
	"        work.container_image_identity_claim_epoch,\n",
	"",
	"    container_image_identity_claim_epoch,\n",
	"",
).Replace(claimReducerWorkQuery)

func claimContainerImageIdentityLegacyPerformanceRow(
	ctx context.Context,
	queue ReducerQueue,
	expectedID string,
) error {
	now := queue.now()
	rows, err := queue.db.QueryContext(
		ctx,
		legacyContainerImageIdentityClaimQuery,
		now,
		queue.claimDomainFilters(),
		queue.LeaseOwner,
		now.Add(queue.LeaseDuration),
		queue.RequireProjectorDrainBeforeClaim,
		queue.ExpectedSourceLocalProjectors,
		queue.semanticEntityClaimLimit(),
	)
	if err != nil {
		return fmt.Errorf("claim legacy reducer work: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("claim legacy reducer work: %w", err)
		}
		return fmt.Errorf("claim legacy reducer work: no row")
	}

	var (
		intentID     string
		scopeID      string
		generationID string
		domain       string
		attemptCount int
		enqueuedAt   time.Time
		availableAt  time.Time
		rawPayload   []byte
	)
	if err := rows.Scan(
		&intentID,
		&scopeID,
		&generationID,
		&domain,
		&attemptCount,
		&enqueuedAt,
		&availableAt,
		&rawPayload,
	); err != nil {
		return fmt.Errorf("scan legacy reducer work: %w", err)
	}
	if intentID != expectedID {
		return fmt.Errorf(
			"legacy claim intent ID = %q, want %q",
			intentID,
			expectedID,
		)
	}
	return nil
}
