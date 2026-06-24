// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"fmt"
	"slices"
	"sort"
	"time"
)

// Collector promotion states form a closed, shareable vocabulary describing
// whether a collector family is ready to promote, blocked, or absent. The set
// is reused by status rendering and the collector-readiness API/MCP read model
// so operators and reviewers see one consistent promotion label.
const (
	// CollectorPromotionImplemented marks a family with healthy, fresh evidence
	// that reached reducer readback. It is the only "ready to promote" state.
	CollectorPromotionImplemented = "implemented"
	// CollectorPromotionPartial marks a family with some evidence that has not
	// yet met the full implemented contract (for example reducer readback is
	// still pending or the lane is fixture-only).
	CollectorPromotionPartial = "partial"
	// CollectorPromotionFailed marks a family whose runtime health is degraded.
	CollectorPromotionFailed = "failed"
	// CollectorPromotionStale marks a family whose newest evidence is older than
	// the configured freshness window.
	CollectorPromotionStale = "stale"
	// CollectorPromotionGated marks a claim-driven family with claims disabled or
	// a family hidden by an active runtime profile gate.
	CollectorPromotionGated = "gated"
	// CollectorPromotionDisabled marks a registered family disabled by
	// configuration or deactivated by reconciliation.
	CollectorPromotionDisabled = "disabled"
	// CollectorPromotionPermissionHidden marks a family hidden from the caller by
	// an active permission scope. The proof is redacted to instance-free metadata.
	CollectorPromotionPermissionHidden = "permission_hidden"
	// CollectorPromotionUnsupported marks a known family with no configured
	// instance and no runtime evidence.
	CollectorPromotionUnsupported = "unsupported"
)

// Reducer readback availability for a collector family.
const (
	// CollectorReadbackAvailable means reducer-projected fact evidence exists.
	CollectorReadbackAvailable = "available"
	// CollectorReadbackPending means source facts exist but reducer readback has
	// not yet been observed.
	CollectorReadbackPending = "pending"
	// CollectorReadbackUnavailable means no fact evidence was observed.
	CollectorReadbackUnavailable = "unavailable"
)

// Claim-execution state for a collector instance.
const (
	// CollectorClaimDriven means the instance runs under workflow claims.
	CollectorClaimDriven = "claim_driven"
	// CollectorClaimDirect means the instance is registered with claims disabled.
	CollectorClaimDirect = "direct"
	// CollectorClaimRegistration means the instance exists only as a disabled
	// registration row.
	CollectorClaimRegistration = "registration_only"
	// CollectorClaimNone means no claim registration exists for the instance.
	CollectorClaimNone = "none"
)

// CollectorPromotionProof is a deterministic, shareable readiness record for one
// collector family or instance. It contains no credentials and no raw source
// payloads: only counts, evidence-source labels, bounded source-system names,
// and safe blocker descriptions derived from existing status evidence.
type CollectorPromotionProof struct {
	// CollectorKind is the durable collector family identifier.
	CollectorKind string
	// InstanceID identifies a configured instance, or is empty for a family-level
	// no-instance or permission-hidden proof.
	InstanceID string
	// DisplayName is the operator-facing family label.
	DisplayName string
	// PromotionState is the derived promotion verdict (see the constants above).
	PromotionState string
	// RuntimeCategory is the underlying CollectorRuntimeStatus category.
	RuntimeCategory string
	// Health is the underlying runtime health, empty when no instance exists.
	Health string
	// ClaimDriven reports the catalog expectation for the family.
	ClaimDriven bool
	// ClaimState describes how the instance executes (claim_driven, direct, ...).
	ClaimState string
	// SourceScope is the static scope kind the family collects against.
	SourceScope string
	// FixtureOnly marks a lane whose evidence is fixture-derived and therefore
	// must not be promoted to implemented on the strength of that evidence alone.
	FixtureOnly bool
	// EvidenceSources lists which evidence streams were observed (source_facts,
	// reducer_facts, workflow_coordinator, ...).
	EvidenceSources []string
	// SourceSystems lists bounded, non-sensitive source-system names.
	SourceSystems []string
	// ObservationCount is the aggregate observed fact count.
	ObservationCount int
	// ReducerReadback reports whether reducer-projected evidence is available.
	ReducerReadback string
	// TelemetryHandles lists stable metric and span names for diagnosis.
	TelemetryHandles []string
	// Blockers are safe, human-readable reasons the family is not implemented.
	Blockers []string
	// LastObservedAt is the newest observation timestamp across evidence.
	LastObservedAt time.Time
	// UpdatedAt is the newest update timestamp across evidence.
	UpdatedAt time.Time
}

// CollectorPromotionOptions controls promotion proof derivation. All fields are
// optional; an empty value yields the default catalog with no staleness window.
type CollectorPromotionOptions struct {
	// Catalog is the readiness catalog to enumerate. Empty uses
	// DefaultCollectorCatalog so every known collector family is covered.
	Catalog []CollectorCatalogEntry
	// AsOf is the evaluation time used for staleness. Zero falls back to the
	// report's AsOf.
	AsOf time.Time
	// StaleAfter is the freshness window. Values <= 0 disable stale derivation.
	StaleAfter time.Duration
	// PermissionHidden maps collector kinds the caller may not see. Hidden lanes
	// are reported as permission_hidden with redacted detail.
	PermissionHidden map[string]bool
	// FixtureOnly maps collector kinds whose current evidence is fixture-derived.
	// Such lanes are never reported as implemented.
	FixtureOnly map[string]bool
}

// CollectorPromotionProofs derives the deterministic per-collector promotion
// proof report from the status report without performing I/O. The catalog is the
// spine: every catalog family yields at least one proof (a no-instance proof
// when nothing is configured), and any runtime evidence for a kind missing from
// the catalog is still surfaced so catalog drift is visible rather than hidden.
func CollectorPromotionProofs(report Report, opts CollectorPromotionOptions) []CollectorPromotionProof {
	catalog := opts.Catalog
	// A nil catalog means "no catalog supplied"; fall back to the full fleet. An
	// explicitly empty (non-nil) catalog means "enumerate nothing".
	if catalog == nil {
		catalog = DefaultCollectorCatalog()
	}
	asOf := opts.AsOf
	if asOf.IsZero() {
		asOf = report.AsOf
	}

	runtimeByKind := map[string][]CollectorRuntimeStatus{}
	for _, row := range CollectorRuntimeStatuses(report) {
		runtimeByKind[row.CollectorKind] = append(runtimeByKind[row.CollectorKind], row)
	}

	proofs := make([]CollectorPromotionProof, 0, len(catalog))
	cataloged := map[string]bool{}
	for _, entry := range catalog {
		cataloged[entry.CollectorKind] = true
		if opts.PermissionHidden[entry.CollectorKind] {
			proofs = append(proofs, permissionHiddenProof(entry))
			continue
		}
		rows := runtimeByKind[entry.CollectorKind]
		if len(rows) == 0 {
			proofs = append(proofs, noInstanceProof(entry))
			continue
		}
		for _, row := range rows {
			proofs = append(proofs, promotionProofForInstance(entry, row, opts, asOf))
		}
	}

	// Surface runtime evidence for kinds the catalog does not declare so catalog
	// drift is visible rather than silently dropped.
	for kind, rows := range runtimeByKind {
		if cataloged[kind] {
			continue
		}
		entry := CollectorCatalogEntry{
			CollectorKind:    kind,
			DisplayName:      synthesizeDisplayName(kind),
			ClaimDriven:      true,
			SourceScope:      kind,
			TelemetryHandles: sharedCollectorTelemetryHandles(),
		}
		for _, row := range rows {
			proof := promotionProofForInstance(entry, row, opts, asOf)
			proof.Blockers = append(proof.Blockers, "collector kind not declared in readiness catalog")
			proofs = append(proofs, proof)
		}
	}

	sort.Slice(proofs, func(i, j int) bool {
		if proofs[i].CollectorKind != proofs[j].CollectorKind {
			return proofs[i].CollectorKind < proofs[j].CollectorKind
		}
		return proofs[i].InstanceID < proofs[j].InstanceID
	})
	return proofs
}

func permissionHiddenProof(entry CollectorCatalogEntry) CollectorPromotionProof {
	return CollectorPromotionProof{
		CollectorKind:    entry.CollectorKind,
		DisplayName:      entry.DisplayName,
		PromotionState:   CollectorPromotionPermissionHidden,
		ClaimDriven:      entry.ClaimDriven,
		ClaimState:       CollectorClaimNone,
		SourceScope:      entry.SourceScope,
		ReducerReadback:  CollectorReadbackUnavailable,
		TelemetryHandles: telemetryHandles(entry),
		Blockers:         []string{"hidden by active permission scope"},
	}
}

func noInstanceProof(entry CollectorCatalogEntry) CollectorPromotionProof {
	return CollectorPromotionProof{
		CollectorKind:    entry.CollectorKind,
		DisplayName:      entry.DisplayName,
		PromotionState:   CollectorPromotionUnsupported,
		ClaimDriven:      entry.ClaimDriven,
		ClaimState:       CollectorClaimNone,
		SourceScope:      entry.SourceScope,
		ReducerReadback:  CollectorReadbackUnavailable,
		TelemetryHandles: telemetryHandles(entry),
		Blockers:         []string{"no configured instance for this collector family"},
	}
}

func promotionProofForInstance(
	entry CollectorCatalogEntry,
	row CollectorRuntimeStatus,
	opts CollectorPromotionOptions,
	asOf time.Time,
) CollectorPromotionProof {
	readback := reducerReadbackState(row.EvidenceSources)
	claimState := claimStateFromRuntime(row.RuntimeMode)
	fixtureOnly := opts.FixtureOnly[entry.CollectorKind]

	proof := CollectorPromotionProof{
		CollectorKind:    entry.CollectorKind,
		InstanceID:       row.InstanceID,
		DisplayName:      displayName(entry, row),
		RuntimeCategory:  row.StatusCategory,
		Health:           row.Health,
		ClaimDriven:      entry.ClaimDriven,
		ClaimState:       claimState,
		SourceScope:      entry.SourceScope,
		FixtureOnly:      fixtureOnly,
		EvidenceSources:  slices.Clone(row.EvidenceSources),
		SourceSystems:    slices.Clone(row.SourceSystems),
		ObservationCount: row.ObservationCount,
		ReducerReadback:  readback,
		TelemetryHandles: telemetryHandles(entry),
		LastObservedAt:   row.LastObservedAt,
		UpdatedAt:        row.UpdatedAt,
	}
	proof.PromotionState, proof.Blockers = derivePromotionState(entry, row, readback, claimState, fixtureOnly, opts, asOf)
	return proof
}

// derivePromotionState evaluates the promotion verdict in fixed precedence:
// disabled, then failed, then gated, then stale, then implemented/partial. The
// precedence keeps the most actionable blocker first for a reviewer.
func derivePromotionState(
	entry CollectorCatalogEntry,
	row CollectorRuntimeStatus,
	readback string,
	claimState string,
	fixtureOnly bool,
	opts CollectorPromotionOptions,
	asOf time.Time,
) (string, []string) {
	switch {
	case row.StatusCategory == CollectorRuntimeDisabled || row.Health == "disabled":
		return CollectorPromotionDisabled, []string{"collector disabled or deactivated"}
	case row.Health == "degraded":
		return CollectorPromotionFailed, []string{degradedBlocker(row)}
	case row.Health == "partial":
		return CollectorPromotionPartial, []string{"runtime health partial: some work did not complete"}
	case isGated(entry, row, claimState):
		return CollectorPromotionGated, []string{gatedBlocker(row)}
	}

	if opts.StaleAfter > 0 && evidenceIsStale(row, opts.StaleAfter, asOf) {
		return CollectorPromotionStale, []string{fmt.Sprintf("newest evidence older than %s", opts.StaleAfter)}
	}

	if fixtureOnly {
		return CollectorPromotionPartial, []string{"evidence is fixture-only; live promotion not proven"}
	}

	if isImplemented(entry, readback, claimState) {
		return CollectorPromotionImplemented, nil
	}
	return CollectorPromotionPartial, []string{partialBlocker(entry, readback, claimState)}
}

func isGated(entry CollectorCatalogEntry, row CollectorRuntimeStatus, claimState string) bool {
	if row.StatusCategory == CollectorRuntimeProfileGated {
		return true
	}
	// Unregistered evidence has no coordinator row to gate; it is partial, not
	// gated, so it never claims a registration that does not exist.
	if row.StatusCategory == CollectorRuntimeUnregistered {
		return false
	}
	// A claim-driven family registered with claims disabled is gated; the same
	// direct mode for a non-claim-driven family is its normal operating mode.
	return entry.ClaimDriven && claimState == CollectorClaimDirect
}

func isImplemented(entry CollectorCatalogEntry, readback string, claimState string) bool {
	if readback != CollectorReadbackAvailable {
		return false
	}
	if entry.ClaimDriven {
		return claimState == CollectorClaimDriven
	}
	return true
}

func reducerReadbackState(evidenceSources []string) string {
	hasSource := false
	for _, source := range evidenceSources {
		switch source {
		case "reducer_facts":
			return CollectorReadbackAvailable
		case "source_facts":
			hasSource = true
		}
	}
	if hasSource {
		return CollectorReadbackPending
	}
	return CollectorReadbackUnavailable
}

func claimStateFromRuntime(runtimeMode string) string {
	switch runtimeMode {
	case CollectorClaimDriven, CollectorClaimDirect, CollectorClaimRegistration:
		return runtimeMode
	default:
		return CollectorClaimNone
	}
}

func evidenceIsStale(row CollectorRuntimeStatus, staleAfter time.Duration, asOf time.Time) bool {
	newest := row.LastObservedAt
	if row.UpdatedAt.After(newest) {
		newest = row.UpdatedAt
	}
	if newest.IsZero() {
		return false
	}
	return newest.Before(asOf.Add(-staleAfter))
}

func degradedBlocker(row CollectorRuntimeStatus) string {
	if detail := row.Detail; detail != "" {
		return "runtime health degraded: " + detail
	}
	return "runtime health degraded"
}

func gatedBlocker(row CollectorRuntimeStatus) string {
	if row.StatusCategory == CollectorRuntimeProfileGated {
		return "hidden by active runtime profile gate"
	}
	return "claim-driven collector registered with claims disabled"
}

func partialBlocker(entry CollectorCatalogEntry, readback string, claimState string) string {
	switch {
	case readback == CollectorReadbackUnavailable:
		return "no fact evidence observed yet"
	case readback == CollectorReadbackPending:
		return "reducer readback not yet available"
	case entry.ClaimDriven && claimState != CollectorClaimDriven:
		return "claim-driven execution not active"
	default:
		return "promotion contract not fully met"
	}
}

func displayName(entry CollectorCatalogEntry, row CollectorRuntimeStatus) string {
	if entry.DisplayName != "" {
		return entry.DisplayName
	}
	return row.DisplayName
}

// telemetryHandles resolves the handles for a catalog entry, falling back to the
// shared collector handles every collector emits when none are declared.
func telemetryHandles(entry CollectorCatalogEntry) []string {
	if len(entry.TelemetryHandles) > 0 {
		return entry.TelemetryHandles
	}
	return sharedCollectorTelemetryHandles()
}

// renderCollectorPromotionProofLines renders one compact, shareable line per
// collector promotion proof for the plain-text status surface.
func renderCollectorPromotionProofLines(rows []CollectorPromotionProof) []string {
	if len(rows) == 0 {
		return nil
	}
	lines := []string{"Collector promotion proofs:"}
	for _, row := range rows {
		identity := row.CollectorKind
		if row.InstanceID != "" {
			identity = row.InstanceID + " kind=" + row.CollectorKind
		}
		line := fmt.Sprintf("  %s state=%s readback=%s", identity, row.PromotionState, row.ReducerReadback)
		if row.FixtureOnly {
			line += " fixture_only=true"
		}
		if len(row.Blockers) > 0 {
			line += fmt.Sprintf(" blocker=%q", row.Blockers[0])
		}
		lines = append(lines, line)
	}
	return lines
}
