// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhookstore

import (
	"context"
	"errors"
	"fmt"
)

// HasQueuedAcceptedTriggers reports whether an accepted trigger is
// queued for the handoff (#8123). It is the read-only probe the
// scheduled git sweep consults at repository boundaries: it never
// claims, so the claim lease and fencing contract stay exactly with
// ClaimQueuedTriggers. A probe that races a claim is harmless in
// both directions — a stale true costs one early cycle boundary, a
// stale false defers the claim to the next probe.
func (s *WebhookTriggerStore) HasQueuedAcceptedTriggers(ctx context.Context) (bool, error) {
	if s.database == nil {
		return false, errors.New("webhook trigger store database is required")
	}
	rows, err := s.database.QueryContext(ctx, hasQueuedAcceptedWebhookTriggersQuery)
	if err != nil {
		return false, fmt.Errorf("probe queued webhook triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, errors.New("probe queued webhook triggers: no rows")
	}
	var queued bool
	if err := rows.Scan(&queued); err != nil {
		return false, fmt.Errorf("probe queued webhook triggers: %w", err)
	}
	return queued, nil
}
