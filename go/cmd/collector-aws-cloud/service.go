package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
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
	return collector.ClaimedService{
		ControlStore: postgres.NewWorkflowControlStore(database),
		Source: awsruntime.ClaimedSource{
			Config:      config.AWS,
			Credentials: awsCredentialProvider{},
			Scanners:    scannerFactory{Tracer: tracer, Instruments: instruments},
			Tracer:      tracer,
			Instruments: instruments,
			Limiter:     limiter,
		},
		Committer:           committer,
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

type scannerFactory struct {
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
}

func (f scannerFactory) Scanner(
	_ context.Context,
	target awsruntime.Target,
	_ awscloud.Boundary,
	lease awsruntime.CredentialLease,
) (awsruntime.ServiceScanner, error) {
	awsLease, ok := lease.(*awsCredentialLease)
	if !ok {
		return nil, fmt.Errorf("unsupported AWS credential lease %T", lease)
	}
	switch target.ServiceKind {
	case awscloud.ServiceIAM:
		return iamservice.Scanner{Client: &iamClient{
			client:      awsiam.NewFromConfig(awsLease.config),
			target:      target,
			tracer:      f.Tracer,
			instruments: f.Instruments,
		}}, nil
	default:
		return nil, fmt.Errorf("unsupported AWS service_kind %q", target.ServiceKind)
	}
}

func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "aws-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("aws-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}
