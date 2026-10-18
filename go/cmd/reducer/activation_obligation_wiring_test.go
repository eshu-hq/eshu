// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/obligation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// activationWiringDB satisfies db.ExecQueryer and, when transactional is
// set, db.Beginner; it fails the test on any statement.
type activationWiringDB struct {
	t *testing.T
}

func (d activationWiringDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	d.t.Fatal("wiring must not query")
	return nil, nil
}

func (d activationWiringDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	d.t.Fatal("wiring must not exec")
	return nil, nil
}

type activationWiringTxDB struct{ activationWiringDB }

func (d activationWiringTxDB) Begin(context.Context) (db.Transaction, error) {
	d.t.Fatal("wiring must not begin")
	return nil, nil
}

func activationEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestActivationObligationConsumerIsEnabledByDefault(t *testing.T) {
	t.Parallel()
	runner, err := activationObligationRunnerFor(activationEnv(map[string]string{}),
		activationWiringTxDB{activationWiringDB{t}}, nil, nil, nil)
	if err != nil || runner == nil {
		t.Fatalf("unset %s: runner=%v err=%v, want enabled by default", activationObligationConsumerEnabledEnv, runner, err)
	}
	for _, value := range []string{"false", "0"} {
		runner, err := activationObligationRunnerFor(activationEnv(map[string]string{
			activationObligationConsumerEnabledEnv: value,
		}), activationWiringTxDB{activationWiringDB{t}}, nil, nil, nil)
		if err != nil || runner != nil {
			t.Fatalf("%s=%q: runner=%v err=%v, want disabled", activationObligationConsumerEnabledEnv, value, runner, err)
		}
	}
	// Unparsable values fall back to the default (enabled), like the other
	// reducer maintenance knobs.
	runner, err = activationObligationRunnerFor(activationEnv(map[string]string{
		activationObligationConsumerEnabledEnv: "not-a-bool",
	}), activationWiringTxDB{activationWiringDB{t}}, nil, nil, nil)
	if err != nil || runner == nil {
		t.Fatalf("%s=%q: runner=%v err=%v, want enabled by fallback default", activationObligationConsumerEnabledEnv, "not-a-bool", runner, err)
	}
}

func TestActivationObligationConsumerWiresTheTargetedMaintainer(t *testing.T) {
	t.Parallel()
	runner, err := activationObligationRunnerFor(activationEnv(map[string]string{
		activationObligationConsumerEnabledEnv: "true",
	}), activationWiringTxDB{activationWiringDB{t}}, nil, nil, nil)
	if err != nil || runner == nil {
		t.Fatalf("enabled consumer: runner=%v err=%v", runner, err)
	}
	if _, ok := runner.Maintainer.(postgres.ActivationMaintainer); !ok {
		t.Fatalf("maintainer = %T, want the partition-scoped postgres.ActivationMaintainer", runner.Maintainer)
	}
	if _, ok := runner.Store.(activation.RunnerStore); !ok {
		t.Fatalf("store = %T, want activation.RunnerStore", runner.Store)
	}
	owner := runner.Config.Owner
	if !strings.HasPrefix(owner, "activation-obligation-consumer:") || !strings.Contains(owner, ":"+strconv.Itoa(os.Getpid())+":") {
		t.Fatalf("lease owner = %q, want a process-unique activation-obligation-consumer owner", owner)
	}
}

// TestActivationObligationConsumerKnobsFlowIntoConfig pins the #7638 item-2
// tuning knobs: valid durations and worker counts flow into the runner
// Config, while empty or unparsable values fall back to the owning package's
// defaults (lease 30m interim) without failing startup. A disabled consumer
// still yields no runner even when the knobs are set.
func TestActivationObligationConsumerKnobsFlowIntoConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		env         map[string]string
		wantLease   time.Duration
		wantPoll    time.Duration
		wantWorkers int
		wantNil     bool
	}{
		{
			name:        "defaults when knobs unset",
			env:         map[string]string{activationObligationConsumerEnabledEnv: "true"},
			wantLease:   obligation.DefaultActivationLease,
			wantPoll:    obligation.DefaultActivationPollInterval,
			wantWorkers: obligation.DefaultActivationWorkers,
		},
		{
			name: "valid overrides flow into config",
			env: map[string]string{
				activationObligationConsumerEnabledEnv: "true",
				activationObligationLeaseEnv:           "5m",
				activationObligationPollIntervalEnv:    "30s",
				activationObligationWorkersEnv:         "4",
			},
			wantLease:   5 * time.Minute,
			wantPoll:    30 * time.Second,
			wantWorkers: 4,
		},
		{
			name: "invalid values fall back to defaults",
			env: map[string]string{
				activationObligationConsumerEnabledEnv: "true",
				activationObligationLeaseEnv:           "not-a-duration",
				activationObligationPollIntervalEnv:    "0",
				activationObligationWorkersEnv:         "-2",
			},
			wantLease:   obligation.DefaultActivationLease,
			wantPoll:    obligation.DefaultActivationPollInterval,
			wantWorkers: obligation.DefaultActivationWorkers,
		},
		{
			name: "empty values fall back to defaults",
			env: map[string]string{
				activationObligationConsumerEnabledEnv: "true",
				activationObligationLeaseEnv:           "",
				activationObligationPollIntervalEnv:    " ",
				activationObligationWorkersEnv:         "banana",
			},
			wantLease:   obligation.DefaultActivationLease,
			wantPoll:    obligation.DefaultActivationPollInterval,
			wantWorkers: obligation.DefaultActivationWorkers,
		},
		{
			name: "disabled consumer yields no runner even with knobs set",
			env: map[string]string{
				activationObligationConsumerEnabledEnv: "false",
				activationObligationLeaseEnv:           "5m",
				activationObligationPollIntervalEnv:    "30s",
				activationObligationWorkersEnv:         "4",
			},
			wantNil: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner, err := activationObligationRunnerFor(activationEnv(tc.env),
				activationWiringTxDB{activationWiringDB{t}}, nil, nil, nil)
			if err != nil {
				t.Fatalf("runnerFor() error = %v, want nil", err)
			}
			if tc.wantNil {
				if runner != nil {
					t.Fatalf("disabled consumer: runner=%v, want nil", runner)
				}
				return
			}
			if runner == nil {
				t.Fatal("enabled consumer: runner is nil")
			}
			if runner.Config.Lease != tc.wantLease {
				t.Errorf("Lease = %v, want %v", runner.Config.Lease, tc.wantLease)
			}
			if runner.Config.PollInterval != tc.wantPoll {
				t.Errorf("PollInterval = %v, want %v", runner.Config.PollInterval, tc.wantPoll)
			}
			if runner.Config.Workers != tc.wantWorkers {
				t.Errorf("Workers = %v, want %v", runner.Config.Workers, tc.wantWorkers)
			}
		})
	}
}

func TestActivationObligationConsumerRefusesANonTransactionalDatabase(t *testing.T) {
	t.Parallel()
	runner, err := activationObligationRunnerFor(activationEnv(map[string]string{
		activationObligationConsumerEnabledEnv: "true",
	}), activationWiringDB{t}, nil, nil, nil)
	if err == nil || runner != nil {
		t.Fatalf("non-transactional database: runner=%v err=%v, want a startup error", runner, err)
	}
}

// obligationConsumerDisabledGetenv wraps a test getenv so the activation
// obligation consumer stays off; do not wrap an enabling getenv. The
// consumer is on by default since the #7638 flip, and the shared builder
// fakes are not transactional, so builder tests that do not exercise the
// consumer pin it off to keep their blank-env defaults meaningful.
func obligationConsumerDisabledGetenv(getenv func(string) string) func(string) string {
	return func(key string) string {
		if key == activationObligationConsumerEnabledEnv {
			return "false"
		}
		return getenv(key)
	}
}

// TestBuildReducerServiceStartsTheActivationObligationConsumerOnlyWhenEnabled
// pins the composition root: the Service carries the consumer only when the
// flag is explicitly true, and stays without one when it is explicitly false.
// A blank env means on-by-default since the #7638 flip, so the disabled case
// must pass "false", not "".
func TestBuildReducerServiceStartsTheActivationObligationConsumerOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		db := txFakeReducerDB{fakeReducerDB: &fakeReducerDB{}}
		getenv := func(key string) string {
			if key == activationObligationConsumerEnabledEnv {
				if enabled {
					return "true"
				}
				return "false"
			}
			return ""
		}
		service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{},
			postgres.NewSharedIntentStore(db.fakeReducerDB), stubCypherReader{}, stubCypherReader{}, getenv, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("enabled=%t: buildReducerService() error = %v", enabled, err)
		}
		if got := service.ActivationObligationRunner != nil; got != enabled {
			t.Fatalf("enabled=%t: Service.ActivationObligationRunner set = %t", enabled, got)
		}
	}
}

// txFakeReducerDB is fakeReducerDB with a Begin, the transactional shape the
// production SQLDB has; building the service never begins a transaction.
type txFakeReducerDB struct {
	*fakeReducerDB
}

func (txFakeReducerDB) Begin(context.Context) (db.Transaction, error) {
	return nil, errors.New("buildReducerService must not begin a transaction")
}
