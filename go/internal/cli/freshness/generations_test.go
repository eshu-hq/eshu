// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeFetcher records the path it was asked for and replays a canned envelope
// or error, so the request-building and rendering halves can be tested without
// an HTTP server.
type fakeFetcher struct {
	gotPath string
	body    string
	err     error
}

func (f *fakeFetcher) GetEnvelope(path string, result any) error {
	f.gotPath = path
	if f.err != nil {
		return f.err
	}
	if f.body == "" {
		return nil
	}
	return json.Unmarshal([]byte(f.body), result)
}

func TestGenerationsPathOmitsEmptySelectorsAndTrims(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts GenerationsOptions
		want string
	}{
		{name: "no selectors", opts: GenerationsOptions{}, want: GenerationsRoute},
		{
			name: "whitespace-only selectors are dropped",
			opts: GenerationsOptions{ScopeID: "   ", Repository: "\t\n"},
			want: GenerationsRoute,
		},
		{
			name: "padded selectors are trimmed",
			opts: GenerationsOptions{ScopeID: "  git-repository-scope:acme/app  ", Limit: 25},
			want: GenerationsRoute + "?limit=25&scope_id=git-repository-scope%3Aacme%2Fapp",
		},
		{
			name: "a zero limit leaves the server default in charge",
			opts: GenerationsOptions{Status: "active", Limit: 0},
			want: GenerationsRoute + "?status=active",
		},
		{
			name: "a negative limit is also dropped",
			opts: GenerationsOptions{Status: "active", Limit: -3},
			want: GenerationsRoute + "?status=active",
		},
		{
			name: "every selector",
			opts: GenerationsOptions{
				ScopeID: "s", Repository: "r", CollectorKind: "git",
				SourceSystem: "github", GenerationID: "g", Status: "active", Limit: 7,
			},
			want: GenerationsRoute + "?collector_kind=git&generation_id=g&limit=7&repository=r&scope_id=s&source_system=github&status=active",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := GenerationsPath(tc.opts); got != tc.want {
				t.Fatalf("GenerationsPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFetchGenerationsDecodesEnvelopeAndUsesTheBuiltPath(t *testing.T) {
	client := &fakeFetcher{body: `{"data":{"count":1},"truth":{"freshness":{"state":"fresh"}},"error":null}`}
	env, err := FetchGenerations(client, GenerationsOptions{ScopeID: "s", Limit: 5})
	if err != nil {
		t.Fatalf("FetchGenerations() error = %v", err)
	}
	if client.gotPath != GenerationsRoute+"?limit=5&scope_id=s" {
		t.Fatalf("requested path = %q", client.gotPath)
	}
	if got := intValue(env.Data, "count"); got != 1 {
		t.Fatalf("data.count = %d, want 1", got)
	}
	if got := truthFreshnessState(env); got != "fresh" {
		t.Fatalf("truth freshness = %q, want fresh", got)
	}
}

func TestFetchGenerationsReturnsZeroEnvelopeOnTransportError(t *testing.T) {
	client := &fakeFetcher{err: errors.New("request failed: boom")}
	env, err := FetchGenerations(client, GenerationsOptions{})
	if err == nil {
		t.Fatal("FetchGenerations() error = nil, want the transport error")
	}
	if env.Data != nil || env.Truth != nil || env.Error != nil {
		t.Fatalf("envelope = %#v, want the zero value on a transport error", env)
	}
}

const generationsFixture = `{"data":{"count":2,"truncated":true,"generations":[
	{"generation_id":"gen-new","status":"active","scope_id":"scope-a","trigger_kind":"push","is_active":true,
	 "queue_status":{"outstanding":1,"failed":2,"dead_letter":3},"latest_failure":{"failure_class":"transient"}},
	{"generation_id":"gen-old","status":"superseded","scope_id":"scope-a","trigger_kind":"webhook","is_active":false},
	"not-an-object"
]},"truth":{"freshness":{"state":"fresh"}},"error":null}`

func TestRunGenerationsRendersSummary(t *testing.T) {
	out := &bytes.Buffer{}
	if err := RunGenerations(out, &fakeFetcher{body: generationsFixture}, GenerationsOptions{}); err != nil {
		t.Fatalf("RunGenerations() error = %v", err)
	}
	want := "Truth freshness: fresh\n" +
		"Generations: 2 (truncated=true)\n" +
		"* gen-new status=active scope=scope-a trigger=push queue[outstanding=1 failed=2 dead_letter=3] failure=transient\n" +
		"  gen-old status=superseded scope=scope-a trigger=webhook\n"
	if got := out.String(); got != want {
		t.Fatalf("summary =\n%q\nwant\n%q", got, want)
	}
}

// TestRunGenerationsExitsZeroWhileTheIndexIsBuilding pins the behaviour that
// separates this family from its siblings. `eshu trace service`, `eshu change
// impact`, and `eshu map` all exit 4 when truth.freshness.state is "building"
// or "stale". The freshness commands report the state and exit 0, because
// reporting a still-building index is the whole point of the command. A later
// change that "harmonizes" the families would fail here.
func TestRunGenerationsExitsZeroWhileTheIndexIsBuilding(t *testing.T) {
	for _, state := range []string{"building", "stale", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			body := `{"data":{"count":0},"truth":{"freshness":{"state":"` + state + `"}},"error":null}`
			out := &bytes.Buffer{}
			err := RunGenerations(out, &fakeFetcher{body: body}, GenerationsOptions{})
			if err != nil {
				t.Fatalf("RunGenerations() error = %v, want nil (a non-fresh state is reported, not an exit code)", err)
			}
			if !strings.Contains(out.String(), "Truth freshness: "+state) {
				t.Fatalf("summary %q did not report the %s state", out.String(), state)
			}
		})
	}
}

func TestRunGenerationsReturnsFailureForAnEnvelopeError(t *testing.T) {
	out := &bytes.Buffer{}
	body := `{"data":null,"truth":null,"error":{"code":"index_building","message":"index is still building"}}`
	err := RunGenerations(out, &fakeFetcher{body: body}, GenerationsOptions{})

	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("RunGenerations() error = %T (%v), want *Failure", err, err)
	}
	if failure.Code != 4 {
		t.Fatalf("exit code = %d, want 4", failure.Code)
	}
	want := "Generation lifecycle error (index_building): index is still building\n"
	if got := out.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRunGenerationsReportsATransportFailureAsAnEnvelope(t *testing.T) {
	out := &bytes.Buffer{}
	client := &fakeFetcher{err: errors.New(`request failed: Get "http://127.0.0.1:1/x": connection refused`)}
	err := RunGenerations(out, client, GenerationsOptions{})

	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("RunGenerations() error = %T (%v), want *Failure", err, err)
	}
	if failure.Code != 1 {
		t.Fatalf("exit code = %d, want 1", failure.Code)
	}
	if !strings.HasPrefix(out.String(), "Generation lifecycle error (backend_unavailable): ") {
		t.Fatalf("output = %q", out.String())
	}
}

// TestRunGenerationsJSONWritesTheEnvelopeAndStillFails proves --json does not
// swallow the exit code: the envelope is written and the failure still comes
// back for the wrapper to convert.
func TestRunGenerationsJSONWritesTheEnvelopeAndStillFails(t *testing.T) {
	out := &bytes.Buffer{}
	body := `{"data":null,"truth":null,"error":{"code":"scope_not_found","message":"no records"}}`
	err := RunGenerations(out, &fakeFetcher{body: body}, GenerationsOptions{JSON: true})

	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("RunGenerations() error = %T, want *Failure", err)
	}
	if failure.Code != 2 {
		t.Fatalf("exit code = %d, want 2", failure.Code)
	}
	var round Envelope
	if jsonErr := json.Unmarshal(out.Bytes(), &round); jsonErr != nil {
		t.Fatalf("--json output is not valid JSON: %v (%q)", jsonErr, out.String())
	}
	if round.Error == nil || round.Error.Code != "scope_not_found" {
		t.Fatalf("round-tripped envelope error = %#v", round.Error)
	}
	if strings.Contains(out.String(), "Generation lifecycle error") {
		t.Fatalf("--json output leaked the human error line: %q", out.String())
	}
}

func TestRunGenerationsJSONSucceedsWithoutTheHumanSummary(t *testing.T) {
	out := &bytes.Buffer{}
	if err := RunGenerations(out, &fakeFetcher{body: generationsFixture}, GenerationsOptions{JSON: true}); err != nil {
		t.Fatalf("RunGenerations() error = %v", err)
	}
	if strings.Contains(out.String(), "Truth freshness:") {
		t.Fatalf("--json output leaked the human summary: %q", out.String())
	}
	if !strings.Contains(out.String(), `"gen-new"`) {
		t.Fatalf("--json output lost the generation rows: %q", out.String())
	}
}

// failingWriter fails every write, so the renderers' write-error paths run.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestRunGenerationsPropagatesAWriteError(t *testing.T) {
	err := RunGenerations(failingWriter{}, &fakeFetcher{body: generationsFixture}, GenerationsOptions{})
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("RunGenerations() error = %v, want the unwrapped write error", err)
	}
	jsonErr := RunGenerations(failingWriter{}, &fakeFetcher{body: generationsFixture}, GenerationsOptions{JSON: true})
	if jsonErr == nil || !strings.Contains(jsonErr.Error(), "disk full") {
		t.Fatalf("RunGenerations(--json) error = %v, want the write error", jsonErr)
	}
}
