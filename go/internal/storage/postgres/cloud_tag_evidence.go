// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PostgresCloudTagEvidenceLoader reads tag-evidence source facts
// (azure_tag_observation and gcp_tag_observation) for one scope generation and
// maps each payload into the shared reducer.CloudTagEvidenceRecord shape the
// admission path attaches by cloud_resource_uid. It is the concrete
// reducer.CloudTagEvidenceLoader for the multi-cloud admission domain (#2192,
// #2334).
//
// The loader is read-only and side-effect free: it resolves no identity and
// folds nothing. Identity resolution and attachment belong to the admission
// handler so a stale generation this loader happens to read is still superseded
// before any canonical write.
type PostgresCloudTagEvidenceLoader struct {
	// DB executes the bounded source-fact read.
	DB Queryer
	// Logger, when set, records bounded skip diagnostics for undecodable rows.
	Logger *slog.Logger
}

// cloudTagEvidenceFactMappings is the closed set of tag-evidence source fact
// kinds the shared admission path consumes. Adding a provider's tag-evidence
// fact kind means adding it both here and in the SQL allowlist so the two stay
// in lockstep.
var cloudTagEvidenceFactMappings = map[string]string{
	facts.AzureTagObservationFactKind: cloudinventory.ProviderAzure,
	facts.GCPTagObservationFactKind:   cloudinventory.ProviderGCP,
}

// LoadCloudTagEvidence implements reducer.CloudTagEvidenceLoader. It returns the
// tag-evidence records in scope for the generation, bound to scope_id and
// generation_id so a stale generation cannot leak rows into a newer admission.
func (l PostgresCloudTagEvidenceLoader) LoadCloudTagEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]reducer.CloudTagEvidenceRecord, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("cloud tag evidence database is required")
	}
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" {
		return nil, fmt.Errorf("cloud tag evidence scope ID must not be blank")
	}
	if generationID == "" {
		return nil, fmt.Errorf("cloud tag evidence generation ID must not be blank")
	}

	rows, err := l.DB.QueryContext(ctx, listCloudTagEvidenceForGenerationQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list cloud tag evidence source facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []reducer.CloudTagEvidenceRecord
	for rows.Next() {
		var factKind, rawIdentity string
		var payload []byte
		if err := rows.Scan(&factKind, &rawIdentity, &payload); err != nil {
			return nil, fmt.Errorf("scan cloud tag evidence source fact: %w", err)
		}
		record, ok := cloudTagEvidenceRecordFromRow(factKind, rawIdentity, payload)
		if !ok {
			l.logSkippedRow(ctx, scopeID, generationID, factKind, rawIdentity)
			continue
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud tag evidence source facts: %w", err)
	}
	return records, nil
}

// cloudTagEvidenceRecordFromRow maps one tag-evidence source-fact row into the
// shared admission record. Rows with an unrecognized fact kind, a blank raw
// identity, an undecodable payload, or no usable fingerprints are dropped so the
// admission path never receives tag evidence it cannot attach.
func cloudTagEvidenceRecordFromRow(
	factKind string,
	rawIdentity string,
	payload []byte,
) (reducer.CloudTagEvidenceRecord, bool) {
	provider, ok := cloudTagEvidenceFactMappings[factKind]
	if !ok {
		return reducer.CloudTagEvidenceRecord{}, false
	}
	rawIdentity = strings.TrimSpace(rawIdentity)
	if rawIdentity == "" {
		return reducer.CloudTagEvidenceRecord{}, false
	}

	var decoded map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return reducer.CloudTagEvidenceRecord{}, false
		}
	}
	fingerprints := stringMapFromJSON(decoded["tag_value_fingerprints"])
	if len(fingerprints) == 0 {
		return reducer.CloudTagEvidenceRecord{}, false
	}

	return reducer.CloudTagEvidenceRecord{
		Provider:             provider,
		RawIdentity:          rawIdentity,
		TagValueFingerprints: fingerprints,
	}, true
}

// stringMapFromJSON coerces a decoded JSON object into a map[string]string,
// dropping blank keys and non-string values. The collector already fingerprinted
// the tag values, so every retained value is a non-secret marker.
func stringMapFromJSON(raw any) map[string]string {
	object, ok := raw.(map[string]any)
	if !ok || len(object) == 0 {
		return nil
	}
	out := make(map[string]string, len(object))
	for key, value := range object {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if marker := strings.TrimSpace(coerceJSONString(value)); marker != "" {
			out[key] = marker
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// logSkippedRow records a bounded diagnostic for one tag-evidence row the loader
// dropped. The fact kind is a bounded enum; the raw identity is emitted through
// the redaction-aware resource attribute helper so an ARM id never lands in a
// structured log verbatim.
func (l PostgresCloudTagEvidenceLoader) logSkippedRow(
	ctx context.Context,
	scopeID string,
	generationID string,
	factKind string,
	rawIdentity string,
) {
	if l.Logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.String(telemetry.LogKeyFailureClass, "cloud_tag_evidence_source_fact_decode"),
		slog.String("fact_kind", factKind),
	}
	attrs = append(attrs, telemetry.SafeResourceLogAttrs(rawIdentity)...)
	l.Logger.LogAttrs(ctx, slog.LevelWarn, "cloud tag evidence loader skipped source fact", attrs...)
}
