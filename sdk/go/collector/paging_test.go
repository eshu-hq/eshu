// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestResultPageValidationV1Alpha3 pins the versioned pagination contract
// (eshu-hq/eshu#9385): page fields are admitted only on
// collector-sdk/v1alpha3, a continuation page must carry a cursor, and a
// final page must not.
func TestResultPageValidationV1Alpha3(t *testing.T) {
	t.Parallel()

	contract := testContract()
	contract.ProtocolVersion = ProtocolVersionV1Alpha3
	validator := NewValidator(contract)

	base := Result{
		ProtocolVersion: ProtocolVersionV1Alpha3,
		State:           ResultComplete,
		Claim:           testClaim(),
		Generation: Generation{
			ID:         "gen-20260609",
			ObservedAt: time.Date(2026, time.July, 8, 12, 0, 0, 0, time.UTC),
		},
		Facts: []Fact{testPageFact()},
		Statuses: []Status{{
			Class:     StatusComplete,
			FactCount: 1,
		}},
	}

	for _, tc := range []struct {
		name    string
		mutate  func(*Result)
		wantErr string
	}{
		{
			name:   "absent page stays a valid single-shot result",
			mutate: func(result *Result) {},
		},
		{
			name: "final page with empty cursor",
			mutate: func(result *Result) {
				result.Page = &ResultPage{PageIndex: 2}
			},
		},
		{
			name: "continuation page with cursor",
			mutate: func(result *Result) {
				result.Page = &ResultPage{Cursor: "cursor-1", More: true, TotalFacts: 3}
			},
		},
		{
			name: "continuation without cursor fails",
			mutate: func(result *Result) {
				result.Page = &ResultPage{More: true}
			},
			wantErr: "cursor",
		},
		{
			name: "final page with cursor fails",
			mutate: func(result *Result) {
				result.Page = &ResultPage{Cursor: "cursor-1"}
			},
			wantErr: "cursor",
		},
		{
			name: "negative page index fails",
			mutate: func(result *Result) {
				result.Page = &ResultPage{PageIndex: -1}
			},
			wantErr: "page_index",
		},
		{
			name: "negative total facts fails",
			mutate: func(result *Result) {
				result.Page = &ResultPage{TotalFacts: -1}
			},
			wantErr: "total_facts",
		},
		{
			name: "retryable continuation fails",
			mutate: func(result *Result) {
				result.State = ResultRetryable
				result.Facts = nil
				result.Statuses = []Status{{
					Class:             StatusFailure,
					FailureClass:      "source_unavailable",
					RetryAfterSeconds: 30,
				}}
				result.Page = &ResultPage{Cursor: "cursor-1", More: true}
			},
			wantErr: "continuation",
		},
		{
			name: "terminal continuation fails",
			mutate: func(result *Result) {
				result.State = ResultTerminal
				result.Facts = nil
				result.Statuses = []Status{{
					Class:        StatusFailure,
					FailureClass: "source_unavailable",
				}}
				result.Page = &ResultPage{Cursor: "cursor-1", More: true}
			},
			wantErr: "continuation",
		},
		{
			name: "unchanged continuation fails",
			mutate: func(result *Result) {
				result.State = ResultUnchanged
				result.Facts = nil
				result.Statuses = nil
				result.Page = &ResultPage{Cursor: "cursor-1", More: true}
			},
			wantErr: "continuation",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := base
			tc.mutate(&result)
			_, err := validator.ValidateResult(result)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateResult() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateResult() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateResult() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

// TestResultPageVersionGate proves a page on the older wire contracts is a
// version violation, and that a legacy single-shot result stays valid when
// validated as the newest protocol version (backward compatibility).
func TestResultPageVersionGate(t *testing.T) {
	t.Parallel()

	oldest := NewValidator(testContract())
	paged := Result{
		ProtocolVersion: ProtocolVersionV1Alpha1,
		State:           ResultComplete,
		Claim:           testClaim(),
		Generation: Generation{
			ID:         "gen-20260609",
			ObservedAt: time.Date(2026, time.July, 8, 12, 0, 0, 0, time.UTC),
		},
		Facts:    []Fact{testPageFact()},
		Statuses: []Status{{Class: StatusComplete, FactCount: 1}},
		Page:     &ResultPage{Cursor: "cursor-1", More: true},
	}

	if _, err := oldest.ValidateResult(paged); err == nil {
		t.Fatal("ValidateResult(v1alpha1 with page) error = nil, want version error")
	} else if !strings.Contains(err.Error(), "page") {
		t.Fatalf("ValidateResult(v1alpha1 with page) error = %v, want page version error", err)
	}

	legacy := readFixture(t, "complete")
	newestContract := testContract()
	newestContract.ProtocolVersion = ProtocolVersionV1Alpha3
	legacy.ProtocolVersion = ProtocolVersionV1Alpha3
	if _, err := NewValidator(newestContract).ValidateResult(legacy); err != nil {
		t.Fatalf("ValidateResult(legacy single-shot as v1alpha3) error = %v, want nil", err)
	}
}

// TestOldestDecoderRejectsPagedResult proves the wire-level version story:
// a strict oldest-shape decoder (no page field) rejects a paged v1alpha3
// result as unknown, so old hosts fail closed instead of silently dropping
// the continuation.
func TestOldestDecoderRejectsPagedResult(t *testing.T) {
	t.Parallel()

	raw := `{"protocol_version":"collector-sdk/v1alpha3","state":"complete",` +
		`"claim":{"component_id":"c","instance_id":"i","collector_kind":"k",` +
		`"source_system":"s","scope":{"id":"scope-1","kind":"repository"},` +
		`"source_run_id":"run-1","generation_id":"gen-1","work_item_id":"work-1",` +
		`"fencing_token":"7","attempt":1,"deadline":"2026-07-08T12:00:00Z",` +
		`"config_handle":"cfg"},` +
		`"generation":{"id":"gen-1","observed_at":"2026-07-08T12:00:00Z"},` +
		`"page":{"cursor":"cursor-1","more":true}}`

	var legacy struct {
		ProtocolVersion string `json:"protocol_version"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err == nil {
		t.Fatal("oldest-shape decode error = nil, want unknown field rejection")
	}
}

// TestSplitFactsRespectsByteBound proves analyzer-side byte accounting: a
// hostile fact set splits into pages that each fit the per-call bound, in
// order, with no fact lost or duplicated, and an explicit error (never a
// silent drop) when one fact alone exceeds the bound.
func TestSplitFactsRespectsByteBound(t *testing.T) {
	t.Parallel()

	facts := make([]Fact, 0, 10)
	for i := 0; i < 10; i++ {
		fact := testPageFact()
		fact.StableKey = strings.Repeat("k", 24) + string(rune('a'+i))
		fact.SourceRef.FactKey = fact.StableKey
		fact.Payload = map[string]any{"blob": strings.Repeat("x", 400)}
		facts = append(facts, fact)
	}
	single, err := FactWireBytes(facts[0])
	if err != nil {
		t.Fatalf("FactWireBytes() error = %v, want nil", err)
	}
	pages, err := SplitFacts(facts, int64(single)*3)
	if err != nil {
		t.Fatalf("SplitFacts() error = %v, want nil", err)
	}
	if len(pages) == 0 {
		t.Fatal("SplitFacts() pages = 0, want at least 1")
	}
	got := 0
	for index, page := range pages {
		if len(page) == 0 {
			t.Fatalf("SplitFacts() pages[%d] is empty", index)
		}
		var size int64
		for _, fact := range page {
			factSize, err := FactWireBytes(fact)
			if err != nil {
				t.Fatalf("FactWireBytes() error = %v, want nil", err)
			}
			size += int64(factSize)
		}
		if size > int64(single)*3 {
			t.Fatalf("SplitFacts() pages[%d] bytes = %d, want <= %d", index, size, single*3)
		}
		got += len(page)
	}
	if got != len(facts) {
		t.Fatalf("SplitFacts() kept %d facts, want %d", got, len(facts))
	}
	var flat []Fact
	for _, page := range pages {
		flat = append(flat, page...)
	}
	for i, fact := range facts {
		if flat[i].StableKey != fact.StableKey {
			t.Fatalf("SplitFacts() order changed at position %d", i)
		}
	}

	oversized := testPageFact()
	oversized.Payload = map[string]any{"blob": strings.Repeat("x", 4096)}
	if _, err := SplitFacts([]Fact{oversized}, 128); err == nil {
		t.Fatal("SplitFacts(oversized) error = nil, want explicit bound error")
	} else if !strings.Contains(err.Error(), oversized.StableKey) {
		t.Fatalf("SplitFacts(oversized) error = %v, want stable key named", err)
	}

	if _, err := SplitFacts(nil, 1024); err == nil {
		t.Fatal("SplitFacts(nil) error = nil, want non-nil")
	}
	if _, err := SplitFacts(facts, 0); err == nil {
		t.Fatal("SplitFacts(non-positive bound) error = nil, want non-nil")
	}
}

// TestPagedFixturesConform proves the checked-in v1alpha3 page fixtures are
// valid paged results: the continuation page and the final page each pass
// validation, and together they name the same generation and total.
func TestPagedFixturesConform(t *testing.T) {
	t.Parallel()

	contract := testContract()
	contract.ProtocolVersion = ProtocolVersionV1Alpha3
	validator := NewValidator(contract)
	first := readPagedFixture(t, "paged_first")
	final := readPagedFixture(t, "paged_final")
	if _, err := validator.ValidateResult(first); err != nil {
		t.Fatalf("ValidateResult(paged_first) error = %v, want nil", err)
	}
	if _, err := validator.ValidateResult(final); err != nil {
		t.Fatalf("ValidateResult(paged_final) error = %v, want nil", err)
	}
	if first.Page == nil || !first.Page.More {
		t.Fatal("paged_first must request continuation")
	}
	if final.Page == nil || final.Page.More {
		t.Fatal("paged_final must close the generation")
	}
	if first.Generation.ID != final.Generation.ID {
		t.Fatalf("generation drift %q vs %q", first.Generation.ID, final.Generation.ID)
	}
	if first.Page.TotalFacts != final.Page.TotalFacts {
		t.Fatalf("total_facts drift %d vs %d", first.Page.TotalFacts, final.Page.TotalFacts)
	}
	if final.Page.PageIndex != first.Page.PageIndex+1 {
		t.Fatalf("page_index gap %d vs %d", first.Page.PageIndex, final.Page.PageIndex)
	}
}

func readPagedFixture(t *testing.T, name string) Result {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "fixtures", name+".json"))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v, want nil", name, err)
	}
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v, want nil", name, err)
	}
	return result
}

// TestV1Alpha3SchemaMatchesGolden pins the v1alpha3 JSON Schema artifact to
// its generator, mirroring the v1alpha1 golden test.
func TestV1Alpha3SchemaMatchesGolden(t *testing.T) {
	t.Parallel()

	got, err := JSONSchemaV1Alpha3()
	if err != nil {
		t.Fatalf("JSONSchemaV1Alpha3() error = %v, want nil", err)
	}
	want, err := os.ReadFile(filepath.Join("schema", "collector-sdk-v1alpha3.schema.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(schema) error = %v, want nil", err)
	}
	if !jsonEqual(got, want) {
		t.Fatalf("JSONSchemaV1Alpha3() did not match golden schema\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

// TestPageBoundForStdout pins the explicit per-call page bound derived from
// the host stdout limit: the bound is the limit minus fixed envelope
// headroom, and a non-positive limit fails closed.
func TestPageBoundForStdout(t *testing.T) {
	t.Parallel()

	const stdoutLimit = int64(4 * 1024 * 1024)
	bound, err := PageBoundForStdout(stdoutLimit)
	if err != nil {
		t.Fatalf("PageBoundForStdout() error = %v, want nil", err)
	}
	if bound <= 0 || bound >= stdoutLimit {
		t.Fatalf("PageBoundForStdout() = %d, want 0 < bound < %d", bound, stdoutLimit)
	}
	if _, err := PageBoundForStdout(0); err == nil {
		t.Fatal("PageBoundForStdout(0) error = nil, want non-nil")
	}
}

// TestSplitFactsBoundsRetainedMemory proves the bounded-emission memory
// contract at first-scan scale (eshu-hq/eshu#9635: a first full scan emitting
// 49,047 facts / ~72MB against a 4MiB page bound): every page's retained backing array
// stays proportional to the page it carries, never to the whole input. A
// splitter that sizes each page for all N facts retains N*pages fact slots
// across the stream, which can OOM the worker the paging contract exists to
// protect. The paging behavior (order, completeness, per-page bound, explicit
// oversize error) is pinned alongside the bound so the fix cannot trade
// correctness for retention.
func TestSplitFactsBoundsRetainedMemory(t *testing.T) {
	t.Parallel()

	const factCount = 49047
	facts := make([]Fact, 0, factCount)
	for i := 0; i < factCount; i++ {
		fact := testPageFact()
		fact.StableKey = "snapshot:github.com/eshu-hq/eshu:" + strconv.Itoa(i)
		fact.SourceRef.FactKey = fact.StableKey
		fact.Payload = map[string]any{"score": 8.7, "blob": strings.Repeat("x", 1024)}
		facts = append(facts, fact)
	}
	bound, err := PageBoundForStdout(4 * 1024 * 1024)
	if err != nil {
		t.Fatalf("PageBoundForStdout() error = %v, want nil", err)
	}
	pages, err := SplitFacts(facts, bound)
	if err != nil {
		t.Fatalf("SplitFacts() error = %v, want nil", err)
	}

	kept := 0
	retained := 0
	for index, page := range pages {
		if len(page) == 0 {
			t.Fatalf("SplitFacts() pages[%d] is empty", index)
		}
		var size int64
		for _, fact := range page {
			factSize, err := FactWireBytes(fact)
			if err != nil {
				t.Fatalf("FactWireBytes() error = %v, want nil", err)
			}
			size += int64(factSize)
		}
		if size > bound {
			t.Fatalf("SplitFacts() pages[%d] bytes = %d, want <= %d", index, size, bound)
		}
		kept += len(page)
		retained += cap(page)
	}
	if kept != len(facts) {
		t.Fatalf("SplitFacts() kept %d facts, want %d", kept, len(facts))
	}
	var flat []Fact
	for _, page := range pages {
		flat = append(flat, page...)
	}
	for i, fact := range facts {
		if flat[i].StableKey != fact.StableKey {
			t.Fatalf("SplitFacts() order changed at position %d", i)
		}
	}
	// Retention stays proportional to the facts carried (append growth keeps
	// each page within ~2x its length), never to the input size times the
	// page count.
	if ceiling := 2*len(facts) + len(pages); retained > ceiling {
		t.Fatalf("SplitFacts() retained %d fact slots, want <= %d (2x facts + pages)", retained, ceiling)
	}
}

func testClaim() Claim {
	return Claim{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    "scorecard-prod",
		CollectorKind: "scorecard",
		SourceSystem:  "openssf_scorecard",
		Scope:         Scope{ID: "repo:github.com/eshu-hq/eshu", Kind: "repository"},
		SourceRunID:   "run-20260609",
		GenerationID:  "gen-20260609",
		WorkItemID:    "work-1",
		FencingToken:  "fence-1",
		Attempt:       1,
		Deadline:      time.Date(2026, time.July, 8, 12, 0, 0, 0, time.UTC),
		ConfigHandle:  "secretref:scorecard-prod",
	}
}

func testPageFact() Fact {
	return Fact{
		Kind:             "dev.eshu.examples.scorecard.snapshot",
		SchemaVersion:    "1.0.0",
		StableKey:        "snapshot:github.com/eshu-hq/eshu",
		SourceConfidence: SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.July, 8, 12, 0, 0, 0, time.UTC),
		SourceRef: SourceRef{
			SourceSystem: "openssf_scorecard",
			ScopeID:      "repo:github.com/eshu-hq/eshu",
			GenerationID: "gen-20260609",
			FactKey:      "snapshot:github.com/eshu-hq/eshu",
			URI:          "https://api.securityscorecards.dev/projects/github.com/eshu-hq/eshu",
			RecordID:     "github.com/eshu-hq/eshu",
		},
		Payload: map[string]any{"score": 8.7},
	}
}
