// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

const (
	packageOwnershipCorrelationFactKind   = factschema.FactKindReducerPackageOwnershipCorrelation
	packageConsumptionCorrelationFactKind = factschema.FactKindReducerPackageConsumptionCorrelation
	packagePublicationCorrelationFactKind = factschema.FactKindReducerPackagePublicationCorrelation
)

// PackageCorrelationWrite carries package ownership, publication, and
// consumption decisions for durable reducer facts.
type PackageCorrelationWrite struct {
	IntentID             string
	ScopeID              string
	GenerationID         string
	SourceSystem         string
	Cause                string
	OwnershipDecisions   []PackageSourceCorrelationDecision
	ConsumptionDecisions []PackageConsumptionDecision
	PublicationDecisions []PackagePublicationDecision
}

// PackageCorrelationWriteResult summarizes durable package correlation writes.
type PackageCorrelationWriteResult struct {
	CanonicalWrites int
	FactsWritten    int
	EvidenceSummary string
}

// PackageCorrelationWriter persists reducer-owned package correlations.
type PackageCorrelationWriter interface {
	WritePackageCorrelations(context.Context, PackageCorrelationWrite) (PackageCorrelationWriteResult, error)
}

// PostgresPackageCorrelationWriter stores ownership candidates and admitted
// consumption decisions in the shared fact store.
type PostgresPackageCorrelationWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WritePackageCorrelations persists source-hint ownership candidates,
// source-hint publication evidence, and manifest-backed consumption truth.
// Ownership and publication candidates keep canonical_writes=0 until stronger
// build, release, or CI evidence exists.
func (w PostgresPackageCorrelationWriter) WritePackageCorrelations(
	ctx context.Context,
	write PackageCorrelationWrite,
) (PackageCorrelationWriteResult, error) {
	if w.DB == nil {
		return PackageCorrelationWriteResult{}, fmt.Errorf("package correlation database is required")
	}
	now := reducerWriterNow(w.Now)
	rows := make(
		[]reducerFactVersionedRow,
		0,
		len(write.OwnershipDecisions)+len(write.ConsumptionDecisions)+len(write.PublicationDecisions),
	)
	for _, decision := range write.OwnershipDecisions {
		row, err := w.buildRow(
			now,
			packageOwnershipCorrelationFactKind,
			packageOwnershipFactID(write, decision),
			packageOwnershipStableFactKey(write, decision),
			packageOwnershipPayload(write, decision),
		)
		if err != nil {
			return PackageCorrelationWriteResult{}, err
		}
		rows = append(rows, row)
	}
	for _, decision := range write.ConsumptionDecisions {
		row, err := w.buildRow(
			now,
			packageConsumptionCorrelationFactKind,
			packageConsumptionFactID(write, decision),
			packageConsumptionStableFactKey(write, decision),
			packageConsumptionPayload(write, decision),
		)
		if err != nil {
			return PackageCorrelationWriteResult{}, err
		}
		rows = append(rows, row)
	}
	for _, decision := range write.PublicationDecisions {
		row, err := w.buildRow(
			now,
			packagePublicationCorrelationFactKind,
			packagePublicationFactID(write, decision),
			packagePublicationStableFactKey(write, decision),
			packagePublicationPayload(write, decision),
		)
		if err != nil {
			return PackageCorrelationWriteResult{}, err
		}
		rows = append(rows, row)
	}
	factsWritten := len(rows)
	// Bounded chunked bulk insert: ownership, consumption, and publication
	// rows share one fact_records table keyed by fact_id, so they upsert
	// safely through a single reducerBatchInsertVersionedFacts call in
	// O(N/batchSize) round-trips rather than one ExecContext per decision
	// across three separate loops.
	if err := reducerBatchInsertVersionedFacts(ctx, w.DB, rows); err != nil {
		return PackageCorrelationWriteResult{}, err
	}
	canonicalWrites := packageCorrelationCanonicalWrites(write.ConsumptionDecisions)
	return PackageCorrelationWriteResult{
		CanonicalWrites: canonicalWrites,
		FactsWritten:    factsWritten,
		EvidenceSummary: fmt.Sprintf(
			"wrote package correlations ownership=%d consumption=%d publication=%d canonical_writes=%d",
			len(write.OwnershipDecisions),
			len(write.ConsumptionDecisions),
			len(write.PublicationDecisions),
			canonicalWrites,
		),
	}, nil
}

// buildRow constructs the batched-insert row for one package correlation
// decision. It deliberately derives scope_id/generation_id/source_system/
// intent_id from the already-built payload map via payloadString — NOT from the
// PackageCorrelationWrite struct fields — because the retired per-row
// writePayload helper derived them from that same payload map, and reading them
// the same way is what makes the batched insert provably byte-identical to the
// per-row canonicalVersionedReducerFactInsertQuery loop it replaces. Switching
// to the struct fields would only be equivalent if they always match the
// payload's values, which is not guaranteed at this seam; the indirection is the
// byte-identity contract, not accidental.
func (w PostgresPackageCorrelationWriter) buildRow(
	now time.Time,
	factKind string,
	factID string,
	stableFactKey string,
	payload map[string]any,
) (reducerFactVersionedRow, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return reducerFactVersionedRow{}, fmt.Errorf("marshal package correlation payload: %w", err)
	}
	sourceSystem := payloadString(payload, "source_system")
	return reducerFactVersionedRow{
		FactID:           factID,
		ScopeID:          payloadString(payload, "scope_id"),
		GenerationID:     payloadString(payload, "generation_id"),
		FactKind:         factKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    facts.ReducerDerivedSchemaVersionV1,
		CollectorKind:    reducerFactCollectorKind(sourceSystem),
		SourceConfidence: facts.SourceConfidenceInferred,
		SourceSystem:     sourceSystem,
		SourceFactKey:    payloadString(payload, "intent_id"),
		ObservedAt:       now,
		IngestedAt:       now,
		Payload:          string(payloadJSON),
	}, nil
}

func packageOwnershipFactID(write PackageCorrelationWrite, decision PackageSourceCorrelationDecision) string {
	return packageOwnershipCorrelationFactKind + ":" + facts.StableID(
		packageOwnershipCorrelationFactKind,
		packageOwnershipIdentity(write, decision),
	)
}

func packageOwnershipStableFactKey(
	write PackageCorrelationWrite,
	decision PackageSourceCorrelationDecision,
) string {
	identity := packageOwnershipIdentity(write, decision)
	return strings.Join([]string{
		"package_ownership_correlation",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["package_id"])),
		strings.TrimSpace(fmt.Sprint(identity["source_url"])),
	}, ":")
}

func packageOwnershipIdentity(
	write PackageCorrelationWrite,
	decision PackageSourceCorrelationDecision,
) map[string]any {
	return map[string]any{
		"generation_id": strings.TrimSpace(write.GenerationID),
		"package_id":    strings.TrimSpace(decision.PackageID),
		"scope_id":      strings.TrimSpace(write.ScopeID),
		"source_url":    strings.TrimSpace(decision.SourceURL),
	}
}

func packageOwnershipPayload(
	write PackageCorrelationWrite,
	decision PackageSourceCorrelationDecision,
) map[string]any {
	return mustPackageCorrelationPayload(typedPackageOwnershipPayload(write, decision))
}

func packageConsumptionFactID(write PackageCorrelationWrite, decision PackageConsumptionDecision) string {
	return packageConsumptionCorrelationFactKind + ":" + facts.StableID(
		packageConsumptionCorrelationFactKind,
		packageConsumptionIdentity(write, decision),
	)
}

func packageConsumptionStableFactKey(
	write PackageCorrelationWrite,
	decision PackageConsumptionDecision,
) string {
	identity := packageConsumptionIdentity(write, decision)
	return strings.Join([]string{
		"package_consumption_correlation",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["package_id"])),
		strings.TrimSpace(fmt.Sprint(identity["repository_id"])),
		strings.TrimSpace(fmt.Sprint(identity["relative_path"])),
	}, ":")
}

func packageConsumptionIdentity(
	write PackageCorrelationWrite,
	decision PackageConsumptionDecision,
) map[string]any {
	return map[string]any{
		"generation_id":  strings.TrimSpace(write.GenerationID),
		"package_id":     strings.TrimSpace(decision.PackageID),
		"relative_path":  strings.TrimSpace(decision.RelativePath),
		"repository_id":  strings.TrimSpace(decision.RepositoryID),
		"scope_id":       strings.TrimSpace(write.ScopeID),
		"package_name":   strings.TrimSpace(decision.PackageName),
		"manifest_range": strings.TrimSpace(decision.DependencyRange),
	}
}

func packageConsumptionPayload(
	write PackageCorrelationWrite,
	decision PackageConsumptionDecision,
) map[string]any {
	return mustPackageCorrelationPayload(typedPackageConsumptionPayload(write, decision))
}

func packagePublicationFactID(write PackageCorrelationWrite, decision PackagePublicationDecision) string {
	return packagePublicationCorrelationFactKind + ":" + facts.StableID(
		packagePublicationCorrelationFactKind,
		packagePublicationIdentity(write, decision),
	)
}

func packagePublicationStableFactKey(
	write PackageCorrelationWrite,
	decision PackagePublicationDecision,
) string {
	identity := packagePublicationIdentity(write, decision)
	return strings.Join([]string{
		"package_publication_correlation",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["package_id"])),
		strings.TrimSpace(fmt.Sprint(identity["version_id"])),
		strings.TrimSpace(fmt.Sprint(identity["source_url"])),
		strings.TrimSpace(fmt.Sprint(identity["source_hint_kind"])),
		strings.TrimSpace(fmt.Sprint(identity["source_hint_version_id"])),
		strings.TrimSpace(fmt.Sprint(identity["source_hint_fact_id"])),
	}, ":")
}

func packagePublicationIdentity(
	write PackageCorrelationWrite,
	decision PackagePublicationDecision,
) map[string]any {
	return map[string]any{
		"generation_id":          strings.TrimSpace(write.GenerationID),
		"package_id":             strings.TrimSpace(decision.PackageID),
		"scope_id":               strings.TrimSpace(write.ScopeID),
		"source_url":             strings.TrimSpace(decision.SourceURL),
		"version_id":             strings.TrimSpace(decision.VersionID),
		"source_hint_fact_id":    strings.TrimSpace(decision.SourceHintFactID),
		"source_hint_kind":       strings.TrimSpace(decision.SourceHintKind),
		"source_hint_version_id": strings.TrimSpace(decision.SourceHintVersionID),
	}
}

func packagePublicationPayload(
	write PackageCorrelationWrite,
	decision PackagePublicationDecision,
) map[string]any {
	return mustPackageCorrelationPayload(typedPackagePublicationPayload(write, decision))
}

func mustPackageCorrelationPayload(payload map[string]any, err error) map[string]any {
	if err != nil {
		panic(fmt.Sprintf("encode package correlation payload: %v", err))
	}
	return payload
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
