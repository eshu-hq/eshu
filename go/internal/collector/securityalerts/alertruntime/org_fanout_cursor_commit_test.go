// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
)

// TestClaimedSourceFanoutCursorDoesNotAdvanceBeforeTheGenerationCommits proves
// the #5429 ordering: collecting a claim moves nothing durable. A claim whose
// generation never commits (a rollback, a lost fence, a failed commit) leaves
// the cursor where it was, so the retry reads the same window.
func TestClaimedSourceFanoutCursorDoesNotAdvanceBeforeTheGenerationCommits(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(10)
	client := truncatedOrgClient()
	failingRepositories(client, allowed[5])
	ledger := newCursorLedger()
	source := newCursorSource(t, client, allowed, ledger)

	first, _, _ := claimOnce(t, source, client) // collected, never committed
	retry, _, writer := claimOnce(t, source, client)
	if !slices.Equal(first, retry) {
		t.Fatalf("retry after an uncommitted claim read a different window: %d reads starting at %s, want %d starting at %s",
			len(retry), retry[0], len(first), first[0])
	}
	if got := ledger.committed[fanoutTestOrgScopeID]; got != (securityalerts.FanoutCursor{}) {
		t.Fatalf("cursor = %+v before any commit, want the top", got)
	}
	if err := writer(context.Background(), nil); err != nil {
		t.Fatalf("state writer error = %v", err)
	}
	next, _ := claimCalls(t, source, client)
	if next[0] != allowed[6] {
		t.Fatalf("claim after the commit started at %s, want %s", next[0], allowed[6])
	}
}

// TestClaimedSourceFanoutCursorStaysPutWhenAnUnchangedGenerationIsSkipped
// proves a repeating stop pattern (allowlist within the cap) is stable: the
// committer skips a generation whose freshness hint equals the newest one, runs
// no state writer, and the cursor read before the next claim equals the cursor
// after the claim that was stored.
func TestClaimedSourceFanoutCursorStaysPutWhenAnUnchangedGenerationIsSkipped(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(10)
	client := truncatedOrgClient()
	failingRepositories(client, allowed[5])
	ledger := newCursorLedger()
	source := newCursorSource(t, client, allowed, ledger)
	committer := &storingCommitter{}

	var calls [][]string
	for range 4 {
		got, _, _ := committer.claim(t, source, client)
		calls = append(calls, got)
	}
	if slices.Equal(calls[0], calls[1]) {
		t.Fatal("claim 2 repeated claim 1: the cursor did not move past the failing repository")
	}
	if !slices.Equal(calls[1], calls[2]) || !slices.Equal(calls[2], calls[3]) {
		t.Fatalf("claims 2 to 4 read different windows %v, want a repeating pattern", calls[1:])
	}
	if got, want := len(ledger.states), 2; got != want {
		t.Fatalf("committed states = %d, want %d (claims 3 and 4 were unchanged and skipped)", got, want)
	}
}

// TestClaimedSourceFanoutConvergesWithSeveralPersistentStoppers pins the start
// of the fan-out (#8946) with a committer-driven table over a 10-repository
// allowlist. The start moves forward to the repository after a failed read; a
// failed read at the last position closes the lap, which returns the start to
// the top and keeps it there. Each case settles within one stored generation
// per stopper plus one, and the committer then skips.
func TestClaimedSourceFanoutConvergesWithSeveralPersistentStoppers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		stoppers []int
		starts   []int // the start offset of each claim
		stored   int   // generations stored over the claims
		lap      int   // allowlist position of the committed lap repository, or -1
	}{
		{"single stopper", []int{4}, []int{0, 5, 5, 5}, 2, -1},
		{"two stoppers", []int{2, 6}, []int{0, 3, 7, 7}, 3, -1},
		{"three stoppers", []int{1, 5, 8}, []int{0, 2, 6, 9, 9}, 4, -1},
		{"stopper at the last position", []int{9}, []int{0, 0, 0}, 1, 9},
		{"stoppers at the top and the last position", []int{0, 9}, []int{0, 1, 0, 0}, 3, 9},
		{"stoppers in the middle and at the last position", []int{2, 9}, []int{0, 3, 0, 0}, 3, 9},
		{"two trailing stoppers", []int{8, 9}, []int{0, 9, 0, 0}, 3, 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed := cursorAllowlist(10)
			client := truncatedOrgClient()
			stoppers := make([]string, 0, len(tc.stoppers))
			for _, k := range tc.stoppers {
				stoppers = append(stoppers, allowed[k])
			}
			failingRepositories(client, stoppers...)
			ledger := newCursorLedger()
			source := newCursorSource(t, client, allowed, ledger)
			committer := &storingCommitter{}

			var lastCalls []string
			for claim, wantStart := range tc.starts {
				calls, _, _ := committer.claim(t, source, client)
				lastCalls = calls
				if calls[0] != allowed[wantStart] {
					t.Fatalf("claim %d started at %s, want %s (starts %v)", claim+1, calls[0], allowed[wantStart], tc.starts)
				}
			}
			if committer.stored != tc.stored {
				t.Fatalf("stored %d generations over %d claims, want %d then skips", committer.stored, len(tc.starts), tc.stored)
			}
			wantLap := ""
			if tc.lap >= 0 {
				wantLap = allowed[tc.lap]
			}
			if got := ledger.committed[fanoutTestOrgScopeID].LapRepository; got != wantLap {
				t.Fatalf("committed lap repository = %q, want %q", got, wantLap)
			}
			// The settled read set is never smaller than today's start-at-top
			// set: every repository before the first stopper is still read.
			for position := range slices.Min(tc.stoppers) {
				if !slices.Contains(lastCalls, allowed[position]) {
					t.Fatalf("settled claim read %v, want it to cover %s before the first stopper", lastCalls, allowed[position])
				}
			}
		})
	}
}

// TestClaimedSourceFanoutTrailingStoppersKeepTodaysReadSet pins the reason for
// the lap: with the last two entries unreadable, the start must not settle on
// them, which would read nothing. After the lap the claim reads the same
// repositories before the first stopper that the start-at-top fan-out reads.
func TestClaimedSourceFanoutTrailingStoppersKeepTodaysReadSet(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(10)
	client := truncatedOrgClient()
	failingRepositories(client, allowed[8], allowed[9])
	source := newCursorSource(t, client, allowed, newCursorLedger())
	committer := &storingCommitter{}
	var calls []string
	for range 4 {
		calls, _, _ = committer.claim(t, source, client)
	}
	if want := allowed[:9]; !slices.Equal(calls, want) {
		t.Fatalf("settled claim read %v, want %v (repo-000 to repo-007 in full, then the failing repo-008)", calls, want)
	}
}

// TestClaimedSourceFanoutHealedStopperKeepsTheSettledStart pins the honest gap
// of the forward walk: when a non-first stopper heals, the start stays, the
// segment between the stoppers stays unread (labeled stopped on its facts and in
// the unread rows) until the first stopper is fixed or leaves the allowlist,
// and the unchanged generation keeps being skipped.
func TestClaimedSourceFanoutHealedStopperKeepsTheSettledStart(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(10)
	client := truncatedOrgClient()
	failingRepositories(client, allowed[2], allowed[6])
	ledger := newCursorLedger()
	source := newCursorSource(t, client, allowed, ledger)
	committer := &storingCommitter{}
	for range 4 {
		committer.claim(t, source, client)
	}
	storedBefore := committer.stored

	failingRepositories(client, allowed[2]) // the stopper at 6 heals
	calls, _, stored := committer.claim(t, source, client)
	if calls[0] != allowed[7] {
		t.Fatalf("claim after the heal started at %s, want the settled start %s", calls[0], allowed[7])
	}
	if stored || committer.stored != storedBefore {
		t.Fatalf("claim after the heal stored a generation (stored %d, was %d), want it skipped: the read set did not change", committer.stored, storedBefore)
	}
	for _, repository := range allowed[3:7] {
		if slices.Contains(calls, repository) {
			t.Fatalf("%s was read after the heal, want the segment behind the first stopper left unread", repository)
		}
	}
}

// lappedSource drives a 10-repository target with stoppers at positions 2 and 9
// to its settled lap (starts 0, 3, 0, 0) and returns the pieces a follow-up
// test needs.
func lappedSource(t *testing.T) ([]string, *fanoutClient, *cursorLedger, *storingCommitter, *ClaimedSource) {
	t.Helper()
	allowed := cursorAllowlist(10)
	client := truncatedOrgClient()
	failingRepositories(client, allowed[2], allowed[9])
	ledger := newCursorLedger()
	source := newCursorSource(t, client, allowed, ledger)
	committer := &storingCommitter{}
	for range 4 {
		committer.claim(t, source, client)
	}
	if got := ledger.committed[fanoutTestOrgScopeID]; got.LapRepository != allowed[9] || got.NextRepository != "" {
		t.Fatalf("lapped cursor = %+v, want the lap at %s and the start at the top", got, allowed[9])
	}
	return allowed, client, ledger, committer, source
}

// TestClaimedSourceFanoutLapStaysWhenTheMiddleStopperHeals proves a lapped
// target reads further when the first stopper heals: one new generation reading
// repo-000 to repo-008, then skips; the lap stays (the last entry still fails)
// until a full read or the last entry leaves the allowlist.
func TestClaimedSourceFanoutLapStaysWhenTheMiddleStopperHeals(t *testing.T) {
	t.Parallel()

	allowed, client, ledger, committer, source := lappedSource(t)
	storedBefore := committer.stored
	failingRepositories(client, allowed[9]) // the stopper at 2 heals
	calls, _, stored := committer.claim(t, source, client)
	if !stored || !slices.Equal(calls, allowed) {
		t.Fatalf("claim after the heal read %v, stored %v; want every repository attempted and a new generation", calls, stored)
	}
	if _, _, stored = committer.claim(t, source, client); stored || committer.stored != storedBefore+1 {
		t.Fatalf("stored %d generations after the heal (was %d), want exactly one new one then skips", committer.stored, storedBefore)
	}
	if got := ledger.committed[fanoutTestOrgScopeID].LapRepository; got != allowed[9] {
		t.Fatalf("lap repository = %q, want it kept until a full read", got)
	}
}

// TestClaimedSourceFanoutLapEndsWhenTheLastEntryLeavesTheAllowlist proves the
// documented remedy for a failing last entry: remove it, and the forward walk
// resumes (starts 0, 3, 3 over the shorter allowlist, reading all but the
// remaining stopper), two new generations, then skips.
func TestClaimedSourceFanoutLapEndsWhenTheLastEntryLeavesTheAllowlist(t *testing.T) {
	t.Parallel()

	allowed, client, ledger, committer, _ := lappedSource(t)
	storedBefore := committer.stored
	shorter := allowed[:9] // repo-009, the lap repository, is removed
	failingRepositories(client, shorter[2])
	source := newCursorSource(t, client, shorter, ledger) // a restart with the edited allowlist
	wantStarts := []string{shorter[0], shorter[3], shorter[3]}
	for i, want := range wantStarts {
		calls, _, _ := committer.claim(t, source, client)
		if calls[0] != want {
			t.Fatalf("claim %d after the edit started at %s, want %s", i+1, calls[0], want)
		}
	}
	if committer.stored != storedBefore+2 {
		t.Fatalf("stored %d generations after the edit (was %d), want two new ones then skips", committer.stored, storedBefore)
	}
	if got := ledger.committed[fanoutTestOrgScopeID].LapRepository; got != "" {
		t.Fatalf("lap repository = %q after the edit, want none", got)
	}
}

// TestClaimedSourceFanoutLapEndsOnAFullRead proves a lapped target whose
// stoppers both heal reads everything, stores one generation, and clears the
// lap.
func TestClaimedSourceFanoutLapEndsOnAFullRead(t *testing.T) {
	t.Parallel()

	allowed, client, ledger, committer, source := lappedSource(t)
	failingRepositories(client) // both heal
	calls, _, stored := committer.claim(t, source, client)
	if !stored || !slices.Equal(calls, allowed) {
		t.Fatalf("claim after both healed read %v, stored %v; want a full read stored", calls, stored)
	}
	if got := ledger.committed[fanoutTestOrgScopeID]; got != (securityalerts.FanoutCursor{}) {
		t.Fatalf("cursor after a full read = %+v, want the top and no lap", got)
	}
}

// TestClaimedSourceFanoutRateLimitWhileLappedKeepsTheLap proves a rate limit on
// a lapped target keeps the start at the top and the lap, so a later claim does
// not resume the forward walk past a failing last entry.
func TestClaimedSourceFanoutRateLimitWhileLappedKeepsTheLap(t *testing.T) {
	t.Parallel()

	allowed, client, ledger, committer, source := lappedSource(t)
	client.repoErrs[allowed[2]] = securityalerts.GitHubDependabotError{
		StatusCode: 403,
		RateLimit:  securityalerts.GitHubRateLimitInfo{Remaining: 0, RemainingKnown: true},
	}
	calls, _, stored := committer.claim(t, source, client)
	if calls[0] != allowed[0] || !stored {
		t.Fatalf("rate limited claim started at %s, stored %v; want the top and a new generation (the outcome changed)", calls[0], stored)
	}
	if got := ledger.committed[fanoutTestOrgScopeID]; got.LapRepository != allowed[9] || got.NextRepository != "" {
		t.Fatalf("cursor after a rate limit while lapped = %+v, want the lap kept at the top", got)
	}
}

// TestClaimedSourceFanoutCursorReadErrorFailsTheClaim proves a cursor read
// error is a retryable storage failure (not a provider failure) and never a silent start at the top, which
// would recreate the bug the cursor fixes.
func TestClaimedSourceFanoutCursorReadErrorFailsTheClaim(t *testing.T) {
	t.Parallel()

	client := truncatedOrgClient()
	ledger := newCursorLedger()
	ledger.readErr = errors.New("connection reset")
	source := newCursorSource(t, client, cursorAllowlist(3), ledger)

	_, _, err := source.NextClaimed(context.Background(), orgWorkItem())
	var failure cursorReadError
	if !errors.As(err, &failure) || failure.FailureClass() != FailureRetryable || failure.TerminalFailure() {
		t.Fatalf("NextClaimed() error = %v, want a retryable cursor read failure", err)
	}
	// The persisted text names the storage read, not the provider, and carries
	// no cause text; the cause stays reachable for logs.
	if got := err.Error(); !strings.Contains(got, "postgres") || strings.Contains(got, "provider") || strings.Contains(got, "connection reset") {
		t.Fatalf("persisted message = %q, want it to name the postgres cursor read and nothing else", got)
	}
	if failure.FailureOperation() != fanoutCursorReadOperation || !errors.Is(err, ledger.readErr) {
		t.Fatalf("operation = %q, cause reachable = %v, want %q and true", failure.FailureOperation(), errors.Is(err, ledger.readErr), fanoutCursorReadOperation)
	}
	if len(client.repoCalls) != 0 {
		t.Fatalf("per-repository reads = %d, want none after a cursor read error", len(client.repoCalls))
	}
}

// TestNewClaimedSourceRequiresCursorReaderWithReadStateWriter proves the
// production wiring cannot silently drop the reader: a writer would commit a
// cursor no claim reads.
func TestNewClaimedSourceRequiresCursorReaderWithReadStateWriter(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets:             []TargetConfig{orgStateTarget("example-org/alpha-repo")},
		ClientFactory:       func(TargetConfig) (RepositoryAlertClient, error) { return truncatedOrgClient(), nil },
		ReadStateWriter:     newCursorLedger().writer,
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want a construction error for a writer without a cursor reader")
	}
}

// TestClaimedSourceWithoutCursorWiringStartsAtTheTop proves the cassette and
// test path (no writer, no reader) starts every claim at the top and records
// nothing.
func TestClaimedSourceWithoutCursorWiringStartsAtTheTop(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(150)
	client := truncatedOrgClient()
	source := newCursorSourceWith(t, client, allowed, nil, nil, nil)
	first, _, writer := claimOnce(t, source, client)
	second, _, _ := claimOnce(t, source, client)
	if writer != nil {
		t.Fatal("StateWriter set without a ReadStateWriter configured")
	}
	if !slices.Equal(first, second) || first[0] != allowed[0] {
		t.Fatalf("claims started at %s then %s, want the top both times", first[0], second[0])
	}
}

// TestClaimedSourceReadStateCarriesTheFanoutCursor proves the generation's read
// state names where its fan-out started and where the next claim starts, the
// evidence an operator reads from the table.
func TestClaimedSourceReadStateCarriesTheFanoutCursor(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(10)
	client := truncatedOrgClient()
	failingRepositories(client, allowed[5])
	ledger := newCursorLedger()
	source := newCursorSource(t, client, allowed, ledger)
	claimCalls(t, source, client)
	claimCalls(t, source, client)

	want := []struct{ start, next string }{
		{"", allowed[6]},
		{allowed[6], allowed[6]},
	}
	if len(ledger.states) != len(want) {
		t.Fatalf("committed states = %d, want %d", len(ledger.states), len(want))
	}
	for i, state := range ledger.states {
		if state.FanoutStartRepository != want[i].start || state.FanoutNextRepository != want[i].next {
			t.Errorf("state %d cursor = %q -> %q, want %q -> %q",
				i+1, state.FanoutStartRepository, state.FanoutNextRepository, want[i].start, want[i].next)
		}
	}
}

// TestNewClaimedSourceWarnsForAnOrgTargetOverTheFanoutCap proves the 3 AM
// signal: one warning, with counts and no repository name, for an org target
// that will read only its first window, and none for a target within the cap.
func TestNewClaimedSourceWarnsForAnOrgTargetOverTheFanoutCap(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	newCursorSourceWith(t, truncatedOrgClient(), cursorAllowlist(maxOrganizationFanoutRepositories), newCursorLedger(), nil, logger)
	if logs.Len() != 0 {
		t.Fatalf("warned for a target within the cap: %s", logs.String())
	}
	newCursorSourceWith(t, truncatedOrgClient(), cursorAllowlist(maxOrganizationFanoutRepositories+1), newCursorLedger(), nil, logger)
	out := logs.String()
	if strings.Count(out, "level=WARN") != 1 {
		t.Fatalf("want exactly one WARN, got: %s", out)
	}
	for _, want := range []string{"repositories=101", "cap=100", "scope_id=" + fanoutTestOrgScopeID, "#9019", "org_fanout_over_cap"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning is missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "repo-0") {
		t.Errorf("warning names a repository: %s", out)
	}
}
