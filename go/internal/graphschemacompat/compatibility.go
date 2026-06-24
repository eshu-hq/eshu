// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphschemacompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/graph"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const latestGraphSchemaApplicationQuery = `
SELECT schema_fingerprint, compatible_fingerprints
FROM graph_schema_applications
WHERE backend = $1
ORDER BY applied_at DESC
LIMIT 1
`

const markGraphSchemaAppliedQuery = `
INSERT INTO graph_schema_applications (
    backend,
    schema_fingerprint,
    statement_count,
    compatible_fingerprints,
    applied_at
) VALUES (
    $1, $2, $3, $4::jsonb, NOW()
)
ON CONFLICT (backend, schema_fingerprint) DO UPDATE
SET statement_count = EXCLUDED.statement_count,
    compatible_fingerprints = EXCLUDED.compatible_fingerprints,
    applied_at = EXCLUDED.applied_at
`

// ErrMissingMarker reports that no graph schema application marker exists for
// the selected backend.
var ErrMissingMarker = errors.New("graph schema marker missing")

// Result describes the schema compatibility decision made during startup.
type Result struct {
	Backend                graph.SchemaBackend
	ExpectedFingerprint    string
	AppliedFingerprint     string
	CompatibleFingerprints []string
}

// MarkApplied records that schema bootstrap applied app successfully.
func MarkApplied(ctx context.Context, db postgres.Executor, app graph.SchemaApplication) error {
	if db == nil {
		return fmt.Errorf("graph schema marker executor is required")
	}
	compatible := app.CompatibleFingerprints
	if compatible == nil {
		compatible = []string{}
	}
	compatibleFingerprints, err := json.Marshal(compatible)
	if err != nil {
		return fmt.Errorf("encode compatible graph schema fingerprints: %w", err)
	}
	if _, err := db.ExecContext(
		ctx,
		markGraphSchemaAppliedQuery,
		string(app.Backend),
		app.Fingerprint,
		app.StatementCount,
		string(compatibleFingerprints),
	); err != nil {
		return fmt.Errorf("mark graph schema applied: %w", err)
	}
	return nil
}

// RequireCompatibleForRuntime validates the graph schema marker for the backend
// selected by ESHU_GRAPH_BACKEND.
func RequireCompatibleForRuntime(
	ctx context.Context,
	db postgres.Queryer,
	getenv func(string) string,
) (Result, error) {
	if graphCompatibilityDisabled(getenv) {
		return Result{}, nil
	}
	runtimeBackend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return Result{}, err
	}
	backend, err := schemaBackendForRuntime(runtimeBackend)
	if err != nil {
		return Result{}, err
	}
	return RequireCompatible(ctx, db, backend)
}

// RequireCompatible validates that the latest applied graph schema for backend
// is safe for the current writer.
func RequireCompatible(ctx context.Context, db postgres.Queryer, backend graph.SchemaBackend) (Result, error) {
	if db == nil {
		return Result{}, fmt.Errorf("graph schema compatibility queryer is required")
	}

	expected, err := graph.SchemaApplicationForBackend(backend)
	if err != nil {
		return Result{}, err
	}

	rows, err := db.QueryContext(ctx, latestGraphSchemaApplicationQuery, string(backend))
	if err != nil {
		return Result{}, fmt.Errorf("query graph schema compatibility marker: %w", err)
	}
	if rows == nil {
		return Result{}, fmt.Errorf("query graph schema compatibility marker: rows are required")
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Result{}, fmt.Errorf("query graph schema compatibility marker: %w", err)
		}
		return Result{}, fmt.Errorf(
			"%w for backend %s; run eshu-bootstrap-data-plane before starting graph writers",
			ErrMissingMarker,
			backend,
		)
	}

	var appliedFingerprint string
	var compatibleRaw []byte
	if err := rows.Scan(&appliedFingerprint, &compatibleRaw); err != nil {
		return Result{}, fmt.Errorf("scan graph schema compatibility marker: %w", err)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("query graph schema compatibility marker: %w", err)
	}

	compatible, err := decodeCompatibleFingerprints(compatibleRaw)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Backend:                backend,
		ExpectedFingerprint:    expected.Fingerprint,
		AppliedFingerprint:     appliedFingerprint,
		CompatibleFingerprints: compatible,
	}
	if appliedFingerprint == expected.Fingerprint || slices.Contains(compatible, expected.Fingerprint) {
		return result, nil
	}

	return Result{}, fmt.Errorf(
		"graph schema incompatible for backend %s: runtime expects fingerprint %s, latest applied fingerprint is %s; run eshu-bootstrap-data-plane with the matching image before starting graph writers",
		backend,
		expected.Fingerprint,
		appliedFingerprint,
	)
}

func schemaBackendForRuntime(backend runtimecfg.GraphBackend) (graph.SchemaBackend, error) {
	switch backend {
	case runtimecfg.GraphBackendNeo4j:
		return graph.SchemaBackendNeo4j, nil
	case runtimecfg.GraphBackendNornicDB:
		return graph.SchemaBackendNornicDB, nil
	default:
		return "", fmt.Errorf("unsupported graph backend for schema compatibility %q", backend)
	}
}

func decodeCompatibleFingerprints(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var compatible []string
	if err := json.Unmarshal(raw, &compatible); err != nil {
		return nil, fmt.Errorf("decode graph schema compatible fingerprints: %w", err)
	}
	return compatible, nil
}

func graphCompatibilityDisabled(getenv func(string) string) bool {
	if strings.EqualFold(strings.TrimSpace(getenv("ESHU_DISABLE_NEO4J")), "true") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(getenv("ESHU_QUERY_PROFILE")), "local_lightweight")
}
