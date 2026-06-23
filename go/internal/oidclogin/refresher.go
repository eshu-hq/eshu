package oidclogin

import (
	"context"
	"errors"
	"sort"
	"time"
)

// DefaultRefreshBatchSize bounds how many stale OIDC sessions one refresh pass
// processes so the worker never performs an unbounded table scan.
const DefaultRefreshBatchSize = 200

// DefaultRefreshWindow is the bounded staleness window applied to a session's
// external provider proof when active-session refresh re-confirms authorization.
const DefaultRefreshWindow = 15 * time.Minute

// StaleSession is the hash-only projection of one OIDC-backed browser session
// whose external provider proof has reached its bounded staleness window. It
// carries only opaque identifiers and the last resolved grant snapshot; it never
// carries raw provider tokens, raw group names, emails, or private endpoints.
type StaleSession struct {
	SessionHash              string
	ExternalProviderConfigID string
	ExternalSubjectIDHash    string
	TenantID                 string
	WorkspaceID              string
	PolicyRevisionHash       string
	ExternalGroupHashes      []string
	RoleIDs                  []string
	AllScopes                bool
	AllowedScopeIDs          []string
	AllowedRepositoryIDs     []string
	ExternalAuthValidatedAt  time.Time
	ExternalAuthStaleAfter   time.Time
}

// SessionAuthProofUpdate is the new hash-only authorization snapshot and bounded
// proof window written back to one session when refresh re-confirms it.
type SessionAuthProofUpdate struct {
	SessionHash             string
	ExternalAuthValidatedAt time.Time
	ExternalAuthStaleAfter  time.Time
	PolicyRevisionHash      string
	ExternalGroupHashes     []string
	RoleIDs                 []string
	AllScopes               bool
	AllowedScopeIDs         []string
	AllowedRepositoryIDs    []string
	UpdatedAt               time.Time
}

// SessionRefreshStore is the bounded read/write surface the refresher needs over
// OIDC-backed browser sessions. Implementations must keep RevokeSession and
// UpdateSessionAuthProof idempotent under concurrent and duplicate delivery.
type SessionRefreshStore interface {
	// ListStaleSessions returns up to limit sessions whose proof is stale as of
	// the supplied time, oldest-stale first. limit is the conflict-free bound on
	// one pass.
	ListStaleSessions(ctx context.Context, asOf time.Time, limit int) ([]StaleSession, error)
	// RevokeSession marks one session revoked. It must be a no-op for a session
	// another worker already revoked.
	RevokeSession(ctx context.Context, sessionHash string, revokedAt time.Time) error
	// UpdateSessionAuthProof rewrites one session's bounded proof window and grant
	// snapshot. It must only touch an unrevoked OIDC session row.
	UpdateSessionAuthProof(ctx context.Context, update SessionAuthProofUpdate) error
}

// RoleGrantResolver re-resolves whether a session's previously granted Eshu
// stored external group hashes still map to concrete grants under current,
// untombstoned, unexpired policy. It is the Eshu-side invalidation hook for
// provider group removal, tombstoned mappings, expired mappings, revoked role
// targets, and policy revision drift. The refresher never reuses stale role IDs
// alone to extend a provider proof.
type RoleGrantResolver interface {
	ResolveGroupGrants(ctx context.Context, query GrantQuery) (GrantResolution, bool, error)
}

// ExternalSubjectLookup reports whether the hashed external subject is still an
// active, non-disabled, non-tombstoned identity for the provider config. It
// fails closed: a disabled or unknown subject must deny subsequent access.
type ExternalSubjectLookup interface {
	ExternalSubjectActive(ctx context.Context, providerConfigID string, subjectIDHash string) (bool, error)
}

// RefreshConfig configures one bounded active-session refresh.
type RefreshConfig struct {
	// BatchSize bounds sessions processed per pass. Zero uses DefaultRefreshBatchSize.
	BatchSize int
	// Window is the bounded proof staleness applied when a session is re-confirmed.
	// Zero uses DefaultRefreshWindow.
	Window time.Duration
	// SubjectLookup gates re-resolution on the external subject still being active.
	// When nil, subject-active enforcement is skipped and only grant re-resolution
	// and policy drift are checked.
	SubjectLookup ExternalSubjectLookup
}

// RefreshOutcome reports per-pass counts for telemetry and operator visibility.
type RefreshOutcome struct {
	Scanned             int
	Refreshed           int
	Revoked             int
	ProviderUnavailable int
}

// Refresher performs bounded active-session revocation refresh for OIDC-backed
// browser sessions. Each pass lists a bounded batch of stale sessions and, per
// session, either revokes it (external group removal reflected as lost grants,
// disabled subject, tombstoned/expired mapping, revoked role target, or policy
// revision drift) or extends its bounded proof window after re-confirming the
// Eshu-owned authorization snapshot.
type Refresher struct {
	store     SessionRefreshStore
	resolver  RoleGrantResolver
	lookup    ExternalSubjectLookup
	batchSize int
	window    time.Duration
	now       func() time.Time
}

// RefreshOption customizes Refresher behavior for tests and wiring.
type RefreshOption func(*Refresher)

// WithRefreshNow overrides the clock used for proof timestamps.
func WithRefreshNow(now func() time.Time) RefreshOption {
	return func(r *Refresher) {
		if now != nil {
			r.now = now
		}
	}
}

// NewRefresher constructs a bounded OIDC session revocation refresher.
func NewRefresher(
	store SessionRefreshStore,
	resolver RoleGrantResolver,
	config RefreshConfig,
	options ...RefreshOption,
) *Refresher {
	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultRefreshBatchSize
	}
	window := config.Window
	if window <= 0 {
		window = DefaultRefreshWindow
	}
	refresher := &Refresher{
		store:     store,
		resolver:  resolver,
		lookup:    config.SubjectLookup,
		batchSize: batchSize,
		window:    window,
		now:       func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		option(refresher)
	}
	return refresher
}

// RefreshOnce runs one bounded refresh pass and returns its outcome counts. It
// is safe to call concurrently and on a timer; revocations and proof updates are
// idempotent at the storage layer.
func (r *Refresher) RefreshOnce(ctx context.Context) (RefreshOutcome, error) {
	if r == nil || r.store == nil || r.resolver == nil {
		return RefreshOutcome{}, errors.New("oidc session refresher is not configured")
	}
	now := r.now().UTC()
	sessions, err := r.store.ListStaleSessions(ctx, now, r.batchSize)
	if err != nil {
		return RefreshOutcome{}, err
	}
	outcome := RefreshOutcome{Scanned: len(sessions)}
	for _, session := range sessions {
		decision, err := r.evaluate(ctx, session, now)
		if err != nil {
			// A provider/storage failure must not eagerly revoke. The request-time
			// fail-closed stale check still protects access until refresh recovers.
			outcome.ProviderUnavailable++
			continue
		}
		switch decision.action {
		case refreshActionRevoke:
			if err := r.store.RevokeSession(ctx, session.SessionHash, now); err != nil {
				outcome.ProviderUnavailable++
				continue
			}
			outcome.Revoked++
		case refreshActionExtend:
			if err := r.store.UpdateSessionAuthProof(ctx, decision.update); err != nil {
				outcome.ProviderUnavailable++
				continue
			}
			outcome.Refreshed++
		}
	}
	return outcome, nil
}

type refreshAction int

const (
	refreshActionRevoke refreshAction = iota
	refreshActionExtend
)

type refreshDecision struct {
	action refreshAction
	update SessionAuthProofUpdate
}

// evaluate decides whether a stale session is revoked or extended. It fails
// closed: a disabled subject denies access before grant re-resolution, and a
// session whose roles no longer map to grants or whose policy revision drifted
// is revoked. A provider or storage failure returns an error so the caller can
// classify it as provider-unavailable rather than revoke.
func (r *Refresher) evaluate(
	ctx context.Context,
	session StaleSession,
	now time.Time,
) (refreshDecision, error) {
	if r.lookup != nil {
		active, err := r.lookup.ExternalSubjectActive(
			ctx,
			session.ExternalProviderConfigID,
			session.ExternalSubjectIDHash,
		)
		if err != nil {
			return refreshDecision{}, err
		}
		if !active {
			return refreshDecision{action: refreshActionRevoke}, nil
		}
	}
	groupHashes := sortedCopy(session.ExternalGroupHashes)
	if len(groupHashes) == 0 {
		return refreshDecision{action: refreshActionRevoke}, nil
	}
	resolution, ok, err := r.resolver.ResolveGroupGrants(ctx, GrantQuery{
		ProviderConfigID: session.ExternalProviderConfigID,
		TenantID:         session.TenantID,
		WorkspaceID:      session.WorkspaceID,
		GroupHashes:      groupHashes,
		AsOf:             now,
	})
	if err != nil {
		return refreshDecision{}, err
	}
	if !ok || len(resolution.RoleIDs) == 0 {
		return refreshDecision{action: refreshActionRevoke}, nil
	}
	if resolution.PolicyRevisionHash != "" && session.PolicyRevisionHash != "" &&
		resolution.PolicyRevisionHash != session.PolicyRevisionHash {
		// Eshu workspace policy revision drifted under the session; force re-login.
		return refreshDecision{action: refreshActionRevoke}, nil
	}
	policyRevisionHash := resolution.PolicyRevisionHash
	if policyRevisionHash == "" {
		policyRevisionHash = session.PolicyRevisionHash
	}
	return refreshDecision{
		action: refreshActionExtend,
		update: SessionAuthProofUpdate{
			SessionHash:             session.SessionHash,
			ExternalAuthValidatedAt: now,
			ExternalAuthStaleAfter:  now.Add(r.window),
			PolicyRevisionHash:      policyRevisionHash,
			ExternalGroupHashes:     groupHashes,
			RoleIDs:                 sortedCopy(resolution.RoleIDs),
			AllScopes:               resolution.AllScopes,
			AllowedScopeIDs:         sortedCopy(resolution.AllowedScopeIDs),
			AllowedRepositoryIDs:    sortedCopy(resolution.AllowedRepositoryIDs),
			UpdatedAt:               now,
		},
	}, nil
}

func sortedCopy(values []string) []string {
	copied := append([]string(nil), values...)
	sort.Strings(copied)
	return copied
}
