// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
)

// upsertSelectorCycleQuery advances one selector's liveness marker in
// repository_selectors (migration 200, #7840). The marker only moves
// forward, mirroring the observation upsert: a lagging replica reporting an
// older cycle changes nothing, so concurrent replicas of one selector can
// never rewind each other's freshness. The stored window follows the newest
// cycle, so a reconfigured window replaces the old one with the cycle that
// carries it.
const upsertSelectorCycleQuery = `
INSERT INTO repository_selectors (selector_id, selector_kind, selector_owner, last_cycle_at, liveness_window_seconds)
VALUES ($1, $2, $3, $4::timestamptz, $5)
ON CONFLICT (selector_id) DO UPDATE SET
    selector_kind = EXCLUDED.selector_kind,
    selector_owner = EXCLUDED.selector_owner,
    last_cycle_at = EXCLUDED.last_cycle_at,
    liveness_window_seconds = EXCLUDED.liveness_window_seconds
WHERE repository_selectors.last_cycle_at < EXCLUDED.last_cycle_at
`

// UpsertSelectorCycle records that selector ran a cycle at cycleAt with
// liveness window window. An invalid selector, a zero cycle time, or a
// window outside the stored seconds range is rejected before anything is
// written.
func (s ObservationStore) UpsertSelectorCycle(ctx context.Context, selector membership.Selector, cycleAt time.Time, window time.Duration) error {
	if s.database == nil {
		return errors.New("repository selection store database is required")
	}
	switch {
	case strings.TrimSpace(selector.ID) == "":
		return errors.New("upsert repository selector cycle: selector id must not be blank")
	case selector.Kind != membership.KindGitHubOrg && selector.Kind != membership.KindExplicit && selector.Kind != membership.KindGitLabGroup:
		return fmt.Errorf("upsert repository selector cycle: unsupported selector kind %q", selector.Kind)
	case strings.TrimSpace(selector.Owner) == "":
		return errors.New("upsert repository selector cycle: selector owner must not be blank")
	case cycleAt.IsZero():
		return errors.New("upsert repository selector cycle: cycle time is required")
	case window < time.Second || window/time.Second > math.MaxInt32:
		return fmt.Errorf("upsert repository selector cycle: liveness window %v is outside 1s..%ds", window, math.MaxInt32)
	}
	if _, err := s.database.ExecContext(ctx, upsertSelectorCycleQuery,
		selector.ID, selector.Kind, selector.Owner, cycleAt.UTC(), int64(window/time.Second),
	); err != nil {
		return fmt.Errorf("upsert repository selector cycle: %w", err)
	}
	return nil
}
