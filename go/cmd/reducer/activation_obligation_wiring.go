// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// activationObligationConsumerEnabledEnv turns on the #7584 activation
// obligation consumer. It is off by default until the default-on decision;
// while off, ProjectorQueue.Ack still writes obligations and they stay
// pending until retention removes their generation.
const activationObligationConsumerEnabledEnv = "ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED"

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
) (*maintenance.ActivationObligationRunner, error) {
	if !loadBoolOrDefault(getenv, activationObligationConsumerEnabledEnv, false) {
		return nil, nil
	}
	storeDB, ok := database.(activation.Database)
	if !ok {
		return nil, fmt.Errorf("%s=true requires a database that supports transactions", activationObligationConsumerEnabledEnv)
	}
	return &maintenance.ActivationObligationRunner{
		Store:      activation.RunnerStore{Store: activation.NewStore(storeDB)},
		Maintainer: postgres.NewActivationMaintainer(postgres.NewIngestionStore(database), tracer, instruments),
		Config: maintenance.ActivationObligationRunnerConfig{
			Owner: loadProcessUniqueProjectionLeaseOwner(getenv, "", "activation-obligation-consumer"),
		},
		Instruments: instruments,
		Logger:      logger,
		Tracer:      tracer,
	}, nil
}
