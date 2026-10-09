// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/obligation"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/producer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// producerActivationConsumerEnabledEnv turns on the #7635 producer
// activation consumer. It is off by default until the default-on decision;
// while off, ProjectorQueue.Ack still writes obligations and they stay
// pending until retention removes their generation.
const producerActivationConsumerEnabledEnv = "ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED"

// producerActivationRunnerFor builds the producer activation consumer, or
// nil when it is disabled. The lease owner is process-unique so a restarted
// reducer never renews a dead process's lease.
func producerActivationRunnerFor(
	getenv func(string) string,
	database db.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (*producer.Runner, error) {
	if !loadBoolOrDefault(getenv, producerActivationConsumerEnabledEnv, false) {
		return nil, nil
	}
	storeDB, ok := database.(activation.Database)
	if !ok {
		return nil, fmt.Errorf("%s=true requires a database that supports transactions", producerActivationConsumerEnabledEnv)
	}
	return &producer.Runner{
		Store: postgres.ProducerActivationRunnerStore{
			Activation: activation.NewStore(storeDB),
			Ingestion:  postgres.NewIngestionStore(database),
		},
		Config: producer.Config{
			Owner: loadProcessUniqueProjectionLeaseOwner(getenv, "", "producer-activation-consumer"),
		},
		Instruments: instruments,
		Logger:      logger,
		Tracer:      tracer,
	}, nil
}

// activationRunners bundles the two activation consumers the reducer
// service wires: the exact-generation obligation consumer (#7584) and the
// producer activation consumer (#7635). Either runner is nil when its
// consumer is disabled.
type activationRunners struct {
	obligation *obligation.Runner
	producer   *producer.Runner
}

// activationRunnersFor builds both activation consumers in one call so the
// service constructor stays under the file cap.
func activationRunnersFor(
	getenv func(string) string,
	database db.ExecQueryer,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (activationRunners, error) {
	obligation, err := activationObligationRunnerFor(getenv, database, tracer, instruments, logger)
	if err != nil {
		return activationRunners{}, err
	}
	producer, err := producerActivationRunnerFor(getenv, database, tracer, instruments, logger)
	if err != nil {
		return activationRunners{}, err
	}
	return activationRunners{obligation: obligation, producer: producer}, nil
}
