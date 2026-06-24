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

// PostgresCloudIdentityPolicyEvidenceLoader reads Azure identity-policy source
// facts for one scope generation and maps each payload into the shared
// reducer.CloudIdentityPolicyEvidenceRecord shape the admission path attaches
// by cloud_resource_uid. It returns only bounded enum/text fields plus keyed
// fingerprints; raw principal GUIDs and raw assignment scopes never leave the
// source payload.
type PostgresCloudIdentityPolicyEvidenceLoader struct {
	// DB executes the bounded source-fact read.
	DB Queryer
	// Logger, when set, records bounded skip diagnostics for undecodable rows.
	Logger *slog.Logger
}

const maxCloudIdentityPolicyEvidenceFieldLength = 256

// LoadCloudIdentityPolicyEvidence implements
// reducer.CloudIdentityPolicyEvidenceLoader.
func (l PostgresCloudIdentityPolicyEvidenceLoader) LoadCloudIdentityPolicyEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]reducer.CloudIdentityPolicyEvidenceRecord, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("cloud identity policy evidence database is required")
	}
	scopeID = strings.TrimSpace(scopeID)
	generationID = strings.TrimSpace(generationID)
	if scopeID == "" {
		return nil, fmt.Errorf("cloud identity policy evidence scope ID must not be blank")
	}
	if generationID == "" {
		return nil, fmt.Errorf("cloud identity policy evidence generation ID must not be blank")
	}

	rows, err := l.DB.QueryContext(ctx, listCloudIdentityPolicyEvidenceForGenerationQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list cloud identity policy evidence source facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []reducer.CloudIdentityPolicyEvidenceRecord
	for rows.Next() {
		var factKind, rawIdentity, stableFactKey string
		var payload []byte
		if err := rows.Scan(&factKind, &rawIdentity, &stableFactKey, &payload); err != nil {
			return nil, fmt.Errorf("scan cloud identity policy evidence source fact: %w", err)
		}
		record, ok := cloudIdentityPolicyEvidenceRecordFromRow(factKind, rawIdentity, stableFactKey, payload)
		if !ok {
			l.logSkippedIdentityPolicyRow(ctx, scopeID, generationID, factKind, rawIdentity)
			continue
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud identity policy evidence source facts: %w", err)
	}
	return records, nil
}

func cloudIdentityPolicyEvidenceRecordFromRow(
	factKind string,
	rawIdentity string,
	stableFactKey string,
	payload []byte,
) (reducer.CloudIdentityPolicyEvidenceRecord, bool) {
	if factKind != facts.AzureIdentityObservationFactKind {
		return reducer.CloudIdentityPolicyEvidenceRecord{}, false
	}
	rawIdentity = strings.TrimSpace(rawIdentity)
	if cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderAzure, rawIdentity).Outcome != cloudinventory.ResolutionOutcomeAdmitted {
		return reducer.CloudIdentityPolicyEvidenceRecord{}, false
	}

	var decoded map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return reducer.CloudIdentityPolicyEvidenceRecord{}, false
		}
	}
	record := reducer.CloudIdentityPolicyEvidenceRecord{
		Provider:             cloudinventory.ProviderAzure,
		RawIdentity:          rawIdentity,
		EvidenceKey:          boundedIdentityPolicyString(stableFactKey),
		IdentityType:         boundedIdentityPolicyString(decoded["identity_type"]),
		RoleClass:            boundedIdentityPolicyString(decoded["role_class"]),
		PrincipalFingerprint: boundedIdentityPolicyString(decoded["principal_fingerprint"]),
		ClientFingerprint:    boundedIdentityPolicyString(decoded["client_fingerprint"]),
		ObjectFingerprint:    boundedIdentityPolicyString(decoded["object_fingerprint"]),
		TenantFingerprint:    boundedIdentityPolicyString(decoded["tenant_fingerprint"]),
	}
	if record.IdentityType == "" || !identityPolicyRecordHasFingerprint(record) {
		return reducer.CloudIdentityPolicyEvidenceRecord{}, false
	}
	return record, true
}

func boundedIdentityPolicyString(value any) string {
	text := strings.TrimSpace(coerceJSONString(value))
	if len(text) <= maxCloudIdentityPolicyEvidenceFieldLength {
		return text
	}
	return text[:maxCloudIdentityPolicyEvidenceFieldLength]
}

func identityPolicyRecordHasFingerprint(record reducer.CloudIdentityPolicyEvidenceRecord) bool {
	return record.PrincipalFingerprint != "" ||
		record.ClientFingerprint != "" ||
		record.ObjectFingerprint != "" ||
		record.TenantFingerprint != ""
}

func (l PostgresCloudIdentityPolicyEvidenceLoader) logSkippedIdentityPolicyRow(
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
		slog.String(telemetry.LogKeyFailureClass, "cloud_identity_policy_evidence_source_fact_decode"),
		slog.String("fact_kind", factKind),
	}
	attrs = append(attrs, telemetry.SafeResourceLogAttrs(rawIdentity)...)
	l.Logger.LogAttrs(ctx, slog.LevelWarn, "cloud identity policy evidence loader skipped source fact", attrs...)
}
