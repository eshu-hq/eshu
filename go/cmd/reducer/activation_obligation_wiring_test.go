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

func TestActivationObligationConsumerIsDisabledByDefault(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "false", "0", "not-a-bool"} {
		runner, err := activationObligationRunnerFor(activationEnv(map[string]string{
			activationObligationConsumerEnabledEnv: value,
		}), activationWiringTxDB{activationWiringDB{t}}, nil, nil, nil)
		if err != nil || runner != nil {
			t.Fatalf("%s=%q: runner=%v err=%v, want disabled", activationObligationConsumerEnabledEnv, value, runner, err)
		}
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

func TestActivationObligationConsumerRefusesANonTransactionalDatabase(t *testing.T) {
	t.Parallel()
	runner, err := activationObligationRunnerFor(activationEnv(map[string]string{
		activationObligationConsumerEnabledEnv: "true",
	}), activationWiringDB{t}, nil, nil, nil)
	if err == nil || runner != nil {
		t.Fatalf("non-transactional database: runner=%v err=%v, want a startup error", runner, err)
	}
}

// TestBuildReducerServiceStartsTheActivationObligationConsumerOnlyWhenEnabled
// pins the composition root: the Service carries the consumer only when the
// flag is true.
func TestBuildReducerServiceStartsTheActivationObligationConsumerOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		db := txFakeReducerDB{fakeReducerDB: &fakeReducerDB{}}
		getenv := func(key string) string {
			if enabled && key == activationObligationConsumerEnabledEnv {
				return "true"
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
