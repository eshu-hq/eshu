// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestCloudInventoryHandlerAccountAliasZeroResultsWarnsOnPreRolloutGap is the
// #5238 follow-up regression: an operator filtering by account_id/project_id/
// subscription_id who gets zero rows cannot tell "no such account" from "this
// account's data predates the rollout and has not been re-admitted yet" from
// the response alone -- both looked byte-identical. When the disambiguation
// probe finds a canonical row in the same provider/access scope with no
// account_id key at all (the exact pre-fix payload shape), the response must
// carry the account_alias_rollout_gap warning flag.
func TestCloudInventoryHandlerAccountAliasZeroResultsWarnsOnPreRolloutGap(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: nil},
		{columns: []string{"exists"}, rows: [][]driver.Value{{true}}},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=aws&account_id=000000000000", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 2; got != want {
		t.Fatalf("Postgres received %d queries, want %d (primary + rollout-gap probe); queries = %#v", got, want, recorder.queries)
	}
	probe := recorder.queries[1]
	for _, fragment := range []string{
		"NOT (fact_records.payload ? 'account_id')",
		"fact_records.payload->>'provider' = $1",
	} {
		if !strings.Contains(probe, fragment) {
			t.Fatalf("rollout-gap probe query missing fragment %q:\n%s", fragment, probe)
		}
	}

	resp := cloudInventoryDecodeEnvelope(t, w.Body.Bytes())
	data := resp.Data.(map[string]any)
	if got, want := len(data["resources"].([]any)), 0; got != want {
		t.Fatalf("resources = %d, want %d", got, want)
	}
	warningFlags := cloudInventoryWarningFlagsFromData(t, data)
	if got, want := warningFlags, []string{cloudInventoryWarningFlagRolloutGap}; !reflect.DeepEqual(got, want) {
		t.Fatalf("warning_flags = %v, want %v", got, want)
	}
}

// TestCloudInventoryHandlerAccountAliasZeroResultsWithoutPreRolloutGapStaysClean
// is the negative control: the probe finds no pre-rollout row anywhere in
// scope, so zero rows genuinely means "no such account" -- the response must
// NOT carry the rollout-gap warning flag (a false positive would be as
// misleading as the missing signal this change fixes).
func TestCloudInventoryHandlerAccountAliasZeroResultsWithoutPreRolloutGapStaysClean(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: nil},
		{columns: []string{"exists"}, rows: [][]driver.Value{{false}}},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=aws&account_id=000000000000", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 2; got != want {
		t.Fatalf("Postgres received %d queries, want %d; queries = %#v", got, want, recorder.queries)
	}
	resp := cloudInventoryDecodeEnvelope(t, w.Body.Bytes())
	data := resp.Data.(map[string]any)
	if _, present := data["warning_flags"]; present {
		t.Fatalf("warning_flags must be absent when no pre-rollout evidence exists: %#v", data["warning_flags"])
	}
}

// TestCloudInventoryHandlerAccountAliasNonEmptyResultsSkipsProbe proves the
// performance constraint directly: an account-alias filter that already
// matched rows must never issue the rollout-gap probe. Non-empty results
// already answer the question the probe exists to resolve.
func TestCloudInventoryHandlerAccountAliasNonEmptyResultsSkipsProbe(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: [][]driver.Value{{cloudInventoryAccountAliasPayloadRow(t, "aws", "aws:scope:1", "aws:res:1", "000000000000")}}},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=aws&account_id=000000000000", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("Postgres received %d queries, want %d (probe must not fire when rows are already present); queries = %#v", got, want, recorder.queries)
	}
	resp := cloudInventoryDecodeEnvelope(t, w.Body.Bytes())
	data := resp.Data.(map[string]any)
	if _, present := data["warning_flags"]; present {
		t.Fatalf("warning_flags must be absent when the alias filter already matched rows: %#v", data["warning_flags"])
	}
}

// TestCloudInventoryHandlerUnfilteredZeroResultsSkipsProbe proves the second
// performance constraint: the probe must never fire on a call with no
// account-alias filter at all, including the hot unfiltered/provider-only
// default path, even when it returns zero rows.
func TestCloudInventoryHandlerUnfilteredZeroResultsSkipsProbe(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: nil},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=aws", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("Postgres received %d queries, want %d (no account alias supplied, probe must not fire); queries = %#v", got, want, recorder.queries)
	}
}

// TestCloudInventoryHandlerAccountAliasZeroResultsProbeErrorDegradesGracefully
// proves the probe's own failure never breaks the already-successful primary
// read: a probe error must not turn a 200 into a 500. It must be reported as
// a distinct warning flag, not swallowed silently -- mirroring the
// content-store-coverage-error precedent in repository_stats.go.
func TestCloudInventoryHandlerAccountAliasZeroResultsProbeErrorDegradesGracefully(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"payload"}, rows: nil},
		{err: errCloudInventoryRolloutProbeTest},
	})
	handler := &CloudInventoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/cloud/inventory?provider=aws&account_id=000000000000", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 2; got != want {
		t.Fatalf("Postgres received %d queries, want %d; queries = %#v", got, want, recorder.queries)
	}
	resp := cloudInventoryDecodeEnvelope(t, w.Body.Bytes())
	data := resp.Data.(map[string]any)
	warningFlags := cloudInventoryWarningFlagsFromData(t, data)
	if got, want := warningFlags, []string{cloudInventoryWarningFlagRolloutGapCheckFailed}; !reflect.DeepEqual(got, want) {
		t.Fatalf("warning_flags = %v, want %v", got, want)
	}
}

// TestBuildCloudInventoryPreRolloutProbeSQLScopesAccessGrantsLikePrimaryQuery
// proves the probe reuses the exact same active-generation join and access-
// scope predicate shape as buildCloudInventoryIdentitiesSQL, so a scoped
// caller's probe can never see -- or report a gap for -- a scope they hold no
// grant for.
func TestBuildCloudInventoryPreRolloutProbeSQLScopesAccessGrantsLikePrimaryQuery(t *testing.T) {
	t.Parallel()

	query, args := buildCloudInventoryPreRolloutProbeSQL(cloudInventoryFilter{
		Provider:             "aws",
		AccountAliasKey:      "account_id",
		AccountAliasValue:    "000000000000",
		AllScopes:            false,
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	})
	for _, fragment := range []string{
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = fact_records.generation_id",
		"JOIN scope_generations AS generation",
		"generation.status = 'active'",
		"NOT (fact_records.payload ? 'account_id')",
		"fact_records.scope_id = ANY(",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("rollout-gap probe query missing fragment %q:\n%s", fragment, query)
		}
	}
	if got, want := len(args), 3; got != want {
		t.Fatalf("len(args) = %d, want %d (provider + two access-scope arrays); args = %#v", got, want, args)
	}
}

// TestBuildCloudInventoryPreRolloutProbeSQLAllScopesOmitsAccessPredicate
// proves the unscoped/admin path stays byte-identical in shape to the primary
// query's own unscoped path: no access-scope predicate, provider still bound.
func TestBuildCloudInventoryPreRolloutProbeSQLAllScopesOmitsAccessPredicate(t *testing.T) {
	t.Parallel()

	query, args := buildCloudInventoryPreRolloutProbeSQL(cloudInventoryFilter{
		Provider:  "gcp",
		AllScopes: true,
	})
	if strings.Contains(query, "fact_records.scope_id = ANY(") {
		t.Fatalf("unscoped/admin probe must omit the access-scope predicate:\n%s", query)
	}
	if got, want := len(args), 1; got != want {
		t.Fatalf("len(args) = %d, want %d (provider only); args = %#v", got, want, args)
	}
}

var errCloudInventoryRolloutProbeTest = &cloudInventoryRolloutProbeTestError{}

type cloudInventoryRolloutProbeTestError struct{}

func (e *cloudInventoryRolloutProbeTestError) Error() string {
	return "synthetic rollout-gap probe failure"
}

func cloudInventoryDecodeEnvelope(t *testing.T, body []byte) ResponseEnvelope {
	t.Helper()
	var resp ResponseEnvelope
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; body = %s", err, body)
	}
	return resp
}

func cloudInventoryWarningFlagsFromData(t *testing.T, data map[string]any) []string {
	t.Helper()
	raw, ok := data["warning_flags"].([]any)
	if !ok {
		t.Fatalf("warning_flags missing or wrong type in response data: %#v", data)
	}
	flags := make([]string, 0, len(raw))
	for _, value := range raw {
		flag, ok := value.(string)
		if !ok {
			t.Fatalf("warning_flags element is not a string: %#v", value)
		}
		flags = append(flags, flag)
	}
	return flags
}
