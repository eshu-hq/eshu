// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const maxCloudResourceChangeEvidenceString = 256

// PostgresCloudResourceChangeEvidenceLoader reads azure_resource_change source
// facts for the resource_changes lane corresponding to one inventory admission
// generation and maps each payload into the shared
// reducer.CloudResourceChangeEvidenceRecord shape. The loader is read-only and
// graph-neutral; identity attachment and stale-generation supersession remain
// owned by the cloud-inventory admission handler.
type PostgresCloudResourceChangeEvidenceLoader struct {
	// DB executes the bounded source-fact read.
	DB Queryer
	// Logger, when set, records bounded skip diagnostics for undecodable rows.
	Logger *slog.Logger
}

// LoadCloudResourceChangeEvidence implements
// reducer.CloudResourceChangeEvidenceLoader. It returns sanitized
// resource-change records for the supplied inventory scope/generation, resolving
// Azure inventory lanes to their active sibling resource_changes generation so
// resource-change facts can attach without fabricating canonical inventory.
func (l PostgresCloudResourceChangeEvidenceLoader) LoadCloudResourceChangeEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]reducer.CloudResourceChangeEvidenceRecord, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("cloud resource change evidence database is required")
	}
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" {
		return nil, fmt.Errorf("cloud resource change evidence scope ID must not be blank")
	}
	if generationID == "" {
		return nil, fmt.Errorf("cloud resource change evidence generation ID must not be blank")
	}

	rows, err := l.DB.QueryContext(ctx, listCloudResourceChangeEvidenceForGenerationQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list cloud resource change evidence source facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []reducer.CloudResourceChangeEvidenceRecord
	for rows.Next() {
		var factKind, rawIdentity, stableKey string
		var payload []byte
		if err := rows.Scan(&factKind, &rawIdentity, &stableKey, &payload); err != nil {
			return nil, fmt.Errorf("scan cloud resource change evidence source fact: %w", err)
		}
		record, ok := cloudResourceChangeEvidenceRecordFromRow(factKind, rawIdentity, stableKey, payload)
		if !ok {
			l.logSkippedRow(ctx, scopeID, generationID, factKind, rawIdentity)
			continue
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud resource change evidence source facts: %w", err)
	}
	return records, nil
}

func cloudResourceChangeEvidenceRecordFromRow(
	factKind string,
	rawIdentity string,
	stableKey string,
	payload []byte,
) (reducer.CloudResourceChangeEvidenceRecord, bool) {
	if factKind != facts.AzureResourceChangeFactKind {
		return reducer.CloudResourceChangeEvidenceRecord{}, false
	}
	rawIdentity = strings.TrimSpace(rawIdentity)
	if rawIdentity == "" {
		return reducer.CloudResourceChangeEvidenceRecord{}, false
	}
	resolution := cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderAzure, rawIdentity)
	if resolution.Outcome != cloudinventory.ResolutionOutcomeAdmitted {
		return reducer.CloudResourceChangeEvidenceRecord{}, false
	}

	var decoded map[string]any
	if len(payload) == 0 {
		return reducer.CloudResourceChangeEvidenceRecord{}, false
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return reducer.CloudResourceChangeEvidenceRecord{}, false
	}
	changeType, ok := cloudResourceChangeTypeFromPayload(decoded)
	if !ok {
		return reducer.CloudResourceChangeEvidenceRecord{}, false
	}
	changeTime, ok := cloudResourceChangeTimeFromPayload(decoded)
	if !ok {
		return reducer.CloudResourceChangeEvidenceRecord{}, false
	}

	return reducer.CloudResourceChangeEvidenceRecord{
		Provider:                 cloudinventory.ProviderAzure,
		RawIdentity:              rawIdentity,
		EvidenceKey:              boundedCloudResourceChangeString(stableKey),
		ChangeType:               changeType,
		ChangeTime:               changeTime,
		Operation:                boundedCloudResourceChangeString(decoded["operation"]),
		ClientType:               boundedCloudResourceChangeString(decoded["client_type"]),
		ActorClass:               boundedCloudResourceChangeString(decoded["actor_class"]),
		ActorFingerprint:         boundedCloudResourceChangeString(decoded["actor_fingerprint"]),
		ChangedPropertyPaths:     cloudResourceChangeStringSlice(decoded["changed_property_paths"]),
		ChangedPropertyTruncated: cloudResourceChangeBool(decoded["changed_property_truncated"]),
		TombstoneCandidate:       changeType == "deleted" && cloudResourceChangeBool(decoded["is_tombstone_candidate"]),
	}, true
}

func cloudResourceChangeTypeFromPayload(decoded map[string]any) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(coerceJSONString(decoded["change_type"]))) {
	case "created":
		return "created", true
	case "updated":
		return "updated", true
	case "deleted":
		return "deleted", true
	default:
		return "", false
	}
}

func cloudResourceChangeTimeFromPayload(decoded map[string]any) (time.Time, bool) {
	raw := strings.TrimSpace(coerceJSONString(decoded["change_time"]))
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func boundedCloudResourceChangeString(value any) string {
	out := strings.TrimSpace(coerceJSONString(value))
	if len(out) > maxCloudResourceChangeEvidenceString {
		out = out[:maxCloudResourceChangeEvidenceString]
	}
	return out
}

func cloudResourceChangeStringSlice(value any) []string {
	values, ok := value.([]any)
	if !ok || len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	for _, candidate := range values {
		if path := boundedCloudResourceChangeString(candidate); path != "" {
			seen[path] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func cloudResourceChangeBool(value any) bool {
	boolValue, _ := value.(bool)
	return boolValue
}

// logSkippedRow records a bounded diagnostic for a change-evidence row the
// loader dropped. The fact kind is a bounded enum; the raw identity is emitted
// through the redaction-aware helper so ARM ids never land in logs verbatim.
func (l PostgresCloudResourceChangeEvidenceLoader) logSkippedRow(
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
		slog.String(telemetry.LogKeyFailureClass, "cloud_resource_change_evidence_source_fact_decode"),
		slog.String("fact_kind", factKind),
	}
	attrs = append(attrs, telemetry.SafeResourceLogAttrs(rawIdentity)...)
	l.Logger.LogAttrs(ctx, slog.LevelWarn, "cloud resource change evidence loader skipped source fact", attrs...)
}
