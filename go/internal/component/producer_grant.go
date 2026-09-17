// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"strings"
	"time"
)

// ProducerGrant is the core-issued authorization for a first-party producer
// to emit an approved core-owned fact kind. Delegation never transfers
// canonical ownership: the grant names who may emit, what kind and schema
// versions, and in which source scope. Anything outside the grant fails
// closed at validation time.
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
	// Revoked marks a withdrawn grant. Revoked grants never match.
	Revoked bool `json:"revoked,omitempty"`
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
	replaced := false
	for i, existing := range state.Grants {
		if existing.ProducerID == grant.ProducerID &&
			existing.Version == grant.Version &&
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
		if grant.ProducerID != producerID || grant.Version != version {
			continue
		}
		if !declared[grant.Scope] {
			continue
		}
		granted[grant.Kind] = append(granted[grant.Kind], grant.SchemaVersions...)
	}
	return granted
}
