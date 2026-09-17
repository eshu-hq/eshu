// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NormalizeGrantVersion canonicalizes a grant version for identity
// comparison, mirroring the manifest metadata.version rule: surrounding
// whitespace is ignored and the v prefix is optional, so "v0.1.0" and
// "0.1.0" name the same grant.
func NormalizeGrantVersion(version string) string {
	return normalizeSemver(version)
}

// ProducerGrant is the core-issued authorization for a first-party producer
// to emit an approved core-owned fact kind. Delegation never transfers
// canonical ownership: the grant names who may emit, what kind and schema
// versions, and in which source scope. Anything outside the grant fails
// closed at validation time.
//
// Versions compare in normalized form everywhere (see NormalizeGrantVersion):
// both spellings validate, so the v prefix is not part of grant identity at
// match, storage, or lookup time.
type ProducerGrant struct {
	// ProducerID is the manifest metadata.id of the granted component.
	ProducerID string `json:"producer_id"`
	// Version pins the granted manifest metadata.version.
	Version string `json:"version"`
	// Kind is the core-owned fact kind the producer may emit.
	Kind string `json:"kind"`
	// SchemaVersions are the fact schema versions covered by the grant.
	SchemaVersions []string `json:"schema_versions"`
	// Scope is the source scope the grant is valid for. Empty never matches.
	Scope string `json:"scope"`
	// ExpiresAt bounds the grant lifetime. Zero means already expired.
	ExpiresAt time.Time `json:"expires_at"`
	// Revoked marks a withdrawn grant. Revoked grants never match. The key
	// is always present so live grants report explicit false, matching the
	// documented CLI JSON shape and the text revoked=false column.
	Revoked bool `json:"revoked"`
}

// RecordGrant persists a producer grant in the registry home. Re-issuing
// the same producer, version, kind, and scope replaces the stored grant so
// rotation and revocation converge on one record per key. It changes no
// validation behavior on its own.
func (r Registry) RecordGrant(grant ProducerGrant) error {
	state, err := r.load()
	if err != nil {
		return err
	}
	// The storage key compares versions in normalized form: both spellings
	// validate, so the v prefix is not part of grant identity. Re-issuing
	// one logical grant under the other spelling replaces the record
	// instead of storing twins that revocation could then miss.
	replaced := false
	for i, existing := range state.Grants {
		if existing.ProducerID == grant.ProducerID &&
			normalizeSemver(existing.Version) == normalizeSemver(grant.Version) &&
			existing.Kind == grant.Kind &&
			existing.Scope == grant.Scope {
			state.Grants[i] = grant
			replaced = true
		}
	}
	if !replaced {
		state.Grants = append(state.Grants, grant)
	}
	return r.save(state)
}

// AuthorizesEmission reports whether the grants authorize one emitted fact
// for a producer identity. Kinds that are not core-owned need no grant and
// always pass; a core-owned kind passes only with a live grant naming this
// producer and version, covering the schema version, scoped to one of the
// declared scopes, neither revoked nor expired. Anything else fails closed.
func AuthorizesEmission(grants []ProducerGrant, producerID, version, kind, schemaVersion string, scopes []string, now time.Time) bool {
	if !facts.IsCoreFactKind(strings.TrimSpace(kind)) {
		return true
	}
	covered := grantedCoreKinds(producerID, version, scopes, grants, now)
	for _, coveredVersion := range covered[strings.TrimSpace(kind)] {
		if coveredVersion == schemaVersion {
			return true
		}
	}
	return false
}

// RevokeGrant marks the stored grant for a producer, version, kind, and
// scope revoked so subsequent emissions fail closed. It reports an error
// when no matching grant exists so rollback automation cannot mistake a
// typo for a completed revocation.
func (r Registry) RevokeGrant(producerID, version, kind, scope string) error {
	state, err := r.load()
	if err != nil {
		return err
	}
	// The lookup normalizes versions exactly like the storage key, so
	// revoking under either spelling revokes the live grant instead of
	// reporting grant_not_found while a twin keeps authorizing.
	revoked := false
	for i, existing := range state.Grants {
		if existing.ProducerID == producerID &&
			normalizeSemver(existing.Version) == normalizeSemver(version) &&
			existing.Kind == kind &&
			existing.Scope == scope {
			state.Grants[i].Revoked = true
			revoked = true
		}
	}
	if !revoked {
		return NewError(ErrorCodeGrantNotFound, "no producer grant for revocation")
	}
	return r.save(state)
}

// grantedCoreKinds indexes the live grants matching a manifest identity to
// the core-owned kinds they cover. A grant binds only when it names this
// producer and version, its scope is one of the manifest-declared collector
// kinds, and it is neither revoked nor expired. The result maps kind to
// covered schema versions; kinds with no live grant are absent.
func grantedCoreKinds(producerID, version string, scopes []string, grants []ProducerGrant, now time.Time) map[string][]string {
	granted := make(map[string][]string)
	declared := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		if strings.TrimSpace(scope) != "" {
			declared[scope] = true
		}
	}
	for _, grant := range grants {
		if grant.Revoked || !now.Before(grant.ExpiresAt) {
			continue
		}
		// Versions compare in normalized form: both spellings validate on
		// both sides, so the v prefix is not part of grant identity.
		if grant.ProducerID != producerID || normalizeSemver(grant.Version) != normalizeSemver(version) {
			continue
		}
		if !declared[grant.Scope] {
			continue
		}
		granted[grant.Kind] = append(granted[grant.Kind], grant.SchemaVersions...)
	}
	return granted
}
