// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unread

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// upsertTargetReadStateQuery records the target-level row of one generation.
// The conflict arm exists for a replay of a still-pending generation: the
// generation id is the key, and the latest write wins. $1 generation id, $2
// scope id, $3 org read truncated, $4 repositories total, $5 unread total, $6
// observed at, $7 the distinct unread reasons, $8 the repository this
// generation's fan-out started at, $9 the repository the next claim starts at
// (both NULL for the top of the allowlist), $10 the last-position repository
// whose failed read closed a lap, NULL when no lap is in progress (#8946).
const upsertTargetReadStateQuery = `
INSERT INTO security_alert_target_read_states (
    generation_id, scope_id, org_read_truncated, repositories_total, unread_total, observed_at, unread_reasons,
    fanout_start_repository, fanout_next_repository, fanout_lap_repository
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (generation_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    org_read_truncated = EXCLUDED.org_read_truncated,
    repositories_total = EXCLUDED.repositories_total,
    unread_total = EXCLUDED.unread_total,
    observed_at = EXCLUDED.observed_at,
    unread_reasons = EXCLUDED.unread_reasons,
    fanout_start_repository = EXCLUDED.fanout_start_repository,
    fanout_next_repository = EXCLUDED.fanout_next_repository,
    fanout_lap_repository = EXCLUDED.fanout_lap_repository`

// deleteUnreadRepositoriesQuery clears the child rows of one generation so a
// replay of the same generation id replaces them instead of merging with a
// stale set. $1 generation id.
const deleteUnreadRepositoriesQuery = `
DELETE FROM security_alert_unread_repositories
WHERE generation_id = $1`

// insertUnreadRepositoriesQuery writes the unread repositories of one
// generation in one statement. $1 generation id, $2 repositories, $3 reasons;
// the two arrays are the same length and in the same order.
const insertUnreadRepositoriesQuery = `
INSERT INTO security_alert_unread_repositories (generation_id, repository, reason)
SELECT $1, unread.repository, unread.reason
FROM unnest($2::text[], $3::text[]) AS unread(repository, reason)`

// Writer returns the GenerationStateWriter that persists state in the commit
// transaction of its generation. The state is validated when the writer runs,
// so an invalid state rolls the generation back instead of committing a row
// the read path cannot trust. Errors never carry repository names.
func Writer(state securityalerts.TargetReadState) collector.GenerationStateWriter {
	return func(ctx context.Context, exec collector.CommitExecutor) error {
		return write(ctx, exec, state)
	}
}

func write(ctx context.Context, exec collector.CommitExecutor, state securityalerts.TargetReadState) error {
	generationID := strings.TrimSpace(state.GenerationID)
	scopeID := strings.TrimSpace(state.ScopeID)
	switch {
	case generationID == "":
		return fmt.Errorf("security alert read state generation id is required")
	case scopeID == "":
		return fmt.Errorf("security alert read state scope id is required")
	case state.RepositoriesTotal < 0:
		return fmt.Errorf("security alert read state repositories total must not be negative")
	}
	unread, err := normalizeUnread(state.Unread)
	if err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, upsertTargetReadStateQuery,
		generationID, scopeID, state.OrgReadTruncated, state.RepositoriesTotal, len(unread), state.ObservedAt.UTC(),
		distinctReasons(unread), cursorArg(state.FanoutStartRepository), cursorArg(state.FanoutNextRepository), cursorArg(state.FanoutLapRepository),
	); err != nil {
		return fmt.Errorf("upsert security alert target read state: %w", err)
	}
	if _, err := exec.ExecContext(ctx, deleteUnreadRepositoriesQuery, generationID); err != nil {
		return fmt.Errorf("clear security alert unread repositories: %w", err)
	}
	if len(unread) == 0 {
		return nil
	}
	repositories := make(array.StringArray, len(unread))
	reasons := make(array.StringArray, len(unread))
	for i, row := range unread {
		repositories[i], reasons[i] = row.Repository, row.Reason
	}
	if _, err := exec.ExecContext(ctx, insertUnreadRepositoriesQuery, generationID, repositories, reasons); err != nil {
		return fmt.Errorf("insert security alert unread repositories: %w", err)
	}
	return nil
}

// normalizeUnread validates the unread repositories, lowercases each slug the
// way the read probe does, drops exact repeats, and sorts by repository so
// every writer of one generation inserts in one order. Two different reasons
// for one repository, including a case twin, are a collector bug, not a merge.
func normalizeUnread(unread []securityalerts.UnreadRepository) ([]securityalerts.UnreadRepository, error) {
	out := make([]securityalerts.UnreadRepository, 0, len(unread))
	for _, row := range unread {
		row.Repository = strings.ToLower(strings.TrimSpace(row.Repository))
		row.Reason = strings.TrimSpace(row.Reason)
		if row.Repository == "" || row.Reason == "" {
			return nil, fmt.Errorf("security alert unread repository needs a repository and a reason")
		}
		out = append(out, row)
	}
	slices.SortFunc(out, func(a, b securityalerts.UnreadRepository) int {
		return strings.Compare(a.Repository, b.Repository)
	})
	out = slices.CompactFunc(out, func(a, b securityalerts.UnreadRepository) bool {
		return a.Repository == b.Repository && a.Reason == b.Reason
	})
	for i := 1; i < len(out); i++ {
		if out[i].Repository == out[i-1].Repository {
			return nil, fmt.Errorf("security alert unread repository has conflicting reasons")
		}
	}
	return out, nil
}

// distinctReasons returns the sorted distinct reasons of unread, never nil, so
// the column stores an empty array instead of NULL.
func distinctReasons(unread []securityalerts.UnreadRepository) array.StringArray {
	reasons := make(array.StringArray, 0, 4)
	for _, row := range unread {
		reasons = append(reasons, row.Reason)
	}
	slices.Sort(reasons)
	return slices.Compact(reasons)
}

// cursorArg normalizes a fan-out cursor slug the way the unread slugs are, and
// stores no cursor as NULL: the next claim then starts at the top.
func cursorArg(repository string) any {
	repository = strings.ToLower(strings.TrimSpace(repository))
	if repository == "" {
		return nil
	}
	return repository
}
