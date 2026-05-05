package workflow

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestCollectorModeValidate(t *testing.T) {
	t.Parallel()

	for _, mode := range []CollectorMode{
		CollectorModeContinuous,
		CollectorModeScheduled,
		CollectorModeManual,
	} {
		if err := mode.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v, want nil", mode, err)
		}
	}

	if err := CollectorMode("burst").Validate(); err == nil {
		t.Fatal("Validate(invalid collector mode) error = nil, want non-nil")
	}
}

func TestTriggerKindValidate(t *testing.T) {
	t.Parallel()

	for _, trigger := range []TriggerKind{
		TriggerKindBootstrap,
		TriggerKindSchedule,
		TriggerKindWebhook,
		TriggerKindReplay,
		TriggerKindOperatorRecovery,
	} {
		if err := trigger.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v, want nil", trigger, err)
		}
	}

	if err := TriggerKind("manual").Validate(); err == nil {
		t.Fatal("Validate(invalid trigger kind) error = nil, want non-nil")
	}
}

func TestRunStatusValidate(t *testing.T) {
	t.Parallel()

	for _, status := range []RunStatus{
		RunStatusCollectionPending,
		RunStatusCollectionActive,
		RunStatusCollectionComplete,
		RunStatusReducerConverging,
		RunStatusComplete,
		RunStatusFailed,
	} {
		if err := status.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v, want nil", status, err)
		}
	}

	if err := RunStatus("queued").Validate(); err == nil {
		t.Fatal("Validate(invalid run status) error = nil, want non-nil")
	}
}

func TestWorkItemStatusValidate(t *testing.T) {
	t.Parallel()

	for _, status := range []WorkItemStatus{
		WorkItemStatusPending,
		WorkItemStatusClaimed,
		WorkItemStatusCompleted,
		WorkItemStatusFailedRetryable,
		WorkItemStatusFailedTerminal,
		WorkItemStatusExpired,
	} {
		if err := status.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v, want nil", status, err)
		}
	}

	if err := WorkItemStatus("done").Validate(); err == nil {
		t.Fatal("Validate(invalid work item status) error = nil, want non-nil")
	}
}

func TestClaimStatusValidate(t *testing.T) {
	t.Parallel()

	for _, status := range []ClaimStatus{
		ClaimStatusActive,
		ClaimStatusCompleted,
		ClaimStatusFailedRetryable,
		ClaimStatusFailedTerminal,
		ClaimStatusExpired,
		ClaimStatusReleased,
	} {
		if err := status.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v, want nil", status, err)
		}
	}

	if err := ClaimStatus("stolen").Validate(); err == nil {
		t.Fatal("Validate(invalid claim status) error = nil, want non-nil")
	}
}

func TestRunValidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 13, 0, 0, 0, time.UTC)
	run := Run{
		RunID:       "run-1",
		TriggerKind: TriggerKindBootstrap,
		Status:      RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := run.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}

	run.RunID = ""
	if err := run.Validate(); err == nil {
		t.Fatal("Validate(blank run id) error = nil, want non-nil")
	}
}

func TestWorkItemValidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 13, 0, 0, 0, time.UTC)
	item := WorkItem{
		WorkItemID:          "item-1",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		SourceSystem:        "git",
		ScopeID:             "scope-1",
		AcceptanceUnitID:    "repository:repo-1",
		SourceRunID:         "source-run-1",
		GenerationID:        "generation-1",
		Status:              WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := item.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}

	item.CollectorKind = ""
	if err := item.Validate(); err == nil {
		t.Fatal("Validate(blank collector kind) error = nil, want non-nil")
	}
}

func TestWorkItemValidateRequiresPhaseStateIdentity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 13, 30, 0, 0, time.UTC)
	valid := WorkItem{
		WorkItemID:          "item-1",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		SourceSystem:        "git",
		ScopeID:             "scope-1",
		AcceptanceUnitID:    "repository:repo-1",
		SourceRunID:         "source-run-1",
		GenerationID:        "generation-1",
		Status:              WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	tests := []struct {
		name string
		edit func(*WorkItem)
	}{
		{
			name: "blank source system",
			edit: func(item *WorkItem) {
				item.SourceSystem = ""
			},
		},
		{
			name: "blank acceptance unit",
			edit: func(item *WorkItem) {
				item.AcceptanceUnitID = ""
			},
		},
		{
			name: "blank source run",
			edit: func(item *WorkItem) {
				item.SourceRunID = ""
			},
		},
		{
			name: "blank generation",
			edit: func(item *WorkItem) {
				item.GenerationID = ""
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			item := valid
			tt.edit(&item)
			if err := item.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
		})
	}
}

func TestClaimValidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 13, 0, 0, 0, time.UTC)
	claim := Claim{
		ClaimID:        "claim-1",
		WorkItemID:     "item-1",
		FencingToken:   1,
		OwnerID:        "collector-git-default",
		Status:         ClaimStatusActive,
		ClaimedAt:      now,
		HeartbeatAt:    now,
		LeaseExpiresAt: now.Add(time.Minute),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := claim.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}

	claim.FencingToken = 0
	if err := claim.Validate(); err == nil {
		t.Fatal("Validate(non-positive fencing token) error = nil, want non-nil")
	}
}
