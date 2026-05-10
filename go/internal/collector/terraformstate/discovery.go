package terraformstate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const waitingOnGitGenerationStatus = "waiting_on_git_generation"

// DiscoveryCandidateSource identifies where one state candidate came from.
type DiscoveryCandidateSource string

const (
	// DiscoveryCandidateSourceSeed identifies an explicit operator seed.
	DiscoveryCandidateSourceSeed DiscoveryCandidateSource = "seed"
	// DiscoveryCandidateSourceGraph identifies Git-observed Terraform backend evidence.
	DiscoveryCandidateSourceGraph DiscoveryCandidateSource = "graph"
)

// DiscoveryConfig controls Terraform state candidate discovery.
type DiscoveryConfig struct {
	Graph      bool
	Seeds      []DiscoverySeed
	LocalRepos []string
}

// DiscoverySeed is one exact operator-approved state locator.
type DiscoverySeed struct {
	Kind          BackendKind
	Path          string
	RepoID        string
	Bucket        string
	Key           string
	Region        string
	VersionID     string
	DynamoDBTable string
	// PreviousETag is durable freshness metadata from a previous S3 read. It is
	// intentionally not populated from collector configuration JSON.
	PreviousETag string
}

// DiscoveryCandidate is one exact Terraform state object to inspect later.
type DiscoveryCandidate struct {
	State         StateKey
	Source        DiscoveryCandidateSource
	RepoID        string
	Region        string
	DynamoDBTable string
	PreviousETag  string
}

// DiscoveryQuery scopes graph-backed Terraform backend fact reads.
type DiscoveryQuery struct {
	RepoIDs []string
}

// GitReadinessChecker reports whether Git evidence for a repo is committed.
type GitReadinessChecker interface {
	GitGenerationCommitted(context.Context, string) (bool, error)
}

// BackendFactReader reads Git-observed Terraform backend facts.
type BackendFactReader interface {
	TerraformStateCandidates(context.Context, DiscoveryQuery) ([]DiscoveryCandidate, error)
}

// PriorSnapshotMetadataReader reads durable freshness metadata from already
// committed Terraform-state snapshot facts.
type PriorSnapshotMetadataReader interface {
	TerraformStatePriorSnapshotMetadata(context.Context, []StateKey) (map[StateKey]PriorSnapshotMetadata, error)
}

// PriorSnapshotMetadata carries freshness metadata safe to reuse for a later
// exact state read.
type PriorSnapshotMetadata struct {
	ETag string
}

// DiscoveryMetrics records resolved Terraform state discovery candidate counts.
type DiscoveryMetrics interface {
	RecordCandidates(context.Context, DiscoveryCandidateSource, int)
}

// DiscoveryResolver resolves exact Terraform state candidates without opening
// any raw state source.
type DiscoveryResolver struct {
	Config         DiscoveryConfig
	GitReadiness   GitReadinessChecker
	BackendFacts   BackendFactReader
	PriorSnapshots PriorSnapshotMetadataReader
	Tracer         trace.Tracer
	Metrics        DiscoveryMetrics
}

// WaitingOnGitGenerationError means graph discovery is blocked on Git evidence.
type WaitingOnGitGenerationError struct {
	RepoIDs []string
}

// Error implements error.
func (e WaitingOnGitGenerationError) Error() string {
	if len(e.RepoIDs) == 0 {
		return waitingOnGitGenerationStatus
	}
	return fmt.Sprintf("%s: %s", waitingOnGitGenerationStatus, strings.Join(e.RepoIDs, ","))
}

// Status returns the operator-facing waiting state.
func (e WaitingOnGitGenerationError) Status() string {
	return waitingOnGitGenerationStatus
}

// FailureClass returns the workflow retry classification for the waiting state.
func (e WaitingOnGitGenerationError) FailureClass() string {
	return e.Status()
}

// IsWaitingOnGitGeneration reports whether err is a Git-readiness wait.
func IsWaitingOnGitGeneration(err error) bool {
	var waitingValue WaitingOnGitGenerationError
	if errors.As(err, &waitingValue) {
		return true
	}
	var waitingPointer *WaitingOnGitGenerationError
	return errors.As(err, &waitingPointer)
}

// Resolve returns exact Terraform state candidates from explicit seeds and
// Git-observed backend facts. It does not open or probe state backends.
func (r DiscoveryResolver) Resolve(ctx context.Context) ([]DiscoveryCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.Tracer != nil {
		var span trace.Span
		ctx, span = r.Tracer.Start(ctx, telemetry.SpanTerraformStateDiscoveryResolve)
		defer span.End()
	}

	candidates := make([]DiscoveryCandidate, 0, len(r.Config.Seeds))
	counts := map[DiscoveryCandidateSource]int{}
	seen := map[string]struct{}{}
	for index, seed := range r.Config.Seeds {
		candidate, err := candidateFromSeed(seed)
		if err != nil {
			return nil, fmt.Errorf("terraform state seed %d: %w", index, err)
		}
		candidates = appendUniqueCandidate(candidates, seen, counts, candidate)
	}

	if !r.Config.Graph {
		return r.finishResolve(ctx, counts, candidates)
	}
	repoIDs := normalizedRepoIDs(r.Config.LocalRepos)
	if len(repoIDs) == 0 {
		return r.finishResolve(ctx, counts, candidates)
	}
	if err := r.requireGitReady(ctx, repoIDs); err != nil {
		if IsWaitingOnGitGeneration(err) && len(candidates) > 0 {
			return r.finishResolve(ctx, counts, candidates)
		}
		return nil, err
	}
	if r.BackendFacts == nil {
		return nil, fmt.Errorf("terraform state graph discovery requires backend fact reader")
	}
	graphCandidates, err := r.BackendFacts.TerraformStateCandidates(ctx, DiscoveryQuery{RepoIDs: repoIDs})
	if err != nil {
		return nil, fmt.Errorf("read terraform backend facts: %w", err)
	}
	for index, candidate := range graphCandidates {
		if candidate.Source == "" {
			candidate.Source = DiscoveryCandidateSourceGraph
		}
		if err := candidate.Validate(); err != nil {
			return nil, fmt.Errorf("terraform state graph candidate %d: %w", index, err)
		}
		if err := validateGraphCandidateScope(candidate, repoIDs); err != nil {
			return nil, fmt.Errorf("terraform state graph candidate %d: %w", index, err)
		}
		candidates = appendUniqueCandidate(candidates, seen, counts, candidate)
	}
	return r.finishResolve(ctx, counts, candidates)
}

func (r DiscoveryResolver) finishResolve(
	ctx context.Context,
	counts map[DiscoveryCandidateSource]int,
	candidates []DiscoveryCandidate,
) ([]DiscoveryCandidate, error) {
	var err error
	candidates, err = r.withPriorSnapshotMetadata(ctx, candidates)
	if err != nil {
		return nil, err
	}
	r.recordCandidates(ctx, counts)
	return candidates, nil
}

func (r DiscoveryResolver) withPriorSnapshotMetadata(
	ctx context.Context,
	candidates []DiscoveryCandidate,
) ([]DiscoveryCandidate, error) {
	if r.PriorSnapshots == nil || len(candidates) == 0 {
		return candidates, nil
	}
	states := make([]StateKey, 0, len(candidates))
	for _, candidate := range candidates {
		states = append(states, candidate.State)
	}
	metadata, err := r.PriorSnapshots.TerraformStatePriorSnapshotMetadata(ctx, states)
	if err != nil {
		return nil, fmt.Errorf("read terraform state prior snapshot metadata: %w", err)
	}
	for index := range candidates {
		prior := metadata[candidates[index].State]
		candidates[index].PreviousETag = prior.ETag
	}
	return candidates, nil
}

func (r DiscoveryResolver) recordCandidates(ctx context.Context, counts map[DiscoveryCandidateSource]int) {
	if r.Metrics == nil {
		return
	}
	for _, source := range []DiscoveryCandidateSource{
		DiscoveryCandidateSourceSeed,
		DiscoveryCandidateSourceGraph,
	} {
		if count := counts[source]; count > 0 {
			r.Metrics.RecordCandidates(ctx, source, count)
		}
	}
}

func (r DiscoveryResolver) requireGitReady(ctx context.Context, repoIDs []string) error {
	if len(repoIDs) == 0 {
		return nil
	}
	if r.GitReadiness == nil {
		return fmt.Errorf("terraform state graph discovery requires git readiness checker")
	}
	waiting := make([]string, 0)
	for _, repoID := range repoIDs {
		ready, err := r.GitReadiness.GitGenerationCommitted(ctx, repoID)
		if err != nil {
			return fmt.Errorf("check git generation readiness for %q: %w", repoID, err)
		}
		if !ready {
			waiting = append(waiting, repoID)
		}
	}
	if len(waiting) > 0 {
		return WaitingOnGitGenerationError{RepoIDs: waiting}
	}
	return nil
}

// Validate checks that the candidate names an exact state source.
func (c DiscoveryCandidate) Validate() error {
	if err := c.State.Validate(); err != nil {
		return err
	}
	if err := validateExactCandidateState(c.State); err != nil {
		return err
	}
	switch c.Source {
	case DiscoveryCandidateSourceSeed, DiscoveryCandidateSourceGraph:
	default:
		return fmt.Errorf("unsupported terraform state discovery source %q", c.Source)
	}
	if c.Source == DiscoveryCandidateSourceGraph && c.State.BackendKind == BackendLocal {
		return fmt.Errorf("local state candidates require an explicit operator seed")
	}
	if strings.TrimSpace(c.DynamoDBTable) != c.DynamoDBTable {
		return fmt.Errorf("terraform state dynamodb table must not have surrounding whitespace")
	}
	if c.DynamoDBTable != "" && c.State.BackendKind != BackendS3 {
		return fmt.Errorf("terraform state dynamodb table is only supported for s3 candidates")
	}
	return nil
}

func validateGraphCandidateScope(candidate DiscoveryCandidate, repoIDs []string) error {
	repoID := strings.TrimSpace(candidate.RepoID)
	if repoID == "" {
		return fmt.Errorf("graph candidate repo_id must not be blank")
	}
	if repoID != candidate.RepoID {
		return fmt.Errorf("graph candidate repo_id must not have surrounding whitespace")
	}
	for _, allowed := range repoIDs {
		if repoID == allowed {
			return nil
		}
	}
	return fmt.Errorf("graph candidate repo_id %q is outside requested repo scope", repoID)
}

func validateExactCandidateState(state StateKey) error {
	locator := strings.TrimSpace(state.Locator)
	if locator != state.Locator {
		return fmt.Errorf("terraform state source locator must not have surrounding whitespace")
	}
	versionID := strings.TrimSpace(state.VersionID)
	if versionID != state.VersionID {
		return fmt.Errorf("terraform state source version_id must not have surrounding whitespace")
	}
	switch state.BackendKind {
	case BackendLocal:
		if !strings.HasPrefix(locator, "/") {
			return fmt.Errorf("local state locator must be absolute")
		}
		if versionID != "" {
			return fmt.Errorf("local state version_id is unsupported")
		}
	case BackendS3:
		rest, ok := strings.CutPrefix(locator, "s3://")
		if !ok {
			return fmt.Errorf("s3 state locator must start with s3://")
		}
		bucket, key, ok := strings.Cut(rest, "/")
		if !ok || strings.TrimSpace(bucket) == "" || strings.TrimSpace(key) == "" {
			return fmt.Errorf("s3 state locator must include bucket and key")
		}
		if strings.HasSuffix(key, "/") {
			return fmt.Errorf("s3 state locator must name an exact object")
		}
	case BackendTerragrunt:
		return fmt.Errorf("terragrunt state candidates must be resolved to an exact backend before discovery emits them")
	default:
		return fmt.Errorf("unsupported terraform state backend kind %q", state.BackendKind)
	}
	return nil
}

func candidateFromSeed(seed DiscoverySeed) (DiscoveryCandidate, error) {
	kind := seed.Kind
	if kind == "" {
		return DiscoveryCandidate{}, fmt.Errorf("kind must not be blank")
	}
	switch kind {
	case BackendLocal:
		path := strings.TrimSpace(seed.Path)
		if path == "" {
			return DiscoveryCandidate{}, fmt.Errorf("local path must not be blank")
		}
		if !strings.HasPrefix(path, "/") {
			return DiscoveryCandidate{}, fmt.Errorf("local path must be absolute")
		}
		versionID := strings.TrimSpace(seed.VersionID)
		if versionID != seed.VersionID {
			return DiscoveryCandidate{}, fmt.Errorf("local version_id must not have surrounding whitespace")
		}
		if versionID != "" {
			return DiscoveryCandidate{}, fmt.Errorf("local version_id is unsupported")
		}
		return DiscoveryCandidate{
			State: StateKey{
				BackendKind: BackendLocal,
				Locator:     path,
			},
			Source: DiscoveryCandidateSourceSeed,
			RepoID: strings.TrimSpace(seed.RepoID),
		}, nil
	case BackendS3:
		bucket := strings.TrimSpace(seed.Bucket)
		key := strings.TrimSpace(seed.Key)
		region := strings.TrimSpace(seed.Region)
		if bucket == "" {
			return DiscoveryCandidate{}, fmt.Errorf("s3 bucket must not be blank")
		}
		if key == "" {
			return DiscoveryCandidate{}, fmt.Errorf("s3 key must not be blank")
		}
		if strings.HasSuffix(key, "/") {
			return DiscoveryCandidate{}, fmt.Errorf("s3 key must name an exact object")
		}
		if region == "" {
			return DiscoveryCandidate{}, fmt.Errorf("s3 region must not be blank")
		}
		return DiscoveryCandidate{
			State: StateKey{
				BackendKind: BackendS3,
				Locator:     "s3://" + bucket + "/" + key,
				VersionID:   strings.TrimSpace(seed.VersionID),
			},
			Source:        DiscoveryCandidateSourceSeed,
			RepoID:        strings.TrimSpace(seed.RepoID),
			Region:        region,
			DynamoDBTable: strings.TrimSpace(seed.DynamoDBTable),
			PreviousETag:  seed.PreviousETag,
		}, nil
	default:
		return DiscoveryCandidate{}, fmt.Errorf("kind %q is unsupported", kind)
	}
}

func appendUniqueCandidate(
	candidates []DiscoveryCandidate,
	seen map[string]struct{},
	counts map[DiscoveryCandidateSource]int,
	candidate DiscoveryCandidate,
) []DiscoveryCandidate {
	key := candidateKey(candidate.State)
	if _, ok := seen[key]; ok {
		return candidates
	}
	seen[key] = struct{}{}
	counts[candidate.Source]++
	return append(candidates, candidate)
}

func candidateKey(state StateKey) string {
	return string(state.BackendKind) + "\x00" + state.Locator + "\x00" + state.VersionID
}

func normalizedRepoIDs(repoIDs []string) []string {
	normalized := make([]string, 0, len(repoIDs))
	seen := map[string]struct{}{}
	for _, repoID := range repoIDs {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		normalized = append(normalized, repoID)
	}
	return normalized
}
