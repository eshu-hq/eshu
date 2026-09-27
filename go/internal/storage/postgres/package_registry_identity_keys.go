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
	packageRegistryIdentityKeyColumns   = 6
	packageRegistryIdentityKeyBatchSize = 500
)

type packageRegistryIdentityKeyRow struct {
	FactID       string
	ScopeID      string
	GenerationID string
	Ecosystem    string
	PackageName  string
	PackageID    string
}

func packageRegistryIdentityKeyRows(envelopes []facts.Envelope) []packageRegistryIdentityKeyRow {
	var rows []packageRegistryIdentityKeyRow
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.PackageRegistryPackageFactKind || envelope.IsTombstone {
			continue
		}
		packageID := packageManifestPayloadString(envelope.Payload, "package_id")
		if packageID == "" {
			continue
		}
		keys := packageidentity.ConsumptionKeysForRegistryIdentity(
			packageManifestPayloadString(envelope.Payload, "ecosystem"),
			packageManifestPayloadString(envelope.Payload, "raw_name"),
			packageManifestPayloadString(envelope.Payload, "normalized_name"),
			packageManifestPayloadString(envelope.Payload, "namespace"),
		)
		for _, key := range keys {
			rows = append(rows, packageRegistryIdentityKeyRow{envelope.FactID, envelope.ScopeID, envelope.GenerationID, string(key.Ecosystem), key.PackageName, packageID})
		}
	}
	return rows
}

func refreshPackageRegistryIdentityKeys(ctx context.Context, database db.ExecQueryer, envelopes []facts.Envelope) error {
	if database == nil || len(envelopes) == 0 {
		return nil
	}
	factIDs := acceptedFactIDs(envelopes)
	if len(factIDs) == 0 {
		return nil
	}
	if _, err := database.ExecContext(ctx, deletePackageRegistryIdentityKeysSQL, array.StringArray(factIDs)); err != nil {
		return fmt.Errorf("delete package registry identity keys: %w", err)
	}
	rows := packageRegistryIdentityKeyRows(envelopes)
	for start := 0; start < len(rows); start += packageRegistryIdentityKeyBatchSize {
		end := min(start+packageRegistryIdentityKeyBatchSize, len(rows))
		if err := insertPackageRegistryIdentityKeyBatch(ctx, database, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func insertPackageRegistryIdentityKeyBatch(ctx context.Context, database db.ExecQueryer, rows []packageRegistryIdentityKeyRow) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*packageRegistryIdentityKeyColumns)
	var values strings.Builder
	for index, row := range rows {
		if index > 0 {
			values.WriteString(", ")
		}
		offset := index * packageRegistryIdentityKeyColumns
		fmt.Fprintf(&values, "($%d, $%d, $%d, $%d, $%d, $%d)", offset+1, offset+2, offset+3, offset+4, offset+5, offset+6)
		args = append(args, row.FactID, row.ScopeID, row.GenerationID, row.Ecosystem, row.PackageName, row.PackageID)
	}
	if _, err := database.ExecContext(ctx, insertPackageRegistryIdentityKeyPrefix+values.String()+insertPackageRegistryIdentityKeySuffix, args...); err != nil {
		return fmt.Errorf("insert package registry identity key batch (%d rows): %w", len(rows), err)
	}
	return nil
}

const (
	deletePackageRegistryIdentityKeysSQL   = `DELETE FROM package_registry_identity_keys WHERE fact_id = ANY($1::text[])`
	insertPackageRegistryIdentityKeyPrefix = `INSERT INTO package_registry_identity_keys (fact_id, scope_id, generation_id, ecosystem, package_name, package_id) VALUES `
	insertPackageRegistryIdentityKeySuffix = `ON CONFLICT (fact_id, ecosystem, package_name, package_id) DO UPDATE SET scope_id=EXCLUDED.scope_id, generation_id=EXCLUDED.generation_id`
)
