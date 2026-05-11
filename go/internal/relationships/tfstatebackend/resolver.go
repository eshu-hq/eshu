package tfstatebackend

import (
	"context"
	"errors"
	"time"
)

// CommitAnchor identifies the latest sealed config snapshot whose
// terraform_backends parser fact matches a state snapshot's
// (backend_kind, locator_hash) composite key. The struct is the resolver's
// single output type; the drift handler converts it into EvidenceAtoms.
type CommitAnchor struct {
	// RepoID is the canonical repo identifier that owns the backend reference.
	RepoID string
	// ScopeID is the repo-snapshot ScopeID corresponding to CommitID.
	ScopeID string
	// CommitID is the commit hash of the selected sealed snapshot.
	CommitID string
	// CommitObservedAt is the snapshot seal time used for deterministic
	// latest-wins selection.
	CommitObservedAt time.Time
	// BackendKind is the Terraform backend kind that produced the join key
	// (s3, gcs, azurerm, local, remote, etc.).
	BackendKind string
	// LocatorHash is the safe locator hash that produced the join key.
	LocatorHash string
}

// ErrNoConfigRepoOwnsBackend means no sealed config snapshot has emitted a
// terraform_backends parser fact for the requested composite key. The drift
// handler MUST NOT classify drift in this case; the state may be operator
// owned outside Eshu's repo set.
var ErrNoConfigRepoOwnsBackend = errors.New("no config repo owns this backend")

// ErrAmbiguousBackendOwner means more than one distinct repo claims the same
// (backend_kind, locator_hash). The drift handler MUST reject the candidate
// with RejectionReasonStructuralMismatch and emit a structured log with
// failure_class="ambiguous_backend_owner". Future ADRs may add tie breaking.
var ErrAmbiguousBackendOwner = errors.New("ambiguous backend owner")

// ResolveConfigCommitForBackend selects the latest sealed config snapshot
// whose terraform_backends parser fact matches the input composite key. The
// stub returns ErrNoConfigRepoOwnsBackend; Phase 1 (Agent A) implements the
// canonical-row query against the projector's TerraformBackend table.
func ResolveConfigCommitForBackend(
	ctx context.Context,
	backendKind string,
	locatorHash string,
) (CommitAnchor, error) {
	_ = ctx
	_ = backendKind
	_ = locatorHash
	return CommitAnchor{}, ErrNoConfigRepoOwnsBackend
}
