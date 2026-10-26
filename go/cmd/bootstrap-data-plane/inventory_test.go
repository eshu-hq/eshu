// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/stretchr/testify/require"
)

type inventoryFixture struct {
	FormatVersion  int    `json:"format_version"`
	DefaultVariant string `json:"default_variant"`
	Migrations     []struct {
		Path               string `json:"path"`
		Variant            string `json:"variant"`
		ChecksumSHA256     string `json:"checksum_sha256"`
		FullChecksumSHA256 string `json:"full_checksum_sha256"`
	} `json:"migrations"`
}

func bootstrapProcess(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(self, append([]string{"-test.run=^TestInventoryCommandProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(),
		"ESHU_INVENTORY_TEST_PROCESS=1",
		"ESHU_POSTGRES_DSN=not-a-valid-dsn",
		"ESHU_GRAPH_BACKEND=invalid-backend",
		"OTEL_EXPORTER_OTLP_ENDPOINT=://invalid",
	)
	return cmd.CombinedOutput()
}

func TestInventoryCommandProcess(t *testing.T) {
	if os.Getenv("ESHU_INVENTORY_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"eshu-bootstrap-data-plane"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing subprocess argument separator")
}

func TestPostgresMigrationInventoryCommand(t *testing.T) {
	first, err := bootstrapProcess(t, "--postgres-migration-inventory")
	require.NoError(t, err, string(first))
	second, err := bootstrapProcess(t, "--postgres-migration-inventory")
	require.NoError(t, err, string(second))
	require.Equal(t, first, second)

	var inventory inventoryFixture
	require.NoError(t, json.Unmarshal(first, &inventory), string(first))
	require.Equal(t, 1, inventory.FormatVersion)
	require.Equal(t, "full", inventory.DefaultVariant)
	full := postgres.BootstrapDefinitions()
	require.Len(t, inventory.Migrations, len(full)+4)
	fullChecksums := make(map[string]string, len(full))
	for _, definition := range full {
		data, err := os.ReadFile(filepath.Join("..", "..", "internal", "storage", "postgres", "migrations", filepath.Base(definition.Path)))
		require.NoError(t, err)
		sum := sha256.Sum256(data)
		fullChecksums[definition.Path] = hex.EncodeToString(sum[:])
	}
	deferredChecksums := make(map[string]string)
	for _, definition := range postgres.BootstrapDefinitionsWithoutContentSearchIndexes() {
		if definition.Variant != "deferred" {
			continue
		}
		sum := sha256.Sum256([]byte(definition.SQL))
		deferredChecksums[definition.Path] = hex.EncodeToString(sum[:])
	}
	deferred := map[string]bool{
		"content_store":                          true,
		"content_substring_index_state":          true,
		"content_entity_name_trgm_index":         true,
		"content_files_relative_path_trgm_index": true,
	}
	seen := make(map[string]bool, len(inventory.Migrations))
	var previousPath, previousVariant string
	deferredCount := 0
	for _, row := range inventory.Migrations {
		require.True(t, row.Path > previousPath || row.Path == previousPath && previousVariant == "full" && row.Variant == "deferred", "wrong row order at %s/%s", row.Path, row.Variant)
		previousPath, previousVariant = row.Path, row.Variant
		require.False(t, seen[row.Path+"/"+row.Variant], "duplicate %s/%s", row.Path, row.Variant)
		seen[row.Path+"/"+row.Variant] = true
		require.Len(t, row.ChecksumSHA256, 64)
		switch row.Variant {
		case "full":
			require.Equal(t, fullChecksums[row.Path], row.ChecksumSHA256)
			require.Empty(t, row.FullChecksumSHA256)
		case "deferred":
			deferredCount++
			name := strings.TrimSuffix(strings.SplitN(filepath.Base(row.Path), "_", 2)[1], ".sql")
			require.True(t, deferred[name], "unexpected deferred variant %s", name)
			require.Equal(t, deferredChecksums[row.Path], row.ChecksumSHA256)
			require.Equal(t, fullChecksums[row.Path], row.FullChecksumSHA256)
		default:
			t.Fatalf("unknown variant %q", row.Variant)
		}
	}
	require.Equal(t, 4, deferredCount)
}

func TestInventoryInvalidArgumentsFailBeforeBootstrap(t *testing.T) {
	for _, args := range [][]string{
		{"--postgres-migration-inventory", "--version"},
		{"--postgres-migration-inventory=maybe"},
		{"--postgres-migration-inventory", "unknown"},
		{"--postgres-migrations-inventory"},
		{"--version", "--postgres-migration-inventory"},
		{"--help"},
		{"unrelated"},
	} {
		out, err := bootstrapProcess(t, args...)
		require.Error(t, err, "%v: %s", args, out)
		require.Contains(t, string(out), "--postgres-migration-inventory")
		require.NotContains(t, string(out), "runtime.startup.failed")
		require.NotContains(t, string(out), "invalid ESHU_GRAPH_BACKEND")
	}
}

func TestInventoryFlagPreservesVersionAndDefaultStartup(t *testing.T) {
	for _, arg := range []string{"--version", "-v"} {
		out, err := bootstrapProcess(t, arg)
		require.NoError(t, err, string(out))
		require.Contains(t, string(out), "eshu-bootstrap-data-plane ")
	}
	out, err := bootstrapProcess(t)
	require.Error(t, err)
	require.NotContains(t, string(out), "\"format_version\"")
}
