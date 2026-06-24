// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

// DefaultPollInterval is the source poll cadence used when a config supplies no
// explicit interval. It keeps idle scaffolding polls bounded.
const DefaultPollInterval = 30 * time.Minute

// Config is the declarative GCP cloud collector runtime configuration. It names
// the collector instance, the bounded parent scopes to scan, and the read-only
// credential references by NAME only. It never carries credential material,
// secret values, or live endpoint authentication.
type Config struct {
	// CollectorInstanceID is the configured runtime instance that owns target
	// policy and credential environment. It is required.
	CollectorInstanceID string
	// PollInterval is the source poll cadence. Zero selects DefaultPollInterval.
	PollInterval time.Duration
	// Scopes is the bounded set of parent scopes the instance is authorized to
	// scan. It must be non-empty.
	Scopes []ScopeConfig
}

// ScopeConfig declares one bounded GCP collector scope: a Cloud Asset Inventory
// parent scope plus the asset, content, and location shard identity. It carries
// the read-only credential reference by name only.
type ScopeConfig struct {
	// ScopeID is the stable Eshu scope id for the shard. It is derived from the
	// parent scope and shard family when empty.
	ScopeID string
	// ParentScopeKind is the bounded CAI parent scope kind.
	ParentScopeKind gcpcloud.ParentScopeKind
	// ParentScopeID is the provider parent identifier (organization number,
	// folder number, or project id). It is source evidence, never a telemetry
	// label.
	ParentScopeID string
	// AssetTypeFamily is the bounded asset family for the shard. Defaults to
	// "mixed" when empty.
	AssetTypeFamily string
	// ContentFamily is the bounded CAI content family for the shard. Defaults to
	// "resource" when empty.
	ContentFamily string
	// LocationBucket is the bounded location bucket for the shard. Defaults to
	// "global" when empty.
	LocationBucket string
	// GenerationID is the collector- or coordinator-assigned id for one bounded
	// scan. When empty the source derives a deterministic per-poll generation id.
	GenerationID string
	// FencingToken fences the scope's generation so a stale scan cannot replace
	// current facts. It must be positive.
	FencingToken int64
	// CredentialRef names the read-only credential the PageProvider resolves out
	// of band. It is a name only; no secret material is stored here. It is
	// required so the runtime never falls back to ambient credentials silently.
	CredentialRef string
	// DirectTagsEnabled opts this scope into Resource Manager tagBindings.list
	// calls for directly attached tags.
	DirectTagsEnabled bool
	// EffectiveTagsEnabled opts this scope into Resource Manager
	// effectiveTags.list calls for inherited/effective tags.
	EffectiveTagsEnabled bool
}

// Validate checks the config has the minimum declarative identity to run.
func (c Config) Validate() error {
	if strings.TrimSpace(c.CollectorInstanceID) == "" {
		return fmt.Errorf("gcp collector instance id is required")
	}
	if len(c.Scopes) == 0 {
		return fmt.Errorf("gcp collector requires at least one configured scope")
	}
	seen := make(map[string]struct{}, len(c.Scopes))
	for i := range c.Scopes {
		resolved := c.Scopes[i].withDefaults()
		if err := resolved.validate(); err != nil {
			return fmt.Errorf("gcp collector scope %d: %w", i, err)
		}
		if _, dup := seen[resolved.ScopeID]; dup {
			return fmt.Errorf("gcp collector scope %d: duplicate scope_id", i)
		}
		seen[resolved.ScopeID] = struct{}{}
	}
	return nil
}

// ResolvedScopes returns the configured scopes with bounded shard families and a
// derived scope id filled in. Callers that build a PageProvider use it so the
// provider serves the same scope identity the Source observes.
func (c Config) ResolvedScopes() []ScopeConfig {
	return c.resolvedScopes()
}

// resolvedScopes returns the configured scopes with defaults applied so the
// source and provider observe identical scope identity.
func (c Config) resolvedScopes() []ScopeConfig {
	resolved := make([]ScopeConfig, len(c.Scopes))
	for i := range c.Scopes {
		resolved[i] = c.Scopes[i].withDefaults()
	}
	return resolved
}

// withDefaults returns a copy of the scope with bounded shard families and a
// derived scope id filled in. It never mutates credential or parent identity.
func (s ScopeConfig) withDefaults() ScopeConfig {
	out := s
	out.AssetTypeFamily = firstNonEmpty(strings.TrimSpace(s.AssetTypeFamily), "mixed")
	out.ContentFamily = firstNonEmpty(strings.TrimSpace(s.ContentFamily), "resource")
	out.LocationBucket = firstNonEmpty(strings.TrimSpace(s.LocationBucket), "global")
	out.ParentScopeID = strings.TrimSpace(s.ParentScopeID)
	out.CredentialRef = strings.TrimSpace(s.CredentialRef)
	out.GenerationID = strings.TrimSpace(s.GenerationID)
	if strings.TrimSpace(out.ScopeID) == "" {
		out.ScopeID = DeriveScopeID(out.ParentScopeKind, out.ParentScopeID, out.AssetTypeFamily, out.ContentFamily, out.LocationBucket)
	} else {
		out.ScopeID = strings.TrimSpace(out.ScopeID)
	}
	return out
}

// validate checks a defaults-applied scope has the durable identity the
// envelope builder requires.
func (s ScopeConfig) validate() error {
	switch {
	case !s.ParentScopeKind.Valid():
		return fmt.Errorf("invalid parent_scope_kind %q", s.ParentScopeKind)
	case s.ParentScopeID == "":
		return fmt.Errorf("parent_scope_id is required")
	case s.CredentialRef == "":
		return fmt.Errorf("credential_ref is required (read-only credential name)")
	case s.FencingToken <= 0:
		return fmt.Errorf("fencing_token must be positive")
	case s.ScopeID == "":
		return fmt.Errorf("scope_id could not be derived")
	default:
		return nil
	}
}

// DeriveScopeID builds the stable Eshu scope id for a bounded GCP shard in the
// contract form gcp:<parent_kind>:<parent_id>:<asset_family>:<content_family>:<location_bucket>.
func DeriveScopeID(kind gcpcloud.ParentScopeKind, parentID, assetFamily, contentFamily, locationBucket string) string {
	return strings.Join([]string{
		gcpcloud.CollectorKind,
		string(kind),
		strings.TrimSpace(parentID),
		strings.TrimSpace(assetFamily),
		strings.TrimSpace(contentFamily),
		strings.TrimSpace(locationBucket),
	}, ":")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
