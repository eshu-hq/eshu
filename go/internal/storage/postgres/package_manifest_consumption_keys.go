// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	packageManifestConsumptionKeyBatchSize     = 500
	columnsPerPackageManifestConsumptionKeyRow = 6
)

const setPackageManifestConsumptionKeysWriterSQL = `
SELECT set_config('eshu.package_manifest_consumption_keys_writer', 'sidecar', true)
`

type packageManifestConsumptionKeyRow struct {
	FactID       string
	ScopeID      string
	GenerationID string
	RepositoryID string
	Ecosystem    string
	PackageName  string
}

func packageManifestConsumptionKeyRows(envelopes []facts.Envelope) []packageManifestConsumptionKeyRow {
	rows := make([]packageManifestConsumptionKeyRow, 0, len(envelopes))
	for _, envelope := range envelopes {
		if !isPackageManifestConsumptionFact(envelope) {
			continue
		}
		payload := envelope.Payload
		keys := packageidentity.ConsumptionKeys(
			packageManifestMetadataString(payload, "package_manager"),
			packageManifestDependencyNames(payload)...,
		)
		for _, key := range keys {
			rows = append(rows, packageManifestConsumptionKeyRow{
				FactID:       envelope.FactID,
				ScopeID:      envelope.ScopeID,
				GenerationID: envelope.GenerationID,
				RepositoryID: packageManifestPayloadString(payload, "repo_id"),
				Ecosystem:    string(key.Ecosystem),
				PackageName:  key.PackageName,
			})
		}
	}
	return rows
}

func isPackageManifestConsumptionFact(envelope facts.Envelope) bool {
	if envelope.FactKind != "content_entity" || envelope.SourceRef.SourceSystem != "git" || envelope.IsTombstone {
		return false
	}
	payload := envelope.Payload
	if packageManifestPayloadString(payload, "entity_type") != "Variable" ||
		packageManifestMetadataString(payload, "config_kind") != "dependency" ||
		packageManifestPayloadString(payload, "repo_id") == "" ||
		packageManifestPayloadString(payload, "entity_name") == "" ||
		packageManifestMetadataString(payload, "package_manager") == "" {
		return false
	}
	return packageManifestMetadataString(payload, "lockfile_unsupported_feature") == "" &&
		!packageManifestMetadataBool(payload, "source_ambiguous")
}

func packageManifestDependencyNames(payload map[string]any) []string {
	name := packageManifestPayloadString(payload, "entity_name")
	namespace := packageManifestMetadataString(payload, "namespace")
	if namespace == "" || name == "" {
		return []string{name}
	}
	return []string{name, strings.TrimSpace(namespace) + "/" + strings.TrimSpace(name)}
}

func packageManifestMetadataString(payload map[string]any, key string) string {
	if value := packageManifestPayloadString(payload, key); value != "" {
		return value
	}
	metadata, _ := payload["entity_metadata"].(map[string]any)
	return packageManifestPayloadString(metadata, key)
}

func packageManifestMetadataBool(payload map[string]any, key string) bool {
	return strings.EqualFold(packageManifestMetadataString(payload, key), "true")
}

func packageManifestPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	valueString := strings.TrimSpace(fmt.Sprint(value))
	if valueString == "<nil>" {
		return ""
	}
	return valueString
}

func refreshPackageManifestConsumptionKeys(ctx context.Context, database db.ExecQueryer, envelopes []facts.Envelope) error {
	if database == nil || len(envelopes) == 0 {
		return nil
	}
	factIDs := acceptedFactIDs(envelopes)
	if len(factIDs) == 0 {
		return nil
	}
	if _, err := database.ExecContext(ctx, deletePackageManifestConsumptionKeysSQL, array.StringArray(factIDs)); err != nil {
		return fmt.Errorf("delete package manifest consumption keys: %w", err)
	}
	rows := packageManifestConsumptionKeyRows(envelopes)
	for start := 0; start < len(rows); start += packageManifestConsumptionKeyBatchSize {
		end := min(start+packageManifestConsumptionKeyBatchSize, len(rows))
		if err := insertPackageManifestConsumptionKeyBatch(ctx, database, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func insertPackageManifestConsumptionKeyBatch(ctx context.Context, database db.ExecQueryer, rows []packageManifestConsumptionKeyRow) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*columnsPerPackageManifestConsumptionKeyRow)
	var values strings.Builder
	for index, row := range rows {
		if index > 0 {
			values.WriteString(", ")
		}
		offset := index * columnsPerPackageManifestConsumptionKeyRow
		fmt.Fprintf(&values, "($%d, $%d, $%d, $%d, $%d, $%d)", offset+1, offset+2, offset+3, offset+4, offset+5, offset+6)
		args = append(args, row.FactID, row.ScopeID, row.GenerationID, row.RepositoryID, row.Ecosystem, row.PackageName)
	}
	if _, err := database.ExecContext(ctx, insertPackageManifestConsumptionKeyPrefix+values.String()+insertPackageManifestConsumptionKeySuffix, args...); err != nil {
		return fmt.Errorf("insert package manifest consumption key batch (%d rows): %w", len(rows), err)
	}
	return nil
}

const deletePackageManifestConsumptionKeysSQL = `
DELETE FROM package_manifest_consumption_keys
WHERE fact_id = ANY($1::text[])
`

const insertPackageManifestConsumptionKeyPrefix = `
INSERT INTO package_manifest_consumption_keys (
    fact_id, scope_id, generation_id, repository_id, ecosystem, package_name
) VALUES `

const insertPackageManifestConsumptionKeySuffix = `
ON CONFLICT (fact_id, ecosystem, package_name) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id,
    repository_id = EXCLUDED.repository_id
`
