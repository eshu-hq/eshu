// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/stretchr/testify/require"
)

// graphInventoryFixture mirrors the --graph-schema-inventory stdout contract:
// one JSON object with a format version and one row per graph backend.
type graphInventoryFixture struct {
	FormatVersion int `json:"format_version"`
	Backends      []struct {
		Backend                string   `json:"backend"`
		Fingerprint            string   `json:"fingerprint"`
		StatementCount         int      `json:"statement_count"`
		CompatibleFingerprints []string `json:"compatible_fingerprints"`
	} `json:"backends"`
}

// TestGraphSchemaInventoryCommand proves the offline fingerprint inventory:
// exact JSON shape per backend, values identical to SchemaApplicationForBackend,
// byte-identical output across runs, and no store access (the subprocess runs
// with an invalid DSN, an invalid graph backend, and a broken OTEL endpoint).
func TestGraphSchemaInventoryCommand(t *testing.T) {
	first, err := bootstrapProcess(t, "--graph-schema-inventory")
	require.NoError(t, err, string(first.stderr))
	require.Empty(t, first.stderr)
	second, err := bootstrapProcess(t, "--graph-schema-inventory")
	require.NoError(t, err, string(second.stderr))
	require.Empty(t, second.stderr)
	require.Equal(t, first.stdout, second.stdout)

	var inventory graphInventoryFixture
	require.NoError(t, json.Unmarshal(first.stdout, &inventory), string(first.stdout))
	require.Equal(t, 1, inventory.FormatVersion)
	require.Len(t, inventory.Backends, 2)

	wantBackends := []graph.SchemaBackend{graph.SchemaBackendNeo4j, graph.SchemaBackendNornicDB}
	for i, want := range wantBackends {
		row := inventory.Backends[i]
		require.Equal(t, string(want), row.Backend)
		app, err := graph.SchemaApplicationForBackend(want)
		require.NoError(t, err)
		require.Equal(t, app.Fingerprint, row.Fingerprint)
		require.Equal(t, app.StatementCount, row.StatementCount)
		require.Len(t, row.Fingerprint, 64)
		require.Greater(t, row.StatementCount, 0)
		require.NotNil(t, row.CompatibleFingerprints)
		require.Equal(t, app.CompatibleFingerprints, row.CompatibleFingerprints)
	}

	// The encoder must not add trailing output beyond the single object.
	trimmed := bytes.TrimSpace(first.stdout)
	require.True(t, bytes.HasPrefix(trimmed, []byte("{")))
	require.True(t, bytes.HasSuffix(trimmed, []byte("}")))
}

// TestGraphInventoryInvalidArgumentsFailBeforeBootstrap proves every
// non-sole use of the flag exits non-zero before any store is touched.
func TestGraphInventoryInvalidArgumentsFailBeforeBootstrap(t *testing.T) {
	for _, args := range [][]string{
		{"--graph-schema-inventory", "--version"},
		{"--graph-schema-inventory=maybe"},
		{"--graph-schema-inventory", "unknown"},
		{"--graph-schema-inventories"},
		{"--version", "--graph-schema-inventory"},
		{"--postgres-migration-inventory", "--graph-schema-inventory"},
		{"--graph-schema-inventory", "--postgres-migration-inventory"},
	} {
		out, err := bootstrapProcess(t, args...)
		require.Error(t, err, "%v: %s", args, out.stderr)
		require.Empty(t, out.stdout)
		require.Contains(t, string(out.stderr), "--graph-schema-inventory")
		require.NotContains(t, string(out.stderr), "runtime.startup.failed")
		require.NotContains(t, string(out.stderr), "invalid ESHU_GRAPH_BACKEND")
	}
}
