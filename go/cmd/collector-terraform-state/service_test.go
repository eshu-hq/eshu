package main

import (
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestBuildClaimedServiceWiresTerraformStateRuntime(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(
		postgres.SQLDB{},
		func(key string) string {
			values := map[string]string{
				"ESHU_COLLECTOR_INSTANCES_JSON":          singleTerraformStateInstanceJSON(),
				"ESHU_TFSTATE_COLLECTOR_OWNER_ID":        "worker-a",
				"ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL":   "5s",
				"ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL": "30s",
				"ESHU_TFSTATE_COLLECTOR_HEARTBEAT":       "10s",
				"ESHU_TFSTATE_REDACTION_KEY":             "test-redaction-key",
			}
			return values[key]
		},
		noop.NewTracerProvider().Tracer("test"),
		nil,
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}

	if got, want := service.CollectorKind, scope.CollectorTerraformState; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := service.CollectorInstanceID, "terraform-state-prod"; got != want {
		t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := service.OwnerID, "worker-a"; got != want {
		t.Fatalf("OwnerID = %q, want %q", got, want)
	}
	if got, want := service.PollInterval, 5*time.Second; got != want {
		t.Fatalf("PollInterval = %v, want %v", got, want)
	}
	if got, want := service.ClaimLeaseTTL, 30*time.Second; got != want {
		t.Fatalf("ClaimLeaseTTL = %v, want %v", got, want)
	}
	if got, want := service.HeartbeatInterval, 10*time.Second; got != want {
		t.Fatalf("HeartbeatInterval = %v, want %v", got, want)
	}
	if _, ok := service.ControlStore.(*postgres.WorkflowControlStore); !ok {
		t.Fatalf("ControlStore type = %T, want *postgres.WorkflowControlStore", service.ControlStore)
	}
	if _, ok := service.Committer.(postgres.IngestionStore); !ok {
		t.Fatalf("Committer type = %T, want postgres.IngestionStore", service.Committer)
	}
	source, ok := service.Source.(tfstateruntime.ClaimedSource)
	if !ok {
		t.Fatalf("Source type = %T, want tfstateruntime.ClaimedSource", service.Source)
	}
	if source.RedactionKey.IsZero() {
		t.Fatal("Source redaction key is zero, want configured key")
	}
	factory, ok := source.SourceFactory.(*targetScopeSourceFactory)
	if !ok {
		t.Fatalf("SourceFactory type = %T, want *targetScopeSourceFactory", source.SourceFactory)
	}
	if got, want := factory.config.DefaultCredentials.Mode, awsCredentialModeDefault; got != want {
		t.Fatalf("DefaultCredentials.Mode = %q, want %q", got, want)
	}
	if factory.s3Client(legacyAWSCredentialCacheKey, factory.config.DefaultCredentials) == nil {
		t.Fatal("source factory S3 client = nil, want read-only AWS adapter")
	}
	if factory.config.S3FallbackLockTableName != "" {
		t.Fatalf("S3FallbackLockTableName = %q, want blank without configured fallback", factory.config.S3FallbackLockTableName)
	}
	if factory.lockMetadataClient(legacyAWSCredentialCacheKey, factory.config.DefaultCredentials) == nil {
		t.Fatal("source factory lock metadata client = nil, want adapter available for candidate lock tables")
	}
	firstClaimID := service.ClaimIDFunc()
	secondClaimID := service.ClaimIDFunc()
	if firstClaimID == "" || secondClaimID == "" {
		t.Fatal("ClaimIDFunc returned blank claim id")
	}
	if firstClaimID == secondClaimID {
		t.Fatal("ClaimIDFunc returned duplicate claim ids")
	}
}

func TestBuildClaimedServiceWiresDynamoDBLockMetadataRuntime(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(
		postgres.SQLDB{},
		func(key string) string {
			values := map[string]string{
				"ESHU_COLLECTOR_INSTANCES_JSON": `[
					{
						"instance_id": "terraform-state-prod",
						"collector_kind": "terraform_state",
						"mode": "continuous",
						"enabled": true,
						"claims_enabled": true,
						"configuration": {
							"aws": {
								"dynamodb_table": "tfstate-locks"
							},
							"discovery": {
								"seeds": [
									{"kind": "local", "path": "/tmp/prod.tfstate"}
								]
							}
						}
					}
				]`,
				"ESHU_TFSTATE_COLLECTOR_OWNER_ID":        "worker-a",
				"ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL": "30s",
				"ESHU_TFSTATE_COLLECTOR_HEARTBEAT":       "10s",
				"ESHU_TFSTATE_REDACTION_KEY":             "test-redaction-key",
			}
			return values[key]
		},
		noop.NewTracerProvider().Tracer("test"),
		nil,
		nil,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}

	source, ok := service.Source.(tfstateruntime.ClaimedSource)
	if !ok {
		t.Fatalf("Source type = %T, want tfstateruntime.ClaimedSource", service.Source)
	}
	factory, ok := source.SourceFactory.(*targetScopeSourceFactory)
	if !ok {
		t.Fatalf("SourceFactory type = %T, want *targetScopeSourceFactory", source.SourceFactory)
	}
	if got, want := factory.config.S3FallbackLockTableName, "tfstate-locks"; got != want {
		t.Fatalf("S3FallbackLockTableName = %q, want %q", got, want)
	}
	if factory.lockMetadataClient(legacyAWSCredentialCacheKey, factory.config.DefaultCredentials) == nil {
		t.Fatal("S3LockMetadataClient = nil, want read-only DynamoDB adapter")
	}
}

var _ collector.ClaimedSource = tfstateruntime.ClaimedSource{}
