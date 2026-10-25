// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func cursorAllowlist(count int) []string {
	allowed := make([]string, 0, count)
	for i := range count {
		allowed = append(allowed, fmt.Sprintf("example-org/repo-%03d", i))
	}
	return allowed
}

func truncatedOrgClient() *fanoutClient {
	return &fanoutClient{org: securityalerts.GitHubDependabotAlertResult{PagesFetched: 1, Truncated: true}}
}

// cursorLedger stands in for the committed read-state rows: it is the
// FanoutCursorReader the source reads and the GenerationStateWriter factory the
// source hands each org claim. Nothing reaches committed until a test commits a
// claim, the way the ingestion store runs the writer in the generation's
// transaction.
type cursorLedger struct {
	mu        sync.Mutex
	committed map[string]securityalerts.FanoutCursor
	states    []securityalerts.TargetReadState
	readErr   error
	reads     int
}

func newCursorLedger() *cursorLedger {
	return &cursorLedger{committed: map[string]securityalerts.FanoutCursor{}}
}

func (l *cursorLedger) FanoutCursor(_ context.Context, scopeID string) (securityalerts.FanoutCursor, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reads++
	if l.readErr != nil {
		return securityalerts.FanoutCursor{}, l.readErr
	}
	return l.committed[scopeID], nil
}

func (l *cursorLedger) writer(state securityalerts.TargetReadState) collector.GenerationStateWriter {
	return func(context.Context, collector.CommitExecutor) error {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.committed[state.ScopeID] = securityalerts.FanoutCursor{
			NextRepository: state.FanoutNextRepository,
			LapRepository:  state.FanoutLapRepository,
		}
		l.states = append(l.states, state)
		return nil
	}
}

// newCursorSource builds a fan-out source over ledger, so a second source over
// the same ledger models a restarted collector.
func newCursorSource(
	t *testing.T,
	client RepositoryAlertClient,
	allowed []string,
	ledger *cursorLedger,
) *ClaimedSource {
	t.Helper()
	return newCursorSourceWith(t, client, allowed, ledger, nil, nil)
}

func newCursorSourceWith(
	t *testing.T,
	client RepositoryAlertClient,
	allowed []string,
	ledger *cursorLedger,
	tracer trace.Tracer,
	logger *slog.Logger,
) *ClaimedSource {
	t.Helper()
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	config := SourceConfig{
		CollectorInstanceID: "security-alert-primary",
		Targets: []TargetConfig{{
			Provider:            ProviderGitHubDependabot,
			Scope:               TargetScopeOrganization,
			ScopeID:             fanoutTestOrgScopeID,
			Organization:        "example-org",
			Token:               "github-token",
			MaxPages:            1,
			AllowedRepositories: allowed,
		}},
		ClientFactory: func(TargetConfig) (RepositoryAlertClient, error) { return client, nil },
		Now:           func() time.Time { return now },
		Tracer:        tracer,
		Logger:        logger,
	}
	if ledger != nil {
		config.ReadStateWriter = ledger.writer
		config.FanoutCursorReader = ledger
	}
	source, err := NewClaimedSource(config)
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	return depthCappedLeaf(source)
}

// claimOnce runs one claim and returns its per-repository reads, freshness
// hint, and state writer, without committing.
func claimOnce(t *testing.T, source *ClaimedSource, client *fanoutClient) ([]string, string, collector.GenerationStateWriter) {
	t.Helper()
	before := len(client.repoCalls)
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	item := testSecurityAlertWorkItem(now)
	item.ScopeID = fanoutTestOrgScopeID
	item.AcceptanceUnitID = fanoutTestOrgScopeID
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = ok %v, err %v, want a collected generation", ok, err)
	}
	drainFacts(collected.Facts)
	return slices.Clone(client.repoCalls[before:]), collected.Generation.FreshnessHint, collected.StateWriter
}

// claimCalls runs one claim and commits its state, as a stored generation does.
func claimCalls(t *testing.T, source *ClaimedSource, client *fanoutClient) ([]string, string) {
	t.Helper()
	calls, hint, writer := claimOnce(t, source, client)
	if writer == nil {
		t.Fatal("org claim has no state writer: the cursor cannot commit")
	}
	if err := writer(context.Background(), nil); err != nil {
		t.Fatalf("state writer error = %v", err)
	}
	return calls, hint
}

// storingCommitter models the ingestion store: a claim whose freshness hint
// equals the newest stored generation's hint is skipped and its state writer
// never runs; any other claim is stored and its state commits.
type storingCommitter struct {
	lastHint string
	stored   int
}

// claim runs one claim through the committer and reports the repositories it
// read, its hint, and whether the generation was stored.
func (c *storingCommitter) claim(t *testing.T, source *ClaimedSource, client *fanoutClient) ([]string, string, bool) {
	t.Helper()
	calls, hint, writer := claimOnce(t, source, client)
	if hint == c.lastHint {
		return calls, hint, false
	}
	c.lastHint = hint
	c.stored++
	if err := writer(context.Background(), nil); err != nil {
		t.Fatalf("state writer error = %v", err)
	}
	return calls, hint, true
}

// failingRepositories makes every listed repository answer 503, a stop that is
// not the repository's fault but repeats on every claim.
func failingRepositories(client *fanoutClient, repositories ...string) {
	client.repoErrs = map[string]error{}
	for _, repository := range repositories {
		client.repoErrs[repository] = securityalerts.GitHubDependabotError{StatusCode: 503}
	}
}

// TestClaimedSourceFanoutCapTargetKeepsItsTopWindow pins the withdrawn
// rotation (#8946): an allowlist over the per-claim cap reads positions 0 to 99
// on every claim, as the org fan-out always did, so it commits one generation
// and then skips. Rotating a window through a full-snapshot scope would store a
// generation per claim, 120 an hour at the default cadence, and retain every
// one for the superseded-generation window; the fix for a target this large is
// to split it (#9019). The per-claim request bound holds.
func TestClaimedSourceFanoutCapTargetKeepsItsTopWindow(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(2*maxOrganizationFanoutRepositories + 50) // 250
	client := truncatedOrgClient()
	ledger := newCursorLedger()
	source := newCursorSource(t, client, allowed, ledger)
	committer := &storingCommitter{}

	for claim := range 4 {
		calls, _, stored := committer.claim(t, source, client)
		if !slices.Equal(calls, allowed[:maxOrganizationFanoutRepositories]) {
			t.Fatalf("claim %d read %d repositories from %s, want the first %d from the top", claim+1, len(calls), calls[0], maxOrganizationFanoutRepositories)
		}
		if stored != (claim == 0) {
			t.Fatalf("claim %d stored = %v, want only the first claim stored", claim+1, stored)
		}
		for _, page := range client.repoPages {
			if page != 1 {
				t.Fatalf("per-repository read max_pages = %d, want 1", page)
			}
		}
	}
	if got := ledger.committed[fanoutTestOrgScopeID]; got != (securityalerts.FanoutCursor{}) {
		t.Fatalf("cursor of a target over the cap = %+v, want the top and no lap", got)
	}
}

// TestClaimedSourceFanoutIgnoresStoredCursorWhenAllowlistGrowsPastTheCap proves
// a target that fit and grew past the cap starts at the top, not at the cursor
// its smaller allowlist committed.
func TestClaimedSourceFanoutIgnoresStoredCursorWhenAllowlistGrowsPastTheCap(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(maxOrganizationFanoutRepositories + 20)
	ledger := newCursorLedger()
	ledger.committed[fanoutTestOrgScopeID] = securityalerts.FanoutCursor{NextRepository: allowed[50]}
	client := truncatedOrgClient()
	calls, _ := claimCalls(t, newCursorSource(t, client, allowed, ledger), client)
	if calls[0] != allowed[0] {
		t.Fatalf("claim started at %s, want the top %s", calls[0], allowed[0])
	}
	if ledger.reads != 0 {
		t.Fatalf("cursor reads = %d, want none for a target over the cap", ledger.reads)
	}
}

// TestClaimedSourceFanoutResumesAfterRestartFromStoredCursor proves a second
// source over the same cursor store (a restart, or a second claim of the same
// target) starts where the first stopped, while an empty store starts at the
// top.
func TestClaimedSourceFanoutResumesAfterRestartFromStoredCursor(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(10)
	store := newCursorLedger()
	client := truncatedOrgClient()
	failingRepositories(client, allowed[5])
	first, _ := claimCalls(t, newCursorSource(t, client, allowed, store), client)
	if got, want := first[0], allowed[0]; got != want {
		t.Fatalf("first claim started at %s, want %s", got, want)
	}

	restarted := truncatedOrgClient()
	failingRepositories(restarted, allowed[5])
	second, _ := claimCalls(t, newCursorSource(t, restarted, allowed, store), restarted)
	if got, want := second[0], allowed[6]; got != want {
		t.Fatalf("restarted claim started at %s, want %s (the repository after the failed one)", got, want)
	}

	fresh := truncatedOrgClient()
	failingRepositories(fresh, allowed[5])
	again, _ := claimCalls(t, newCursorSource(t, fresh, allowed, newCursorLedger()), fresh)
	if got, want := again[0], allowed[0]; got != want {
		t.Fatalf("claim with no stored cursor started at %s, want %s", got, want)
	}
}

// TestClaimedSourceFanoutIgnoresCursorOutsideAllowlist proves an allowlist
// change that removes the stored repository restarts at the top instead of
// failing or skipping repositories.
func TestClaimedSourceFanoutIgnoresCursorOutsideAllowlist(t *testing.T) {
	t.Parallel()

	store := newCursorLedger()
	store.committed[fanoutTestOrgScopeID] = securityalerts.FanoutCursor{NextRepository: "example-org/removed-repo"}
	allowed := cursorAllowlist(5)
	client := truncatedOrgClient()
	calls, _ := claimCalls(t, newCursorSource(t, client, allowed, store), client)
	if !slices.Equal(calls, allowed) {
		t.Fatalf("calls = %v, want the whole allowlist from the top %v", calls, allowed)
	}
}

// TestClaimedSourceFanoutPersistentFailureDoesNotPinTheTail proves a
// repository whose read stops the fan-out on every claim (a persistent 503)
// does not pin the repositories after it: the next claim starts past it and
// wraps around, so the failing repository is retried last, and the start then
// settles: claims 2 and 3 share a hint. Only the repositories between the
// settled start and the stopper in the walk stay read; the stopper's own
// segment behind it is the documented gap.
func TestClaimedSourceFanoutPersistentFailureDoesNotPinTheTail(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(10)
	client := truncatedOrgClient()
	client.repoErrs = map[string]error{allowed[5]: securityalerts.GitHubDependabotError{StatusCode: 503}}
	source := newCursorSource(t, client, allowed, newCursorLedger())

	first, firstHint := claimCalls(t, source, client)
	if want := allowed[:6]; !slices.Equal(first, want) {
		t.Fatalf("first claim calls = %v, want %v (stop at the failing repository)", first, want)
	}
	second, secondHint := claimCalls(t, source, client)
	want := append(slices.Clone(allowed[6:]), allowed[:6]...)
	if !slices.Equal(second, want) {
		t.Fatalf("second claim calls = %v, want %v (resume past the failure, wrap, retry it last)", second, want)
	}
	third, thirdHint := claimCalls(t, source, client)
	if !slices.Equal(third, want) {
		t.Fatalf("third claim calls = %v, want the same sequence as the second %v", third, want)
	}
	if secondHint != thirdHint {
		t.Fatal("freshness hint changed between claims that read the same data")
	}
	if firstHint == secondHint {
		t.Fatal("freshness hint unchanged after the tail was read, want a new boundary")
	}
}

// TestClaimedSourceFanoutRateLimitKeepsTheStart proves a rate limit does not
// move the start: the limit is provider-wide and the repository is not at
// fault, so the next claim begins where this one did.
func TestClaimedSourceFanoutRateLimitKeepsTheStart(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(6)
	client := truncatedOrgClient()
	client.repoErrs = map[string]error{allowed[2]: securityalerts.GitHubDependabotError{
		StatusCode: 403,
		RateLimit:  securityalerts.GitHubRateLimitInfo{Remaining: 0, RemainingKnown: true},
	}}
	ledger := newCursorLedger()
	source := newCursorSource(t, client, allowed, ledger)

	claimCalls(t, source, client)
	second, _ := claimCalls(t, source, client)
	if got, want := second[0], allowed[0]; got != want {
		t.Fatalf("claim after a rate limit started at %s, want the unchanged start %s", got, want)
	}
	if got := ledger.committed[fanoutTestOrgScopeID]; got != (securityalerts.FanoutCursor{}) {
		t.Fatalf("cursor after a rate limit = %+v, want the top it started at", got)
	}
}

// TestClaimedSourceFanoutStableWhenAllowlistFitsTheCap proves an allowlist that
// fits the per-claim cap and reads in full never moves: the cursor stays at the top and the
// freshness digest is the same on every claim over unchanged data.
func TestClaimedSourceFanoutStableWhenAllowlistFitsTheCap(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(maxOrganizationFanoutRepositories)
	client := truncatedOrgClient()
	store := newCursorLedger()
	source := newCursorSource(t, client, allowed, store)

	firstCalls, first := claimCalls(t, source, client)
	secondCalls, second := claimCalls(t, source, client)
	if !slices.Equal(firstCalls, secondCalls) || !slices.Equal(firstCalls, allowed) {
		t.Fatalf("claims read %d then %d repositories, want the whole allowlist in order both times", len(firstCalls), len(secondCalls))
	}
	if first == "" || first != second {
		t.Fatalf("freshness hint = %q then %q, want a stable non-empty digest", first, second)
	}
	if cursor := store.committed[fanoutTestOrgScopeID]; cursor != (securityalerts.FanoutCursor{}) {
		t.Fatalf("cursor = %+v, want the top (a full read)", cursor)
	}
}

// TestClaimedSourceFanoutSpanCarriesCursorOffsets proves an operator can read
// where a claim started and where the next one resumes without a repository
// name on the span.
func TestClaimedSourceFanoutSpanCarriesCursorOffsets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		failing int // allowlist position of the persistent stopper
		claims  []struct {
			start, next int
			lapped      bool
		}
	}{
		{"a stopper in the middle", 5, []struct {
			start, next int
			lapped      bool
		}{{0, 6, false}, {6, 6, false}}},
		{"a stopper at the last position closes the lap", 9, []struct {
			start, next int
			lapped      bool
		}{{0, 0, true}, {0, 0, true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed := cursorAllowlist(10)
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			client := truncatedOrgClient()
			failingRepositories(client, allowed[tc.failing])
			source := newCursorSourceWith(t, client, allowed, newCursorLedger(), provider.Tracer("test"), nil)
			for _, want := range tc.claims {
				claimCalls(t, source, client)
				var observe sdktrace.ReadOnlySpan
				for _, span := range recorder.Ended() {
					if span.Name() == telemetry.SpanSecurityAlertObserve {
						observe = span
					}
				}
				got := map[string]attribute.Value{}
				for _, kv := range observe.Attributes() {
					got[string(kv.Key)] = kv.Value
				}
				if v := got[contract.AttrSecurityAlertFanoutStartOffset]; v != attribute.IntValue(want.start) {
					t.Fatalf("start offset = %v, want %d", v, want.start)
				}
				if v := got[contract.AttrSecurityAlertFanoutNextOffset]; v != attribute.IntValue(want.next) {
					t.Fatalf("next offset = %v, want %d", v, want.next)
				}
				if v := got[contract.AttrSecurityAlertFanoutLapped]; v != attribute.BoolValue(want.lapped) {
					t.Fatalf("lapped = %v, want %v", v, want.lapped)
				}
			}
		})
	}
}

func orgWorkItem() workflow.WorkItem {
	item := testSecurityAlertWorkItem(time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC))
	item.ScopeID = fanoutTestOrgScopeID
	item.AcceptanceUnitID = fanoutTestOrgScopeID
	return item
}
