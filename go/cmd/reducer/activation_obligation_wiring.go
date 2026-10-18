// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/obligation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// activationObligationConsumerEnabledEnv turns on the #7584 activation
	// obligation consumer. It is off by default until the default-on decision;
	// while off, ProjectorQueue.Ack still writes obligations and they stay
	// pending until retention removes their generation.
	activationObligationConsumerEnabledEnv = "ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED"
	// activationObligationLeaseEnv overrides how long one claim holds an
	// obligation (#7638 item 2, arbiter-ruled 30m interim). An unparsable
	// or non-positive value uses the default, as the other reducer
	// maintenance knobs do; lower the default back only when a rewrite
	// re-measures the pass p99 far below the lease-minus-a-fifth deadline.
	activationObligationLeaseEnv = "ESHU_ACTIVATION_OBLIGATION_LEASE"
	// activationObligationPollIntervalEnv overrides the wait after a cycle
	// that claimed nothing. An unparsable or non-positive value uses the
	// default, as the other reducer maintenance knobs do.
	activationObligationPollIntervalEnv = "ESHU_ACTIVATION_OBLIGATION_POLL_INTERVAL"
	// activationObligationWorkersEnv overrides the number of concurrent
	// claim loops. An unparsable or non-positive value uses the default, as
	// the other reducer maintenance knobs do.
	activationObligationWorkersEnv = "ESHU_ACTIVATION_OBLIGATION_WORKERS"
)

// activationObligationRunnerFor builds the activation obligation consumer,
// or nil when it is disabled. Its maintainer is the partition-scoped pass on
// the obligation's own (scope, generation); the whole-corpus pass is never
// wired here. The lease owner is process-unique so a restarted reducer never
// renews a dead process's lease.
func activationObligationRunnerFor(
	getenv func(string) string,
	database db.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (*obligation.Runner, error) {
	if !loadBoolOrDefault(getenv, activationObligationConsumerEnabledEnv, false) {
		return nil, nil
	}
	storeDB, ok := database.(activation.Database)
	if !ok {
		return nil, fmt.Errorf("%s=true requires a database that supports transactions", activationObligationConsumerEnabledEnv)
	}
	return &obligation.Runner{
		Store:      activation.RunnerStore{Store: activation.NewStore(storeDB)},
		Maintainer: postgres.NewActivationMaintainer(postgres.NewIngestionStore(database), tracer, instruments),
		Config: obligation.Config{
			Owner:        loadProcessUniqueProjectionLeaseOwner(getenv, "", "activation-obligation-consumer"),
			Lease:        loadDurationOrDefault(getenv, activationObligationLeaseEnv, obligation.DefaultActivationLease),
			PollInterval: loadDurationOrDefault(getenv, activationObligationPollIntervalEnv, obligation.DefaultActivationPollInterval),
			Workers:      loadPositiveIntOrDefault(getenv, activationObligationWorkersEnv, obligation.DefaultActivationWorkers),
		},
		Instruments: instruments,
		Logger:      logger,
		Tracer:      tracer,
	}, nil
}
