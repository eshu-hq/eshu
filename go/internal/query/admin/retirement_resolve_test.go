// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestRetireRefusesMissingReason(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	var calls []string
	h := retireHandler(singleRepoCatalog(),
		&stubRetirementFencer{calls: &calls}, &stubRetirementPurger{calls: &calls}, audit)
	body := retireBody()
	delete(body, "reason")
	rec := postRetire(t, h, body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(calls) != 0 {
		t.Fatalf("fence or purge ran on a missing reason; calls = %v", calls)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_missing_reason" {
		t.Fatalf("want retire_refused_missing_reason denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireRefusesMissingIdempotencyKey(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	var calls []string
	h := retireHandler(singleRepoCatalog(),
		&stubRetirementFencer{calls: &calls}, &stubRetirementPurger{calls: &calls}, audit)
	body := retireBody()
	delete(body, "idempotency_key")
	rec := postRetire(t, h, body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_missing_idempotency_key" {
		t.Fatalf("want retire_refused_missing_idempotency_key denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireRefusesEmptyRepositories(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	var calls []string
	h := retireHandler(singleRepoCatalog(),
		&stubRetirementFencer{calls: &calls}, &stubRetirementPurger{calls: &calls}, audit)
	body := retireBody()
	body["repositories"] = []string{}
	rec := postRetire(t, h, body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRetireRefusesOverCapRepositories(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	var calls []string
	h := retireHandler(singleRepoCatalog(),
		&stubRetirementFencer{calls: &calls}, &stubRetirementPurger{calls: &calls}, audit)
	body := retireBody()
	selectors := make([]string, 0, pgstatus.RetirementFenceMaxRepositories+1)
	for i := 0; i <= pgstatus.RetirementFenceMaxRepositories; i++ {
		selectors = append(selectors, "acme/api")
	}
	body["repositories"] = selectors
	rec := postRetire(t, h, body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(calls) != 0 {
		t.Fatalf("fence or purge ran over the cap; calls = %v", calls)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_too_many_repositories" {
		t.Fatalf("want retire_refused_too_many_repositories denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireCollapsesDuplicateSelectors(t *testing.T) {
	var calls []string
	audit := &testutil.FakeGovernanceAuditAppender{}
	fencer := &stubRetirementFencer{calls: &calls, result: pgstatus.FenceResult{
		RetirementIDs: []string{"rr_dup01"},
		RepoIDs:       []string{"PARTITION:acme/api"},
	}}
	purger := &stubRetirementPurger{calls: &calls, result: pgstatus.PurgeResult{
		Completed: []string{"PARTITION:acme/api"},
	}}
	var gotReq pgstatus.FenceRequest
	h := retireHandler(singleRepoCatalog(), nil, nil, audit)
	h.RetirementFencer = &captureFencer{calls: &calls, result: fencer.result, req: &gotReq}
	h.RetirementPurger = purger
	body := retireBody()
	body["repositories"] = []string{"acme/api", "acme/api"}
	rec := postRetire(t, h, body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(gotReq.Retirements) != 1 {
		t.Fatalf("fence retirements = %v, want one collapsed entry", gotReq.Retirements)
	}
}

func TestRetireRefusesUnauthorizedScopedToken(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	var calls []string
	h := retireHandler(singleRepoCatalog(),
		&stubRetirementFencer{calls: &calls}, &stubRetirementPurger{calls: &calls}, audit)
	authCtx := &auth.AuthContext{Mode: auth.AuthModeScoped, SubjectIDHash: "sha256:deadbeef", AllScopes: false}
	rec := postRetire(t, h, retireBody(), authCtx)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if len(calls) != 0 {
		t.Fatalf("fence or purge ran for a scoped token; calls = %v", calls)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_unauthorized" {
		t.Fatalf("want retire_refused_unauthorized denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireRefusesUnknownRepository(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	var calls []string
	h := retireHandler(singleRepoCatalog(),
		&stubRetirementFencer{calls: &calls}, &stubRetirementPurger{calls: &calls}, audit)
	body := retireBody()
	body["repositories"] = []string{"acme/gone"}
	rec := postRetire(t, h, body, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("fence or purge ran for an unknown repository; calls = %v", calls)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_unknown_repository" {
		t.Fatalf("want retire_refused_unknown_repository denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireRefusesAmbiguousRepository(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	var calls []string
	catalog := &stubRetirementCatalog{entries: map[string][]querycontract.RepositoryCatalogEntry{
		"acme": {
			gitCatalogEntry("PARTITION:acme/a", "git-repository-scope:PARTITION:acme/a"),
			gitCatalogEntry("PARTITION:acme/b", "git-repository-scope:PARTITION:acme/b"),
		},
	}}
	h := retireHandler(catalog,
		&stubRetirementFencer{calls: &calls}, &stubRetirementPurger{calls: &calls}, audit)
	body := retireBody()
	body["repositories"] = []string{"acme"}
	rec := postRetire(t, h, body, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_ambiguous_repository" {
		t.Fatalf("want retire_refused_ambiguous_repository denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireRefusesNonGitScope(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	var calls []string
	catalog := &stubRetirementCatalog{entries: map[string][]querycontract.RepositoryCatalogEntry{
		"wiki": {gitCatalogEntry("PARTITION:wiki", "confluence-space")},
	}}
	h := retireHandler(catalog,
		&stubRetirementFencer{calls: &calls}, &stubRetirementPurger{calls: &calls}, audit)
	body := retireBody()
	body["repositories"] = []string{"wiki"}
	rec := postRetire(t, h, body, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if len(calls) != 0 {
		t.Fatalf("fence or purge ran for a non-git scope; calls = %v", calls)
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_refused_not_git_repository" {
		t.Fatalf("want retire_refused_not_git_repository denied audit, got %+v", audit.Events)
	}
	assertRetireAuditValid(t, audit.Events)
}

func TestRetireUnconfiguredPortsAreUnavailable(t *testing.T) {
	h := &Handler{Repositories: singleRepoCatalog()}
	rec := postRetire(t, h, retireBody(), nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
