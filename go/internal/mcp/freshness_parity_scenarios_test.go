package mcp

// Incremental-freshness parity scenarios (issue #1804, parent #1797).
//
// Each scenario drives ONE freshness question through the HTTP surface and the
// MCP surface against the SAME query.FreshnessHandler fixture, then asserts the
// canonical envelope agrees on truth, freshness state, and source generation
// ids. Building / stale / unavailable states are asserted non-green: the exact
// state string is pinned so a regression that rendered them as "fresh" fails.
//
// Scenario coverage (the #1804 acceptance list):
//
//	Unchanged refresh skipped by freshness hint  -> TestFreshnessParityUnchangedRefreshSkipped
//	Changed generation supersedes prior active   -> TestFreshnessParityChangedGenerationSupersedes
//	Projector work superseded by newer generation-> TestFreshnessParityProjectorWorkSuperseded
//	Reducer work filtered/superseded inactive gen-> TestFreshnessParityReducerWorkFilteredInactive
//	Retired evidence disappears from active reads-> TestFreshnessParityRetiredEvidenceDisappears
//	Webhook-triggered refresh reaches current    -> TestFreshnessParityWebhookRefreshReachesCurrent
//
// Two non-green guards round out the set:
//
//	Pending generation building (lifecycle)      -> TestFreshnessParityPendingGenerationBuilding
//	No current active generation unavailable      -> TestFreshnessParityNoActiveGenerationUnavailable

import (
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

const (
	parityScope    = "git-repository-scope:acme/app"
	parityRepo     = "acme/app"
	genGenPath     = "/api/v0/freshness/generations"
	genChangedPath = "/api/v0/freshness/changed-since"
)

// driveGenerationParity drives the generation-lifecycle question through HTTP
// and MCP against one handler and returns the two comparable projections plus
// the HTTP-side projection's anchor fields for direct assertion.
func driveGenerationParity(t *testing.T, handler http.Handler, rawQuery string, args map[string]any) (httpCmp, mcpCmp freshnessComparable) {
	t.Helper()

	httpEnv := httpEnvelope(t, handler, http.MethodGet, genGenPath+rawQuery, nil)
	mcpEnv, summary := mcpEnvelope(t, handler, "get_generation_lifecycle", args)

	httpCmp = extractFreshnessComparable(t, httpEnv)
	mcpCmp = extractFreshnessComparable(t, mcpEnv)
	requireFreshnessParity(t, "http", "mcp", httpCmp, mcpCmp)
	requireConvenienceSummary(t, summary, mcpEnv)
	return httpCmp, mcpCmp
}

// driveChangedSinceParity drives the changed-since question through HTTP and
// MCP against one handler and returns the two comparable projections.
func driveChangedSinceParity(t *testing.T, handler http.Handler, rawQuery string, args map[string]any) (httpCmp, mcpCmp freshnessComparable) {
	t.Helper()

	httpEnv := httpEnvelope(t, handler, http.MethodGet, genChangedPath+rawQuery, nil)
	mcpEnv, summary := mcpEnvelope(t, handler, "get_changed_since", args)

	httpCmp = extractFreshnessComparable(t, httpEnv)
	mcpCmp = extractFreshnessComparable(t, mcpEnv)
	requireFreshnessParity(t, "http", "mcp", httpCmp, mcpCmp)
	requireConvenienceSummary(t, summary, mcpEnv)
	return httpCmp, mcpCmp
}

// TestFreshnessParityUnchangedRefreshSkipped proves that when a refresh is
// skipped by the freshness hint (the active generation is the prior generation,
// marked "unchanged"), both surfaces report the SAME fresh truth and the SAME
// active generation. No false delta, no building state.
func TestFreshnessParityUnchangedRefreshSkipped(t *testing.T) {
	t.Parallel()

	handler := mountFreshnessHandler(t, fixedGenerationReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{{
			ScopeID:                   parityScope,
			GenerationID:              "gen-base",
			ScopeKind:                 "repository",
			CurrentActiveGenerationID: "gen-base",
			IsActive:                  true,
			Status:                    "completed",
			TriggerKind:               "scheduled",
			FreshnessHint:             "unchanged",
			QueueStatus:               status.GenerationQueueStatus{Total: 4, Succeeded: 4},
		}},
		Limit: 50,
	}}, nil)

	httpCmp, _ := driveGenerationParity(
		t,
		handler,
		"?scope_id="+parityScope,
		map[string]any{"scope_id": parityScope},
	)

	if httpCmp.freshnessState != query.FreshnessFresh {
		t.Fatalf("freshness = %q, want fresh (unchanged refresh is current truth)", httpCmp.freshnessState)
	}
	requireGenerationIDs(t, httpCmp, []string{"gen:gen-base", "active:gen-base"})
}

// TestFreshnessParityChangedGenerationSupersedes proves that when a changed
// generation supersedes the prior active generation, both surfaces report the
// SAME superseded prior row and the SAME new active generation. The active
// pointer must be the new generation on both surfaces.
func TestFreshnessParityChangedGenerationSupersedes(t *testing.T) {
	t.Parallel()

	handler := mountFreshnessHandler(t, fixedGenerationReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{
			{
				ScopeID:                   parityScope,
				GenerationID:              "gen-new",
				CurrentActiveGenerationID: "gen-new",
				IsActive:                  true,
				Status:                    "active",
				TriggerKind:               "push",
				FreshnessHint:             "changed",
				QueueStatus:               status.GenerationQueueStatus{Total: 6, Succeeded: 6},
			},
			{
				ScopeID:                   parityScope,
				GenerationID:              "gen-old",
				CurrentActiveGenerationID: "gen-new",
				IsActive:                  false,
				Status:                    "superseded",
			},
		},
		Limit: 50,
	}}, nil)

	httpCmp, _ := driveGenerationParity(
		t,
		handler,
		"?scope_id="+parityScope,
		map[string]any{"scope_id": parityScope},
	)

	if httpCmp.freshnessState != query.FreshnessFresh {
		t.Fatalf("freshness = %q, want fresh (new generation is active)", httpCmp.freshnessState)
	}
	// Both the new active row and the superseded prior row must report the new
	// generation as the active pointer, on BOTH surfaces (parity already
	// asserted). The superseded row must not claim it is active.
	requireGenerationIDs(t, httpCmp, []string{
		"gen:gen-new", "active:gen-new",
		"gen:gen-old", "active:gen-new",
	})
}

// TestFreshnessParityProjectorWorkSuperseded proves that projector work tied to
// a superseded generation is reported as superseded (not active) and that a
// newer pending generation still in flight surfaces as building on both
// surfaces — never silently fresh.
func TestFreshnessParityProjectorWorkSuperseded(t *testing.T) {
	t.Parallel()

	handler := mountFreshnessHandler(t, fixedGenerationReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{
			{
				ScopeID:                   parityScope,
				GenerationID:              "gen-pending",
				CurrentActiveGenerationID: "gen-prev",
				IsActive:                  false,
				Status:                    "pending",
				TriggerKind:               "push",
				// Outstanding projector/reducer work items -> building.
				QueueStatus: status.GenerationQueueStatus{Total: 5, Outstanding: 3, Succeeded: 2},
			},
			{
				ScopeID:                   parityScope,
				GenerationID:              "gen-prev",
				CurrentActiveGenerationID: "gen-prev",
				IsActive:                  true,
				Status:                    "completed",
				QueueStatus:               status.GenerationQueueStatus{Total: 4, Succeeded: 4},
			},
		},
		Limit: 50,
	}}, nil)

	httpCmp, _ := driveGenerationParity(
		t,
		handler,
		"?scope_id="+parityScope,
		map[string]any{"scope_id": parityScope},
	)

	// A newer generation with outstanding projector work is building, NOT fresh.
	requireNonGreenState(t, httpCmp, query.FreshnessBuilding)
	requireGenerationIDs(t, httpCmp, []string{
		"gen:gen-pending", "active:gen-prev",
		"gen:gen-prev", "active:gen-prev",
	})
}

// TestFreshnessParityReducerWorkFilteredInactive proves that filtering the
// lifecycle to a single inactive (superseded) generation returns that row with
// the active pointer aimed at the newer generation, identically across
// surfaces. Reducer work for the inactive generation is not promoted to active.
func TestFreshnessParityReducerWorkFilteredInactive(t *testing.T) {
	t.Parallel()

	handler := mountFreshnessHandler(t, fixedGenerationReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{{
			ScopeID:                   parityScope,
			GenerationID:              "gen-inactive",
			CurrentActiveGenerationID: "gen-current",
			IsActive:                  false,
			Status:                    "superseded",
			QueueStatus:               status.GenerationQueueStatus{Total: 3, Succeeded: 3},
		}},
		Limit: 50,
	}}, nil)

	httpCmp, _ := driveGenerationParity(
		t,
		handler,
		"?scope_id="+parityScope+"&status=superseded",
		map[string]any{"scope_id": parityScope, "status": "superseded"},
	)

	if httpCmp.freshnessState != query.FreshnessFresh {
		t.Fatalf("freshness = %q, want fresh (no outstanding work in the filtered page)", httpCmp.freshnessState)
	}
	requireGenerationIDs(t, httpCmp, []string{"gen:gen-inactive", "active:gen-current"})
}

// TestFreshnessParityRetiredEvidenceDisappears proves that retired evidence is
// reported as a retired delta (and NOT collapsed into unchanged or superseded)
// identically across surfaces. The changed-since diff is the read surface where
// retired evidence is visible as "gone from active reads".
func TestFreshnessParityRetiredEvidenceDisappears(t *testing.T) {
	t.Parallel()

	handler := mountFreshnessHandler(t, nil, fixedChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:                   parityScope,
		ScopeKind:                 "repository",
		Repository:                parityRepo,
		SinceGenerationID:         "gen-prior",
		CurrentActiveGenerationID: "gen-current",
		SampleLimit:               25,
		Categories: []status.ChangedSinceCategoryDelta{
			{
				Category: status.ChangedSinceCategoryFiles,
				Counts:   status.ChangedSinceCounts{Added: 1, Updated: 0, Unchanged: 7, Retired: 2, Superseded: 0},
				Samples: map[status.ChangedSinceClassification][]status.ChangedSinceSample{
					status.ChangedSinceRetired: {
						{StableFactKey: "file:removed-a", FactKind: "file"},
						{StableFactKey: "file:removed-b", FactKind: "file"},
					},
				},
			},
		},
	}})

	httpCmp, _ := driveChangedSinceParity(
		t,
		handler,
		"?scope_id="+parityScope+"&since_generation_id=gen-prior",
		map[string]any{"scope_id": parityScope, "since_generation_id": "gen-prior"},
	)

	if httpCmp.freshnessState != query.FreshnessFresh {
		t.Fatalf("freshness = %q, want fresh (diff fully computed)", httpCmp.freshnessState)
	}
	requireGenerationIDs(t, httpCmp, []string{"since:gen-prior", "current:gen-current"})
	// Retired count must be exactly 2 and must not collapse into superseded.
	// changeCounts order is [added, updated, unchanged, retired, superseded].
	wantCounts := []int{1, 0, 7, 2, 0}
	if !equalIntSlices(httpCmp.changeCounts, wantCounts) {
		t.Fatalf("change counts = %v, want %v (retired must stay separate)", httpCmp.changeCounts, wantCounts)
	}
}

// TestFreshnessParityWebhookRefreshReachesCurrent proves that a
// webhook-triggered refresh that has completed reaches the current answer state:
// the new generation is active and the changed-since diff against the prior
// generation reports the change deltas, identically across surfaces, with fresh
// truth.
func TestFreshnessParityWebhookRefreshReachesCurrent(t *testing.T) {
	t.Parallel()

	handler := mountFreshnessHandler(t,
		fixedGenerationReader{page: status.GenerationLifecyclePage{
			Records: []status.GenerationLifecycleRecord{{
				ScopeID:                   parityScope,
				GenerationID:              "gen-webhook",
				CurrentActiveGenerationID: "gen-webhook",
				IsActive:                  true,
				Status:                    "active",
				TriggerKind:               "webhook",
				FreshnessHint:             "changed",
				QueueStatus:               status.GenerationQueueStatus{Total: 8, Succeeded: 8},
			}},
			Limit: 50,
		}},
		fixedChangedSinceReader{summary: status.ChangedSinceSummary{
			ScopeID:                   parityScope,
			ScopeKind:                 "repository",
			Repository:                parityRepo,
			SinceGenerationID:         "gen-prior",
			CurrentActiveGenerationID: "gen-webhook",
			SampleLimit:               25,
			Categories: []status.ChangedSinceCategoryDelta{
				{Category: status.ChangedSinceCategoryFiles, Counts: status.ChangedSinceCounts{Added: 3, Updated: 2, Unchanged: 5}},
			},
		}})

	// Lifecycle surface: webhook generation is active and fresh.
	genCmp, _ := driveGenerationParity(
		t,
		handler,
		"?scope_id="+parityScope,
		map[string]any{"scope_id": parityScope},
	)
	if genCmp.freshnessState != query.FreshnessFresh {
		t.Fatalf("lifecycle freshness = %q, want fresh after webhook refresh", genCmp.freshnessState)
	}
	requireGenerationIDs(t, genCmp, []string{"gen:gen-webhook", "active:gen-webhook"})

	// Changed-since surface: the diff reaches the webhook generation as current.
	chgCmp, _ := driveChangedSinceParity(
		t,
		handler,
		"?scope_id="+parityScope+"&since_generation_id=gen-prior",
		map[string]any{"scope_id": parityScope, "since_generation_id": "gen-prior"},
	)
	if chgCmp.freshnessState != query.FreshnessFresh {
		t.Fatalf("changed-since freshness = %q, want fresh after webhook refresh", chgCmp.freshnessState)
	}
	requireGenerationIDs(t, chgCmp, []string{"since:gen-prior", "current:gen-webhook"})
}

// TestFreshnessParityPendingGenerationBuilding proves the building state is
// surfaced as exactly "building" on both surfaces when a pending generation is
// in flight. This is a non-green guard: building must never render as fresh.
func TestFreshnessParityPendingGenerationBuilding(t *testing.T) {
	t.Parallel()

	handler := mountFreshnessHandler(t, fixedGenerationReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{{
			ScopeID:      parityScope,
			GenerationID: "gen-inflight",
			Status:       "pending",
			TriggerKind:  "webhook",
			QueueStatus:  status.GenerationQueueStatus{Total: 4, Outstanding: 4},
		}},
		Limit: 50,
	}}, nil)

	httpCmp, _ := driveGenerationParity(
		t,
		handler,
		"?repository="+parityRepo,
		map[string]any{"repository": parityRepo},
	)
	requireNonGreenState(t, httpCmp, query.FreshnessBuilding)
}

// TestFreshnessParityNoActiveGenerationUnavailable proves the unavailable state
// is surfaced as exactly "unavailable" on both surfaces when the scope has no
// current active generation and the diff cannot be computed. Non-green guard.
func TestFreshnessParityNoActiveGenerationUnavailable(t *testing.T) {
	t.Parallel()

	handler := mountFreshnessHandler(t, nil, fixedChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:     parityScope,
		ScopeKind:   "repository",
		Repository:  parityRepo,
		Unavailable: true,
		SampleLimit: 25,
		Categories: []status.ChangedSinceCategoryDelta{
			{Category: status.ChangedSinceCategoryFiles, Unavailable: true},
		},
	}})

	httpCmp, _ := driveChangedSinceParity(
		t,
		handler,
		"?scope_id="+parityScope+"&since_generation_id=gen-prior",
		map[string]any{"scope_id": parityScope, "since_generation_id": "gen-prior"},
	)
	requireNonGreenState(t, httpCmp, query.FreshnessUnavailable)
}

// requireGenerationIDs asserts the answer reports exactly the expected source
// generation identities in order. Parity has already proven HTTP and MCP agree;
// this anchors the SHARED contract so a regression cannot make both wrong in
// lockstep and still pass parity.
func requireGenerationIDs(t *testing.T, cmp freshnessComparable, want []string) {
	t.Helper()
	if !equalStringSlices(cmp.generationIDs, want) {
		t.Fatalf("source generation ids = %v, want %v", cmp.generationIDs, want)
	}
}

// requireNonGreenState asserts the freshness state is exactly the expected
// non-green state (building / stale / unavailable) and is NOT fresh. It is the
// explicit guard the #1804 acceptance criteria require: a non-green freshness
// state must be surfaced as such, never silently rendered as fresh.
func requireNonGreenState(t *testing.T, cmp freshnessComparable, want query.FreshnessState) {
	t.Helper()
	if want == query.FreshnessFresh {
		t.Fatalf("requireNonGreenState called with fresh; use a building/stale/unavailable state")
	}
	if cmp.freshnessState == query.FreshnessFresh {
		t.Fatalf("freshness state = fresh, want non-green %q (must not silently render as fresh)", want)
	}
	if cmp.freshnessState != want {
		t.Fatalf("freshness state = %q, want %q", cmp.freshnessState, want)
	}
}
