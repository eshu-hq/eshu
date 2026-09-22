// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline binds the
// defaults to the chart: both bounds are wall clock (the retry deadline is
// shared by every statement of the run and counts each attempt's
// lock_timeout), so their sum plus a floor for pod start, the migrations'
// own work and the graph schema must fit inside the schema bootstrap Job's
// activeDeadlineSeconds, read from deploy/helm/eshu/values.yaml, or the
// holder diagnostic can never print before Kubernetes kills the Job.
func TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline(t *testing.T) {
	t.Parallel()
	const workFloor = 4 * time.Minute // pod start, image pull, migrations, graph schema
	values, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "helm", "eshu", "values.yaml"))
	if err != nil {
		t.Fatalf("read chart values: %v", err)
	}
	match := regexp.MustCompile(`(?m)^schemaBootstrap:\n(?:[ \t]+.*\n)*?[ \t]+activeDeadlineSeconds:[ \t]*([0-9]+)`).FindSubmatch(values)
	if match == nil {
		t.Fatal("chart values.yaml has no schemaBootstrap.activeDeadlineSeconds")
	}
	seconds, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatalf("parse activeDeadlineSeconds: %v", err)
	}
	jobDeadline := time.Duration(seconds) * time.Second
	c := schemaBootstrapCoordination{}.withDefaults()
	if got := c.ownershipWait + c.lockRetryBudget + workFloor; got > jobDeadline {
		t.Fatalf("defaults %s + %s plus the %s work floor = %s exceed the chart's %s Job deadline", c.ownershipWait, c.lockRetryBudget, workFloor, got, jobDeadline)
	}
	if c.ownershipWait <= 0 || c.lockRetryBudget <= 0 {
		t.Fatalf("defaults must be positive: %+v", c)
	}
	explicit := schemaBootstrapCoordination{ownershipWait: time.Minute, lockRetryBudget: 2 * time.Minute}.withDefaults()
	if explicit.ownershipWait != time.Minute || explicit.lockRetryBudget != 2*time.Minute {
		t.Fatalf("explicit bounds overwritten: %+v", explicit)
	}
}
