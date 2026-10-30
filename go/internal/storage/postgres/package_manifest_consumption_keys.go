// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/consumption"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
		keys := packageidentity.ManifestConsumptionKeys(packageidentity.ManifestKeyInput{
			PackageManager:    packageManifestMetadataString(payload, "package_manager"),
			Names:             packageManifestDependencyNames(payload),
			SBTCrossVersion:   packageManifestMetadataString(payload, "sbt_cross_version"),
			SBTBinaryArtifact: packageManifestMetadataString(payload, "sbt_binary_artifact"),
		})
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
		!packageManifestMetadataBool(payload, "source_ambiguous") &&
		!IsCentralVersionDeclaration(payload)
}

// IsCentralVersionDeclaration reports whether a dependency fact payload is a
// central version pin, such as a NuGet PackageVersion item in
// Directory.Packages.props (#8824). A pin sets the version a project gets if
// it references the package; it does not prove any project does, so it is
// never package consumption. The flag is read top-level first, then from
// entity_metadata, ASCII-trimmed, and matches "true" case-insensitively.
// NotCentralVersionDeclarationSQL is the SQL form; the two classify every
// payload alike, which a table test and a live test pin on the same shapes.
// The reducer's isCentralVersionDeclaration (go/internal/reducer/packages/
// correlation) mirrors this rule without importing it and mirrors the table.
func IsCentralVersionDeclaration(payload map[string]any) bool {
	text := centralFlagText(payload, "central_version_declaration")
	if text == "" {
		metadata, _ := payload["entity_metadata"].(map[string]any)
		text = centralFlagText(metadata, "central_version_declaration")
	}
	return strings.EqualFold(text, "true")
}

// centralFlagText returns the flag as the SQL form reads it: null and a
// missing key are empty, and only ASCII blanks are trimmed (the set BTRIM
// takes in NotCentralVersionDeclarationSQL), with no special text for a
// literal "<nil>" and no Unicode-space trim. Matching the SQL rule exactly
// is what keeps the sidecar and the readiness count in agreement.
func centralFlagText(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return strings.Trim(fmt.Sprint(value), " \t\n\r\f\v")
}

// NotCentralVersionDeclarationSQL is the SQL form of !IsCentralVersionDeclaration
// over a jsonb column named payload: true unless the fact is a central version
// pin. A missing flag, null, empty string, or any value other than "true"
// (trimmed, any case) keeps the row, so the COALESCE never lets a NULL drop a
// real dependency. Readiness splices it into the repository-anchored
// package.consumption count; the package-anchored count reads
// package_manifest_consumption_keys, which never holds a pin because
// packageManifestConsumptionKeyRows skips it.
const NotCentralVersionDeclarationSQL = `COALESCE(LOWER(COALESCE(
        NULLIF(BTRIM(payload->>'central_version_declaration', E' \t\n\r\f\v'), ''),
        BTRIM(payload->'entity_metadata'->>'central_version_declaration', E' \t\n\r\f\v'))), '') <> 'true'`

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
	_, err := writePackageManifestConsumptionKeys(ctx, database, envelopes)
	return err
}

// writePackageManifestConsumptionKeys replaces the sidecar keys of envelopes
// and returns, by ecosystem, how many manifest facts it keyed under a package
// name that holds non-ASCII letters (#9584). The ingester records that count;
// the backfill rebuild discards it so a rebuilt scope is not counted twice.
func writePackageManifestConsumptionKeys(ctx context.Context, database db.ExecQueryer, envelopes []facts.Envelope) (map[string]int64, error) {
	if database == nil || len(envelopes) == 0 {
		return nil, nil
	}
	factIDs := acceptedFactIDs(envelopes)
	if len(factIDs) == 0 {
		return nil, nil
	}
	if _, err := database.ExecContext(ctx, deletePackageManifestConsumptionKeysSQL, array.StringArray(factIDs)); err != nil {
		return nil, fmt.Errorf("delete package manifest consumption keys: %w", err)
	}
	rows := packageManifestConsumptionKeyRows(envelopes)
	for start := 0; start < len(rows); start += packageManifestConsumptionKeyBatchSize {
		end := min(start+packageManifestConsumptionKeyBatchSize, len(rows))
		if err := insertPackageManifestConsumptionKeyBatch(ctx, database, rows[start:end]); err != nil {
			return nil, err
		}
	}
	return packageManifestNonASCIINameCounts(rows), nil
}

// packageManifestNonASCIINameCounts counts, by ecosystem, the distinct manifest
// facts that hold at least one key whose package name has a non-ASCII byte.
// LowerASCII keeps such letters as written, so every key of a non-ASCII name
// carries them and one key is enough to classify the fact. The ecosystem is
// the normalized key ecosystem, a closed set, so the metric label is bounded.
func packageManifestNonASCIINameCounts(rows []packageManifestConsumptionKeyRow) map[string]int64 {
	var counts map[string]int64
	var counted map[string]struct{}
	for _, row := range rows {
		if !packageidentity.ContainsNonASCII(row.PackageName) {
			continue
		}
		if counts == nil {
			counts = make(map[string]int64)
			counted = make(map[string]struct{})
		}
		if _, ok := counted[row.FactID]; ok {
			continue
		}
		counted[row.FactID] = struct{}{}
		counts[row.Ecosystem]++
	}
	return counts
}

// recordPackageManifestNonASCIINames adds counts to
// eshu_dp_package_manifest_non_ascii_names_total and writes one log line per
// ecosystem, so an operator can see how many manifest rows the #9584
// ASCII-only fold keeps apart from an ASCII package name, and in which scope.
func recordPackageManifestNonASCIINames(ctx context.Context, instruments *telemetry.Instruments, scopeID, generationID string, counts map[string]int64) {
	for ecosystem, count := range counts {
		if count <= 0 {
			continue
		}
		if instruments != nil && instruments.PackageManifestNonASCIINames != nil {
			instruments.PackageManifestNonASCIINames.Add(ctx, count, metric.WithAttributes(
				attribute.String(telemetry.MetricDimensionEcosystem, ecosystem)))
		}
		log.Printf("event_name=package_manifest_consumption_keys.non_ascii_names %s=%q %s=%q ecosystem=%q rows=%d",
			telemetry.LogKeyScopeID, scopeID, telemetry.LogKeyGenerationID, generationID, ecosystem, count)
	}
}

func insertPackageManifestConsumptionKeyBatch(ctx context.Context, database db.ExecQueryer, rows []packageManifestConsumptionKeyRow) error {
	_, err := execPackageManifestConsumptionKeyBatch(ctx, database, rows, insertPackageManifestConsumptionKeySuffix)
	return err
}

// execPackageManifestConsumptionKeyBatch inserts rows with the given conflict
// suffix and returns the statement's result. A nil result with a nil error
// means there was nothing to insert.
func execPackageManifestConsumptionKeyBatch(ctx context.Context, database db.ExecQueryer, rows []packageManifestConsumptionKeyRow, suffix string) (sql.Result, error) {
	if len(rows) == 0 {
		return nil, nil
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
	result, err := database.ExecContext(ctx, insertPackageManifestConsumptionKeyPrefix+values.String()+suffix, args...)
	if err != nil {
		return nil, fmt.Errorf("insert package manifest consumption key batch (%d rows): %w", len(rows), err)
	}
	return result, nil
}

// insertPackageManifestConsumptionKeysIfAbsent adds the keys derived from
// envelopes that are not already stored and returns how many it inserted. It
// is the sbt repair's writer (#8961): it never deletes and never overwrites,
// so a key a concurrent writer already stored is left exactly as it is.
func insertPackageManifestConsumptionKeysIfAbsent(ctx context.Context, database db.ExecQueryer, envelopes []facts.Envelope) (int64, error) {
	rows := packageManifestConsumptionKeyRows(envelopes)
	var inserted int64
	for start := 0; start < len(rows); start += packageManifestConsumptionKeyBatchSize {
		end := min(start+packageManifestConsumptionKeyBatchSize, len(rows))
		result, err := execPackageManifestConsumptionKeyBatch(ctx, database, rows[start:end], insertPackageManifestConsumptionKeyIfAbsentSuffix)
		if err != nil {
			return inserted, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return inserted, fmt.Errorf("count package manifest consumption key batch (%d rows): %w", end-start, err)
		}
		inserted += affected
	}
	return inserted, nil
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

const insertPackageManifestConsumptionKeyIfAbsentSuffix = `
ON CONFLICT (fact_id, ecosystem, package_name) DO NOTHING
`

// listPackageManifestConsumptionKeySBTScopeFactsSQL is the sbt repair's page
// query: the scope-facts query above plus one predicate, derived from it so the
// two cannot drift. The predicate only narrows rows migration 181's index
// already returns in (scope_id, fact_id) order.
var listPackageManifestConsumptionKeySBTScopeFactsSQL = strings.Replace(
	listPackageManifestConsumptionKeyScopeFactsSQL,
	"ORDER BY fact_id",
	"  AND payload->'entity_metadata'->>'package_manager' = 'sbt'\nORDER BY fact_id",
	1,
)

// listPackageManifestSBTRepairScopesSQL lists the scopes that hold an sbt
// dependency row, after a cursor, reading the package_manager = 'sbt' prefix of
// fact_records_active_package_dependency_entity_idx.
const listPackageManifestSBTRepairScopesSQL = `
SELECT DISTINCT scope_id
FROM fact_records
WHERE fact_kind = 'content_entity'
  AND source_system = 'git'
  AND payload->>'entity_type' = 'Variable'
  AND payload->'entity_metadata'->>'config_kind' = 'dependency'
  AND payload->'entity_metadata'->>'package_manager' = 'sbt'
  AND scope_id > $1
ORDER BY scope_id
LIMIT $2
`

// sbtRepair wires the additive sbt repair to this package's page loader and
// key writer. The page query is the shipped scope-facts query with one more
// predicate, so it keeps migration 181's index.
func (b PackageManifestConsumptionKeyBackfill) sbtRepair(beginner db.Beginner) consumption.SBTRepair {
	return consumption.SBTRepair{
		Beginner:   beginner,
		ListScopes: listPackageManifestSBTRepairScopes,
		LoadPage: func(ctx context.Context, queryer db.Queryer, scopeID, afterFactID string) ([]facts.Envelope, error) {
			return loadPackageManifestConsumptionKeyFactsWith(ctx, queryer, listPackageManifestConsumptionKeySBTScopeFactsSQL, scopeID, afterFactID)
		},
		Insert:        insertPackageManifestConsumptionKeysIfAbsent,
		PageSize:      packageManifestConsumptionKeyBackfillBatchSize,
		ScopesPerPass: consumption.DefaultSBTRepairScopesPerPass,
		LockTimeout:   consumption.DefaultLockTimeout,
	}
}

func listPackageManifestSBTRepairScopes(ctx context.Context, queryer db.Queryer, cursor string, limit int) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, listPackageManifestSBTRepairScopesSQL, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list package manifest sbt repair scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopes []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("scan package manifest sbt repair scope: %w", err)
		}
		scopes = append(scopes, scopeID)
	}
	return scopes, rows.Err()
}

// logPackageManifestSBTRepairPass writes one line per repair pass that did
// work or failed, with the numbers an operator needs to see the repair move.
func logPackageManifestSBTRepairPass(report consumption.SBTRepairReport, err error) {
	if err == nil && !report.Pending && !report.Completed {
		return
	}
	line := fmt.Sprintf(
		"event_name=package_manifest_consumption_keys.sbt_repair.pass pending=%t completed=%t restarted=%t cursor=%q scopes=%d pages=%d facts=%d inserted_rows=%d walk_inserted_rows=%d max_transaction_ms=%d max_lock_wait_ms=%d duration_ms=%d",
		report.Pending, report.Completed, report.Restarted, report.Cursor, report.Scopes, report.Pages, report.Facts,
		report.InsertedRows, report.WalkInsertedRows, report.MaxTransaction.Milliseconds(), report.MaxLockWait.Milliseconds(), report.Duration.Milliseconds(),
	)
	if err != nil {
		line += fmt.Sprintf(" error=%q", err.Error())
	}
	log.Print(line)
}
