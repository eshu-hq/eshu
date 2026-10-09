// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// markedWriteOldestAgeSeed plants one marked generation per lifecycle state:
// gen-marked-live is the stuck write (active, marker 90s old, running work),
// gen-marked-retry is a marked write awaiting retry, gen-marked-retired is a
// completed generation whose monotonic marker must not alarm, and gen-unmarked
// is live work that never started its graph write.
const markedWriteOldestAgeSeed = `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
    projection_write_started_at
) VALUES ('gen-marked-live', 'scope-hb', 'push', now() - interval '5 minutes',
          now() - interval '5 minutes', 'active', now() - interval '90 seconds'),
         ('gen-marked-retry', 'scope-hb', 'push', now() - interval '4 minutes',
          now() - interval '4 minutes', 'active', now() - interval '30 seconds'),
         ('gen-marked-retired', 'scope-hb', 'push', now() - interval '2 hours',
          now() - interval '2 hours', 'completed', now() - interval '1 hour'),
         ('gen-unmarked', 'scope-hb', 'push', now() - interval '3 minutes',
          now() - interval '3 minutes', 'active', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-hb_gen-marked-live', 'scope-hb', 'gen-marked-live',
          'projector', 'source_local', 'running', 1, 'proof-worker',
          now() + interval '1 hour', now(), '{}'::jsonb, now(), now()),
         ('projector_scope-hb_gen-marked-retry', 'scope-hb', 'gen-marked-retry',
          'projector', 'source_local', 'retrying', 2, 'proof-worker',
          now() - interval '1 minute', now(), '{}'::jsonb, now(), now()),
         ('projector_scope-hb_gen-unmarked', 'scope-hb', 'gen-unmarked',
          'projector', 'source_local', 'pending', 0, NULL,
          NULL, now(), '{}'::jsonb, now(), now());
`

// TestQueueObserverStoreProjectorMarkedWriteOldestAgeLive proves the #7471
// stuck-write signal: the age of the oldest set marker on a non-retired
// generation with open projector work. The retired generation's hour-old
// marker and the unmarked generation must not move the gauge.
func TestQueueObserverStoreProjectorMarkedWriteOldestAgeLive(t *testing.T) {
	database := heartbeatProofDB(t, markedWriteOldestAgeSeed)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	observer := NewQueueObserverStore(SQLDB{DB: database})
	age, err := observer.ProjectorMarkedWriteOldestAge(ctx)
	if err != nil {
		t.Fatalf("ProjectorMarkedWriteOldestAge() error = %v", err)
	}
	if age < 80 || age > 150 {
		t.Fatalf("ProjectorMarkedWriteOldestAge() = %v, want the ~90s marked live write (retired and unmarked excluded)", age)
	}
}

// TestQueueObserverStoreProjectorMarkedWriteOldestAgeEmptyLive proves the
// gauge rests at zero when no non-retired generation with open projector
// work holds a marker.
func TestQueueObserverStoreProjectorMarkedWriteOldestAgeEmptyLive(t *testing.T) {
	database := heartbeatProofDB(t, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
    projection_write_started_at
) VALUES ('gen-old-retired', 'scope-hb', 'push', now() - interval '2 hours',
          now() - interval '2 hours', 'completed', now() - interval '1 hour');
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	observer := NewQueueObserverStore(SQLDB{DB: database})
	age, err := observer.ProjectorMarkedWriteOldestAge(ctx)
	if err != nil {
		t.Fatalf("ProjectorMarkedWriteOldestAge() error = %v", err)
	}
	if age != 0 {
		t.Fatalf("ProjectorMarkedWriteOldestAge() = %v, want 0 with no marked live write", age)
	}
}
