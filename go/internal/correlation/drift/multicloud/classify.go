// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package multicloud

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// Classify returns the provider-neutral cloud-runtime finding for one canonical
// identity join, or an empty kind when observed, state, and config converge. The
// join decision is shared with the AWS path: it is provider-independent because
// it depends only on which evidence layers are present.
func Classify(cloud, state, config *cloudruntime.ResourceRow) cloudruntime.FindingKind {
	return cloudruntime.Classify(cloud, state, config)
}

// Row couples one canonical cloud_resource_uid with the observed cloud,
// Terraform-state, and Terraform-config views the classifier joins. Provider and
// RawIdentity preserve provider source evidence; the uid is the canonical join
// key. FindingKind and ManagementStatus are optional overrides the reducer uses
// to carry ambiguous-ownership and coverage-gap decisions the bare structural
// join cannot derive on its own.
type Row struct {
	// Provider is the normalized provider token (aws, gcp, azure).
	Provider string
	// RawIdentity preserves the provider raw identity for source-evidence
	// payloads; it is never used as a canonical key.
	RawIdentity string
	// CloudResourceUID is the canonical join key. When empty, the row resolves
	// it from Provider and RawIdentity.
	CloudResourceUID string
	// ResourceType carries the provider resource/asset type for explanation.
	ResourceType string
	// ScopeID is the durable source-local scope for evidence atoms.
	ScopeID string

	// Cloud, State, and Config are the optional joined evidence layers.
	Cloud  *cloudruntime.ResourceRow
	State  *cloudruntime.ResourceRow
	Config *cloudruntime.ResourceRow

	// FindingKind overrides the structural join when the reducer has a stronger
	// deterministic signal (ambiguous ownership or unknown coverage).
	FindingKind cloudruntime.FindingKind
	// ManagementStatus overrides the finding-derived management status.
	ManagementStatus string
	// MissingEvidence and WarningFlags carry reducer-derived explanation atoms.
	MissingEvidence []string
	WarningFlags    []string
	// RecommendedAction carries an optional operator-facing next step.
	RecommendedAction string
}

// EffectiveFindingKind returns the row's finding kind, preferring an explicit
// override (ambiguous or unknown) over the structural cloud/state/config join.
// An override of ambiguous or unknown wins even when the bare layers converge,
// because conflicting or unproven ownership must not be presented as managed.
func (r Row) EffectiveFindingKind() cloudruntime.FindingKind {
	if r.FindingKind != "" {
		return r.FindingKind
	}
	return Classify(r.Cloud, r.State, r.Config)
}

// ResolveUID returns the canonical cloud_resource_uid for the row, preferring an
// explicit CloudResourceUID and otherwise resolving Provider plus RawIdentity
// through the shared cloudinventory keyspace. The second return reports whether a
// stable uid was admitted; a blank, malformed, or unsupported identity yields
// false instead of fabricating canonical truth.
func (r Row) ResolveUID() (string, bool) {
	if uid := strings.TrimSpace(r.CloudResourceUID); uid != "" {
		return uid, true
	}
	resolution := cloudinventory.ResolveProviderIdentity(r.Provider, r.RawIdentity)
	if resolution.Outcome != cloudinventory.ResolutionOutcomeAdmitted {
		return "", false
	}
	return resolution.CloudResourceUID, true
}
