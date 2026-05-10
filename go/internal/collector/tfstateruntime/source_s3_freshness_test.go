package tfstateruntime_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestDefaultSourceFactoryCarriesPriorETagIntoExactS3Read(t *testing.T) {
	t.Parallel()

	client := &recordingRuntimeS3Client{
		output: terraformstate.S3GetObjectOutput{
			Body: io.NopCloser(strings.NewReader(`{"serial":17}`)),
			Size: 13,
		},
	}
	factory := tfstateruntime.DefaultSourceFactory{S3Client: client}
	stateSource, err := factory.OpenSource(context.Background(), terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
		},
		Source:       terraformstate.DiscoveryCandidateSourceSeed,
		Region:       "us-east-1",
		PreviousETag: `"prior-etag"`,
	})
	if err != nil {
		t.Fatalf("OpenSource() error = %v, want nil", err)
	}

	reader, _, err := stateSource.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer func() { _ = reader.Close() }()

	if got, want := client.input.IfNoneMatch, `"prior-etag"`; got != want {
		t.Fatalf("IfNoneMatch = %q, want prior ETag %q", got, want)
	}
}

func TestDefaultSourceFactoryCarriesDynamoDBLockMetadataConfigIntoExactS3Read(t *testing.T) {
	t.Parallel()

	s3Client := &recordingRuntimeS3Client{
		output: terraformstate.S3GetObjectOutput{
			Body: io.NopCloser(strings.NewReader(`{"serial":17}`)),
			Size: 13,
		},
	}
	lockClient := &recordingRuntimeLockMetadataClient{
		output: terraformstate.LockMetadataOutput{Digest: "digest-123"},
	}
	factory := tfstateruntime.DefaultSourceFactory{
		S3Client:                s3Client,
		S3FallbackLockTableName: "tfstate-locks-fallback",
		S3LockMetadataClient:    lockClient,
	}
	stateSource, err := factory.OpenSource(context.Background(), terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
		},
		Source:        terraformstate.DiscoveryCandidateSourceSeed,
		Region:        "us-east-1",
		DynamoDBTable: "tfstate-locks-api",
	})
	if err != nil {
		t.Fatalf("OpenSource() error = %v, want nil", err)
	}

	reader, metadata, err := stateSource.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer func() { _ = reader.Close() }()

	if got, want := lockClient.input.TableName, "tfstate-locks-api"; got != want {
		t.Fatalf("lock input TableName = %q, want %q", got, want)
	}
	if got, want := lockClient.input.LockID, "tfstate-prod/services/api/terraform.tfstate-md5"; got != want {
		t.Fatalf("lock input LockID = %q, want %q", got, want)
	}
	if got, want := lockClient.input.Region, "us-east-1"; got != want {
		t.Fatalf("lock input Region = %q, want %q", got, want)
	}
	if got, want := metadata.LockDigest, "digest-123"; got != want {
		t.Fatalf("metadata.LockDigest = %q, want %q", got, want)
	}
}

func TestDefaultSourceFactoryUsesFallbackDynamoDBLockTableOnlyWhenCandidateOmitsIt(t *testing.T) {
	t.Parallel()

	s3Client := &recordingRuntimeS3Client{
		output: terraformstate.S3GetObjectOutput{
			Body: io.NopCloser(strings.NewReader(`{"serial":17}`)),
			Size: 13,
		},
	}
	lockClient := &recordingRuntimeLockMetadataClient{
		output: terraformstate.LockMetadataOutput{Digest: "digest-123"},
	}
	factory := tfstateruntime.DefaultSourceFactory{
		S3Client:                s3Client,
		S3FallbackLockTableName: "tfstate-locks-fallback",
		S3LockMetadataClient:    lockClient,
	}

	stateSource, err := factory.OpenSource(context.Background(), terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
		},
		Source: terraformstate.DiscoveryCandidateSourceSeed,
		Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("OpenSource() error = %v, want nil", err)
	}

	reader, _, err := stateSource.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer func() { _ = reader.Close() }()

	if got, want := lockClient.input.TableName, "tfstate-locks-fallback"; got != want {
		t.Fatalf("lock input TableName = %q, want fallback %q", got, want)
	}
}

func TestClaimedSourceReleasesS3NotModifiedClaimWhenPriorGenerationUnavailable(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
	}
	candidate := terraformstate.DiscoveryCandidate{
		State:  stateKey,
		Source: terraformstate.DiscoveryCandidateSourceSeed,
		Region: "us-east-1",
	}
	candidateID, err := terraformstate.CandidatePlanningID(candidate)
	if err != nil {
		t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
	}
	scopeValue, err := scope.NewTerraformStateSnapshotScope("", string(terraformstate.BackendS3), stateKey.Locator, nil)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	stateSource := &fakeStateSource{key: stateKey, openErr: terraformstate.ErrStateNotModified}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{{
					Kind:   terraformstate.BackendS3,
					Bucket: "tfstate-prod",
					Key:    "services/api/terraform.tfstate",
					Region: "us-east-1",
				}},
			},
		},
		SourceFactory: &fakeFactory{source: stateSource},
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-work-not-modified",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    scopeValue.PartitionKey,
		SourceRunID:         candidateID,
		GenerationID:        candidateID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil not-modified workflow outcome", err)
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want released claim until prior generation is known")
	}
	if collected.Unchanged {
		t.Fatal("CollectedGeneration.Unchanged = true, want no completed generation without prior identity")
	}
	if got, want := stateSource.opens, 1; got != want {
		t.Fatalf("source opens = %d, want %d", got, want)
	}
}

func TestClaimedSourceCompletesS3NotModifiedCandidateWhenPriorGenerationKnown(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 10, 30, 0, 0, time.UTC)
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
	}
	candidate := terraformstate.DiscoveryCandidate{
		State:  stateKey,
		Source: terraformstate.DiscoveryCandidateSourceSeed,
		Region: "us-east-1",
	}
	candidateID, err := terraformstate.CandidatePlanningID(candidate)
	if err != nil {
		t.Fatalf("CandidatePlanningID() error = %v, want nil", err)
	}
	scopeValue, err := scope.NewTerraformStateSnapshotScope("", string(terraformstate.BackendS3), stateKey.Locator, nil)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	priorGenerationID := "terraform_state:state_snapshot:s3:locator-hash:lineage-123:serial:17"
	stateSource := &fakeStateSource{key: stateKey, openErr: terraformstate.ErrStateNotModified}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{{
					Kind:   terraformstate.BackendS3,
					Bucket: "tfstate-prod",
					Key:    "services/api/terraform.tfstate",
					Region: "us-east-1",
				}},
			},
			PriorSnapshots: fakePriorSnapshotReader{
				metadata: map[terraformstate.StateKey]terraformstate.PriorSnapshotMetadata{
					stateKey: {
						ETag:         `"etag-123"`,
						GenerationID: priorGenerationID,
					},
				},
			},
		},
		SourceFactory: &fakeFactory{source: stateSource},
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-work-not-modified-prior",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    scopeValue.PartitionKey,
		SourceRunID:         candidateID,
		GenerationID:        candidateID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil not-modified workflow outcome", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want completed unchanged outcome")
	}
	if !collected.Unchanged {
		t.Fatal("CollectedGeneration.Unchanged = false, want true")
	}
	if got, want := stateSource.opens, 1; got != want {
		t.Fatalf("source opens = %d, want %d", got, want)
	}
}

type recordingRuntimeS3Client struct {
	input  terraformstate.S3GetObjectInput
	output terraformstate.S3GetObjectOutput
	err    error
}

func (c *recordingRuntimeS3Client) GetObject(
	ctx context.Context,
	input terraformstate.S3GetObjectInput,
) (terraformstate.S3GetObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return terraformstate.S3GetObjectOutput{}, err
	}
	c.input = input
	if c.err != nil {
		return terraformstate.S3GetObjectOutput{}, c.err
	}
	return c.output, nil
}

type recordingRuntimeLockMetadataClient struct {
	input  terraformstate.LockMetadataInput
	output terraformstate.LockMetadataOutput
	err    error
}

func (c *recordingRuntimeLockMetadataClient) ReadLockMetadata(
	ctx context.Context,
	input terraformstate.LockMetadataInput,
) (terraformstate.LockMetadataOutput, error) {
	if err := ctx.Err(); err != nil {
		return terraformstate.LockMetadataOutput{}, err
	}
	c.input = input
	if c.err != nil {
		return terraformstate.LockMetadataOutput{}, c.err
	}
	return c.output, nil
}

type fakePriorSnapshotReader struct {
	metadata map[terraformstate.StateKey]terraformstate.PriorSnapshotMetadata
}

func (r fakePriorSnapshotReader) TerraformStatePriorSnapshotMetadata(
	_ context.Context,
	states []terraformstate.StateKey,
) (map[terraformstate.StateKey]terraformstate.PriorSnapshotMetadata, error) {
	out := map[terraformstate.StateKey]terraformstate.PriorSnapshotMetadata{}
	for _, state := range states {
		if metadata, ok := r.metadata[state]; ok {
			out[state] = metadata
		}
	}
	return out, nil
}
