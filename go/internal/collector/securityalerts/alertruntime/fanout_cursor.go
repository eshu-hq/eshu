// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"log/slog"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// FanoutCursorReader returns the committed fan-out position of one scope: the
// allowlisted repository the next org fan-out claim starts at and, while a lap
// is in progress, the last-position repository whose failed read closed it
// (#8946). The position is part of the read state the generation commits
// (TargetReadState.FanoutNextRepository and .FanoutLapRepository), so it moves
// only when a generation commits and survives a restart; NextClaimed computes
// the next position and carries it to the commit, and nothing durable moves
// before that.
type FanoutCursorReader interface {
	// FanoutCursor returns the committed position, the zero value for the top
	// of the allowlist with no lap. An error fails the claim; it never means
	// "start at the top".
	FanoutCursor(ctx context.Context, scopeID string) (securityalerts.FanoutCursor, error)
}

// fanoutOffsetRepository names the allowlisted repository at offset, or ""
// for the top of the allowlist (offset 0 or out of range), the form the cursor
// is stored in so an allowlist edit cannot make it point at another repository.
func fanoutOffsetRepository(allowlisted []string, offset int) string {
	if offset <= 0 || offset >= len(allowlisted) {
		return ""
	}
	return allowlisted[offset]
}

// fanoutStart returns the allowlist position this claim's fan-out starts at and
// the lap repository when the target is lapped. A missing cursor, or one naming
// a repository that left the allowlist, starts at the top: the fan-out stays
// bounded and no repository is skipped permanently. A lap ends when its
// repository leaves the allowlist, the same way, so removing a failing last
// entry resumes the forward walk. A lapped target starts at the top.
func (s *ClaimedSource) fanoutStart(ctx context.Context, scopeID string, allowlisted []string) (int, string, error) {
	if s.cursors == nil || len(allowlisted) > maxOrganizationFanoutRepositories {
		// A target over the cap always reads its top window (fanoutNext);
		// a cursor stored while the allowlist still fit does not apply.
		return 0, "", nil
	}
	cursor, err := s.cursors.FanoutCursor(ctx, scopeID)
	if err != nil {
		return 0, "", cursorReadError{cause: err}
	}
	if cursor.LapRepository != "" && slices.Contains(allowlisted, cursor.LapRepository) {
		return 0, cursor.LapRepository, nil
	}
	if cursor.NextRepository != "" {
		if offset := slices.Index(allowlisted, cursor.NextRepository); offset >= 0 {
			return offset, "", nil
		}
	}
	return 0, "", nil
}

// fanoutWalkOrder returns the allowlist starting at start and wrapping around,
// so the repositories behind a failed read are read before the failed repository
// is retried.
func fanoutWalkOrder(allowlisted []string, start int) []string {
	if start <= 0 || start >= len(allowlisted) {
		return allowlisted
	}
	walk := make([]string, 0, len(allowlisted))
	walk = append(walk, allowlisted[start:]...)
	return append(walk, allowlisted[:start]...)
}

// fanoutNext returns the allowlist position the next claim starts at and the
// lap repository to commit with it. start is this claim's offset and lap the
// lap repository it started with ("" when not lapped).
//
// The start moves forward through the allowlist, to the repository after a
// failed read. A failed read at the last position closes the lap: the start
// returns to the top and stays there until a claim reads every repository or
// that last repository leaves the allowlist. A rate limit leaves the start where
// the claim began. Concretely:
//
//   - A target over the per-claim cap always starts at the top. A rotating
//     window through a scope that is a full snapshot would store a generation
//     per claim and retain every one for the superseded-generation window, so
//     the repositories past the cap are left to a split target (#9019).
//   - A claim that read every repository returns to the top and ends the lap.
//   - A rate limit keeps the start and the lap: the limit is provider-wide and
//     the repository is not at fault.
//   - A lapped claim keeps the start (the top) and the lap.
//   - Any other stop at allowlist position p moves the start to p+1; a stop
//     before the start (the walk had wrapped) keeps it; a stop at the last
//     position closes the lap.
//
// One durable bit of state is needed: with stoppers cutting the ring into
// segments, no stateless rule both moves past the first stopper and then settles
// on a read set that contains the start-at-top set. With the lap the start
// settles in at most one stored generation per persistent stopper plus one,
// the committer then skips, and the settled read set is never smaller than the
// start-at-top set. A repository behind a stopper stays unread, labeled
// org_repository_fanout_stopped, until the stopper is fixed or removed.
func fanoutNext(allowlisted []string, start int, lap string, stopIndex int, stopOutcome string) (int, string) {
	size := len(allowlisted)
	if size == 0 || size > maxOrganizationFanoutRepositories || stopIndex < 0 {
		return 0, ""
	}
	if stopOutcome == contract.SecurityAlertFanoutOutcomeRateLimited || lap != "" {
		return start, lap
	}
	stopper := (start + stopIndex) % size
	switch {
	case stopper < start:
		return start, ""
	case stopper == size-1:
		return 0, allowlisted[stopper]
	default:
		return stopper + 1, ""
	}
}

// warnOversizedTargets logs one warning per org target whose same-org allowlist
// exceeds the per-claim fan-out cap. Such a target reads only its first
// maxOrganizationFanoutRepositories repositories on every claim and labels the
// rest org_repository_fanout_cap_reached; the fix is to split it into org
// targets of at most the cap (#9019 automates that). The warning is the 3 AM
// signal, and it carries counts and the scope id, never a repository name.
func warnOversizedTargets(logger *slog.Logger, targets map[string]targetRuntime) {
	if logger == nil {
		return
	}
	scopeIDs := make([]string, 0, len(targets))
	for scopeID := range targets {
		scopeIDs = append(scopeIDs, scopeID)
	}
	slices.Sort(scopeIDs)
	for _, scopeID := range scopeIDs {
		target := targets[scopeID].config
		if !target.IsOrganizationScope() {
			continue
		}
		repositories := len(organizationFanoutRepositories(target))
		if repositories <= maxOrganizationFanoutRepositories {
			continue
		}
		logger.Warn(
			"security alert org target exceeds the per-claim fan-out cap; it reads only the first repositories up to the cap on every claim, split it into org targets of at most the cap, see #9019",
			telemetry.EventAttr("security_alert.org_fanout_over_cap"),
			slog.String(telemetry.LogKeyScopeID, scopeID),
			slog.Int("repositories", repositories),
			slog.Int("cap", maxOrganizationFanoutRepositories),
		)
	}
}

// fanoutCursorReadOperation is the bounded operation token a failed cursor read
// persists with the claim, so a status row reads as a storage failure.
const fanoutCursorReadOperation = "read_fanout_cursor"

// cursorReadError is a failed read of the committed fan-out cursor. It is a
// storage failure, not a provider failure: the persisted message names the
// cursor read and Postgres, never the cause text (which may name a database or
// a host), so an operator at 3 AM looks at Postgres and not at GitHub. The
// claim stays retryable; starting at the top instead would silently recreate
// the bug the cursor fixes.
type cursorReadError struct {
	cause error
}

// Error returns a bounded message that carries no cause text.
func (cursorReadError) Error() string {
	return "security alert fan-out cursor read from postgres failed"
}

// Unwrap returns the storage error for logs and errors.Is.
func (e cursorReadError) Unwrap() error { return e.cause }

// FailureClass reports the retryable workflow class.
func (cursorReadError) FailureClass() string { return FailureRetryable }

// TerminalFailure is false: the next claim reads the cursor again.
func (cursorReadError) TerminalFailure() bool { return false }

// FailureOperation names the cursor read as the failed stage.
func (cursorReadError) FailureOperation() string { return fanoutCursorReadOperation }
