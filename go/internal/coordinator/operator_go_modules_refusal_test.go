// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/gomodules"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestOperatorPlanNotEligibleVariants(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		repository string
		reads      map[string]gomodules.GoModuleRepositoryRead
		noGen      map[string]bool
		wantReason string
	}{
		{
			name:       "unknown repository",
			repository: "repository:r_00000bad",
			reads:      operatorFleetReads(),
			wantReason: "repository_not_found",
		},
		{
			name:       "no active generation",
			repository: "repository:r_00000001",
			reads:      operatorFleetReads(),
			noGen:      map[string]bool{"repository:r_00000001": true},
			wantReason: "no_active_generation",
		},
		{
			name:       "no go.mod and no evidence",
			repository: "repository:r_0000000c",
			reads: map[string]gomodules.GoModuleRepositoryRead{
				"repository:r_0000000c": {
					Repository: scannercomponent.GoModuleRepository{
						RepositoryID:    "repository:r_0000000c",
						RemoteURL:       "https://github.com/eshu-hq/plain",
						SourceCommitSHA: testGoCommit,
					},
					TotalCandidates: 257,
				},
			},
			wantReason: "no_go_module_or_evidence",
		},
		{
			name:       "unsupported remote surfaces the planner reason",
			repository: "repository:r_0000000a",
			reads: map[string]gomodules.GoModuleRepositoryRead{
				"repository:r_0000000a": {
					Repository: scannercomponent.GoModuleRepository{
						RepositoryID:    "repository:r_0000000a",
						RemoteURL:       "/srv/repos/local",
						SourceCommitSHA: testGoCommit,
					},
					HasGoModule:     true,
					TotalCandidates: 257,
				},
			},
			wantReason: "unsupported_remote",
		},
		{
			name:       "missing commit surfaces the planner reason",
			repository: "repository:r_0000000b",
			reads: map[string]gomodules.GoModuleRepositoryRead{
				"repository:r_0000000b": {
					Repository: scannercomponent.GoModuleRepository{
						RepositoryID: "repository:r_0000000b",
						RemoteURL:    "https://github.com/eshu-hq/nocommit",
					},
					HasGoModule:     true,
					TotalCandidates: 257,
				},
			},
			wantReason: "missing_commit",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{instances: []workflow.CollectorInstance{operatorInstance(now)}}
			reader := &fakeOperatorRepositoryReader{
				reads:        test.reads,
				planned:      map[string]bool{},
				noGeneration: test.noGen,
			}
			var logs bytes.Buffer
			service := operatorService(t, now, store, reader, &fakeGoEvidenceFencer{}, &logs)
			request := operatorRequest()
			request.RepositoryID = test.repository
			result, err := service.PlanGoReachabilityScopeNow(context.Background(), request)
			if err != nil {
				t.Fatalf("PlanGoReachabilityScopeNow() error = %v, want a not_eligible outcome", err)
			}
			if result.Outcome != OperatorPlanNotEligible {
				t.Fatalf("outcome = %q, want %q", result.Outcome, OperatorPlanNotEligible)
			}
			if result.Reason != test.wantReason {
				t.Fatalf("reason = %q, want %q", result.Reason, test.wantReason)
			}
			if len(store.createdRuns) != 0 || len(store.enqueuedItems) != 0 {
				t.Fatalf("ineligible plan wrote runs=%d items=%d, want zero", len(store.createdRuns), len(store.enqueuedItems))
			}
		})
	}
}

// TestOperatorPlanFenceErrorFailsTheAttempt proves a fence failure fails the
// attempt before anything is admitted.
func TestOperatorPlanFenceErrorFailsTheAttempt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{instances: []workflow.CollectorInstance{operatorInstance(now)}}
	reader := &fakeOperatorRepositoryReader{reads: operatorFleetReads(), planned: map[string]bool{}}
	fencer := &fakeGoEvidenceFencer{err: errors.New("fence exploded")}
	var logs bytes.Buffer
	service := operatorService(t, now, store, reader, fencer, &logs)

	if _, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest()); err == nil {
		t.Fatal("fence error = nil, want the attempt to fail")
	}
	if len(store.createdRuns) != 0 || len(store.enqueuedItems) != 0 {
		t.Fatalf("failed fence wrote runs=%d items=%d, want zero", len(store.createdRuns), len(store.enqueuedItems))
	}
}

// TestOperatorPlanRefusesBadInstances proves the durable-set resolution gates:
// zero matches, multiple matches, a disabled derivation, a host mismatch, and
// a bad interval all refuse before any read.
func TestOperatorPlanRefusesBadInstances(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	newService := func(instances []workflow.CollectorInstance) (Service, *fakeStore, *fakeOperatorRepositoryReader) {
		store := &fakeStore{instances: instances}
		reader := &fakeOperatorRepositoryReader{reads: operatorFleetReads(), planned: map[string]bool{}}
		var logs bytes.Buffer
		return operatorService(t, now, store, reader, &fakeGoEvidenceFencer{}, &logs), store, reader
	}
	t.Run("zero matches", func(t *testing.T) {
		t.Parallel()
		service, _, reader := newService(nil)
		if _, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest()); err == nil {
			t.Fatal("zero instances = nil, want a refusal")
		}
		if len(reader.getCalls) != 0 {
			t.Fatal("refused instance still read the repository")
		}
	})
	t.Run("multiple matches", func(t *testing.T) {
		t.Parallel()
		instance := operatorInstance(now)
		service, _, _ := newService([]workflow.CollectorInstance{instance, instance})
		if _, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest()); err == nil {
			t.Fatal("two instances = nil, want a refusal")
		}
	})
	t.Run("derivation disabled", func(t *testing.T) {
		t.Parallel()
		instance := operatorInstance(now)
		instance.Configuration = goModulesActivationConfiguration(
			`{"derive_from_go_modules":{"enabled":false,"scan_interval":"1h"}}`, "scanner_worker", "scanner_worker")
		service, _, _ := newService([]workflow.CollectorInstance{instance})
		if _, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest()); err == nil {
			t.Fatal("disabled derivation = nil, want a refusal")
		}
	})
	t.Run("host mismatch", func(t *testing.T) {
		t.Parallel()
		instance := operatorInstance(now)
		instance.Configuration = goModulesActivationConfiguration(testGoModulesBlock, "git", "repository")
		service, _, _ := newService([]workflow.CollectorInstance{instance})
		if _, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest()); err == nil {
			t.Fatal("host mismatch = nil, want a refusal")
		}
	})
	t.Run("bad interval", func(t *testing.T) {
		t.Parallel()
		instance := operatorInstance(now)
		instance.Configuration = goModulesActivationConfiguration(
			`{"derive_from_go_modules":{"enabled":true,"scan_interval":"45s"}}`, "scanner_worker", "scanner_worker")
		service, _, _ := newService([]workflow.CollectorInstance{instance})
		if _, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest()); err == nil {
			t.Fatal("bad interval = nil, want a refusal")
		}
	})
}

// TestOperatorPlanRefusesNilStore proves a service without the control store
// fails closed instead of panicking on the instance resolution read.
func TestOperatorPlanRefusesNilStore(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	reader := &fakeOperatorRepositoryReader{reads: operatorFleetReads(), planned: map[string]bool{}}
	var logs bytes.Buffer
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive, ClaimsEnabled: true, ReconcileInterval: time.Hour,
		},
		GoModuleRepositoryReader:       reader,
		GoModuleSingleRepositoryReader: reader,
		GoEvidenceFencer:               &fakeGoEvidenceFencer{},
		Clock:                          func() time.Time { return now },
	}
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	if _, err := service.PlanGoReachabilityScopeNow(context.Background(), operatorRequest()); err == nil {
		t.Fatal("nil store = nil, want a refusal")
	}
	if len(reader.getCalls) != 0 {
		t.Fatal("refused attempt still read the repository")
	}
}
