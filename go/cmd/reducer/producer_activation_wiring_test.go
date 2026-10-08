// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestProducerActivationConsumerIsDisabledByDefault(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "false", "0", "not-a-bool"} {
		runner, err := producerActivationRunnerFor(activationEnv(map[string]string{
			producerActivationConsumerEnabledEnv: value,
		}), activationWiringTxDB{activationWiringDB{t}}, nil, nil, nil)
		if err != nil || runner != nil {
			t.Fatalf("%s=%q: runner=%v err=%v, want disabled", producerActivationConsumerEnabledEnv, value, runner, err)
		}
	}
}

func TestProducerActivationConsumerWiresThePostgresStore(t *testing.T) {
	t.Parallel()
	runner, err := producerActivationRunnerFor(activationEnv(map[string]string{
		producerActivationConsumerEnabledEnv: "true",
	}), activationWiringTxDB{activationWiringDB{t}}, nil, nil, nil)
	if err != nil || runner == nil {
		t.Fatalf("enabled consumer: runner=%v err=%v", runner, err)
	}
	if _, ok := runner.Store.(postgres.ProducerActivationRunnerStore); !ok {
		t.Fatalf("store = %T, want postgres.ProducerActivationRunnerStore", runner.Store)
	}
	owner := runner.Config.Owner
	if !strings.HasPrefix(owner, "producer-activation-consumer:") || !strings.Contains(owner, ":"+strconv.Itoa(os.Getpid())+":") {
		t.Fatalf("lease owner = %q, want a process-unique producer-activation-consumer owner", owner)
	}
}

func TestProducerActivationConsumerRefusesANonTransactionalDatabase(t *testing.T) {
	t.Parallel()
	runner, err := producerActivationRunnerFor(activationEnv(map[string]string{
		producerActivationConsumerEnabledEnv: "true",
	}), activationWiringDB{t}, nil, nil, nil)
	if err == nil || runner != nil {
		t.Fatalf("non-transactional database: runner=%v err=%v, want a startup error", runner, err)
	}
}

// TestBuildReducerServiceStartsTheProducerActivationConsumerOnlyWhenEnabled
// pins the composition root: the Service carries the consumer only when the
// flag is true.
func TestBuildReducerServiceStartsTheProducerActivationConsumerOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		db := txFakeReducerDB{fakeReducerDB: &fakeReducerDB{}}
		getenv := func(key string) string {
			if enabled && key == producerActivationConsumerEnabledEnv {
				return "true"
			}
			return ""
		}
		service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{},
			postgres.NewSharedIntentStore(db.fakeReducerDB), stubCypherReader{}, stubCypherReader{}, getenv, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("enabled=%t: buildReducerService() error = %v", enabled, err)
		}
		if got := service.ProducerActivationRunner != nil; got != enabled {
			t.Fatalf("enabled=%t: Service.ProducerActivationRunner set = %t", enabled, got)
		}
	}
}
