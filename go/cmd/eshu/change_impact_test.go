// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/change"
)

func stubChangeImpactFetch(t *testing.T, envelope change.Envelope, err error) func() {
	t.Helper()
	original := changeImpactFetch
	changeImpactFetch = func(_ *APIClient, _ change.Options) (change.Envelope, error) {
		return envelope, err
	}
	return func() { changeImpactFetch = original }
}

func stubChangePlanFetch(t *testing.T, envelope change.Envelope, err error) func() {
	t.Helper()
	original := changePlanFetch
	changePlanFetch = func(_ *APIClient, _ change.Options) (change.Envelope, error) {
		return envelope, err
	}
	return func() { changePlanFetch = original }
}

func TestChangeImpactCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"change", "impact"})
	if err != nil {
		t.Fatalf("rootCmd.Find(change impact) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "impact" {
		t.Fatalf("resolved command = %#v, want impact", cmd)
	}
	for _, name := range []string{"json", "repo-id", "base", "head", "file", "repo-path", "topic", "service-name", "limit", "max-depth", "service-url"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("change impact flag %q missing", name)
		}
	}
}

func TestChangePlanCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"change", "plan"})
	if err != nil {
		t.Fatalf("rootCmd.Find(change plan) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "plan" {
		t.Fatalf("resolved command = %#v, want plan", cmd)
	}
	for _, name := range []string{"json", "repo-id", "file", "intent", "limit", "max-depth", "service-url"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("change plan flag %q missing", name)
		}
	}
}

func TestFetchChangeImpactRequestsCanonicalEnvelope(t *testing.T) {
	var gotAccept string
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.EscapedPath()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"workflow":"pre_change_impact"},"truth":{"freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	if _, err := fetchChangeImpact(client, change.Options{
		RepoID:       "repo-1",
		BaseRef:      "main",
		HeadRef:      "feature/pre-change",
		ChangedPaths: []string{"go/internal/query/prechange_impact.go"},
		Changes: []change.FileChange{{
			Path:    "go/internal/query/prechange_impact.go",
			OldPath: "go/internal/query/change_impact.go",
			Status:  "renamed",
		}},
		Topic:    "pre-change workflow",
		MaxDepth: 3,
		Limit:    25,
	}); err != nil {
		t.Fatalf("fetchChangeImpact() error = %v", err)
	}
	if gotAccept != eshuEnvelopeMIMEType {
		t.Fatalf("Accept = %q, want %q", gotAccept, eshuEnvelopeMIMEType)
	}
	if gotPath != "/api/v0/impact/pre-change" {
		t.Fatalf("path = %q", gotPath)
	}
	for key, want := range map[string]any{
		"repo_id":   "repo-1",
		"base_ref":  "main",
		"head_ref":  "feature/pre-change",
		"topic":     "pre-change workflow",
		"max_depth": float64(3),
		"limit":     float64(25),
	} {
		if got := gotBody[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
	changes := gotBody["changes"].([]any)
	first := changes[0].(map[string]any)
	if got, want := first["status"], "renamed"; got != want {
		t.Fatalf("changes[0].status = %#v, want %#v", got, want)
	}
}

func TestFetchChangePlanRequestsDeveloperPlanRoute(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"schema_version":"developer_change_plan.v1"},"truth":{"freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	if _, err := fetchChangePlan(client, change.Options{
		RepoID:          "repo-1",
		DeveloperIntent: "rename helper safely",
		Changes: []change.FileChange{{
			Path:    "go/internal/query/developer_change_plan.go",
			OldPath: "go/internal/query/prechange_impact.go",
			Status:  "renamed",
		}},
		Limit: 10,
	}); err != nil {
		t.Fatalf("fetchChangePlan() error = %v", err)
	}
	if gotPath != "/api/v0/impact/developer-change-plan" {
		t.Fatalf("path = %q", gotPath)
	}
	for key, want := range map[string]any{
		"repo_id":          "repo-1",
		"developer_intent": "rename helper safely",
		"limit":            float64(10),
	} {
		if got := gotBody[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestRunChangeImpactRendersSummary(t *testing.T) {
	reset := stubChangeImpactFetch(t, change.Envelope{
		Data: map[string]any{
			"changed_file_count": float64(2),
			"truncated":          false,
			"code_surface": map[string]any{
				"symbol_count": float64(3),
			},
			"impact_summary": map[string]any{
				"direct_count":     float64(1),
				"transitive_count": float64(2),
			},
			"missing_evidence": []any{},
			"coverage": map[string]any{
				"state": "supported",
			},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}, nil)
	defer reset()

	out := &bytes.Buffer{}
	cmd := newChangeImpactCommand()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--repo-id", "repo-1", "--file", "go/a.go"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("change impact command error = %v", err)
	}
	output := out.String()
	for _, want := range []string{"Truth freshness: fresh", "Pre-change impact: 2 changed files", "symbols=3 direct=1 transitive=2"} {
		if !strings.Contains(output, want) {
			t.Fatalf("summary missing %q: %q", want, output)
		}
	}
}

func TestRunChangePlanRendersSummary(t *testing.T) {
	reset := stubChangePlanFetch(t, change.Envelope{
		Data: map[string]any{
			"changed_file_count": float64(1),
			"blocked":            true,
			"truncated":          false,
			"actions": []any{map[string]any{
				"kind":  "rename_safety_check",
				"risk":  "high",
				"title": "Verify old and new path evidence before refactor guidance",
			}},
			"bounded_next_calls": []any{map[string]any{
				"kind":   "api",
				"target": "POST /api/v0/impact/pre-change",
			}},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}, nil)
	defer reset()

	out := &bytes.Buffer{}
	cmd := newChangePlanCommand()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--repo-id", "repo-1", "--file", "go/a.go", "--intent", "rename helper safely"})

	var exitErr commandExitError
	if err := cmd.Execute(); !errors.As(err, &exitErr) || exitErr.code != 5 {
		t.Fatalf("change plan command error = %#v, want commandExitError code 5", err)
	}
	output := out.String()
	for _, want := range []string{"Developer change plan: 1 actions", "blocked=true", "action=rename_safety_check", "next=api"} {
		if !strings.Contains(output, want) {
			t.Fatalf("summary missing %q: %q", want, output)
		}
	}
}

func TestRunChangeImpactRendersPartialSummaryBeforeFailClosed(t *testing.T) {
	reset := stubChangeImpactFetch(t, change.Envelope{
		Data: map[string]any{
			"changed_file_count": float64(1),
			"truncated":          true,
			"code_surface": map[string]any{
				"symbol_count": float64(1),
			},
			"impact_summary": map[string]any{
				"direct_count":     float64(0),
				"transitive_count": float64(0),
			},
			"missing_evidence": []any{map[string]any{"reason": "changed_path_no_symbol_evidence"}},
			"coverage": map[string]any{
				"state": "partial",
			},
			"answer_packet": map[string]any{
				"partial": true,
			},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}, nil)
	defer reset()

	out := &bytes.Buffer{}
	cmd := newChangeImpactCommand()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--repo-id", "repo-1", "--file", "go/a.go"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("change impact command error = nil, want fail-closed partial error")
	}
	output := out.String()
	for _, want := range []string{"Pre-change impact: 1 changed files", "coverage=partial truncated=true", "missing_evidence=1"} {
		if !strings.Contains(output, want) {
			t.Fatalf("partial summary missing %q: %q", want, output)
		}
	}
}

func TestPreChangeImpactDogfoodFixtureProvesWorkflowAdvantage(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/prechange_impact_dogfood.json")
	if err != nil {
		t.Fatalf("read dogfood fixture: %v", err)
	}
	var document struct {
		Fixture struct {
			Task struct {
				ExpectedAffectedEntityIDs []string `json:"expected_affected_entity_ids"`
			} `json:"task"`
			Baselines map[string]struct {
				Steps                    int      `json:"steps"`
				Tokens                   int      `json:"tokens"`
				AffectedEntityIDs        []string `json:"affected_entity_ids"`
				MissingEvidenceReasons   []string `json:"missing_evidence_reasons"`
				RecommendedNextCallCount int      `json:"recommended_next_call_count"`
			} `json:"baselines"`
		} `json:"fixture"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	fixture := document.Fixture
	rawRelationships := fixture.Baselines["raw_relationship_queries"]
	preChange := fixture.Baselines["pre_change_impact"]
	if preChange.Steps >= rawRelationships.Steps {
		t.Fatalf("pre-change steps = %d, raw steps = %d", preChange.Steps, rawRelationships.Steps)
	}
	if preChange.Tokens >= rawRelationships.Tokens {
		t.Fatalf("pre-change tokens = %d, raw tokens = %d", preChange.Tokens, rawRelationships.Tokens)
	}
	if len(preChange.MissingEvidenceReasons) == 0 || len(rawRelationships.MissingEvidenceReasons) != 0 {
		t.Fatalf("missing evidence reasons pre-change=%v raw=%v", preChange.MissingEvidenceReasons, rawRelationships.MissingEvidenceReasons)
	}
	if preChange.RecommendedNextCallCount == 0 {
		t.Fatal("pre-change dogfood baseline must include bounded next calls")
	}
	if strings.Join(preChange.AffectedEntityIDs, ",") != strings.Join(fixture.Task.ExpectedAffectedEntityIDs, ",") {
		t.Fatalf("pre-change affected ids = %v, want %v", preChange.AffectedEntityIDs, fixture.Task.ExpectedAffectedEntityIDs)
	}
}

// TestChangeExitCodeMapping pins the reason-to-exit-code table this wrapper
// owns, including the two rows where answering directly differs from routing
// through traceExitCode.
//
// The freshness and incomplete rows are the ones that matter. traceExitCode
// answers 1 for both "building" and "truncated"; the change family exits 4 and
// 5. A future simplification that fed change.Failure.Kind into traceExitCode
// would leave every other row identical and silently change these two, so they
// are asserted against traceExitCode explicitly as well as against the number.
func TestChangeExitCodeMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		failure change.Failure
		want    int
	}{
		{name: "invalid argument", failure: change.Failure{Kind: change.KindInvalidArgument}, want: 2},
		{name: "freshness", failure: change.Failure{Kind: change.KindFreshness}, want: 4},
		{name: "incomplete", failure: change.Failure{Kind: change.KindIncomplete}, want: 5},
		{name: "envelope not_found", failure: change.Failure{Kind: change.KindEnvelope, Code: "not_found"}, want: 2},
		{name: "envelope ambiguous", failure: change.Failure{Kind: change.KindEnvelope, Code: "ambiguous"}, want: 3},
		{name: "envelope stale", failure: change.Failure{Kind: change.KindEnvelope, Code: "stale"}, want: 4},
		{name: "envelope partial", failure: change.Failure{Kind: change.KindEnvelope, Code: "partial"}, want: 5},
		{name: "envelope unsupported_capability", failure: change.Failure{Kind: change.KindEnvelope, Code: "unsupported_capability"}, want: 6},
		{name: "envelope backend_unavailable", failure: change.Failure{Kind: change.KindEnvelope, Code: "backend_unavailable"}, want: 1},
		{name: "unknown kind", failure: change.Failure{Kind: change.FailureKind("something_new")}, want: 1},
	}

	// Every kind change.Kinds() declares needs a row here, and at least one of
	// its rows must expect something other than what an unrecognised kind gets.
	// Without this the table guards only the kinds someone remembered to type:
	// a new kind added without its changeExitCode arm falls to the default,
	// exits 1, and reads as correct. The exhaustive linter cannot cover the
	// gap either, because go/.golangci.yml sets default-signifies-exhaustive
	// and that switch has a default.
	kinds := change.Kinds()
	if len(kinds) == 0 {
		t.Fatal("change.Kinds() returned nothing; this check would pass having evaluated no kinds")
	}
	unrecognised := changeExitCode(change.Failure{Kind: change.FailureKind("something_new")})
	for _, kind := range kinds {
		rows, offDefault := 0, 0
		for _, tc := range cases {
			if tc.failure.Kind != kind {
				continue
			}
			rows++
			if tc.want != unrecognised {
				offDefault++
			}
		}
		if rows == 0 {
			t.Fatalf("no row exercises change.FailureKind %q; add the row and the changeExitCode arm it needs", kind)
		}
		if offDefault == 0 {
			t.Fatalf("every row for %q expects %d, which is what an unrecognised kind gets, so changeExitCode has no arm of its own for it", kind, unrecognised)
		}
	}
	t.Logf("checked %d declared kinds against %d table rows", len(kinds), len(cases))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := changeExitCode(tc.failure); got != tc.want {
				t.Fatalf("changeExitCode(%+v) = %d, want %d", tc.failure, got, tc.want)
			}
		})
	}

	if got, want := traceExitCode("building"), 1; got != want {
		t.Fatalf("traceExitCode(building) = %d, want %d; the divergence this table documents is gone", got, want)
	}
	if got, want := traceExitCode("truncated"), 1; got != want {
		t.Fatalf("traceExitCode(truncated) = %d, want %d; the divergence this table documents is gone", got, want)
	}
	if got := changeExitCode(change.Failure{Kind: change.KindFreshness}); got == traceExitCode("building") {
		t.Fatal("freshness now maps to the same code as traceExitCode(building); update changeExitCode's comment or this test")
	}
}

// TestChangeExitErrorPassesThroughForeignErrors proves changeExitError only
// rewrites this family's own failures. A cobra flag error must reach the
// operator unchanged rather than being relabelled with an exit code it never
// had.
func TestChangeExitErrorPassesThroughForeignErrors(t *testing.T) {
	t.Parallel()

	if err := changeExitError(nil); err != nil {
		t.Fatalf("changeExitError(nil) = %v, want nil", err)
	}
	foreign := errors.New("flag accessed but not defined: intent")
	if got := changeExitError(foreign); !errors.Is(got, foreign) {
		t.Fatalf("changeExitError(foreign) = %v, want the original error", got)
	}
	var exitErr commandExitError
	if got := changeExitError(change.Failure{Kind: change.KindFreshness, Message: "pre-change impact freshness is building"}); !errors.As(got, &exitErr) {
		t.Fatalf("changeExitError(failure) = %#v, want commandExitError", got)
	}
	if exitErr.ExitCode() != 4 || exitErr.Error() != "pre-change impact freshness is building" {
		t.Fatalf("commandExitError = {%q %d}, want {%q 4}", exitErr.Error(), exitErr.ExitCode(), "pre-change impact freshness is building")
	}
}

// TestTransportErrorCodeParity holds change.ErrorCodeFromTransport and
// traceErrorCodeFromTransport to the same answers.
//
// The two are copies. cmd/eshu is package main, so neither can import the
// other, and both still have callers: `eshu change impact` and
// `eshu change plan` use the copy, while `eshu trace`, `eshu map`,
// component_api, and the freshness family use the original. #6117 edited the
// original mid-epic. Without this test the next such edit would reach the
// trace side and leave the change side classifying on the old table, with
// nothing in the tree going red.
//
// Every branch of the shared body gets a row: the two strings.Contains checks
// that run first, the four mapped statuses, and the default that catches
// everything else. The precedence row -- an HTTP 400 whose body says
// "connection refused" -- is the only input whose answer changes if the
// message checks move after the status switch.
func TestTransportErrorCodeParity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil error", err: nil, want: "api_error"},
		{name: "message connection refused", err: errors.New("dial tcp 127.0.0.1:8080: connect: connection refused"), want: "backend_unavailable"},
		{name: "message request failed", err: errors.New("request failed: context deadline exceeded"), want: "backend_unavailable"},
		{name: "message beats status", err: &apiHTTPError{StatusCode: http.StatusBadRequest, Body: "connection refused"}, want: "backend_unavailable"},
		{name: "status 400", err: &apiHTTPError{StatusCode: http.StatusBadRequest, Body: "bad selector"}, want: "invalid_argument"},
		{name: "status 404", err: &apiHTTPError{StatusCode: http.StatusNotFound, Body: "no such repo"}, want: "not_found"},
		{name: "status 501", err: &apiHTTPError{StatusCode: http.StatusNotImplemented, Body: "no such capability"}, want: "unsupported_capability"},
		{name: "status 503", err: &apiHTTPError{StatusCode: http.StatusServiceUnavailable, Body: "backend down"}, want: "backend_unavailable"},
		{name: "status 409 falls through", err: &apiHTTPError{StatusCode: http.StatusConflict, Body: "ambiguous scope"}, want: "api_error"},
		{name: "status 500 falls through", err: &apiHTTPError{StatusCode: http.StatusInternalServerError, Body: "boom"}, want: "api_error"},
		{name: "no status carried", err: errors.New("json: cannot unmarshal"), want: "api_error"},
	}

	// Every arm of the switch, plus the default, has to be represented or a
	// divergence in an unvisited arm passes unnoticed.
	seen := map[string]bool{}
	for _, tc := range cases {
		seen[tc.want] = true
	}
	for _, code := range []string{"invalid_argument", "not_found", "unsupported_capability", "backend_unavailable", "api_error"} {
		if !seen[code] {
			t.Fatalf("no row expects %q; the table no longer covers every arm", code)
		}
	}
	if len(cases) != 11 {
		t.Fatalf("parity table has %d rows, want 11; add the row and say why rather than editing this number", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			trace := traceErrorCodeFromTransport(tc.err)
			extracted := change.ErrorCodeFromTransport(tc.err)
			if trace != extracted {
				t.Fatalf("traceErrorCodeFromTransport(%v) = %q, change.ErrorCodeFromTransport = %q; the two copies have diverged", tc.err, trace, extracted)
			}
			if trace != tc.want {
				t.Fatalf("both copies answered %q for %v, want %q", trace, tc.err, tc.want)
			}
		})
	}
}
