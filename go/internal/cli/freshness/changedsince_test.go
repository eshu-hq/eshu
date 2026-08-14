// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"bytes"
	"errors"
	"testing"
)

func TestChangedSincePathOmitsEmptySelectorsAndTrims(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts ChangedSinceOptions
		want string
	}{
		{name: "no selectors", opts: ChangedSinceOptions{}, want: ChangedSinceRoute},
		{
			name: "padded selectors are trimmed",
			opts: ChangedSinceOptions{Repository: "  acme/app  ", SinceGenerationID: " gen-prior "},
			want: ChangedSinceRoute + "?repository=acme%2Fapp&since_generation_id=gen-prior",
		},
		{
			name: "a zero sample limit leaves the server default in charge",
			opts: ChangedSinceOptions{ScopeID: "s", SampleLimit: 0},
			want: ChangedSinceRoute + "?scope_id=s",
		},
		{
			name: "observed-at is passed through unvalidated",
			opts: ChangedSinceOptions{ScopeID: "s", SinceObservedAt: "not-a-time", SampleLimit: 40},
			want: ChangedSinceRoute + "?sample_limit=40&scope_id=s&since_observed_at=not-a-time",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChangedSincePath(tc.opts); got != tc.want {
				t.Fatalf("ChangedSincePath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestServiceChangedSincePathOmitsEmptySelectorsAndTrims(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts ServiceChangedSinceOptions
		want string
	}{
		{name: "no selectors", opts: ServiceChangedSinceOptions{}, want: ServiceChangedSinceRoute},
		{
			name: "padded selectors are trimmed",
			opts: ServiceChangedSinceOptions{ServiceID: " svc-a ", SinceGenerationID: " gen-prior ", SampleLimit: 40},
			want: ServiceChangedSinceRoute + "?sample_limit=40&service_id=svc-a&since_generation_id=gen-prior",
		},
		{
			name: "a zero sample limit leaves the server default in charge",
			opts: ServiceChangedSinceOptions{ServiceID: "svc-a"},
			want: ServiceChangedSinceRoute + "?service_id=svc-a",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ServiceChangedSincePath(tc.opts); got != tc.want {
				t.Fatalf("ServiceChangedSincePath() = %q, want %q", got, tc.want)
			}
		})
	}
}

const changedSinceFixture = `{"data":{"scope_id":"scope-a","since_generation_id":"gen-prior",
	"current_active_generation_id":"gen-current","unavailable":false,"categories":[
	{"category":"files","unavailable":false,"counts":{"added":2,"updated":1,"unchanged":5,"retired":1,"superseded":1}},
	{"category":"symbols","unavailable":true},
	"not-an-object"
]},"truth":{"freshness":{"state":"fresh"}},"error":null}`

func TestRunChangedSinceRendersSummary(t *testing.T) {
	out := &bytes.Buffer{}
	client := &fakeFetcher{body: changedSinceFixture}
	if err := RunChangedSince(out, client, ChangedSinceOptions{ScopeID: "scope-a"}); err != nil {
		t.Fatalf("RunChangedSince() error = %v", err)
	}
	want := "Truth freshness: fresh\n" +
		"Changed since gen-prior -> gen-current (scope=scope-a)\n" +
		"  files            added=2 updated=1 unchanged=5 retired=1 superseded=1\n" +
		"  symbols          unavailable\n"
	if got := out.String(); got != want {
		t.Fatalf("summary =\n%q\nwant\n%q", got, want)
	}
	if client.gotPath != ChangedSinceRoute+"?scope_id=scope-a" {
		t.Fatalf("requested path = %q", client.gotPath)
	}
}

// TestRunChangedSinceUnavailableSaysSoInsteadOfZeroing proves a scope with no
// current active generation renders an explicit notice. Rendering five zeroed
// counts would read as "nothing changed", which is a different claim.
func TestRunChangedSinceUnavailableSaysSoInsteadOfZeroing(t *testing.T) {
	body := `{"data":{"scope_id":"scope-a","unavailable":true,"categories":[]},
		"truth":{"freshness":{"state":"unavailable"}},"error":null}`
	out := &bytes.Buffer{}
	if err := RunChangedSince(out, &fakeFetcher{body: body}, ChangedSinceOptions{}); err != nil {
		t.Fatalf("RunChangedSince() error = %v", err)
	}
	want := "Truth freshness: unavailable\n" +
		"Changed since (unknown) ->  (scope=scope-a)\n" +
		"  diff unavailable: scope has no current active generation\n"
	if got := out.String(); got != want {
		t.Fatalf("summary =\n%q\nwant\n%q", got, want)
	}
}

func TestChangedSinceBaselineLabelFallsBackThroughObservedAt(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]any
		want string
	}{
		{name: "nil data", data: nil, want: "(unknown)"},
		{name: "generation id wins", data: map[string]any{"since_generation_id": "gen-a", "since_observed_at": "t"}, want: "gen-a"},
		{name: "observed-at is the fallback", data: map[string]any{"since_observed_at": "2026-01-02T03:04:05Z"}, want: "2026-01-02T03:04:05Z"},
		{name: "blank generation id falls through", data: map[string]any{"since_generation_id": "  ", "since_observed_at": "t"}, want: "t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChangedSinceBaselineLabel(tc.data); got != tc.want {
				t.Fatalf("ChangedSinceBaselineLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunServiceChangedSinceRendersSummary(t *testing.T) {
	body := `{"data":{"service_id":"svc-a","since_generation_id":"gen-prior",
		"current_active_generation_id":"gen-current","unavailable":false,"categories":[
		{"category":"evidence","unavailable":false,"counts":{"added":4,"updated":0,"unchanged":9,"retired":2,"superseded":3}}
	]},"truth":{"freshness":{"state":"stale"}},"error":null}`
	out := &bytes.Buffer{}
	if err := RunServiceChangedSince(out, &fakeFetcher{body: body}, ServiceChangedSinceOptions{ServiceID: "svc-a"}); err != nil {
		t.Fatalf("RunServiceChangedSince() error = %v", err)
	}
	want := "Truth freshness: stale\n" +
		"Service changed since gen-prior -> gen-current (service=svc-a)\n" +
		"  evidence         added=4 updated=0 unchanged=9 retired=2 superseded=3\n"
	if got := out.String(); got != want {
		t.Fatalf("summary =\n%q\nwant\n%q", got, want)
	}
}

// TestRunServiceChangedSinceHasNoObservedAtFallback pins the one rendering
// difference between the two changed-since summaries: the service route takes
// a generation id only, so an absent baseline renders as an empty field rather
// than the scope summary's "(unknown)".
func TestRunServiceChangedSinceHasNoObservedAtFallback(t *testing.T) {
	body := `{"data":{"service_id":"svc-a","unavailable":true,"categories":[]},"truth":{},"error":null}`
	out := &bytes.Buffer{}
	if err := RunServiceChangedSince(out, &fakeFetcher{body: body}, ServiceChangedSinceOptions{}); err != nil {
		t.Fatalf("RunServiceChangedSince() error = %v", err)
	}
	want := "Service changed since  ->  (service=svc-a)\n" +
		"  diff unavailable: service has no current active generation\n"
	if got := out.String(); got != want {
		t.Fatalf("summary =\n%q\nwant\n%q", got, want)
	}
}

func TestChangedSinceRunsShareTheExitCodeContract(t *testing.T) {
	body := `{"data":null,"truth":null,"error":{"code":"unsupported_capability","message":"capability off"}}`
	for _, tc := range []struct {
		name string
		run  func(*bytes.Buffer) error
	}{
		{name: "changed-since", run: func(w *bytes.Buffer) error {
			return RunChangedSince(w, &fakeFetcher{body: body}, ChangedSinceOptions{})
		}},
		{name: "service-changed-since", run: func(w *bytes.Buffer) error {
			return RunServiceChangedSince(w, &fakeFetcher{body: body}, ServiceChangedSinceOptions{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			err := tc.run(out)
			var failure *Failure
			if !errors.As(err, &failure) {
				t.Fatalf("error = %T (%v), want *Failure", err, err)
			}
			if failure.Code != 6 {
				t.Fatalf("exit code = %d, want 6", failure.Code)
			}
			want := "Generation lifecycle error (unsupported_capability): capability off\n"
			if got := out.String(); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}
