package workflow

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestFamilyFairnessSchedulerUsesWeightedRoundRobinAcrossFamilies(t *testing.T) {
	t.Parallel()

	scheduler, err := NewFamilyFairnessScheduler([]FairnessCandidate{
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "collector-git-primary", Weight: 2},
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "collector-aws-primary", Weight: 1},
		{CollectorKind: scope.CollectorWebhook, CollectorInstanceID: "collector-webhook-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFamilyFairnessScheduler() error = %v, want nil", err)
	}

	got := make([]scope.CollectorKind, 0, 8)
	for range 8 {
		target, ok := scheduler.Next()
		if !ok {
			t.Fatal("Next() ok = false, want true")
		}
		got = append(got, target.CollectorKind)
	}
	want := []scope.CollectorKind{
		scope.CollectorGit,
		scope.CollectorAWS,
		scope.CollectorWebhook,
		scope.CollectorGit,
		scope.CollectorGit,
		scope.CollectorAWS,
		scope.CollectorWebhook,
		scope.CollectorGit,
	}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q; full order = %#v", i, got[i], want[i], got)
		}
	}
}

func TestFamilyFairnessSchedulerRotatesInstancesInsideFamilyDeterministically(t *testing.T) {
	t.Parallel()

	scheduler, err := NewFamilyFairnessScheduler([]FairnessCandidate{
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "collector-git-b", Weight: 1},
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "collector-git-a", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFamilyFairnessScheduler() error = %v, want nil", err)
	}

	var got []string
	for range 4 {
		target, ok := scheduler.Next()
		if !ok {
			t.Fatal("Next() ok = false, want true")
		}
		got = append(got, target.CollectorInstanceID)
	}
	want := []string{"collector-git-a", "collector-git-b", "collector-git-a", "collector-git-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("instance order[%d] = %q, want %q; full order = %#v", i, got[i], want[i], got)
		}
	}
}

func TestFairnessCandidatesFromCollectorInstancesUsesEnabledClaimInstances(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 20, 21, 0, 0, 0, time.UTC)
	candidates, err := FairnessCandidatesFromCollectorInstances([]CollectorInstance{
		{
			InstanceID:     "collector-git-primary",
			CollectorKind:  scope.CollectorGit,
			Mode:           CollectorModeContinuous,
			Enabled:        true,
			ClaimsEnabled:  true,
			Configuration:  `{"fairness_weight":3}`,
			LastObservedAt: observedAt,
			CreatedAt:      observedAt,
			UpdatedAt:      observedAt,
		},
		{
			InstanceID:     "collector-aws-disabled",
			CollectorKind:  scope.CollectorAWS,
			Mode:           CollectorModeContinuous,
			Enabled:        false,
			ClaimsEnabled:  true,
			Configuration:  `{"fairness_weight":8}`,
			LastObservedAt: observedAt,
			CreatedAt:      observedAt,
			UpdatedAt:      observedAt,
		},
		{
			InstanceID:     "collector-webhook-no-claims",
			CollectorKind:  scope.CollectorWebhook,
			Mode:           CollectorModeContinuous,
			Enabled:        true,
			ClaimsEnabled:  false,
			Configuration:  `{}`,
			LastObservedAt: observedAt,
			CreatedAt:      observedAt,
			UpdatedAt:      observedAt,
		},
	})
	if err != nil {
		t.Fatalf("FairnessCandidatesFromCollectorInstances() error = %v, want nil", err)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got, want := candidates[0].CollectorInstanceID, "collector-git-primary"; got != want {
		t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := candidates[0].Weight, 3; got != want {
		t.Fatalf("Weight = %d, want %d", got, want)
	}
}

func TestFairnessCandidatesFromCollectorInstancesRejectsInvalidWeight(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 20, 21, 30, 0, 0, time.UTC)
	_, err := FairnessCandidatesFromCollectorInstances([]CollectorInstance{
		{
			InstanceID:     "collector-git-primary",
			CollectorKind:  scope.CollectorGit,
			Mode:           CollectorModeContinuous,
			Enabled:        true,
			ClaimsEnabled:  true,
			Configuration:  `{"fairness_weight":0}`,
			LastObservedAt: observedAt,
			CreatedAt:      observedAt,
			UpdatedAt:      observedAt,
		},
	})
	if err == nil {
		t.Fatal("FairnessCandidatesFromCollectorInstances() error = nil, want non-nil")
	}
}
