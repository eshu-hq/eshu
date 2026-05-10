package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

var fallbackClaimSequence uint64

func buildClaimedService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	discoveryMetrics terraformstate.DiscoveryMetrics,
	logger *slog.Logger,
) (collector.ClaimedService, error) {
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	discoveryConfig, err := terraformstate.ParseDiscoveryConfig(config.Instance.Configuration)
	if err != nil {
		return collector.ClaimedService{}, err
	}

	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger

	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config:         discoveryConfig,
			GitReadiness:   postgres.TerraformStateGitReadinessChecker{DB: database},
			BackendFacts:   postgres.TerraformStateBackendFactReader{DB: database},
			PriorSnapshots: postgres.TerraformStatePriorSnapshotReader{DB: database},
			Tracer:         tracer,
			Metrics:        discoveryMetrics,
		},
		SourceFactory: tfstateruntime.DefaultSourceFactory{
			S3Client:                newAWSS3ObjectClient(config.AWSRoleARN),
			S3FallbackLockTableName: config.AWSDynamoDBLockTable,
			S3LockMetadataClient:    newAWSDynamoDBLockMetadataClient(config.AWSRoleARN),
			MaxBytes:                config.SourceMaxBytes,
		},
		RedactionKey: config.RedactionKey,
		Tracer:       tracer,
		Instruments:  instruments,
	}

	return collector.ClaimedService{
		ControlStore:        postgres.NewWorkflowControlStore(database),
		Source:              source,
		Committer:           committer,
		CollectorKind:       scope.CollectorTerraformState,
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
		return "tfstate-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("tfstate-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}
