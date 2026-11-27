// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strconv"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestFanOutCoverRuleIsSharedByThePrefilterAndTheReopen pins the #10972
// lockstep from the shipped constants: the prefilter and the reopen render the
// one cover rule, the attempt arm is the claim's own non-counting predicate,
// and the merge reads the Go stamp cap and byte order.
func TestFanOutCoverRuleIsSharedByThePrefilterAndTheReopen(t *testing.T) {
	t.Parallel()

	checks := []struct {
		name, text, want string
	}{
		{"prefilter cover rule", findQueuedSupplyChainImpactHoldersQuery, "HAVING bool_and(" + fanOutQueuedRowCoversSQL("$2::jsonb", "$3::boolean") + ")"},
		{"reopen queued widen", reopenSupplyChainImpactHolderQuery, fanOutQueuedRowCoversSQL("$5::jsonb", "$6::boolean")},
		{"reopen flagged widen", reopenSupplyChainImpactHolderQuery, fanOutStampCoversSQL(fanOutStampSQL, "$5::jsonb", "$6::boolean")},
		{"reopen merge", reopenSupplyChainImpactHolderQuery, fanOutMergedStampSQL("$3::jsonb", "$5::jsonb", "$6::boolean")},
		{"claim attempt arithmetic", fanOutQueuedRowCoversSQL("$2", "$3"), reducerNonCountingFailureClassPredicateSQL("work")},
		{"claim uses the same predicate", reducerClaimAttemptCountCaseSQL(), reducerNonCountingFailureClassPredicateSQL("work")},
		{"merge cap from the Go constant", fanOutMergedStampSQL("$3", "$5", "$6"), "count(*) > " + strconv.Itoa(supplychaincore.FanOutOriginCVEIDCap) + " "},
		{"merge byte order", fanOutMergedStampSQL("$3", "$5", "$6"), `ORDER BY merged.id COLLATE "C"`},
		{"merge reads the locked row", fanOutMergedStampSQL("$3", "$5", "$6"), fanOutStampSQL},
		// Amendment 1: one fragment carries the absent-stamp rule for both the
		// cover rule and the merge, so it flips with one edit.
		{"merge reads the absent rule", fanOutMergedStampSQL("$3", "$5", "$6"), "WHEN " + fanOutStampAbsentSQL(fanOutStampSQL) + " THEN $3"},
		{"cover reads the absent rule", fanOutStampUnboundedSQL(fanOutStampSQL), "WHEN " + fanOutStampAbsentSQL(fanOutStampSQL) + " THEN TRUE"},
	}
	for _, check := range checks {
		if !strings.Contains(check.text, check.want) {
			t.Errorf("%s: shipped text does not contain %q", check.name, check.want)
		}
	}
	// Seeded violation: a prefilter whose attempt arm drops the claim's
	// non-counting predicate must fail the same containment check.
	drifted := strings.Replace(findQueuedSupplyChainImpactHoldersQuery, reducerNonCountingFailureClassPredicateSQL("work"), "(FALSE)", 1)
	if drifted == findQueuedSupplyChainImpactHoldersQuery ||
		strings.Contains(drifted, "HAVING bool_and("+fanOutQueuedRowCoversSQL("$2::jsonb", "$3::boolean")+")") {
		t.Error("a drifted attempt arm still passes the cover-rule check: the guard is inert")
	}
	// The merge must never read the target CTE's snapshot.
	if strings.Contains(fanOutMergedStampSQL("$3", "$5", "$6"), "target.") {
		t.Error("the stamp merge reads the target CTE snapshot instead of the locked row")
	}
	// The widen arms must not move the claim order: updated_at is written only
	// for rows returned to pending or newly flagged.
	if !strings.Contains(reopenSupplyChainImpactHolderQuery, "WHEN "+reopenSupplyChainImpactToPending+" OR "+reopenSupplyChainImpactSetsFlag+" THEN $2\n            ELSE work.updated_at") {
		t.Error("the reopen no longer guards updated_at to reopened and newly flagged rows")
	}
}

// TestFanOutChangeArgs pins the bind values for a fan-out's stamp set: a
// bounded set binds as its jsonb array, and an empty or sentinel set binds as
// unbounded with an empty array, so no cover arm reads NULL.
func TestFanOutChangeArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		in          []string
		wantCVEs    string
		wantBounded bool
	}{
		{"nil", nil, "[]", false},
		{"empty", []string{}, "[]", false},
		{"sentinel", []string{supplychaincore.FanOutStampSentinel}, "[]", false},
		{"bounded", []string{"CVE-1", "CVE-2"}, `["CVE-1","CVE-2"]`, true},
	}
	for _, tc := range cases {
		cves, bounded, err := fanOutChangeArgs(tc.in)
		if err != nil || cves != tc.wantCVEs || bounded != tc.wantBounded {
			t.Errorf("%s: fanOutChangeArgs = %q %v %v, want %q %v", tc.name, cves, bounded, err, tc.wantCVEs, tc.wantBounded)
		}
	}
}

// TestClassifyHolderReopenNamesAStampWiden pins the outcome mapping: a
// rewritten queued row, or a rewritten in-flight row whose flag was already
// on, is stamp_widened; a rewritten in-flight row whose flag was off is
// replay_flagged; and stamp_widened ranks above pending_noop.
func TestClassifyHolderReopenNamesAStampWiden(t *testing.T) {
	t.Parallel()

	cases := []struct {
		prior     string
		rewritten bool
		priorFlag bool
		want      supplychaincore.FanOutOutcome
	}{
		{"pending", true, false, supplychaincore.FanOutStampWidened},
		{"retrying", true, false, supplychaincore.FanOutStampWidened},
		{"pending", false, false, supplychaincore.FanOutPendingNoop},
		{"claimed", true, true, supplychaincore.FanOutStampWidened},
		{"running", true, true, supplychaincore.FanOutStampWidened},
		{"claimed", true, false, supplychaincore.FanOutReplayFlagged},
		{"claimed", false, true, supplychaincore.FanOutPendingNoop},
		{"succeeded", true, false, supplychaincore.FanOutReopened},
	}
	for _, tc := range cases {
		if got := classifyHolderReopen(tc.prior, tc.rewritten, tc.priorFlag, "", false).Outcome; got != tc.want {
			t.Errorf("classify(%s, rewritten=%v, flag=%v) = %q, want %q", tc.prior, tc.rewritten, tc.priorFlag, got, tc.want)
		}
	}
	if got := classifyHolderReopen("succeeded", true, false, "", true).Outcome; got != supplychaincore.FanOutShadowWouldSkip {
		t.Errorf("classify(succeeded, rewritten, shadowed) = %q, want shadow_would_skip", got)
	}
	if got := classifyHolderReopen("pending", true, false, "", true).Outcome; got != supplychaincore.FanOutStampWidened {
		t.Errorf("classify(pending, rewritten, shadowed) = %q, want stamp_widened: the shadow flag never fires off the succeeded arm", got)
	}
	if reopenRank(supplychaincore.FanOutShadowWouldSkip) <= reopenRank(supplychaincore.FanOutDeadLetterReopened) {
		t.Error("shadow_would_skip must rank above dead_letter_reopened")
	}
	if reopenRank(supplychaincore.FanOutShadowWouldSkip) >= reopenRank(supplychaincore.FanOutReopened) {
		t.Error("a genuine reopen must outrank a would-skip shadow")
	}
	if reopenRank(supplychaincore.FanOutStampWidened) <= reopenRank(supplychaincore.FanOutPendingNoop) {
		t.Error("stamp_widened must rank above pending_noop")
	}
	if reopenRank(supplychaincore.FanOutStampWidened) >= reopenRank(supplychaincore.FanOutReplayFlagged) {
		t.Error("a flag must outrank a stamp widen")
	}
}
