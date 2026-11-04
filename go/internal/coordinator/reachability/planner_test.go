// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachability

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	commitA = "0123456789abcdef0123456789abcdef01234567"
	commitB = "fedcba9876543210fedcba9876543210fedcba98"
)

func testInstance() workflow.CollectorInstance {
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	return workflow.CollectorInstance{
		InstanceID: "scanner-go-reachability", CollectorKind: scope.CollectorScannerWorker,
		Mode: workflow.CollectorModeScheduled, Enabled: true, ClaimsEnabled: true,
		Configuration: `{}`, CreatedAt: now, UpdatedAt: now, LastObservedAt: now,
	}
}

func testRequest(repositories ...scannercomponent.GoModuleRepository) PlanRequest {
	return PlanRequest{
		Instance:     testInstance(),
		ObservedAt:   time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC),
		PlanKey:      "go-modules-scheduled-20261104T120000Z",
		Repositories: repositories,
	}
}

func goRepository(hex, owner, commit string) scannercomponent.GoModuleRepository {
	return scannercomponent.GoModuleRepository{
		RepositoryID:    "repository:r_" + hex,
		RemoteURL:       "https://github.com/" + owner + "/repo" + hex,
		SourceCommitSHA: commit,
	}
}

func TestPlanBuildsOneWorkItemPerRepository(t *testing.T) {
	t.Parallel()

	run, items, skipped, err := Plan(testRequest(
		goRepository("00000001", "eshu-hq", commitA),
		goRepository("00000002", "other", commitB),
	))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	wantRun := RunID(testInstance(), "go-modules-scheduled-20261104T120000Z")
	if run.RunID != wantRun || run.RequestedCollector != "scanner_worker" {
		t.Fatalf("run = %+v, want id %q for scanner_worker", run, wantRun)
	}
	item := items[0]
	if item.ScopeID != "go-reachability://repository:r_00000001" {
		t.Fatalf("scope id = %q", item.ScopeID)
	}
	if item.AcceptanceUnitID != item.ScopeID || item.RunID != run.RunID ||
		item.ClaimEcosystem != "go" || item.SourceSystem != "scanner_worker" ||
		item.CollectorKind != scope.CollectorScannerWorker ||
		item.CollectorInstanceID != "scanner-go-reachability" {
		t.Fatalf("item = %+v", item)
	}
	// The collector runtime requires GenerationID == SourceRunID for this kind.
	if item.GenerationID == "" || item.GenerationID != item.SourceRunID {
		t.Fatalf("generation %q source run %q, want equal and set", item.GenerationID, item.SourceRunID)
	}
	if strings.Contains(item.FairnessKey, "github.com") || strings.Contains(item.WorkItemID, "github.com") {
		t.Fatalf("item identity carries a URL: %+v", item)
	}

	target, err := scannercomponent.ResolveGoReachabilityTarget(run.RequestedScopeSet, item.ScopeID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.SourceCommitSHA != commitA || target.CloneURL != "https://github.com/eshu-hq/repo00000001" ||
		target.AcceptanceUnitID != item.AcceptanceUnitID || target.RepositoryID != "repository:r_00000001" {
		t.Fatalf("target = %+v", target)
	}
}

// TestPlanNeverPutsARemoteURLInAnIdentifier walks every identity field of a
// planned run and item, because those reach logs, labels, and status rows.
func TestPlanNeverPutsARemoteURLInAnIdentifier(t *testing.T) {
	t.Parallel()

	run, items, _, err := Plan(testRequest(goRepository("00000001", "secret-owner", commitA)))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	item := items[0]
	for name, value := range map[string]string{
		"run_id": run.RunID, "scope_id": item.ScopeID, "work_item_id": item.WorkItemID,
		"generation_id": item.GenerationID, "source_run_id": item.SourceRunID,
		"fairness_key": item.FairnessKey, "acceptance_unit_id": item.AcceptanceUnitID,
	} {
		if strings.Contains(value, "secret-owner") || strings.Contains(value, "https:") {
			t.Errorf("%s = %q carries the remote", name, value)
		}
	}
}

// TestPlanIsIdempotentForTheSameCommit proves a repeat pass mints identical ids,
// and a new commit mints a new generation for the same scope.
func TestPlanIsIdempotentForTheSameCommit(t *testing.T) {
	t.Parallel()

	repository := goRepository("00000001", "eshu-hq", commitA)
	_, first, _, err := Plan(testRequest(repository))
	if err != nil {
		t.Fatal(err)
	}
	_, second, _, err := Plan(testRequest(repository))
	if err != nil {
		t.Fatal(err)
	}
	if first[0].WorkItemID != second[0].WorkItemID || first[0].GenerationID != second[0].GenerationID {
		t.Fatalf("repeat plan changed identity: %+v vs %+v", first[0], second[0])
	}
	repository.SourceCommitSHA = commitB
	_, moved, _, err := Plan(testRequest(repository))
	if err != nil {
		t.Fatal(err)
	}
	if moved[0].ScopeID != first[0].ScopeID {
		t.Fatalf("scope moved with the commit: %q", moved[0].ScopeID)
	}
	if moved[0].GenerationID == first[0].GenerationID {
		t.Fatal("a new commit kept the generation id, want a new generation")
	}
}

// TestPlanSkipsUnplannableRepositoriesWithAReason proves one bad row skips with
// a counted reason rather than failing the page, and that the reason text holds
// no repository value.
func TestPlanSkipsUnplannableRepositoriesWithAReason(t *testing.T) {
	t.Parallel()

	_, items, skipped, err := Plan(testRequest(
		goRepository("00000001", "eshu-hq", commitA),
		scannercomponent.GoModuleRepository{RepositoryID: "repository:r_00000002", RemoteURL: "/srv/repos/local", SourceCommitSHA: commitA},
		scannercomponent.GoModuleRepository{RepositoryID: "repository:r_00000003", RemoteURL: "", SourceCommitSHA: commitA},
		scannercomponent.GoModuleRepository{RepositoryID: "repository:r_00000004", RemoteURL: "https://github.com/a/b", SourceCommitSHA: ""},
		scannercomponent.GoModuleRepository{RepositoryID: "not-canonical", RemoteURL: "https://github.com/a/b", SourceCommitSHA: commitA},
		goRepository("00000001", "eshu-hq", commitA),
	))
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	want := map[string]int{
		SkipUnsupportedRemote: 2, SkipMissingCommit: 1, SkipInvalidRepositoryID: 1, SkipDuplicateRepository: 1,
	}
	for reason, count := range want {
		if skipped[reason] != count {
			t.Errorf("skipped[%s] = %d, want %d (%v)", reason, skipped[reason], count, skipped)
		}
	}
}

func TestPlanWithNoPlannableRepositoryReturnsNoRun(t *testing.T) {
	t.Parallel()

	run, items, _, err := Plan(testRequest())
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if run.RunID != "" || len(items) != 0 {
		t.Fatalf("run = %+v items = %d, want none", run, len(items))
	}
}

func TestPlanRefusesAnInvalidRequest(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*PlanRequest){
		"other kind":  func(r *PlanRequest) { r.Instance.CollectorKind = scope.CollectorVulnerabilityIntelligence },
		"zero clock":  func(r *PlanRequest) { r.ObservedAt = time.Time{} },
		"unsafe key":  func(r *PlanRequest) { r.PlanKey = "a key/with:spaces " },
		"blank key":   func(r *PlanRequest) { r.PlanKey = "" },
		"over a page": func(r *PlanRequest) { r.Repositories = make([]scannercomponent.GoModuleRepository, 257) },
	} {
		request := testRequest()
		mutate(&request)
		if _, _, _, err := Plan(request); err == nil {
			t.Errorf("%s: Plan() error = nil, want refusal", name)
		}
	}
}

func TestDerivationInterval(t *testing.T) {
	t.Parallel()

	unset, err := DerivationFromConfig(json.RawMessage(`{"derive_from_go_modules":{"enabled":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := unset.Interval(); got != 24*time.Hour {
		t.Fatalf("default interval = %s, want 24h", got)
	}
	set, err := DerivationFromConfig(json.RawMessage(`{"derive_from_go_modules":{"enabled":true,"scan_interval":"6h"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Interval(); got != 6*time.Hour {
		t.Fatalf("interval = %s, want 6h", got)
	}
}

func TestDerivationFromConfig(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		raw     string
		enabled bool
		wantErr bool
	}{
		{name: "absent block", raw: ""},
		{name: "no section", raw: `{"targets":[]}`},
		{name: "disabled", raw: `{"derive_from_go_modules":{"enabled":false}}`},
		{name: "enabled", raw: `{"derive_from_go_modules":{"enabled":true}}`, enabled: true},
		{name: "unknown keys ignored", raw: `{"surprise":1,"derive_from_go_modules":{"enabled":true,"extra":2}}`, enabled: true},
		{name: "enabled with interval", raw: `{"derive_from_go_modules":{"enabled":true,"scan_interval":"6h"}}`, enabled: true},
		{name: "bad interval", raw: `{"derive_from_go_modules":{"enabled":true,"scan_interval":"soon"}}`, wantErr: true},
		{name: "zero interval", raw: `{"derive_from_go_modules":{"enabled":true,"scan_interval":"0s"}}`, wantErr: true},
		{name: "negative interval", raw: `{"derive_from_go_modules":{"enabled":true,"scan_interval":"-1h"}}`, wantErr: true},
		{name: "bare boolean", raw: `{"derive_from_go_modules":true}`, wantErr: true},
		{name: "not an object", raw: `[]`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := DerivationFromConfig(json.RawMessage(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if got.Enabled() != tc.enabled {
				t.Fatalf("Enabled() = %v, want %v", got.Enabled(), tc.enabled)
			}
		})
	}
}
