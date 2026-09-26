// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// GrantStage names the lifecycle point at which a producer-grant decision was
// made. The set is closed so operator signals stay low-cardinality.
type GrantStage string

// The closed set of grant-decision stages.
const (
	// GrantStageInstall is Registry.Install admitting a manifest.
	GrantStageInstall GrantStage = "install"
	// GrantStageReadback is Registry.Readback re-validating a stored manifest.
	GrantStageReadback GrantStage = "readback"
	// GrantStageActivation is Registry.Enable or extension-host Source
	// construction admitting a component to run.
	GrantStageActivation GrantStage = "activation"
	// GrantStageEmission is the per-result recheck against the live grants.
	GrantStageEmission GrantStage = "emission"
)

// GrantReason is the closed decision reason: GrantReasonGranted for an allow,
// otherwise exactly one deny reason.
type GrantReason string

// The closed set of grant-decision reasons.
const (
	// GrantReasonGranted marks an allow: a live grant covers the emission.
	GrantReasonGranted GrantReason = "granted"
	// GrantReasonNoMatchingGrant marks a deny with no grant naming this
	// producer, version, and kind.
	GrantReasonNoMatchingGrant GrantReason = "no_matching_grant"
	// GrantReasonRevoked marks a deny where every matching grant is revoked.
	GrantReasonRevoked GrantReason = "revoked"
	// GrantReasonExpired marks a deny where every unrevoked matching grant is
	// past its expiry.
	GrantReasonExpired GrantReason = "expired"
	// GrantReasonScopeMismatch marks a deny where a live grant exists but its
	// scope is not one the manifest declares.
	GrantReasonScopeMismatch GrantReason = "scope_mismatch"
	// GrantReasonSchemaNotCovered marks a deny where a live in-scope grant
	// exists but does not cover the emitted schema version.
	GrantReasonSchemaNotCovered GrantReason = "schema_not_covered"
	// GrantReasonGrantsUnreadable marks a fail-closed deny because the grant
	// set could not be read.
	GrantReasonGrantsUnreadable GrantReason = "grants_unreadable"
)

// GrantDecision is one producer-grant allow or deny for a core-owned fact
// kind. It carries identifiers only: never grant contents beyond the
// producer, version, and kind, and never payloads or credentials.
type GrantDecision struct {
	Stage      GrantStage
	Allowed    bool
	Reason     GrantReason
	ProducerID string
	Version    string
	Kind       string
}

// GrantObserver receives producer-grant decisions at the owning decision
// site. Implementations must be safe for concurrent use and must not retain
// or mutate the decision. A nil observer disables observation.
type GrantObserver interface {
	ObserveGrantDecision(ctx context.Context, decision GrantDecision)
}

// ClassifyEmission returns GrantReasonGranted when the grants authorize the
// emission, else the single deny reason. It applies exactly the predicates of
// AuthorizesEmission and grantedCoreKinds (a kind that is not core-owned needs
// no grant and is granted), so it never returns a granted reason where
// AuthorizesEmission returns false. It is the cold-path companion: hot-path
// callers run AuthorizesEmission and classify only after a denial.
//
// A deny names the furthest gate a matching grant cleared: no grant for the
// producer, version, and kind; every match revoked; every unrevoked match
// expired; a live match whose scope the manifest does not declare; or a live
// in-scope match that does not cover the schema version.
func ClassifyEmission(grants []ProducerGrant, producerID, version, kind, schemaVersion string, scopes []string, now time.Time) GrantReason {
	kind = strings.TrimSpace(kind)
	if !facts.IsCoreFactKind(kind) {
		return GrantReasonGranted
	}
	normalizedVersion := normalizeSemver(version)
	var matched, unrevoked, live, inScope bool
	for _, grant := range grants {
		if grant.ProducerID != producerID || grant.Kind != kind ||
			normalizeSemver(grant.Version) != normalizedVersion {
			continue
		}
		matched = true
		if grant.Revoked {
			continue
		}
		unrevoked = true
		if !now.Before(grant.ExpiresAt) {
			continue
		}
		live = true
		if !scopeDeclared(scopes, grant.Scope) {
			continue
		}
		inScope = true
		if slices.Contains(grant.SchemaVersions, schemaVersion) {
			return GrantReasonGranted
		}
	}
	switch {
	case !matched:
		return GrantReasonNoMatchingGrant
	case !unrevoked:
		return GrantReasonRevoked
	case !live:
		return GrantReasonExpired
	case !inScope:
		return GrantReasonScopeMismatch
	default:
		return GrantReasonSchemaNotCovered
	}
}

// scopeDeclared reports whether scope is one of the manifest-declared
// collector kinds. Empty scopes never match, mirroring grantedCoreKinds.
func scopeDeclared(scopes []string, scope string) bool {
	if strings.TrimSpace(scope) == "" {
		return false
	}
	return slices.Contains(scopes, scope)
}

// WithGrantObserver returns a registry that reports grant decisions for
// install, readback, and enable to observer. A nil observer disables it.
func (r Registry) WithGrantObserver(observer GrantObserver) Registry {
	r.observer = observer
	return r
}

// ObserveManifestGrants reports one decision per core-owned fact family the
// manifest declares, evaluated against grants at now. Families that are not
// core-owned need no grant and report nothing. A family is allowed only when
// a live grant covers every schema version it declares; otherwise the first
// uncovered version's deny reason is reported. A nil observer is a no-op.
func (m Manifest) ObserveManifestGrants(ctx context.Context, observer GrantObserver, stage GrantStage, grants []ProducerGrant, now time.Time) {
	if observer == nil {
		return
	}
	// Report only for a manifest whose identity is valid: the producer id and
	// version reach spans and logs, so an unvalidated identity never does.
	if m.validateIdentity() != nil {
		return
	}
	for _, family := range m.Spec.EmittedFacts {
		kind := strings.TrimSpace(family.Kind)
		if !facts.IsCoreFactKind(kind) {
			continue
		}
		versions := family.SchemaVersions
		if len(versions) == 0 {
			versions = []string{""}
		}
		reason := GrantReasonGranted
		for _, schemaVersion := range versions {
			if reason = ClassifyEmission(
				grants, m.Metadata.ID, m.Metadata.Version, kind, schemaVersion, m.Spec.CollectorKinds, now,
			); reason != GrantReasonGranted {
				break
			}
		}
		observer.ObserveGrantDecision(ctx, GrantDecision{
			Stage:      stage,
			Allowed:    reason == GrantReasonGranted,
			Reason:     reason,
			ProducerID: m.Metadata.ID,
			Version:    m.Metadata.Version,
			Kind:       kind,
		})
	}
}

// observeEnableGrants reports the activation-stage grant decisions for the
// candidate component being enabled, and only for it: the collision checks
// that follow reload other installed manifests without observation, so an
// enable never re-reports a neighbor's decision. It only observes: a manifest
// that cannot be loaded is reported by the fail-closed enable checks that run
// next, so this never changes the enable outcome.
func (r Registry) observeEnableGrants(candidate InstalledComponent, state registryState) {
	if r.observer == nil {
		return
	}
	_, _ = loadManifestObserved(
		r.manifestPath(candidate.ID, candidate.Version), state.Grants, r.observer, GrantStageActivation,
	)
}
