// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// scriptRunner is a PagedRunner that replays canned satellite calls and
// records every request, so pagination tests prove the cursor the host sends
// on call N+1 is the cursor the satellite minted on call N.
type scriptRunner struct {
	requests []Request
	calls    []CallResult
	failAt   int
	failErr  error
}

func (r *scriptRunner) RunCollector(ctx context.Context, request Request) (sdkcollector.Result, error) {
	call, err := r.RunPagedCollector(ctx, request)
	return call.Result, err
}

func (r *scriptRunner) RunPagedCollector(_ context.Context, request Request) (CallResult, error) {
	r.requests = append(r.requests, request)
	index := len(r.requests) - 1
	if r.failErr != nil && index == r.failAt {
		return CallResult{}, r.failErr
	}
	if index >= len(r.calls) {
		return CallResult{}, errors.New("script calls exhausted")
	}
	return r.calls[index], nil
}

func pagedCall(item workflow.WorkItem, facts []sdkcollector.Fact, page *ResultPage) CallResult {
	result := completeResult(item, facts...)
	result.ProtocolVersion = collectorSDKProtocolV1Alpha3
	return CallResult{Result: result, Page: page}
}

func testManifestV1Alpha3() component.Manifest {
	manifest := testManifest()
	manifest.Spec.Runtime.SDKProtocol = component.CollectorSDKProtocolV1Alpha3
	return manifest
}

func mustNewPagedSource(t testing.TB, runner Runner, maxPages int) *Source {
	t.Helper()

	source, err := NewSource(Config{
		Manifest:            testManifestV1Alpha3(),
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              runner,
		Clock:               testObservedAt,
		MaxResultPages:      maxPages,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

// TestPagedDecodeAcceptsLegacyAndPaged proves the host stdio decode admits
// both shapes: legacy single-shot output decodes with a nil page, and
// v1alpha3 output carries the continuation, while unknown fields and trailing
// JSON still fail closed.
func TestPagedDecodeAcceptsLegacyAndPaged(t *testing.T) {
	t.Parallel()

	legacy := `{"protocol_version":"collector-sdk/v1alpha1","state":"complete",` +
		`"claim":{"component_id":"c","instance_id":"i","collector_kind":"k",` +
		`"source_system":"s","scope":{"id":"scope-1","kind":"repository"},` +
		`"source_run_id":"run-1","generation_id":"gen-1","work_item_id":"work-1",` +
		`"fencing_token":"7","attempt":1,"deadline":"2026-07-08T12:00:00Z",` +
		`"config_handle":"cfg"},` +
		`"generation":{"id":"gen-1","observed_at":"2026-07-08T12:00:00Z"}}`
	call, err := decodePagedSDKResult([]byte(legacy))
	if err != nil {
		t.Fatalf("decodePagedSDKResult(legacy) error = %v, want nil", err)
	}
	if call.Page != nil {
		t.Fatalf("decodePagedSDKResult(legacy) page = %+v, want nil", call.Page)
	}

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "sdk", "go", "collector", "testdata", "fixtures", "paged_first.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(paged_first) error = %v, want nil", err)
	}
	call, err = decodePagedSDKResult(raw)
	if err != nil {
		t.Fatalf("decodePagedSDKResult(paged) error = %v, want nil", err)
	}
	if call.Page == nil {
		t.Fatal("decodePagedSDKResult(paged) page = nil, want continuation")
	}
	if call.Page.Cursor != "page-cursor-1" || !call.Page.More {
		t.Fatalf("decodePagedSDKResult(paged) page = %+v, want cursor continuation", call.Page)
	}
	if call.Page.PageIndex != 0 || call.Page.TotalFacts != 2 {
		t.Fatalf("decodePagedSDKResult(paged) page = %+v, want index 0 total 2", call.Page)
	}
	if len(call.Result.Facts) != 1 {
		t.Fatalf("decodePagedSDKResult(paged) facts = %d, want 1", len(call.Result.Facts))
	}

	withUnknown := strings.Replace(string(raw), `"more": true`, `"more": true, "smuggled": 1`, 1)
	if _, err := decodePagedSDKResult([]byte(withUnknown)); err == nil {
		t.Fatal("decodePagedSDKResult(unknown field) error = nil, want non-nil")
	}
	if _, err := decodePagedSDKResult(append(raw, []byte(`{}`)...)); err == nil {
		t.Fatal("decodePagedSDKResult(trailing JSON) error = nil, want non-nil")
	} else if !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("decodePagedSDKResult(trailing) error = %v, want trailing JSON", err)
	}
}

// TestPagedDecodePreservesNumbers proves the paged decode keeps the exact
// JSON number treatment the single-shot decode guarantees.
func TestPagedDecodePreservesNumbers(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "sdk", "go", "collector", "testdata", "fixtures", "paged_first.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(paged_first) error = %v, want nil", err)
	}
	var shaped map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&shaped); err != nil {
		t.Fatalf("Decode() error = %v, want nil", err)
	}
	call, err := decodePagedSDKResult(raw)
	if err != nil {
		t.Fatalf("decodePagedSDKResult() error = %v, want nil", err)
	}
	want, _ := json.Marshal(shaped["facts"])
	got, _ := json.Marshal(call.Result.Facts)
	var wantFacts, gotFacts []map[string]any
	if err := json.Unmarshal(want, &wantFacts); err != nil {
		t.Fatal(err)
	}
	decoder = json.NewDecoder(strings.NewReader(string(got)))
	decoder.UseNumber()
	if err := decoder.Decode(&gotFacts); err != nil {
		t.Fatal(err)
	}
	if len(wantFacts) != len(gotFacts) {
		t.Fatalf("fact count = %d, want %d", len(gotFacts), len(wantFacts))
	}
}

// TestCollectPagesReassemblesIdenticalFactSets proves host-side reassembly:
// three split pages collected through NextClaimed commit the identical fact
// set, generation, and stable keys as the one-shot admission of the same
// facts.
func TestCollectPagesReassemblesIdenticalFactSets(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	first := testSDKFact(item)
	second := testSDKFact(item)
	second.StableKey = "scorecard:repo-eshu:second"
	second.SourceRef.FactKey = second.StableKey
	third := testSDKFact(item)
	third.StableKey = "scorecard:repo-eshu:third"
	third.SourceRef.FactKey = third.StableKey

	script := &scriptRunner{calls: []CallResult{
		pagedCall(item, []sdkcollector.Fact{first}, &ResultPage{Cursor: "cursor-1", More: true, TotalFacts: 3}),
		pagedCall(item, []sdkcollector.Fact{second}, &ResultPage{Cursor: "cursor-2", More: true, PageIndex: 1, TotalFacts: 3}),
		pagedCall(item, []sdkcollector.Fact{third}, &ResultPage{PageIndex: 2, TotalFacts: 3}),
	}}
	source := mustNewPagedSource(t, script, 0)
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%t error=%v, want true nil", ok, err)
	}
	pagedEnvelopes := collectFacts(t, collected)

	oneShot := &recordingRunner{result: completeResult(item, first, second, third)}
	legacy := mustNewSource(t, oneShot, nil)
	collected, ok, err = legacy.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%t error=%v, want true nil", ok, err)
	}
	singleEnvelopes := collectFacts(t, collected)

	if len(pagedEnvelopes) != len(singleEnvelopes) {
		t.Fatalf("paged envelopes = %d, one-shot = %d", len(pagedEnvelopes), len(singleEnvelopes))
	}
	pagedKeys := map[string]string{}
	for _, envelope := range pagedEnvelopes {
		pagedKeys[envelope.FactKind+"\x00"+envelope.StableFactKey] = envelope.GenerationID
	}
	for _, envelope := range singleEnvelopes {
		key := envelope.FactKind + "\x00" + envelope.StableFactKey
		generation, ok := pagedKeys[key]
		if !ok {
			t.Fatalf("one-shot fact %q missing from paged reassembly", key)
		}
		if generation != envelope.GenerationID {
			t.Fatalf("fact %q generation %q vs %q", key, generation, envelope.GenerationID)
		}
	}

	if len(script.requests) != 3 {
		t.Fatalf("runner calls = %d, want 3", len(script.requests))
	}
	if script.requests[0].Continuation != nil {
		t.Fatalf("first request continuation = %+v, want nil", script.requests[0].Continuation)
	}
	if script.requests[1].Continuation == nil || script.requests[1].Continuation.Cursor != "cursor-1" {
		t.Fatalf("second request continuation = %+v, want cursor-1", script.requests[1].Continuation)
	}
	if script.requests[2].Continuation == nil || script.requests[2].Continuation.Cursor != "cursor-2" {
		t.Fatalf("third request continuation = %+v, want cursor-2", script.requests[2].Continuation)
	}
}

// TestCollectPagesRejectsOutOfContractPages pins every cross-page rule to its
// failure class: generation drift is an identity mismatch, and sequence,
// total, envelope, version, and bound violations are invalid results. All are
// terminal.
func TestCollectPagesRejectsOutOfContractPages(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	fact := testSDKFact(item)
	other := testSDKFact(item)
	other.StableKey = "scorecard:repo-eshu:other"
	other.SourceRef.FactKey = other.StableKey

	// Generation drift keeps every identity the per-page validator checks
	// (claim and generation IDs agree inside the page) while moving the
	// generation timestamp the host pins from the first page.
	drifted := pagedCall(item, []sdkcollector.Fact{other}, &ResultPage{PageIndex: 1})
	drifted.Result.Generation.ObservedAt = testObservedAt().Add(time.Hour)

	legacyPaged := pagedCall(item, []sdkcollector.Fact{other}, &ResultPage{PageIndex: 1})
	legacyPaged.Result.ProtocolVersion = sdkcollector.ProtocolVersionV1Alpha1

	conflictSecond := pagedCall(item, []sdkcollector.Fact{other}, &ResultPage{PageIndex: 1})
	conflictSecond.Result.Facts = append(conflictSecond.Result.Facts, fact)
	conflictSecond.Result.Facts[1].Payload = map[string]any{"score": 1}

	for _, tc := range []struct {
		name      string
		calls     []CallResult
		maxPages  int
		wantClass string
	}{
		{
			name: "generation drift",
			calls: []CallResult{
				pagedCall(item, []sdkcollector.Fact{fact}, &ResultPage{Cursor: "c1", More: true, PageIndex: 0}),
				drifted,
			},
			wantClass: FailureClassIdentityMismatch,
		},
		{
			name: "page index gap",
			calls: []CallResult{
				pagedCall(item, []sdkcollector.Fact{fact}, &ResultPage{Cursor: "c1", More: true, PageIndex: 0}),
				pagedCall(item, []sdkcollector.Fact{other}, &ResultPage{PageIndex: 5}),
			},
			wantClass: FailureClassInvalidResult,
		},
		{
			name: "page index replay",
			calls: []CallResult{
				pagedCall(item, []sdkcollector.Fact{fact}, &ResultPage{Cursor: "c1", More: true, PageIndex: 0}),
				pagedCall(item, []sdkcollector.Fact{other}, &ResultPage{Cursor: "c2", More: true, PageIndex: 0}),
			},
			wantClass: FailureClassInvalidResult,
		},
		{
			name: "missing page envelope mid-stream",
			calls: []CallResult{
				pagedCall(item, []sdkcollector.Fact{fact}, &ResultPage{Cursor: "c1", More: true, PageIndex: 0}),
				{Result: completeResult(item, other)},
			},
			wantClass: FailureClassInvalidResult,
		},
		{
			name: "total facts drift",
			calls: []CallResult{
				pagedCall(item, []sdkcollector.Fact{fact}, &ResultPage{Cursor: "c1", More: true, PageIndex: 0, TotalFacts: 2}),
				pagedCall(item, []sdkcollector.Fact{other}, &ResultPage{PageIndex: 1, TotalFacts: 9}),
			},
			wantClass: FailureClassInvalidResult,
		},
		{
			name: "total facts shortfall",
			calls: []CallResult{
				pagedCall(item, []sdkcollector.Fact{fact}, &ResultPage{Cursor: "c1", More: true, PageIndex: 0, TotalFacts: 3}),
				pagedCall(item, []sdkcollector.Fact{other}, &ResultPage{PageIndex: 1, TotalFacts: 3}),
			},
			wantClass: FailureClassInvalidResult,
		},
		{
			name: "page on v1alpha1",
			calls: []CallResult{
				pagedCall(item, []sdkcollector.Fact{fact}, &ResultPage{Cursor: "c1", More: true, PageIndex: 0}),
				legacyPaged,
			},
			wantClass: FailureClassInvalidResult,
		},
		{
			name: "page bound exceeded",
			calls: []CallResult{
				pagedCall(item, []sdkcollector.Fact{fact}, &ResultPage{Cursor: "c1", More: true, PageIndex: 0}),
				pagedCall(item, []sdkcollector.Fact{other}, &ResultPage{Cursor: "c2", More: true, PageIndex: 1}),
				pagedCall(item, []sdkcollector.Fact{other}, &ResultPage{PageIndex: 2}),
			},
			maxPages:  2,
			wantClass: FailureClassInvalidResult,
		},
		{
			name: "cross-page duplicate conflict",
			calls: []CallResult{
				pagedCall(item, []sdkcollector.Fact{fact}, &ResultPage{Cursor: "c1", More: true, PageIndex: 0}),
				conflictSecond,
			},
			wantClass: FailureClassInvalidResult,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			script := &scriptRunner{calls: tc.calls}
			source := mustNewPagedSource(t, script, tc.maxPages)
			_, _, err := source.NextClaimed(context.Background(), item)
			if err == nil {
				t.Fatalf("NextClaimed() error = nil, want %q", tc.wantClass)
			}
			assertFailure(t, err, tc.wantClass, true)
		})
	}
}

// BenchmarkCollectPagesReassembly measures the host cost of splitting one
// 30-fact generation into three pages versus admitting it one-shot through
// the same v1alpha3 source. It isolates the reassembly loop (extra calls,
// per-page validation, accumulation) from satellite runtime.
func BenchmarkCollectPagesReassembly(b *testing.B) {
	item := testWorkItem()
	makeFacts := func(tag string) []sdkcollector.Fact {
		facts := make([]sdkcollector.Fact, 0, 10)
		for i := 0; i < 10; i++ {
			fact := testSDKFact(item)
			fact.StableKey = "scorecard:repo-eshu:" + tag + string(rune('a'+i))
			fact.SourceRef.FactKey = fact.StableKey
			facts = append(facts, fact)
		}
		return facts
	}
	b.Run("one-shot", func(b *testing.B) {
		call := pagedCall(item, append(append(makeFacts("a"), makeFacts("b")...), makeFacts("c")...), nil)
		script := &scriptRunner{calls: []CallResult{call}}
		source := mustNewPagedSource(b, script, 0)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			script.requests = script.requests[:0]
			if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
				b.Fatalf("NextClaimed() error = %v, want nil", err)
			}
		}
	})
	b.Run("three-pages", func(b *testing.B) {
		calls := []CallResult{
			pagedCall(item, makeFacts("a"), &ResultPage{Cursor: "cursor-1", More: true, TotalFacts: 30}),
			pagedCall(item, makeFacts("b"), &ResultPage{Cursor: "cursor-2", More: true, PageIndex: 1, TotalFacts: 30}),
			pagedCall(item, makeFacts("c"), &ResultPage{PageIndex: 2, TotalFacts: 30}),
		}
		script := &scriptRunner{calls: calls}
		source := mustNewPagedSource(b, script, 0)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			script.requests = script.requests[:0]
			if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
				b.Fatalf("NextClaimed() error = %v, want nil", err)
			}
		}
	})
}

// TestLegacyRunnerPathStaysSingleCall proves backward compatibility: a Runner
// without paged support keeps the exact single-call path, and the first
// request carries no continuation envelope.
func TestLegacyRunnerPathStaysSingleCall(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	source := mustNewSource(t, runner, nil)
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%t error=%v, want true nil", ok, err)
	}
	if envelopes := collectFacts(t, collected); len(envelopes) != 1 {
		t.Fatalf("collected facts = %d, want 1", len(envelopes))
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.requests))
	}
	if runner.requests[0].Continuation != nil {
		t.Fatalf("legacy request continuation = %+v, want nil", runner.requests[0].Continuation)
	}
}

// TestCheckPageEnvelopeContinuationErrorOmitsRawState pins eshu-hq/eshu#9310
// for the sibling echo in the same validation path: checkPageEnvelope runs
// before SDK validation, so a hostile extension-controlled result state
// rejected on a continuation page must not reach the invalid_result error.
// The hostile input leads with out-of-alphabet bytes, so the exact-match
// assertion fails on a truncation-only mutant and pins the alphabet filter,
// not just the length.
func TestCheckPageEnvelopeContinuationErrorOmitsRawState(t *testing.T) {
	t.Parallel()

	hostile := sdkcollector.ResultState("!!" + strings.Repeat("z", 200))
	call := CallResult{
		Result: sdkcollector.Result{
			State:           hostile,
			ProtocolVersion: collectorSDKProtocolV1Alpha3,
		},
		Page: &ResultPage{PageIndex: 0, More: true, Cursor: "resume-here"},
	}
	err := (&Source{}).checkPageEnvelope(call, 0, false)
	if err == nil {
		t.Fatal("checkPageEnvelope(hostile state) error = nil, want rejection")
	}
	// The filter drops both '!' bytes and keeps the next 16 alphabet bytes;
	// a truncation-only mutant would emit "!!zzzzzzzzzzzzzz".
	want := `collector extension failure: invalid_result: extension result must not request continuation (state 202 bytes, prefix "zzzzzzzzzzzzzzzz")`
	if err.Error() != want {
		t.Fatalf("checkPageEnvelope error = %q, want %q", err.Error(), want)
	}
}

// TestV1Alpha3ManifestAdmitted proves the version gate opens explicitly: a
// v1alpha3 manifest constructs the source, while any other protocol stays
// rejected.
func TestV1Alpha3ManifestAdmitted(t *testing.T) {
	t.Parallel()

	manifest := testManifestV1Alpha3()
	if err := manifest.ValidateWithGrants(nil); err != nil {
		t.Fatalf("ValidateWithGrants(v1alpha3) error = %v, want nil", err)
	}
	bogus := testManifest()
	bogus.Spec.Runtime.SDKProtocol = "collector-sdk/v9"
	if err := bogus.ValidateWithGrants(nil); err == nil {
		t.Fatal("ValidateWithGrants(v9) error = nil, want non-nil")
	}
}
