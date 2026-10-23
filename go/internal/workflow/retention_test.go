// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultWorkItemRetentionPolicyIsValidAndConservative(t *testing.T) {
	t.Parallel()

	policy := DefaultWorkItemRetentionPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("DefaultWorkItemRetentionPolicy().Validate() error = %v, want nil", err)
	}
	if policy.Age != 30*24*time.Hour {
		t.Fatalf("default Age = %s, want 720h", policy.Age)
	}
	if policy.BatchSize != 1000 {
		t.Fatalf("default BatchSize = %d, want 1000", policy.BatchSize)
	}
	if policy.MaxBatches != 100 {
		t.Fatalf("default MaxBatches = %d, want 100", policy.MaxBatches)
	}
	if policy.LockTimeout != 2*time.Second {
		t.Fatalf("default LockTimeout = %s, want 2s", policy.LockTimeout)
	}
}

func TestWorkItemRetentionPolicyValidateRejectsUnsafeBounds(t *testing.T) {
	t.Parallel()

	valid := DefaultWorkItemRetentionPolicy()
	cases := map[string]struct {
		mutate func(*WorkItemRetentionPolicy)
		want   string
	}{
		"age below one day": {
			mutate: func(p *WorkItemRetentionPolicy) { p.Age = 23 * time.Hour },
			want:   "age",
		},
		"zero batch": {
			mutate: func(p *WorkItemRetentionPolicy) { p.BatchSize = 0 },
			want:   "batch size",
		},
		"batch above ceiling": {
			mutate: func(p *WorkItemRetentionPolicy) { p.BatchSize = 10001 },
			want:   "batch size",
		},
		"zero max batches": {
			mutate: func(p *WorkItemRetentionPolicy) { p.MaxBatches = 0 },
			want:   "max batches",
		},
		"zero lock timeout": {
			mutate: func(p *WorkItemRetentionPolicy) { p.LockTimeout = 0 },
			want:   "lock timeout",
		},
		"lock timeout below one millisecond": {
			mutate: func(p *WorkItemRetentionPolicy) { p.LockTimeout = time.Microsecond },
			want:   "lock timeout",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			policy := valid
			tc.mutate(&policy)
			err := policy.Validate()
			if err == nil {
				t.Fatalf("Validate() error = nil, want error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestWorkItemRetentionPolicyCutoff(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 23, 12, 0, 0, 0, time.FixedZone("x", 3600))
	policy := DefaultWorkItemRetentionPolicy()
	want := time.Date(2026, 9, 23, 11, 0, 0, 0, time.UTC)
	if got := policy.Cutoff(asOf); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("Cutoff() = %s, want %s in UTC", got, want)
	}
}

func TestWorkItemRetentionResultDeletedTotal(t *testing.T) {
	t.Parallel()

	result := WorkItemRetentionResult{DeletedCompleted: 3, DeletedFailedTerminal: 4}
	if got := result.Deleted(); got != 7 {
		t.Fatalf("Deleted() = %d, want 7", got)
	}
}
