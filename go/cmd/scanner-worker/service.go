package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var fallbackClaimSequence uint64

func buildService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (scannerworker.Service, error) {
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return scannerworker.Service{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return scannerworker.Service{
		ControlStore:        postgres.NewWorkflowControlStore(database),
		Committer:           committer,
		Analyzer:            scannerworker.WarningAnalyzer{Reason: "analyzer_not_configured"},
		AnalyzerKind:        config.Analyzer,
		CollectorInstanceID: config.Instance.InstanceID,
		OwnerID:             config.OwnerID,
		ClaimIDFunc:         newClaimID,
		PollInterval:        config.PollInterval,
		ClaimLeaseTTL:       config.ClaimLeaseTTL,
		HeartbeatInterval:   config.HeartbeatInterval,
		ResourceLimits:      config.Limits,
		Clock:               time.Now,
		Tracer:              tracer,
		Instruments:         instruments,
		Logger:              logger,
	}, nil
}

func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return string(scope.CollectorScannerWorker) + "-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("%s-claim-fallback-%d-%d", scope.CollectorScannerWorker, time.Now().UTC().UnixNano(), next)
}
