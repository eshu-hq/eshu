package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var fallbackClaimSequence uint64

func buildClaimedService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	meter metric.Meter,
) (collector.ClaimedService, error) {
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	limiter := awsruntime.NewAccountLimiter(config.AWS.Targets)
	if err := telemetry.RegisterAWSClaimConcurrencyGauge(instruments, meter, limiter); err != nil {
		return collector.ClaimedService{}, fmt.Errorf("register AWS claim concurrency gauge: %w", err)
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	checkpoints := postgres.NewAWSPaginationCheckpointStore(database)
	checkpoints.Instruments = instruments
	scanStatus := postgres.NewAWSScanStatusStore(database)
	commitStatus := newAWSStatusCommitter(committer, scanStatus, config.Instance.InstanceID, time.Now)
	return collector.ClaimedService{
		ControlStore: postgres.NewWorkflowControlStore(database),
		Source: awsruntime.ClaimedSource{
			Config:      config.AWS,
			Credentials: awsruntime.SDKCredentialProvider{},
			Scanners: awsruntime.DefaultScannerFactory{
				Tracer:       tracer,
				Instruments:  instruments,
				Checkpoints:  checkpoints,
				RedactionKey: config.AWSRedactionKey,
			},
			Tracer:      tracer,
			Instruments: instruments,
			Limiter:     limiter,
			Checkpoints: checkpoints,
			ScanStatus:  scanStatus,
		},
		Committer:           commitStatus,
		CollectorKind:       scope.CollectorAWS,
		CollectorInstanceID: config.Instance.InstanceID,
		OwnerID:             config.OwnerID,
		ClaimIDFunc:         newClaimID,
		PollInterval:        config.PollInterval,
		ClaimLeaseTTL:       config.ClaimLeaseTTL,
		HeartbeatInterval:   config.HeartbeatInterval,
		Clock:               time.Now,
		Tracer:              tracer,
		Instruments:         instruments,
	}, nil
}

func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "aws-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("aws-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}
