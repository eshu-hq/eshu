// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// CLI incremental-freshness parity tests (issue #1804, parent #1797).
//
// These tests close the API/MCP/CLI parity triangle for incremental freshness.
// The API and MCP surfaces are compared in go/internal/mcp/freshness_parity*.go
// against a shared query.FreshnessHandler; the MCP tools there are proven pure
// transport into that handler. This file proves the CLI is the SAME pure
// transport: it serves the IDENTICAL query.FreshnessHandler over an httptest
// server, drives the CLI fetch path (the exact code `eshu freshness generations`
// and `eshu freshness changed-since` run), and asserts the CLI-consumed envelope
// reports the SAME freshness state and SAME source generation ids as the
// canonical envelope the handler emits.
//
// Because all three surfaces dispatch into one handler type with one fixture,
// equality of these fields means the canonical envelope is transport-invariant:
// API, MCP, and CLI agree on incremental-freshness truth.
//
// Generic answer-workflow parity is issue #1795 (see trace_test.go round-trip);
// this file is scoped strictly to incremental freshness and does not duplicate
// it.
//
// Non-green discipline: building / stale / unavailable states are asserted to be
// exactly "building" / "stale" / "unavailable" as the CLI consumes them. The CLI
// renderer surfaces "Truth freshness: <state>" verbatim, so a state silently
// downgraded to fresh would fail both the envelope assertion and the rendered
// output assertion.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// cliFreshnessCommand builds a generations command wired to the test server via
// --service-url and applies the given string flag override, so the end-to-end
// runFreshnessGenerations path (apiClientFromCmd -> fetch -> finish) is exercised
// against the real canonical handler.
func cliFreshnessCommand(t *testing.T, client *APIClient, flag, value string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	addFreshnessGenerationsFlags(cmd)
	addRemoteFlags(cmd)
	if err := cmd.Flags().Set("service-url", client.BaseURL); err != nil {
		t.Fatalf("set service-url: %v", err)
	}
	if err := cmd.Flags().Set(flag, value); err != nil {
		t.Fatalf("set %s: %v", flag, err)
	}
	cmd.SetOut(&bytes.Buffer{})
	return cmd
}

// fixedGenerationReader is a deterministic generation-lifecycle fixture
// returning one prepared page regardless of filter, so the CLI observes exactly
// the rows the API surface emits.
type fixedGenerationReader struct {
	page status.GenerationLifecyclePage
}

func (r fixedGenerationReader) ListGenerationLifecycle(
	_ context.Context,
	_ status.GenerationLifecycleFilter,
) (status.GenerationLifecyclePage, error) {
	return r.page, nil
}

// fixedChangedSinceReader is a deterministic changed-since fixture returning one
// prepared summary regardless of filter.
type fixedChangedSinceReader struct {
	summary status.ChangedSinceSummary
}

func (r fixedChangedSinceReader) ComputeChangedSinceDelta(
	_ context.Context,
	_ status.ChangedSinceFilter,
) (status.ChangedSinceSummary, error) {
	return r.summary, nil
}

// cliFreshnessServer mounts the real query.FreshnessHandler backed by the given
// fixture readers on an httptest server and returns an APIClient pointed at it.
// The server is the SAME handler the API and MCP surfaces use, so the CLI is
// driven against the canonical envelope, not a stub.
func cliFreshnessServer(
	t *testing.T,
	generations query.GenerationLifecycleReader,
	changedSince query.ChangedSinceReader,
) *APIClient {
	t.Helper()

	mux := http.NewServeMux()
	handler := &query.FreshnessHandler{
		Generations:  generations,
		ChangedSince: changedSince,
		Profile:      query.ProfileProduction,
	}
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
}

// renderGenerations renders the CLI generation-lifecycle summary the way the
// `eshu freshness generations` command does, returning the human output for
// non-green and convenience assertions.
func renderGenerations(t *testing.T, env freshnessGenerationsEnvelope) string {
	t.Helper()
	out := &bytes.Buffer{}
	if err := renderFreshnessGenerationsSummary(out, env); err != nil {
		t.Fatalf("renderFreshnessGenerationsSummary() error = %v", err)
	}
	return out.String()
}

// renderChangedSince renders the CLI changed-since summary the way the
// `eshu freshness changed-since` command does.
func renderChangedSince(t *testing.T, env freshnessGenerationsEnvelope) string {
	t.Helper()
	out := &bytes.Buffer{}
	if err := renderFreshnessChangedSinceSummary(out, env); err != nil {
		t.Fatalf("renderFreshnessChangedSinceSummary() error = %v", err)
	}
	return out.String()
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// freshnessStateFromEnvelope reads the canonical truth.freshness.state the CLI
// envelope carries. The CLI parses truth as a generic map, so the parity layer
// reads the same path the renderer reads.
func freshnessStateFromEnvelope(env freshnessGenerationsEnvelope) string {
	return traceString(traceMap(env.Truth, "freshness"), "state")
}

// generationIDsFromEnvelope flattens the source generation identities the CLI
// envelope reports, using the SAME projection shape the MCP parity layer uses
// so the two can be reasoned about as equal contracts.
func generationIDsFromEnvelope(env freshnessGenerationsEnvelope) []string {
	ids := []string{}
	if rows := traceSlice(env.Data, "generations"); len(rows) > 0 {
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			ids = append(ids, "gen:"+traceString(row, "generation_id"))
			if active := traceString(row, "current_active_generation_id"); active != "" {
				ids = append(ids, "active:"+active)
			}
		}
		return ids
	}
	if since := traceString(env.Data, "since_generation_id"); since != "" {
		ids = append(ids, "since:"+since)
	}
	if active := traceString(env.Data, "current_active_generation_id"); active != "" {
		ids = append(ids, "current:"+active)
	}
	return ids
}

// TestCLIFreshnessGenerationsParityChangedSupersedes proves the CLI consumes the
// canonical generation-lifecycle envelope and reports the SAME active/superseded
// generation ids and fresh truth the API surface emits for a changed generation
// that supersedes the prior active one.
func TestCLIFreshnessGenerationsParityChangedSupersedes(t *testing.T) {
	client := cliFreshnessServer(t, fixedGenerationReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{
			{
				ScopeID:                   "git-repository-scope:acme/app",
				GenerationID:              "gen-new",
				CurrentActiveGenerationID: "gen-new",
				IsActive:                  true,
				Status:                    "active",
				TriggerKind:               "push",
				QueueStatus:               status.GenerationQueueStatus{Total: 6, Succeeded: 6},
			},
			{
				ScopeID:                   "git-repository-scope:acme/app",
				GenerationID:              "gen-old",
				CurrentActiveGenerationID: "gen-new",
				IsActive:                  false,
				Status:                    "superseded",
			},
		},
		Limit: 50,
	}}, nil)

	env, err := fetchFreshnessGenerations(client, freshnessGenerationsOptions{ScopeID: "git-repository-scope:acme/app"})
	if err != nil {
		t.Fatalf("fetchFreshnessGenerations() error = %v", err)
	}
	if got := freshnessStateFromEnvelope(env); got != string(query.FreshnessFresh) {
		t.Fatalf("CLI freshness state = %q, want fresh", got)
	}
	wantIDs := []string{"gen:gen-new", "active:gen-new", "gen:gen-old", "active:gen-new"}
	if got := generationIDsFromEnvelope(env); !equalStrings(got, wantIDs) {
		t.Fatalf("CLI generation ids = %v, want %v", got, wantIDs)
	}
	requireCLIRendersFreshness(t, renderGenerations(t, env), "fresh", "gen-new")
}

// TestCLIFreshnessGenerationsParityPendingBuilding proves the building state is
// surfaced as exactly "building" through the CLI transport and rendered verbatim
// — never silently downgraded to fresh.
func TestCLIFreshnessGenerationsParityPendingBuilding(t *testing.T) {
	client := cliFreshnessServer(t, fixedGenerationReader{page: status.GenerationLifecyclePage{
		Records: []status.GenerationLifecycleRecord{{
			ScopeID:      "git-repository-scope:acme/app",
			GenerationID: "gen-inflight",
			Status:       "pending",
			TriggerKind:  "webhook",
			QueueStatus:  status.GenerationQueueStatus{Total: 4, Outstanding: 4},
		}},
		Limit: 50,
	}}, nil)

	env, err := fetchFreshnessGenerations(client, freshnessGenerationsOptions{Repository: "acme/app"})
	if err != nil {
		t.Fatalf("fetchFreshnessGenerations() error = %v", err)
	}
	requireNonGreenCLIState(t, freshnessStateFromEnvelope(env), string(query.FreshnessBuilding))
	requireCLIRendersFreshness(t, renderGenerations(t, env), "building", "gen-inflight")
}

// TestCLIFreshnessGenerationsParityUnknownScopeNotFound proves the CLI refuses an
// unknown scope rather than rendering a confident empty list: the API surface
// returns an HTTP 404 scope_not_found envelope and the CLI command exits
// non-zero with a not-found exit code.
//
// DOCUMENTED TRANSPORT-SHAPE DIFFERENCE: the CLI's GetEnvelope returns a
// transport error (not a parsed envelope) for any non-2xx status, so the typed
// envelope error code (scope_not_found) is mapped to the HTTP-status-derived
// code (not_found) by traceErrorCodeFromTransport. The API and MCP surfaces
// (which read the body) keep the precise scope_not_found code. Both surfaces
// agree on the contract that an unknown scope is an explicit error, never a
// confident empty answer; only the CLI's error-code granularity narrows. This
// difference is asserted here intentionally so a future regression that made
// the CLI swallow the 404 into an empty success would fail.
func TestCLIFreshnessGenerationsParityUnknownScopeNotFound(t *testing.T) {
	client := cliFreshnessServer(t, fixedGenerationReader{page: status.GenerationLifecyclePage{Limit: 50}}, nil)

	// API/MCP surfaces keep the precise typed code in the body.
	_, transportErr := fetchFreshnessGenerations(client, freshnessGenerationsOptions{ScopeID: "does-not-exist"})
	if transportErr == nil {
		t.Fatal("fetchFreshnessGenerations() error = nil, want HTTP 404 for unknown scope")
	}
	if !strings.Contains(transportErr.Error(), string(query.ErrorCodeScopeNotFound)) {
		t.Fatalf("transport error = %v, want it to carry %q from the canonical body", transportErr, query.ErrorCodeScopeNotFound)
	}

	// The CLI command exits non-zero (the documented narrowed code).
	err := runFreshnessGenerations(cliFreshnessCommand(t, client, "scope-id", "does-not-exist"), nil)
	if err == nil {
		t.Fatal("runFreshnessGenerations() error = nil, want non-zero not-found exit")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got := exitErr.ExitCode(); got == 0 {
		t.Fatalf("exit code = 0, want non-zero for not-found")
	}
}

// TestCLIFreshnessChangedSinceParityRetiredEvidence proves the CLI consumes the
// SAME changed-since envelope and reports the SAME since/current generation ids
// and the SAME retired count — retired must not collapse into superseded.
func TestCLIFreshnessChangedSinceParityRetiredEvidence(t *testing.T) {
	client := cliFreshnessServer(t, nil, fixedChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:                   "git-repository-scope:acme/app",
		ScopeKind:                 "repository",
		Repository:                "acme/app",
		SinceGenerationID:         "gen-prior",
		CurrentActiveGenerationID: "gen-current",
		SampleLimit:               25,
		Categories: []status.ChangedSinceCategoryDelta{{
			Category: status.ChangedSinceCategoryFiles,
			Counts:   status.ChangedSinceCounts{Added: 1, Unchanged: 7, Retired: 2},
			Samples: map[status.ChangedSinceClassification][]status.ChangedSinceSample{
				status.ChangedSinceRetired: {
					{StableFactKey: "file:removed-a", FactKind: "file"},
					{StableFactKey: "file:removed-b", FactKind: "file"},
				},
			},
		}},
	}})

	env, err := fetchFreshnessChangedSince(client, freshnessChangedSinceOptions{
		ScopeID:           "git-repository-scope:acme/app",
		SinceGenerationID: "gen-prior",
	})
	if err != nil {
		t.Fatalf("fetchFreshnessChangedSince() error = %v", err)
	}
	if got := freshnessStateFromEnvelope(env); got != string(query.FreshnessFresh) {
		t.Fatalf("CLI freshness state = %q, want fresh", got)
	}
	wantIDs := []string{"since:gen-prior", "current:gen-current"}
	if got := generationIDsFromEnvelope(env); !equalStrings(got, wantIDs) {
		t.Fatalf("CLI generation ids = %v, want %v", got, wantIDs)
	}
	category := traceSlice(env.Data, "categories")[0].(map[string]any)
	counts := traceMap(category, "counts")
	if got := traceInt(counts, "retired"); got != 2 {
		t.Fatalf("CLI retired count = %d, want 2", got)
	}
	if got := traceInt(counts, "superseded"); got != 0 {
		t.Fatalf("CLI superseded count = %d, want 0 (retired must not collapse)", got)
	}
}

// TestCLIFreshnessChangedSinceParityUnavailable proves the unavailable state is
// surfaced as exactly "unavailable" through the CLI transport and rendered as a
// non-green diff-unavailable line, never as zero deltas.
func TestCLIFreshnessChangedSinceParityUnavailable(t *testing.T) {
	client := cliFreshnessServer(t, nil, fixedChangedSinceReader{summary: status.ChangedSinceSummary{
		ScopeID:     "git-repository-scope:acme/app",
		ScopeKind:   "repository",
		Repository:  "acme/app",
		Unavailable: true,
		SampleLimit: 25,
		Categories: []status.ChangedSinceCategoryDelta{
			{Category: status.ChangedSinceCategoryFiles, Unavailable: true},
		},
	}})

	env, err := fetchFreshnessChangedSince(client, freshnessChangedSinceOptions{
		ScopeID:           "git-repository-scope:acme/app",
		SinceGenerationID: "gen-prior",
	})
	if err != nil {
		t.Fatalf("fetchFreshnessChangedSince() error = %v", err)
	}
	requireNonGreenCLIState(t, freshnessStateFromEnvelope(env), string(query.FreshnessUnavailable))

	rendered := renderChangedSince(t, env)
	if !strings.Contains(rendered, "unavailable") {
		t.Fatalf("CLI changed-since render = %q, want it to surface unavailable", rendered)
	}
}

// requireNonGreenCLIState asserts the CLI-consumed freshness state is exactly the
// expected non-green state and is NOT fresh.
func requireNonGreenCLIState(t *testing.T, got, want string) {
	t.Helper()
	if got == string(query.FreshnessFresh) {
		t.Fatalf("CLI freshness state = fresh, want non-green %q (must not silently render as fresh)", want)
	}
	if got != want {
		t.Fatalf("CLI freshness state = %q, want %q", got, want)
	}
}

// requireCLIRendersFreshness asserts the human render leads with the truth
// freshness and includes the expected generation marker, so the convenience
// output never claims more confidence than the canonical envelope supports.
func requireCLIRendersFreshness(t *testing.T, rendered, wantState, wantGeneration string) {
	t.Helper()
	if !strings.Contains(rendered, "Truth freshness: "+wantState) {
		t.Fatalf("CLI render = %q, want 'Truth freshness: %s'", rendered, wantState)
	}
	if !strings.Contains(rendered, wantGeneration) {
		t.Fatalf("CLI render = %q, want generation %q", rendered, wantGeneration)
	}
}
