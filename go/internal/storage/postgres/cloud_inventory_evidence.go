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

// PostgresCloudInventoryEvidenceLoader reads the provider cloud-inventory source
// facts for one scope generation and maps each provider payload into the shared
// reducer.CloudInventoryRecord shape the admission path consumes. It is the
// concrete implementation of reducer.CloudInventoryEvidenceLoader for the
// multi-cloud admission domain (issues #1997, #1998).
//
// The loader is read-only and side-effect free: it does not resolve canonical
// identity, fold records, or write anything. Identity resolution and evidence
// folding belong to the admission handler so a stale generation that this loader
// happens to read can still be superseded before any canonical write.
type PostgresCloudInventoryEvidenceLoader struct {
	// DB executes the bounded source-fact read.
	DB Queryer
	// Logger, when set, records bounded skip diagnostics for rows the loader
	// could not decode. Nil disables loader logging.
	Logger *slog.Logger
}

// cloudInventorySourceFactMapping describes how one provider inventory source
// fact kind maps onto the shared admission record: which provider token it
// carries and which payload key holds the provider resource type.
type cloudInventorySourceFactMapping struct {
	provider        string
	resourceTypeKey string
}

// cloudInventorySourceFactMappings is the closed set of provider inventory
// source fact kinds the shared admission path consumes. Adding a provider means
// adding its source fact kind both here and in the SQL allowlist so the two stay
// in lockstep.
var cloudInventorySourceFactMappings = map[string]cloudInventorySourceFactMapping{
	facts.AWSResourceFactKind: {
		provider:        cloudinventory.ProviderAWS,
		resourceTypeKey: "resource_type",
	},
	facts.GCPCloudResourceFactKind: {
		provider:        cloudinventory.ProviderGCP,
		resourceTypeKey: "asset_type",
	},
	facts.AzureCloudResourceFactKind: {
		provider:        cloudinventory.ProviderAzure,
		resourceTypeKey: "resource_type",
	},
}

// LoadCloudInventoryEvidence implements reducer.CloudInventoryEvidenceLoader. It
// returns the provider cloud-inventory records in scope for the given
// generation, bound to scope_id and generation_id so a stale generation cannot
// leak rows into a newer admission.
func (l PostgresCloudInventoryEvidenceLoader) LoadCloudInventoryEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]reducer.CloudInventoryRecord, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("cloud inventory evidence database is required")
	}
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" {
		return nil, fmt.Errorf("cloud inventory scope ID must not be blank")
	}
	if generationID == "" {
		return nil, fmt.Errorf("cloud inventory generation ID must not be blank")
	}

	rows, err := l.DB.QueryContext(ctx, listCloudInventorySourceFactsForGenerationQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list cloud inventory source facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []reducer.CloudInventoryRecord
	for rows.Next() {
		var factKind, rawIdentity string
		var payload []byte
		if err := rows.Scan(&factKind, &rawIdentity, &payload); err != nil {
			return nil, fmt.Errorf("scan cloud inventory source fact: %w", err)
		}
		record, ok := cloudInventoryRecordFromRow(factKind, rawIdentity, payload)
		if !ok {
			l.logSkippedRow(ctx, scopeID, generationID, factKind, rawIdentity)
			continue
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud inventory source facts: %w", err)
	}
	return records, nil
}

// cloudInventoryRecordFromRow maps one source-fact row into the shared admission
// record. The raw identity comes from the SQL COALESCE so the provider-specific
// key is already resolved; the resource type is read from the provider's own
// payload key. Rows with an unrecognized fact kind, a blank raw identity, or an
// undecodable payload are dropped so the admission path never receives evidence
// it cannot key.
func cloudInventoryRecordFromRow(
	factKind string,
	rawIdentity string,
	payload []byte,
) (reducer.CloudInventoryRecord, bool) {
	mapping, ok := cloudInventorySourceFactMappings[factKind]
	if !ok {
		return reducer.CloudInventoryRecord{}, false
	}
	rawIdentity = strings.TrimSpace(rawIdentity)
	if rawIdentity == "" {
		return reducer.CloudInventoryRecord{}, false
	}

	var decoded map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return reducer.CloudInventoryRecord{}, false
		}
	}

	return reducer.CloudInventoryRecord{
		Provider:     mapping.provider,
		FactKind:     factKind,
		RawIdentity:  rawIdentity,
		ResourceType: strings.TrimSpace(coerceJSONString(decoded[mapping.resourceTypeKey])),
		// The three inventory source fact kinds are provider control-plane
		// observations, so every loaded record is the observed evidence layer.
		// Declared and applied layers arrive from IaC/state source fact kinds in
		// a follow-up slice; the admission handler already keeps declared and
		// applied strictly above observed when those layers are wired.
		SourceLayer: reducer.SourceLayerObserved,
		Attributes:  boundedCloudInventoryAttributes(decoded["attributes"]),
	}, true
}

// boundedCloudInventoryAttributes extracts the bounded attributes map from the
// decoded provider payload. It keeps only non-blank string keys (cap 64 keys)
// whose values are string, bool, json.Number, float64, or []any of strings.
// Everything else is dropped. This is defense-in-depth; the collector already
// bounds attributes before emission.
func boundedCloudInventoryAttributes(raw any) map[string]any {
	const maxKeys = 64
	object, ok := raw.(map[string]any)
	if !ok || len(object) == 0 {
		return nil
	}
	out := make(map[string]any, len(object))
	for key, value := range object {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if len(out) >= maxKeys {
			break
		}
		switch v := value.(type) {
		case string:
			out[key] = v
		case bool:
			out[key] = v
		case float64:
			out[key] = v
		case json.Number:
			out[key] = v
		case []any:
			// Keep only string-typed elements; drop blank strings.
			strs := make([]string, 0, len(v))
			for _, elem := range v {
				if s, ok := elem.(string); ok && strings.TrimSpace(s) != "" {
					strs = append(strs, s)
				}
			}
			if len(strs) > 0 {
				out[key] = strs
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// logSkippedRow records a bounded diagnostic for one source-fact row the loader
// dropped. The fact kind is a bounded enum and is safe to log; the raw identity
// is emitted through the redaction-aware resource attribute helper so a resource
// id, ARN, full resource name, or ARM id never lands in a structured log
// verbatim.
func (l PostgresCloudInventoryEvidenceLoader) logSkippedRow(
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
		slog.String(telemetry.LogKeyFailureClass, "cloud_inventory_source_fact_decode"),
		slog.String("fact_kind", factKind),
	}
	attrs = append(attrs, telemetry.SafeResourceLogAttrs(rawIdentity)...)
	l.Logger.LogAttrs(ctx, slog.LevelWarn, "cloud inventory evidence loader skipped source fact", attrs...)
}
