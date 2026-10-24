// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Reasons the collector stores for an allowlisted repository of an
// organization target that it did not read in full (#8943). They are the
// collector's own collection_incomplete_reasons tokens, pinned here because
// query must not import the collector packages. Sources, all in
// collector/securityalerts/alertruntime: provider_open_alert_page_limit_reached
// (source.go collectionOpenPageLimitReachedReason) and the four org_repository_*
// reasons plus org_repository_fanout_cap_reached (org_fanout.go). A stored
// value outside this set maps to org_repository_read_incomplete so free text
// from a newer collector build never reaches a payload.
const (
	securityAlertReasonUnreadRateLimited = "org_repository_read_rate_limited"
	securityAlertReasonUnreadFailed      = "org_repository_read_failed"
	securityAlertReasonUnreadCapReached  = "org_repository_fanout_cap_reached"
	securityAlertReasonUnreadStopped     = "org_repository_fanout_stopped"
	securityAlertReasonUnreadDisabled    = "org_repository_alerts_disabled"
	securityAlertReasonUnreadPageLimit   = "provider_open_alert_page_limit_reached"
	securityAlertReasonUnreadIncomplete  = "org_repository_read_incomplete"
)

// securityAlertUnreadReasons is every reason token an unread repository can
// report. securityAlertCoverageReasons includes it, so the OpenAPI enum test
// covers it.
var securityAlertUnreadReasons = []string{
	securityAlertReasonUnreadRateLimited,
	securityAlertReasonUnreadFailed,
	securityAlertReasonUnreadCapReached,
	securityAlertReasonUnreadStopped,
	securityAlertReasonUnreadDisabled,
	securityAlertReasonUnreadPageLimit,
	securityAlertReasonUnreadIncomplete,
}

// securityAlertUnreadReason maps a stored reason to a closed token.
func securityAlertUnreadReason(stored string) string {
	if slices.Contains(securityAlertUnreadReasons, stored) {
		return stored
	}
	return securityAlertReasonUnreadIncomplete
}

// securityAlertUnreadReasonsFor returns the sorted, unique reasons that keep a
// committed, healthy target from answering the read in full. An anchored read
// asks about its own repositories: only an unread requested repository counts,
// so a target that left other repositories unread still answers this one. An
// unanchored read spans every repository, so any unread repository counts.
func securityAlertUnreadReasonsFor(state SecurityAlertTargetState, anchored bool) []string {
	var stored []string
	if anchored {
		for _, reason := range state.UnreadRequested {
			stored = append(stored, reason)
		}
	} else if state.UnreadTotal > 0 {
		stored = state.UnreadReasons
		if len(stored) == 0 {
			stored = []string{securityAlertReasonUnreadIncomplete}
		}
	}
	reasons := make([]string, 0, len(stored))
	for _, reason := range stored {
		reasons = append(reasons, securityAlertUnreadReason(reason))
	}
	return UniqueSortedNonEmpty(reasons)
}

// securityAlertUnreadVerdict is the verdict for a committed target whose read of
// the requested repositories was incomplete. The facts the answer rests on are
// current, so freshness stays fresh; the answer is derived, not exact, because
// the collector never read the repository in full. It carries no readiness
// envelope and no freshness cause: the collector is healthy and completed its
// run, and coverage.reasons names what it left unread.
func securityAlertUnreadVerdict(reasons []string) securityAlertCoverageVerdict {
	return securityAlertCoverageVerdict{
		coverageState:  securityAlertCoverageTargetIncomplete,
		reasons:        reasons,
		level:          querycontract.TruthLevelDerived,
		freshness:      querycontract.FreshnessFresh,
		collectorState: securityAlertCollectorStatePartialRead,
	}
}
