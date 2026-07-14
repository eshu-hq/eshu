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
	"github.com/eshu-hq/eshu/sdk/go/factschema"
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

	fingerprints, ok := tagValueFingerprintsForFactKind(factKind, decoded)
	if !ok {
		return reducer.CloudTagEvidenceRecord{}, false
	}

	return reducer.CloudTagEvidenceRecord{
		Provider:             provider,
		RawIdentity:          rawIdentity,
		TagValueFingerprints: fingerprints,
	}, true
}

// tagValueFingerprintsForFactKind decodes payload through the factschema
// typed seam for factKind and returns its keyed tag-value fingerprints. It
// dispatches to a per-provider function (rather than inlining both branches
// here) so each decode call's bound result variable is scoped to its own
// function body: the payload-usage-manifest gate's AST-based usage scanner
// (go/internal/payloadusage) attributes a field read to whichever decode call
// most recently bound the same identifier name within one function, so two
// same-named bindings in one function would misattribute one provider's
// field read to the other's decode seam.
func tagValueFingerprintsForFactKind(factKind string, payload map[string]any) (map[string]string, bool) {
	switch factKind {
	case facts.AzureTagObservationFactKind:
		return azureTagValueFingerprints(payload)
	case facts.GCPTagObservationFactKind:
		return gcpTagValueFingerprints(payload)
	default:
		return nil, false
	}
}

// azureTagValueFingerprints decodes payload as an azure_tag_observation
// envelope and returns its trimmed tag-value fingerprints. A decode failure
// (a missing required field, or a fingerprint value that is not a JSON
// string) returns ok=false so the caller drops and logs the row instead of
// attaching a coerced or partial fingerprint map.
func azureTagValueFingerprints(payload map[string]any) (map[string]string, bool) {
	observation, err := decodeAzureTagObservation(factschema.Envelope{
		FactKind:      facts.AzureTagObservationFactKind,
		SchemaVersion: facts.AzureTagObservationSchemaVersion,
		Payload:       payload,
	})
	if err != nil {
		return nil, false
	}
	return trimTagValueFingerprints(observation.TagValueFingerprints)
}

// gcpTagValueFingerprints decodes payload as a gcp_tag_observation envelope
// and returns its trimmed tag-value fingerprints. See
// azureTagValueFingerprints for the decode-failure contract.
func gcpTagValueFingerprints(payload map[string]any) (map[string]string, bool) {
	observation, err := decodeGCPTagObservation(factschema.Envelope{
		FactKind:      facts.GCPTagObservationFactKind,
		SchemaVersion: facts.GCPTagObservationSchemaVersion,
		Payload:       payload,
	})
	if err != nil {
		return nil, false
	}
	return trimTagValueFingerprints(observation.TagValueFingerprints)
}

// trimTagValueFingerprints trims whitespace from tag-evidence keys and
// fingerprint markers and drops any entry left with a blank key or marker,
// returning ok=false when nothing usable remains. The typed decode already
// guarantees every value is a JSON string (a non-string value fails decode
// before reaching here, see azureTagValueFingerprints); this only guards
// against a blank key/value slipping through, matching the pre-typed-decode
// loader's behavior of dropping a fact with no usable tags.
func trimTagValueFingerprints(raw map[string]string) (map[string]string, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if marker := strings.TrimSpace(value); marker != "" {
			out[key] = marker
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
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
