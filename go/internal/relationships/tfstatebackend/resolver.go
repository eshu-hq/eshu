package tfstatebackend

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
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

// TerraformBackendRow is one sealed config-side row from a
// TerraformBackendQuery implementation. The shape mirrors the
// projector.TerraformBackend canonical row plus the owning repo/scope/commit
// metadata the resolver needs to anchor the join.
type TerraformBackendRow struct {
	RepoID           string
	ScopeID          string
	CommitID         string
	CommitObservedAt time.Time
	BackendKind      string
	LocatorHash      string
}

// TerraformBackendQuery is the narrow port the resolver depends on. The port
// returns every sealed TerraformBackend row whose (backend_kind, locator_hash)
// matches the input; the resolver groups, sorts, and selects. Keeping this
// interface in the resolver package avoids pulling the storage adapter into
// the relationships layer.
type TerraformBackendQuery interface {
	// ListTerraformBackendsByLocator returns the sealed config-side
	// TerraformBackend rows for the composite key. Implementations MUST NOT
	// pre-filter to a single owner — the resolver must observe ambiguity to
	// emit ErrAmbiguousBackendOwner.
	ListTerraformBackendsByLocator(
		ctx context.Context, backendKind string, locatorHash string,
	) ([]TerraformBackendRow, error)
}

// Resolver carries the canonical-row query port and exposes
// ResolveConfigCommitForBackend. Construct with NewResolver; a nil port is
// permitted and causes every resolve call to return
// ErrNoConfigRepoOwnsBackend (matches the Phase 0 stub contract for callers
// that have not yet wired the query backend).
type Resolver struct {
	query TerraformBackendQuery
}

// NewResolver constructs a Resolver around the supplied query port. A nil
// query yields a "no owner" resolver that always returns
// ErrNoConfigRepoOwnsBackend — useful for ingester and runtime paths that do
// not have the canonical-row reader wired yet.
func NewResolver(query TerraformBackendQuery) *Resolver {
	return &Resolver{query: query}
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
// whose terraform_backends parser fact matches the input composite key.
// Inputs must be non-blank. The resolver returns:
//
//   - ErrNoConfigRepoOwnsBackend when zero rows match (state is operator-owned
//     or the parser fact has not yet been ingested).
//   - ErrAmbiguousBackendOwner when two or more distinct RepoIDs claim the
//     composite key (single-owner-per-backend is the v1 policy).
//   - A populated CommitAnchor selected by max(CommitObservedAt) with
//     lexicographic-ascending CommitID tie-break.
//
// The "latest" rule is deterministic and ADR-able: no last-write-wins
// randomness.
func (r *Resolver) ResolveConfigCommitForBackend(
	ctx context.Context,
	backendKind string,
	locatorHash string,
) (CommitAnchor, error) {
	backendKind = strings.TrimSpace(backendKind)
	if backendKind == "" {
		return CommitAnchor{}, errors.New("backend kind must not be blank")
	}
	locatorHash = strings.TrimSpace(locatorHash)
	if locatorHash == "" {
		return CommitAnchor{}, errors.New("locator hash must not be blank")
	}
	if r == nil || r.query == nil {
		return CommitAnchor{}, ErrNoConfigRepoOwnsBackend
	}

	rows, err := r.query.ListTerraformBackendsByLocator(ctx, backendKind, locatorHash)
	if err != nil {
		return CommitAnchor{}, err
	}
	if len(rows) == 0 {
		return CommitAnchor{}, ErrNoConfigRepoOwnsBackend
	}

	// Group by RepoID; if more than one distinct repo owns the composite
	// key, return the ambiguous error rather than picking a winner.
	repoIDs := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		repoIDs[row.RepoID] = struct{}{}
	}
	if len(repoIDs) > 1 {
		return CommitAnchor{}, ErrAmbiguousBackendOwner
	}

	// Single-owner: pick the latest sealed snapshot.
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b TerraformBackendRow) int {
		if !a.CommitObservedAt.Equal(b.CommitObservedAt) {
			// Descending by observed_at: newer first.
			if a.CommitObservedAt.After(b.CommitObservedAt) {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.CommitID, b.CommitID)
	})
	winner := sorted[0]
	return CommitAnchor(winner), nil
}
