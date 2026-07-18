// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// bearerEntry is one provider's routing and verification state, held inside
// a snapshot.
type bearerEntry struct {
	provider BearerProvider
	verifier *oidc.IDTokenVerifier
}

// snapshot is the verifier cache's immutable point-in-time view. Reads take
// an atomic.Pointer load of a *snapshot and never mutate it; only rebuild
// (below) constructs a new one and swaps the pointer. This is the "lock-free
// reads, single rebuild in flight" design in the package README.
type snapshot struct {
	builtAt time.Time
	// byIssuer routes an unverified token's "iss" claim to the entry whose
	// verifier and grant-resolution scope apply. When two providers share an
	// issuer, the later one processed during rebuild wins; see
	// ComposeProviderSources's doc comment.
	byIssuer map[string]*bearerEntry
	// byProviderConfigID exists only to let the next rebuild decide whether
	// a provider's verifier can be reused (same IssuerURL and RevisionID) or
	// must be rebuilt, without a second pass over byIssuer.
	byProviderConfigID map[string]*bearerEntry
}

// empty reports whether this snapshot has zero enabled providers, the
// resolver's zero-provider fast path (AC #4): no verifier factory call, no
// JWKS traffic, ever, for a deployment with no bearer-token IdP configured.
func (s *snapshot) empty() bool {
	return s == nil || len(s.byIssuer) == 0
}

// cache owns the atomic snapshot pointer and the single-rebuild-in-flight
// guard. It is embedded in Resolver rather than exported: nothing outside
// this package constructs one directly.
type cache struct {
	ptr atomic.Pointer[snapshot]

	rebuildMu  sync.Mutex
	rebuilding bool

	source          ProviderSource
	verifierFactory VerifierFactory
	audience        string
	ttl             time.Duration
	now             func() time.Time
	instruments     *telemetry.Instruments
	logger          *slog.Logger
}

// currentAndMaybeRebuild returns the current snapshot (possibly nil on the
// very first call before any rebuild has ever completed) and, if the
// snapshot is missing or older than ttl, starts exactly one background
// rebuild. It never blocks the caller: this is "rebuild off the read path"
// from the locked design. A concurrent caller that also observes staleness
// while a rebuild is already in flight is a no-op (the rebuilding guard),
// not a second rebuild.
func (c *cache) currentAndMaybeRebuild(ctx context.Context) *snapshot {
	current := c.ptr.Load()
	if current != nil && c.now().Sub(current.builtAt) <= c.ttl {
		return current
	}
	c.triggerRebuild()
	return current
}

// triggerRebuild starts exactly one background rebuild if none is already
// running. The rebuild runs on its own bounded-timeout context
// (defaultRebuildTimeout), independent of any single request's context,
// because it outlives the request that happened to notice the snapshot was
// stale.
func (c *cache) triggerRebuild() {
	c.rebuildMu.Lock()
	if c.rebuilding {
		c.rebuildMu.Unlock()
		return
	}
	c.rebuilding = true
	c.rebuildMu.Unlock()

	go func() {
		defer func() {
			c.rebuildMu.Lock()
			c.rebuilding = false
			c.rebuildMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), defaultRebuildTimeout)
		defer cancel()
		c.rebuild(ctx)
	}()
}

// rebuildSync runs one rebuild synchronously on the caller's goroutine. Used
// only at construction time (NewResolver) so the very first
// ResolveScopedToken call already has a real snapshot — including the
// zero-provider snapshot, which is what makes the zero-provider fast path
// instant from the first request rather than only after the first
// background rebuild completes.
func (c *cache) rebuildSync(ctx context.Context) {
	c.rebuild(ctx)
}

// rebuild lists the currently enabled providers and builds a new snapshot,
// reusing a prior verifier whenever a provider's (IssuerURL, RevisionID) is
// unchanged from the snapshot being replaced. A provider whose verifier
// cannot be built (issuer discovery or JWKS fetch failure) is logged and
// excluded from the new snapshot rather than failing the whole rebuild —
// one misconfigured or unreachable IdP must not take every other enabled
// IdP's bearer validation down with it.
func (c *cache) rebuild(ctx context.Context) {
	providers, err := c.source.ActiveBearerProviders(ctx)
	if err != nil {
		c.logWarn("oidc bearer provider source list failed; keeping the current verifier snapshot", "error", err)
		return
	}

	prev := c.ptr.Load()
	byIssuer := make(map[string]*bearerEntry, len(providers))
	byProviderConfigID := make(map[string]*bearerEntry, len(providers))
	for _, provider := range providers {
		entry := c.buildEntry(ctx, prev, provider)
		if entry == nil {
			continue
		}
		byIssuer[provider.IssuerURL] = entry
		byProviderConfigID[provider.ProviderConfigID] = entry
	}

	c.ptr.Store(&snapshot{
		builtAt:            c.now(),
		byIssuer:           byIssuer,
		byProviderConfigID: byProviderConfigID,
	})
}

// buildEntry reuses prev's verifier for provider when its IssuerURL and
// RevisionID are unchanged, otherwise calls verifierFactory. Returns nil
// (excluding the provider from the new snapshot) when no reusable verifier
// exists and the factory call fails.
func (c *cache) buildEntry(ctx context.Context, prev *snapshot, provider BearerProvider) *bearerEntry {
	if prev != nil {
		if old, ok := prev.byProviderConfigID[provider.ProviderConfigID]; ok &&
			old.provider.IssuerURL == provider.IssuerURL &&
			old.provider.RevisionID == provider.RevisionID {
			return &bearerEntry{provider: provider, verifier: old.verifier}
		}
	}
	verifier, err := c.verifierFactory(ctx, provider.IssuerURL, c.audience)
	if err != nil {
		c.recordOutcome(ctx, outcomeJWKSFetchFailure)
		c.logWarn("oidc bearer verifier build failed; provider excluded from this snapshot",
			"provider_config_id", provider.ProviderConfigID, "error", err)
		return nil
	}
	return &bearerEntry{provider: provider, verifier: verifier}
}

func (c *cache) logWarn(msg string, args ...any) {
	if c.logger == nil {
		return
	}
	c.logger.Warn(msg, args...)
}
