// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
	"time"
)

// TestBootstrapOptionsFromEnv pins the #6956 knob contract for BOTH
// variables: unset leaves the package default (zero) in force, a duration
// is honored, and garbage, zero, and negative values are refused by name.
func TestBootstrapOptionsFromEnv(t *testing.T) {
	t.Parallel()
	for _, env := range []string{OwnershipWaitEnv, LockRetryBudgetEnv} {
		pick := func(options BootstrapOptions) time.Duration {
			if env == OwnershipWaitEnv {
				return options.OwnershipWait
			}
			return options.LockRetryBudget
		}
		for _, tc := range []struct {
			name   string
			value  string
			want   time.Duration
			refuse bool
		}{
			{"unset", "", 0, false},
			{"set", "3m", 3 * time.Minute, false},
			{"garbage", "soon", 0, true},
			{"zero", "0s", 0, true},
			{"negative", "-1s", 0, true},
		} {
			t.Run(env+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				got, err := BootstrapOptionsFromEnv(func(key string) string {
					if key == env {
						return tc.value
					}
					return ""
				})
				if tc.refuse {
					if err == nil || !strings.Contains(err.Error(), env) {
						t.Fatalf("err = %v, want a refusal naming %s", err, env)
					}
					return
				}
				if err != nil || pick(got) != tc.want {
					t.Fatalf("got %v, %v; want %v", pick(got), err, tc.want)
				}
			})
		}
	}
	// Both set at once thread independently.
	got, err := BootstrapOptionsFromEnv(func(key string) string {
		switch key {
		case OwnershipWaitEnv:
			return "2m"
		case LockRetryBudgetEnv:
			return "45s"
		}
		return ""
	})
	if err != nil || got.OwnershipWait != 2*time.Minute || got.LockRetryBudget != 45*time.Second {
		t.Fatalf("both set: got %+v, %v", got, err)
	}
}

// TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline pins the reason
// for the defaults: their sum must leave room for the migrations inside the
// chart's 600 s schema bootstrap Job deadline, or the holder diagnostic can
// never print before Kubernetes kills the Job.
func TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline(t *testing.T) {
	t.Parallel()
	const jobDeadline = 600 * time.Second
	c := schemaBootstrapCoordination{}.withDefaults()
	if c.ownershipWait+c.lockRetryBudget >= jobDeadline {
		t.Fatalf("defaults %s + %s do not fit inside the %s Job deadline", c.ownershipWait, c.lockRetryBudget, jobDeadline)
	}
	if c.ownershipWait <= 0 || c.lockRetryBudget <= 0 {
		t.Fatalf("defaults must be positive: %+v", c)
	}
	explicit := schemaBootstrapCoordination{ownershipWait: time.Minute, lockRetryBudget: 2 * time.Minute}.withDefaults()
	if explicit.ownershipWait != time.Minute || explicit.lockRetryBudget != 2*time.Minute {
		t.Fatalf("explicit bounds overwritten: %+v", explicit)
	}
}
