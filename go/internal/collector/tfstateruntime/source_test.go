package tfstateruntime_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceParsesResolvedCandidateMatchingClaim(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 21, 0, 0, 0, time.UTC)
	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
	}
	scopeValue, err := scope.NewTerraformStateSnapshotScope(
		"repo-scope-123",
		string(stateKey.BackendKind),
		stateKey.Locator,
		map[string]string{"repo_id": "platform-infra"},
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	generation, err := scope.NewTerraformStateSnapshotGeneration(scopeValue.ScopeID, 17, "lineage-123", observedAt)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	state := `{
		"format_version": "1.0",
		"terraform_version": "1.9.8",
		"serial": 17,
		"lineage": "lineage-123",
		"resources": [{
			"mode": "managed",
			"type": "aws_s3_bucket",
			"name": "logs",
			"instances": [{"attributes": {"arn": "arn:aws:s3:::logs"}}]
		}]
	}`
	factory := &fakeFactory{source: &fakeStateSource{key: stateKey, state: state, observedAt: observedAt}}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{{
					Kind:   terraformstate.BackendS3,
					Bucket: "tfstate-prod",
					Key:    "services/api/terraform.tfstate",
					Region: "us-east-1",
					RepoID: "platform-infra",
				}},
			},
		},
		SourceFactory: factory,
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-work-1",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         generation.GenerationID,
		GenerationID:        generation.GenerationID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Scope.ScopeID, scopeValue.ScopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := collected.Generation.GenerationID, generation.GenerationID; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := factory.source.opens, 2; got != want {
		t.Fatalf("source opens = %d, want %d", got, want)
	}
	envelopes := drainRuntimeFacts(t, collected.Facts)
	if len(envelopes) == 0 {
		t.Fatal("facts = 0, want terraform state facts")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.FencingToken, int64(42); got != want {
			t.Fatalf("FencingToken = %d, want %d", got, want)
		}
		if envelope.FactKind == facts.TerraformStateResourceFactKind {
			return
		}
	}
	t.Fatalf("facts did not include %s: %#v", facts.TerraformStateResourceFactKind, envelopes)
}

func TestClaimedSourceReturnsNoGenerationWhenClaimDoesNotMatchResolvedCandidate(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 21, 5, 0, 0, time.UTC)
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
	}
	factory := &fakeFactory{
		source: &fakeStateSource{
			key:        stateKey,
			state:      `{"serial":17,"lineage":"lineage-123","resources":[]}`,
			observedAt: observedAt,
		},
	}
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
		SourceFactory: factory,
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-work-1",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             "different-scope",
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         "different-generation",
		GenerationID:        "different-generation",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false for non-matching claim")
	}
}

func TestDefaultSourceFactoryOpensLocalCandidate(t *testing.T) {
	t.Parallel()

	path := writeRuntimeStateFile(t, `{"serial":17,"lineage":"lineage-123","resources":[]}`)
	factory := tfstateruntime.DefaultSourceFactory{}

	stateSource, err := factory.OpenSource(context.Background(), terraformstate.DiscoveryCandidate{
		State:  terraformstate.StateKey{BackendKind: terraformstate.BackendLocal, Locator: path},
		Source: terraformstate.DiscoveryCandidateSourceSeed,
	})
	if err != nil {
		t.Fatalf("OpenSource() error = %v, want nil", err)
	}
	if got, want := stateSource.Identity().Locator, path; got != want {
		t.Fatalf("Identity().Locator = %q, want %q", got, want)
	}
}

func TestDefaultSourceFactoryRequiresS3Client(t *testing.T) {
	t.Parallel()

	factory := tfstateruntime.DefaultSourceFactory{}

	_, err := factory.OpenSource(context.Background(), terraformstate.DiscoveryCandidate{
		State: terraformstate.StateKey{
			BackendKind: terraformstate.BackendS3,
			Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
		},
		Source: terraformstate.DiscoveryCandidateSourceSeed,
		Region: "us-east-1",
	})
	if err == nil {
		t.Fatal("OpenSource() error = nil, want missing S3 client error")
	}
}

func TestClaimedSourceRedactsSourceLocatorFromOpenError(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 21, 10, 0, 0, time.UTC)
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	locator := "s3://tfstate-prod/services/api/terraform.tfstate"
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
		SourceFactory: &failingFactory{err: errors.New("failed to open " + locator)},
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
	}

	_, _, err = source.NextClaimed(context.Background(), matchingRuntimeItem(t, locator, observedAt))
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want source error")
	}
	if strings.Contains(err.Error(), locator) {
		t.Fatalf("NextClaimed() error = %q, must not include raw locator", err.Error())
	}
}

func TestClaimedSourceDoesNotDoubleWrapDefaultFactoryErrors(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 21, 15, 0, 0, time.UTC)
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	locator := "s3://tfstate-prod/services/api/terraform.tfstate"
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
		SourceFactory: tfstateruntime.DefaultSourceFactory{},
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
	}

	_, _, err = source.NextClaimed(context.Background(), matchingRuntimeItem(t, locator, observedAt))
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want missing S3 client error")
	}
	if got := strings.Count(err.Error(), "build terraform state s3 source"); got != 1 {
		t.Fatalf("build source error prefixes = %d, want 1: %q", got, err.Error())
	}
}

type fakeFactory struct {
	source *fakeStateSource
}

func (f *fakeFactory) OpenSource(context.Context, terraformstate.DiscoveryCandidate) (terraformstate.StateSource, error) {
	return f.source, nil
}

type failingFactory struct {
	err error
}

func (f *failingFactory) OpenSource(context.Context, terraformstate.DiscoveryCandidate) (terraformstate.StateSource, error) {
	return nil, f.err
}

type fakeStateSource struct {
	key        terraformstate.StateKey
	state      string
	observedAt time.Time
	opens      int
}

func (s *fakeStateSource) Identity() terraformstate.StateKey {
	return s.key
}

func (s *fakeStateSource) Open(context.Context) (io.ReadCloser, terraformstate.SourceMetadata, error) {
	s.opens++
	return io.NopCloser(strings.NewReader(s.state)), terraformstate.SourceMetadata{
		ObservedAt: s.observedAt,
		Size:       int64(len(s.state)),
	}, nil
}

func drainRuntimeFacts(t *testing.T, factStream <-chan facts.Envelope) []facts.Envelope {
	t.Helper()
	var envelopes []facts.Envelope
	for envelope := range factStream {
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func writeRuntimeStateFile(t *testing.T, content string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "terraform-*.tfstate")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return file.Name()
}

func matchingRuntimeItem(t *testing.T, locator string, observedAt time.Time) workflow.WorkItem {
	t.Helper()
	scopeValue, err := scope.NewTerraformStateSnapshotScope("", string(terraformstate.BackendS3), locator, nil)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	generation, err := scope.NewTerraformStateSnapshotGeneration(scopeValue.ScopeID, 17, "lineage-123", observedAt)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}
	return workflow.WorkItem{
		WorkItemID:          "tfstate-work-error",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         generation.GenerationID,
		GenerationID:        generation.GenerationID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}
}
