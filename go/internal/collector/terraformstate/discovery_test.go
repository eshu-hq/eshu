package terraformstate

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestDiscoveryResolvesExplicitSeedsWhenGraphCold(t *testing.T) {
	t.Parallel()

	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Seeds: []DiscoverySeed{
				{
					Kind:   BackendLocal,
					Path:   "/tmp/eshu/prod.tfstate",
					RepoID: "platform-infra",
				},
				{
					Kind:   BackendS3,
					Bucket: "app-tfstate-prod",
					Key:    "services/api/terraform.tfstate",
					Region: "us-east-1",
					RepoID: "platform-infra",
				},
			},
		},
		BackendFacts: &stubBackendFactReader{},
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if got, want := len(candidates), 2; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got, want := candidates[0].Source, DiscoveryCandidateSourceSeed; got != want {
		t.Fatalf("candidates[0].Source = %q, want %q", got, want)
	}
	if got, want := candidates[0].State.BackendKind, BackendLocal; got != want {
		t.Fatalf("candidates[0].State.BackendKind = %q, want %q", got, want)
	}
	if got, want := candidates[1].State.Locator, "s3://app-tfstate-prod/services/api/terraform.tfstate"; got != want {
		t.Fatalf("candidates[1].State.Locator = %q, want %q", got, want)
	}
}

func TestDiscoveryWaitsForGitGenerationBeforeGraphDiscovery(t *testing.T) {
	t.Parallel()

	facts := &stubBackendFactReader{}
	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Graph:      true,
			LocalRepos: []string{"platform-infra"},
		},
		GitReadiness: &stubGitReadiness{ready: map[string]bool{"platform-infra": false}},
		BackendFacts: facts,
	}

	candidates, err := resolver.Resolve(context.Background())
	if !IsWaitingOnGitGeneration(err) {
		t.Fatalf("Resolve() error = %v, want waiting_on_git_generation", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("len(candidates) = %d, want 0", len(candidates))
	}
	if facts.calls != 0 {
		t.Fatalf("backend fact reader calls = %d, want 0 while waiting on Git", facts.calls)
	}
	var waiting WaitingOnGitGenerationError
	if !errors.As(err, &waiting) {
		t.Fatalf("Resolve() error type = %T, want WaitingOnGitGenerationError", err)
	}
	if got, want := waiting.Status(), "waiting_on_git_generation"; got != want {
		t.Fatalf("Status() = %q, want %q", got, want)
	}
}

func TestDiscoveryReturnsSeedsWhenGraphWaitsForGitGeneration(t *testing.T) {
	t.Parallel()

	facts := &stubBackendFactReader{}
	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Graph: true,
			Seeds: []DiscoverySeed{{
				Kind:   BackendS3,
				Bucket: "app-tfstate-prod",
				Key:    "services/api/terraform.tfstate",
				Region: "us-east-1",
				RepoID: "platform-infra",
			}},
			LocalRepos: []string{"platform-infra"},
		},
		GitReadiness: &stubGitReadiness{ready: map[string]bool{"platform-infra": false}},
		BackendFacts: facts,
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil for seed fallback", err)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got, want := candidates[0].Source, DiscoveryCandidateSourceSeed; got != want {
		t.Fatalf("candidates[0].Source = %q, want %q", got, want)
	}
	if facts.calls != 0 {
		t.Fatalf("backend fact reader calls = %d, want 0 while graph waits", facts.calls)
	}
}

func TestDiscoveryDoesNotReadGraphFactsWithoutRepoScope(t *testing.T) {
	t.Parallel()

	facts := &stubBackendFactReader{}
	resolver := DiscoveryResolver{
		Config:       DiscoveryConfig{Graph: true},
		BackendFacts: facts,
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("len(candidates) = %d, want 0", len(candidates))
	}
	if facts.calls != 0 {
		t.Fatalf("backend fact reader calls = %d, want 0 without repo scope", facts.calls)
	}
}

func TestDiscoveryReadsGraphFactsAfterGitGenerationCommitted(t *testing.T) {
	t.Parallel()

	facts := &stubBackendFactReader{
		candidates: []DiscoveryCandidate{{
			State: StateKey{
				BackendKind: BackendS3,
				Locator:     "s3://app-tfstate-prod/services/api/terraform.tfstate",
			},
			Source: DiscoveryCandidateSourceGraph,
			RepoID: "platform-infra",
		}},
	}
	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Graph:      true,
			LocalRepos: []string{"platform-infra"},
		},
		GitReadiness: &stubGitReadiness{ready: map[string]bool{"platform-infra": true}},
		BackendFacts: facts,
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if got, want := facts.calls, 1; got != want {
		t.Fatalf("backend fact reader calls = %d, want %d", got, want)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got, want := candidates[0].Source, DiscoveryCandidateSourceGraph; got != want {
		t.Fatalf("candidates[0].Source = %q, want %q", got, want)
	}
}

func TestDiscoveryRejectsGraphLocalCandidate(t *testing.T) {
	t.Parallel()

	resolver := DiscoveryResolver{
		Config:       DiscoveryConfig{Graph: true, LocalRepos: []string{"platform-infra"}},
		GitReadiness: &stubGitReadiness{ready: map[string]bool{"platform-infra": true}},
		BackendFacts: &stubBackendFactReader{
			candidates: []DiscoveryCandidate{{
				State: StateKey{
					BackendKind: BackendLocal,
					Locator:     "/tmp/eshu/prod.tfstate",
				},
				Source: DiscoveryCandidateSourceGraph,
				RepoID: "platform-infra",
			}},
		},
	}

	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("Resolve() error = nil, want graph-local candidate rejection")
	}
}

func TestDiscoveryRejectsGraphCandidateOutsideRepoScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		repoID string
	}{
		{name: "blank", repoID: ""},
		{name: "out of scope", repoID: "other-infra"},
		{name: "untrimmed", repoID: " platform-infra "},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resolver := DiscoveryResolver{
				Config:       DiscoveryConfig{Graph: true, LocalRepos: []string{"platform-infra"}},
				GitReadiness: &stubGitReadiness{ready: map[string]bool{"platform-infra": true}},
				BackendFacts: &stubBackendFactReader{
					candidates: []DiscoveryCandidate{{
						State: StateKey{
							BackendKind: BackendS3,
							Locator:     "s3://app-tfstate-prod/services/api/terraform.tfstate",
						},
						Source: DiscoveryCandidateSourceGraph,
						RepoID: tt.repoID,
					}},
				},
			}

			if _, err := resolver.Resolve(context.Background()); err == nil {
				t.Fatal("Resolve() error = nil, want repo-scope rejection")
			}
		})
	}
}

func TestDiscoveryDeduplicatesSeedAndGraphCandidates(t *testing.T) {
	t.Parallel()

	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Graph: true,
			Seeds: []DiscoverySeed{{
				Kind:   BackendS3,
				Bucket: "app-tfstate-prod",
				Key:    "services/api/terraform.tfstate",
				Region: "us-east-1",
				RepoID: "platform-infra",
			}},
			LocalRepos: []string{"platform-infra"},
		},
		GitReadiness: &stubGitReadiness{ready: map[string]bool{"platform-infra": true}},
		BackendFacts: &stubBackendFactReader{
			candidates: []DiscoveryCandidate{{
				State: StateKey{
					BackendKind: BackendS3,
					Locator:     "s3://app-tfstate-prod/services/api/terraform.tfstate",
				},
				Source: DiscoveryCandidateSourceGraph,
				RepoID: "platform-infra",
			}},
		},
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got, want := candidates[0].Source, DiscoveryCandidateSourceSeed; got != want {
		t.Fatalf("candidates[0].Source = %q, want seed to win over graph duplicate", got)
	}
}

func TestDiscoveryRejectsPrefixGraphCandidate(t *testing.T) {
	t.Parallel()

	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{Graph: true, LocalRepos: []string{"platform-infra"}},
		GitReadiness: &stubGitReadiness{
			ready: map[string]bool{"platform-infra": true},
		},
		BackendFacts: &stubBackendFactReader{
			candidates: []DiscoveryCandidate{{
				State: StateKey{
					BackendKind: BackendS3,
					Locator:     "s3://app-tfstate-prod/services/api/",
				},
				Source: DiscoveryCandidateSourceGraph,
				RepoID: "platform-infra",
			}},
		},
	}

	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("Resolve() error = nil, want prefix candidate rejection")
	}
}

func TestDiscoveryRejectsTerragruntGraphCandidateUntilResolved(t *testing.T) {
	t.Parallel()

	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{Graph: true, LocalRepos: []string{"platform-infra"}},
		GitReadiness: &stubGitReadiness{
			ready: map[string]bool{"platform-infra": true},
		},
		BackendFacts: &stubBackendFactReader{
			candidates: []DiscoveryCandidate{{
				State: StateKey{
					BackendKind: BackendTerragrunt,
					Locator:     "terragrunt://platform-infra/live/prod",
				},
				Source: DiscoveryCandidateSourceGraph,
				RepoID: "platform-infra",
			}},
		},
	}

	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("Resolve() error = nil, want unresolved terragrunt rejection")
	}
}

func TestDiscoveryRejectsUntrimmedGraphCandidateLocator(t *testing.T) {
	t.Parallel()

	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{Graph: true, LocalRepos: []string{"platform-infra"}},
		GitReadiness: &stubGitReadiness{
			ready: map[string]bool{"platform-infra": true},
		},
		BackendFacts: &stubBackendFactReader{
			candidates: []DiscoveryCandidate{{
				State: StateKey{
					BackendKind: BackendS3,
					Locator:     " s3://app-tfstate-prod/services/api/terraform.tfstate ",
				},
				Source: DiscoveryCandidateSourceGraph,
			}},
		},
	}

	if _, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatal("Resolve() error = nil, want untrimmed locator rejection")
	}
}

func TestDiscoveryRecordsCandidateMetricsBySource(t *testing.T) {
	t.Parallel()

	metrics := &stubDiscoveryMetrics{}
	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Graph:      true,
			LocalRepos: []string{"platform-infra"},
			Seeds: []DiscoverySeed{{
				Kind:   BackendS3,
				Bucket: "app-tfstate-prod",
				Key:    "services/api/terraform.tfstate",
				Region: "us-east-1",
			}},
		},
		BackendFacts: &stubBackendFactReader{
			candidates: []DiscoveryCandidate{{
				State: StateKey{
					BackendKind: BackendS3,
					Locator:     "s3://app-tfstate-prod/services/worker/terraform.tfstate",
				},
				Source: DiscoveryCandidateSourceGraph,
				RepoID: "platform-infra",
			}},
		},
		GitReadiness: &stubGitReadiness{
			ready: map[string]bool{"platform-infra": true},
		},
		Metrics: metrics,
	}

	if _, err := resolver.Resolve(context.Background()); err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if got, want := metrics.counts[DiscoveryCandidateSourceSeed], 1; got != want {
		t.Fatalf("seed metric count = %d, want %d", got, want)
	}
	if got, want := metrics.counts[DiscoveryCandidateSourceGraph], 1; got != want {
		t.Fatalf("graph metric count = %d, want %d", got, want)
	}
}

func TestParseDiscoveryConfigMapsCollectorJSON(t *testing.T) {
	t.Parallel()

	config, err := ParseDiscoveryConfig(`{
		"discovery": {
			"graph": true,
			"local_repos": ["platform-infra"],
			"seeds": [{
				"kind": "s3",
				"bucket": "app-tfstate-prod",
				"key": "services/api/terraform.tfstate",
				"region": "us-east-1",
				"repo_id": "platform-infra"
			}]
		}
	}`)
	if err != nil {
		t.Fatalf("ParseDiscoveryConfig() error = %v, want nil", err)
	}
	if !config.Graph {
		t.Fatal("Graph = false, want true")
	}
	if got, want := config.LocalRepos, []string{"platform-infra"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("LocalRepos = %#v, want %#v", got, want)
	}
	if got, want := config.Seeds[0].Kind, BackendS3; got != want {
		t.Fatalf("Seeds[0].Kind = %q, want %q", got, want)
	}
}

func TestNewDiscoveryMetricsPublishesCandidateCounter(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics, err := NewDiscoveryMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewDiscoveryMetrics() error = %v, want nil", err)
	}

	metrics.RecordCandidates(context.Background(), DiscoveryCandidateSourceSeed, 2)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got, want := discoveryCounterValue(t, rm, "eshu_dp_tfstate_discovery_candidates_total", "seed"), int64(2); got != want {
		t.Fatalf("candidate counter = %d, want %d", got, want)
	}
}

func TestIsWaitingOnGitGenerationMatchesPointerError(t *testing.T) {
	t.Parallel()

	err := &WaitingOnGitGenerationError{RepoIDs: []string{"platform-infra"}}

	if !IsWaitingOnGitGeneration(err) {
		t.Fatal("IsWaitingOnGitGeneration() = false, want true for pointer error")
	}
}

type stubGitReadiness struct {
	ready map[string]bool
	err   error
}

func (s *stubGitReadiness) GitGenerationCommitted(_ context.Context, repoID string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.ready[repoID], nil
}

type stubBackendFactReader struct {
	calls      int
	candidates []DiscoveryCandidate
	err        error
}

func (s *stubBackendFactReader) TerraformStateCandidates(
	context.Context,
	DiscoveryQuery,
) ([]DiscoveryCandidate, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.candidates, nil
}

type stubDiscoveryMetrics struct {
	counts map[DiscoveryCandidateSource]int
}

func (s *stubDiscoveryMetrics) RecordCandidates(_ context.Context, source DiscoveryCandidateSource, count int) {
	if s.counts == nil {
		s.counts = map[DiscoveryCandidateSource]int{}
	}
	s.counts[source] += count
}

func discoveryCounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	source string,
) int64 {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == "source" && attr.Value.AsString() == source {
						return point.Value
					}
				}
			}
		}
	}
	return 0
}
