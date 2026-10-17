// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

// serveDeltaUnionTraced serves one GET of the completeness route through the
// real mux under a recording request span, canceling the request context
// first when cancel is set.
func serveDeltaUnionTraced(
	t *testing.T, handler *Handler, query string, cancel bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx, parent := provider.Tracer("delta-union-failure-test").Start(context.Background(), "request")
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cancel {
		stop()
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/delta-union/completeness"+query, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended()
}

// TestDeltaUnionCompletenessFailuresAnswerFixedText is the #7674 regression
// for GET /api/v0/admin/delta-union/completeness. A failed ACTIVE or graph
// read answered 500 with err.Error() in "detail", so the body quoted the
// backend error, and a client cancel answered that 500 as a server fault.
// Both reads now answer the fixed detail through the server-failure helper:
// 500 with the error on the request span, or 499 for a client cancel. The
// {"status","detail"} body shape is unchanged.
func TestDeltaUnionCompletenessFailuresAnswerFixedText(t *testing.T) {
	t.Parallel()

	failActive := func(err error) *Handler {
		checker, active := deltaUnionChecker()
		active.err = err
		return &Handler{Completeness: checker}
	}
	failGraph := func(err error) *Handler {
		checker, _ := deltaUnionChecker()
		checker.Graph = &stubDeltaUnionGraph{err: err}
		return &Handler{Completeness: checker}
	}
	for _, tc := range []struct {
		name    string
		handler func(error) *Handler
	}{
		{"active read", failActive},
		{"graph read", failGraph},
	} {
		for _, kind := range []adminFailureKind{adminFailureBackend, adminFailureCancel} {
			t.Run(tc.name+"/"+kind.String(), func(t *testing.T) {
				t.Parallel()
				rec, ended := serveDeltaUnionTraced(t, tc.handler(adminFailureError(kind)), "?scope=scope-1", kind == adminFailureCancel)
				step := adminFailureStep{name: tc.name, message: deltaUnionCompletenessFailedMessage}
				assertAdminFailure(t, step, kind, rec, ended)
				if got := adminFailureBodyStatus(t, rec); got != "error" {
					t.Fatalf("body status = %q, want the unchanged \"error\"; body = %s", got, rec.Body.String())
				}
			})
		}
	}
}

// TestDeltaUnionCompletenessCapAnswersItsOwnFixedText pins that a check past
// the repository cap keeps its 500 and its narrow-the-filter text, which the
// route builds from the counts alone, not from an error's text. It covers
// both cap checks: the scope filter past MaxRepos before the ACTIVE read
// ("scopes"), and the delta-active subset past MaxRepos after it
// ("repositories"). The second case requests one scope, inside the cap, and
// the ACTIVE stub answers two delta-active scopes and one full active
// generation, so only the post-read check can fire, and it must count the
// delta-active subset, not every assembled repository.
func TestDeltaUnionCompletenessCapAnswersItsOwnFixedText(t *testing.T) {
	t.Parallel()

	const capText = "delta union completeness covers %d %s, past the %d cap: narrow the scope filter"
	for _, tc := range []struct {
		name   string
		scopes []string
		unit   string
		extend func(*stubDeltaUnionActive)
	}{
		{name: "scope filter", scopes: []string{"scope-1", "scope-2"}, unit: "scopes"},
		{name: "delta-active subset", scopes: []string{"scope-1"}, unit: "repositories", extend: addDeltaAndFullScopes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checker, active := deltaUnionChecker()
			checker.MaxRepos = 1
			if tc.extend != nil {
				tc.extend(active)
			}
			count := len(tc.scopes)
			if tc.unit == "repositories" {
				if count > checker.MaxRepos {
					t.Fatalf("fixture requests %d scopes past the %d cap; the pre-read check would fire instead", count, checker.MaxRepos)
				}
				count = deltaActiveScopeCount(active.snapshot.Rows)
				if all := activeScopeCount(active.snapshot.Rows); all <= count {
					t.Fatalf("fixture has %d active scopes and %d delta-active; it needs a full generation to tell them apart", all, count)
				}
			}
			if count <= checker.MaxRepos {
				t.Fatalf("fixture count %d is not past the %d cap", count, checker.MaxRepos)
			}
			query := "?scope=" + strings.Join(tc.scopes, "&scope=")
			rec, ended := serveDeltaUnionTraced(t, &Handler{Completeness: checker}, query, false)
			step := adminFailureStep{name: tc.name, message: fmt.Sprintf(capText, count, tc.unit, checker.MaxRepos)}
			assertAdminFailure(t, step, adminFailureBackend, rec, ended)
		})
	}
}

// TestDeltaUnionCompletenessAtCapAnswersReport pins the post-read cap
// boundary: exactly MaxRepos delta-active repositories, plus a full active
// generation outside the count, answer the report rather than the cap error.
func TestDeltaUnionCompletenessAtCapAnswersReport(t *testing.T) {
	t.Parallel()

	checker, active := deltaUnionChecker()
	addDeltaAndFullScopes(active)
	checker.MaxRepos = deltaActiveScopeCount(active.snapshot.Rows)
	rec, _ := serveDeltaUnionTraced(t, &Handler{Completeness: checker}, "?scope=scope-1", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 at the %d cap; body = %s", rec.Code, checker.MaxRepos, rec.Body.String())
	}
	var report struct {
		Repos []struct {
			RepoID  string `json:"repo_id"`
			Outcome string `json:"outcome"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v; body = %s", err, rec.Body.String())
	}
	outcomes := map[string]string{}
	for _, repo := range report.Repos {
		outcomes[repo.RepoID] = repo.Outcome
	}
	if len(report.Repos) != 3 || outcomes["repo-1"] != "complete" || outcomes["repo-3"] != "skipped" {
		t.Fatalf("report repos = %+v, want repo-1 complete, repo-2 compared, repo-3 skipped", report.Repos)
	}
}

// addDeltaAndFullScopes adds a second delta-active scope (repo-2) and a scope
// whose active generation is full, not a delta (repo-3). The cap counts
// repo-1 and repo-2 only.
func addDeltaAndFullScopes(active *stubDeltaUnionActive) {
	active.snapshot.Rows = append(active.snapshot.Rows,
		goldengate.DeltaUnionActiveRow{
			RepoID: "repo-2", ScopeID: "scope-2", GenerationID: "gen-delta-2",
			GenerationStatus: "active", IsDelta: true,
			Kind: "file", StableKey: "file:repo-2:a.go", Path: "a.go",
		},
		goldengate.DeltaUnionActiveRow{
			RepoID: "repo-3", ScopeID: "scope-3", GenerationID: "gen-full-3",
			GenerationStatus: "active", IsDelta: false,
			Kind: "file", StableKey: "file:repo-3:a.go", Path: "a.go",
		})
}

// deltaActiveScopeCount counts the distinct scopes whose rows include an
// active delta generation: the subset the post-read cap check counts.
func deltaActiveScopeCount(rows []goldengate.DeltaUnionActiveRow) int {
	return activeScopeCountWhere(rows, func(row goldengate.DeltaUnionActiveRow) bool { return row.IsDelta })
}

// activeScopeCount counts the distinct scopes with any active generation.
func activeScopeCount(rows []goldengate.DeltaUnionActiveRow) int {
	return activeScopeCountWhere(rows, func(goldengate.DeltaUnionActiveRow) bool { return true })
}

func activeScopeCountWhere(rows []goldengate.DeltaUnionActiveRow, keep func(goldengate.DeltaUnionActiveRow) bool) int {
	scopes := map[string]struct{}{}
	for _, row := range rows {
		if row.GenerationStatus == "active" && keep(row) {
			scopes[row.ScopeID] = struct{}{}
		}
	}
	return len(scopes)
}

// TestDeltaUnionCompletenessKeepsClientVerdicts pins the route's existing
// non-fault answers: a missing scope filter stays 400 with its sentinel text,
// and an unconfigured checker stays the plain 503.
func TestDeltaUnionCompletenessKeepsClientVerdicts(t *testing.T) {
	t.Parallel()

	checker, _ := deltaUnionChecker()
	rec, _ := serveDeltaUnionTraced(t, &Handler{Completeness: checker}, "", false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no-scope status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := adminFailureBodyMessage(t, rec), ErrDeltaUnionScopeRequired.Error()+": pass at least one ?scope= query value"; got != want {
		t.Fatalf("no-scope detail = %q, want %q", got, want)
	}
	rec, _ = serveDeltaUnionTraced(t, &Handler{}, "?scope=scope-1", false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}

// adminFailureBodyStatus reads the "status" field of a {"status","detail"}
// body.
func adminFailureBodyStatus(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body = %s", err, rec.Body.String())
	}
	return body.Status
}
