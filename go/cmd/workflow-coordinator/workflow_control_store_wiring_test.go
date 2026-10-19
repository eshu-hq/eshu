// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestNewWorkflowControlStoreWiresInstruments guards the codex review finding
// on PR #4518: run() must assign the shared meter-provider Instruments onto
// the WorkflowControlStore it constructs, or the #4459 terminal-dead-letter
// operator metric (eshu_dp_workflow_run_terminal_dead_letter_blocks_total)
// never emits on the real production path even though ReconcileWorkflowRuns
// still fails and logs the blocked run correctly. This test fails if a
// future edit drops the store.Instruments assignment inside
// newWorkflowControlStore.
func TestNewWorkflowControlStoreWiresInstruments(t *testing.T) {
	instruments := &telemetry.Instruments{}

	store := newWorkflowControlStore(postgres.SQLDB{}, instruments, nil)

	if store == nil {
		t.Fatal("newWorkflowControlStore() = nil, want a constructed store")
	}
	if store.Instruments != instruments {
		t.Fatalf("store.Instruments = %p, want the same instance passed in (%p) — the production wiring must assign it, not leave it nil", store.Instruments, instruments)
	}
}

// TestNewWorkflowControlStoreAllowsNilInstruments proves the wiring helper
// does not require Instruments to be non-nil: WorkflowControlStore treats a
// nil Instruments as a no-op for the dead-letter-block metric (never a
// panic), so a caller without a wired meter provider is unaffected.
func TestNewWorkflowControlStoreAllowsNilInstruments(t *testing.T) {
	store := newWorkflowControlStore(postgres.SQLDB{}, nil, nil)

	if store == nil {
		t.Fatal("newWorkflowControlStore() = nil, want a constructed store")
	}
	if store.Instruments != nil {
		t.Fatalf("store.Instruments = %v, want nil when the caller passes nil", store.Instruments)
	}
	if store.QuotaLookup != nil {
		t.Fatal("store.QuotaLookup != nil, want nil when the caller passes nil (every claim takes the plain statement)")
	}
}

// TestNewWorkflowControlStoreWiresQuotaLookup proves the coordinator
// construction site carries a manifest-derived quota lookup into the store,
// so the gated claim path is reachable from construction: the lookup is
// built through component.Quota.Resolve (manifest declares, operator
// override wins per field), the store serves the resolved budget for the
// covered kind, and uncovered kinds stay unthrottled.
func TestNewWorkflowControlStoreWiresQuotaLookup(t *testing.T) {
	manifestLimit := 10
	overrideLimit := 5
	manifest := component.Quota{MaxGenerationsPerHour: &manifestLimit}
	newQuotaLookup := func(override component.Quota) workflow.QuotaLookup {
		effective := manifest.Resolve(override)
		return func(kind scope.CollectorKind) (workflow.ClaimQuota, bool) {
			if kind != scope.CollectorGit {
				return workflow.ClaimQuota{}, false
			}
			limit, ok := effective.GenerationsPerHourLimit()
			if !ok {
				return workflow.ClaimQuota{}, false
			}
			return workflow.ClaimQuota{MaxGenerationsPerHour: limit}, true
		}
	}

	store := newWorkflowControlStore(postgres.SQLDB{}, nil, newQuotaLookup(component.Quota{MaxGenerationsPerHour: &overrideLimit}))
	if store == nil {
		t.Fatal("newWorkflowControlStore() = nil, want a constructed store")
	}
	if store.QuotaLookup == nil {
		t.Fatal("store.QuotaLookup = nil, want the manifest-derived lookup (a nil lookup keeps every claim on the plain statement)")
	}
	quota, ok := store.QuotaLookup(scope.CollectorGit)
	if !ok {
		t.Fatal("store.QuotaLookup(git) ok = false, want true (manifest declares a quota)")
	}
	if quota.MaxGenerationsPerHour != overrideLimit {
		t.Fatalf("store.QuotaLookup(git) limit = %d, want %d (operator override wins per field)", quota.MaxGenerationsPerHour, overrideLimit)
	}
	if _, ok := store.QuotaLookup(scope.CollectorAWS); ok {
		t.Fatal("store.QuotaLookup(aws) ok = true, want false (uncovered kinds stay unthrottled)")
	}

	// Without an override the manifest declaration stands.
	manifestStore := newWorkflowControlStore(postgres.SQLDB{}, nil, newQuotaLookup(component.Quota{}))
	quota, ok = manifestStore.QuotaLookup(scope.CollectorGit)
	if !ok {
		t.Fatal("store.QuotaLookup(git) ok = false with empty override, want true (manifest declaration stands)")
	}
	if quota.MaxGenerationsPerHour != manifestLimit {
		t.Fatalf("store.QuotaLookup(git) limit = %d, want %d (empty override keeps the manifest declaration)", quota.MaxGenerationsPerHour, manifestLimit)
	}
}
