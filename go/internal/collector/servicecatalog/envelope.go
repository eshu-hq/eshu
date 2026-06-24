// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// newEnvelope constructs one observed-confidence service-catalog fact envelope.
// Every service-catalog fact shares this construction so schema version,
// collector kind, and source reference stay consistent with the reducer's
// expectations.
func newEnvelope(ctx FixtureContext, factKind, stableKey, sourceRecordID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:           serviceCatalogFactID(factKind, stableKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.ServiceCatalogSchemaVersionV1,
		CollectorKind:    CollectorKind,
		FencingToken:     ctx.FencingToken,
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        ctx.ScopeID,
			GenerationID:   ctx.GenerationID,
			FactKey:        stableKey,
			SourceURI:      stripSensitiveURL(ctx.SourceURI),
			SourceRecordID: sourceRecordID,
		},
	}
}

// serviceCatalogFactID derives the content-stable fact identity. Re-emitting an
// unchanged manifest in a new generation reuses the same stable key, so the
// fact store upserts rather than duplicates.
func serviceCatalogFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("ServiceCatalogFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func normalizedObservedAt(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now().UTC()
	}
	return observedAt.UTC()
}

func validateContext(ctx FixtureContext) error {
	if strings.TrimSpace(ctx.ScopeID) == "" {
		return fmt.Errorf("service catalog fixture scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("service catalog fixture generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("service catalog fixture collector_instance_id must not be blank")
	}
	return nil
}

func trim(value string) string {
	return strings.TrimSpace(value)
}

// stripSensitiveURL drops URLs that carry credentials or query strings, which is
// where catalog manifests routinely embed integration tokens.
func stripSensitiveURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if parsed.User != nil || parsed.RawQuery != "" {
		return ""
	}
	return value
}

// isSafeURL reports whether a URL is free of credentials and query strings and
// is therefore safe to emit into a fact payload.
func isSafeURL(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && stripSensitiveURL(value) == value
}

// redactSensitiveText masks credential-bearing URLs embedded in free text such
// as descriptions and owner notes without dropping the surrounding content.
func redactSensitiveText(value string) string {
	fields := strings.Fields(value)
	for i, field := range fields {
		trimmed := strings.Trim(field, ".,;")
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			if stripSensitiveURL(trimmed) == "" {
				fields[i] = strings.Replace(field, trimmed, "[redacted_url]", 1)
			}
		}
	}
	return strings.Join(fields, " ")
}

// deduplicateEnvelopes drops envelopes that share a stable fact identity, which
// keeps re-emission idempotent within one manifest.
func deduplicateEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	seen := make(map[string]bool, len(envelopes))
	out := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		if seen[envelope.FactID] {
			continue
		}
		seen[envelope.FactID] = true
		out = append(out, envelope)
	}
	return out
}
