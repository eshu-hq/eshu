// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

const postgresMigrationInventoryFlag = "--postgres-migration-inventory"

type postgresMigrationInventory struct {
	FormatVersion  int                             `json:"format_version"`
	DefaultVariant string                          `json:"default_variant"`
	Migrations     []postgresMigrationInventoryRow `json:"migrations"`
}

type postgresMigrationInventoryRow struct {
	Path               string `json:"path"`
	Variant            string `json:"variant"`
	ChecksumSHA256     string `json:"checksum_sha256"`
	FullChecksumSHA256 string `json:"full_checksum_sha256,omitempty"`
}

// printPostgresMigrationInventoryFlag handles the offline mode and rejects
// unsupported arguments before normal bootstrap can change either store.
func printPostgresMigrationInventoryFlag(args []string, output io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) != 1 || args[0] != postgresMigrationInventoryFlag {
		return true, fmt.Errorf("unsupported arguments: use %s, --version, or -v alone", postgresMigrationInventoryFlag)
	}
	return true, json.NewEncoder(output).Encode(buildPostgresMigrationInventory())
}

func buildPostgresMigrationInventory() postgresMigrationInventory {
	full := postgres.BootstrapDefinitions()
	deferred := postgres.BootstrapDefinitionsWithoutContentSearchIndexes()
	deferredByPath := make(map[string]migrations.Definition, len(deferred))
	for _, definition := range deferred {
		if definition.Variant == "deferred" {
			deferredByPath[definition.Path] = definition
		}
	}
	rows := make([]postgresMigrationInventoryRow, 0, len(full)+len(deferredByPath))
	for _, definition := range full {
		rows = append(rows, postgresMigrationInventoryRow{
			Path:           definition.Path,
			Variant:        "full",
			ChecksumSHA256: migrations.Checksum(definition.SQL),
		})
		if variant, exists := deferredByPath[definition.Path]; exists {
			rows = append(rows, postgresMigrationInventoryRow{
				Path:               variant.Path,
				Variant:            variant.Variant,
				ChecksumSHA256:     migrations.Checksum(variant.SQL),
				FullChecksumSHA256: variant.FullChecksum,
			})
		}
	}
	return postgresMigrationInventory{
		FormatVersion:  1,
		DefaultVariant: "full",
		Migrations:     rows,
	}
}
