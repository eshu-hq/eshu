package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ecr"
	"github.com/eshu-hq/eshu/go/internal/collector/sbomruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var fallbackClaimSequence uint64

func buildClaimedService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.ClaimedService, error) {
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	config.Source.Provider = newDocumentProvider(logger)
	source, err := sbomruntime.NewClaimedSource(config.Source)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return collector.ClaimedService{
		ControlStore:        postgres.NewWorkflowControlStore(database),
		Source:              source,
		Committer:           committer,
		CollectorKind:       scope.CollectorSBOMAttestation,
		CollectorInstanceID: config.Instance.InstanceID,
		OwnerID:             config.OwnerID,
		ClaimIDFunc:         newClaimID,
		PollInterval:        config.PollInterval,
		ClaimLeaseTTL:       config.ClaimLeaseTTL,
		HeartbeatInterval:   config.HeartbeatInterval,
		MaxAttempts:         workflow.DefaultClaimMaxAttempts(),
		Clock:               time.Now,
		Tracer:              tracer,
		Instruments:         instruments,
	}, nil
}

// newDocumentProvider builds the SBOM/attestation document provider with the
// ECR oci_referrer auth path wired in. provider=ecr referrer targets mint
// short-lived Distribution credentials from the AWS GetAuthorizationToken
// exchange using the AWS default credential chain; every other provider stays on
// the static-credential path.
func newDocumentProvider(logger *slog.Logger) sbomruntime.HTTPProvider {
	return sbomruntime.HTTPProvider{
		ClientFactory: sbomruntime.ECRReferrerClientFactory{
			AuthorizationClient: ecrAuthorizationClient,
			Logger:              logger,
		},
	}
}

// ecrAuthorizationClient loads the AWS default credential chain for one
// oci_referrer target and returns an ECR GetAuthorizationToken client. It
// mirrors the OCI registry collector's ECR wiring so both collectors authenticate
// to ECR the same way. Region and profile come from the target when set;
// otherwise the AWS default chain resolves them.
func ecrAuthorizationClient(ctx context.Context, target sbomruntime.TargetConfig) (ecr.AuthorizationTokenAPI, error) {
	options := make([]func(*awsconfig.LoadOptions) error, 0, 2)
	if target.Region != "" {
		options = append(options, awsconfig.WithRegion(target.Region))
	}
	if target.AWSProfile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(target.AWSProfile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config for ECR oci_referrer: %w", err)
	}
	return awsecr.NewFromConfig(cfg), nil
}

func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "sbom-attestation-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("sbom-attestation-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}
