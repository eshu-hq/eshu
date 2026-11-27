// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"encoding/json"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The #11613 PR1 holder-freshness watermark proofs (red-first): the origin
// stamp carries the origin pass's fencing token (absent = never skip), the
// shadow outcome counts separately from pending_noop, a merged stamp scopes
// both changes, and the handler stamps the origin with its own pass token.

// TestFanOutStampCarriesTheOriginPassToken proves the #11613 token rule: a
// token-bearing origin marshals its fencing token into the stamp, and the
// stamped set round-trips with it.
func TestFanOutStampCarriesTheOriginPassToken(t *testing.T) {
	t.Parallel()

	origin := FanOutOrigin{
		SourceScopeID:      "scope:source",
		SourceGenerationID: "gen-1",
		SourceIntentID:     "intent-1",
		CVEIDs:             []string{"CVE-2026-7002"},
		FencingToken:       41,
	}
	encoded, err := FanOutStampJSON(origin)
	if err != nil {
		t.Fatalf("FanOutStampJSON() error = %v, want nil", err)
	}
	var stamp map[string]any
	if err := json.Unmarshal(encoded, &stamp); err != nil {
		t.Fatalf("unmarshal stamp: %v", err)
	}
	token, ok := stamp[FanOutStampFencingTokenKey]
	if !ok {
		t.Fatalf("stamp %v has no %q key: the origin pass token must ride the stamp", stamp, FanOutStampFencingTokenKey)
	}
	if token != float64(41) {
		t.Fatalf("stamp token = %v, want 41", token)
	}
	// The CVE set rides with the token: a token without a set cannot scope,
	// and a set without a token cannot skip.
	raw, ok := stamp[FanOutStampCVEKey].([]any)
	if !ok || len(raw) != 1 || raw[0] != "CVE-2026-7002" {
		t.Fatalf("stamp set = %v, want [CVE-2026-7002]", stamp[FanOutStampCVEKey])
	}
}

// TestFanOutStampOmitsTheTokenWhenUnset pins the additive rule: a token-less
// origin (manifest markers, the retraction in-transaction reopen, old
// binaries) marshals byte-identical to before, and absent stays fail-closed.
func TestFanOutStampOmitsTheTokenWhenUnset(t *testing.T) {
	t.Parallel()

	withSet := FanOutOrigin{
		SourceScopeID:      "scope:source",
		SourceGenerationID: "gen-1",
		SourceIntentID:     "intent-1",
		CVEIDs:             []string{"CVE-2026-7002"},
	}
	encoded, err := FanOutStampJSON(withSet)
	if err != nil {
		t.Fatalf("FanOutStampJSON() error = %v, want nil", err)
	}
	var stamp map[string]any
	if err := json.Unmarshal(encoded, &stamp); err != nil {
		t.Fatalf("unmarshal stamp: %v", err)
	}
	if _, ok := stamp[FanOutStampFencingTokenKey]; ok {
		t.Fatalf("token-less stamp %v carries a token key: unset must stay absent, never zero", stamp)
	}
}

// TestShadowOutcomeCountsSeparatelyFromPendingNoop proves the PR1 shadow
// signal: a would-skip report increments its own counter and never lands in
// pending_noop, so the Gate-B stock metric cannot absorb it silently.
func TestShadowOutcomeCountsSeparatelyFromPendingNoop(t *testing.T) {
	t.Parallel()

	var result fanOutResult
	result.record(FanOutShadowWouldSkip)
	if result.shadowSkipped != 1 {
		t.Fatalf("shadowSkipped = %d, want 1", result.shadowSkipped)
	}
	if result.pendingNoop != 0 {
		t.Fatalf("pendingNoop = %d, want 0: the shadow outcome must not inflate the no-op count", result.pendingNoop)
	}
	subSignals := map[string]float64{}
	result.mergeSubSignals(subSignals)
	if got := subSignals["fanout_shadow_would_skip"]; got != 1 {
		t.Fatalf("fanout_shadow_would_skip = %v, want 1", got)
	}
}

// TestMergedStampScopesBothChanges is the handler-level proof the ruling
// demands: an intent stamped with a widened {A, B} stamp scopes its
// re-derivation to the union, so the second origin's change B is derived.
func TestMergedStampScopesBothChanges(t *testing.T) {
	t.Parallel()

	stamp, err := FanOutStampJSON(FanOutOrigin{
		SourceScopeID:      "scope:merged",
		SourceGenerationID: "gen-1",
		SourceIntentID:     "intent-merged",
		CVEIDs:             []string{"CVE-2026-7002", "CVE-2026-7001"},
		FencingToken:       9,
	})
	if err != nil {
		t.Fatalf("FanOutStampJSON() error = %v, want nil", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(stamp, &decoded); err != nil {
		t.Fatalf("unmarshal stamp: %v", err)
	}
	intent := reducercontract.Intent{
		IntentID:     "intent-merged",
		ScopeID:      "scope:holder",
		GenerationID: "gen-1",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		AttemptCount: 1,
		Payload: map[string]any{
			PayloadKeyFanOutOrigin: decoded,
			"fact_id":              "intent-merged",
		},
	}
	got := ScopedPassCVEIDs(intent)
	want := map[string]bool{"CVE-2026-7001": true, "CVE-2026-7002": true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Fatalf("ScopedPassCVEIDs() = %v, want both CVE-2026-7001 and CVE-2026-7002 derived", got)
	}
	if !fanOutOneHop(intent) {
		t.Fatal("a merged-stamp first attempt is not one hop: the reopened pass would run full, not scoped")
	}
}

// TestFanOutOriginCarriesThePassFencingToken proves the handler plumbs its
// own evidence-read token into the fan-out origin: the token the stamp
// carries is the token the writer admitted the pass with, so the holder
// watermark comparison orders real passes, not wall times.
func TestFanOutOriginCarriesThePassFencingToken(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	handler := fanOutHandler(port, writer, nil)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(writer.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(writer.writes))
	}
	passToken := writer.writes[0].FencingToken
	if passToken <= 0 {
		t.Fatalf("pass fencing token = %d, want the issued positive token", passToken)
	}
	if len(port.origins) == 0 {
		t.Fatal("no fan-out origin reached a holder: the change fanned out nowhere")
	}
	if got := port.origins[0].FencingToken; got != passToken {
		t.Fatalf("origin token = %d, want the pass token %d", got, passToken)
	}
}

// TestOldStampWithoutATokenParsesAsTokenLess pins the rolling-upgrade read:
// a stamp written before the token key existed parses with no token, so the
// shadow predicate fails closed on it instead of misreading a zero token.
func TestOldStampWithoutATokenParsesAsTokenLess(t *testing.T) {
	t.Parallel()

	stamp, err := FanOutStampJSON(FanOutOrigin{
		SourceScopeID:      "scope:source",
		SourceGenerationID: "gen-1",
		SourceIntentID:     "intent-1",
		CVEIDs:             []string{"CVE-2026-7002"},
	})
	if err != nil {
		t.Fatalf("FanOutStampJSON() error = %v, want nil", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(stamp, &decoded); err != nil {
		t.Fatalf("unmarshal stamp: %v", err)
	}
	if got := fanOutStampFencingToken(decoded); got != 0 {
		t.Fatalf("token of a pre-token stamp = %d, want 0 (absent reads as unset)", got)
	}
	if got := fanOutStampFencingToken(map[string]any{
		FanOutStampFencingTokenKey: "not-a-number",
	}); got != 0 {
		t.Fatalf("token of a corrupt stamp = %d, want 0 (fail closed, never coerce)", got)
	}
}
