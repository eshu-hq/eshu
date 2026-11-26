// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// graphSchemaInventoryFlag prints the checked-in graph schema fingerprint for
// every backend without touching any store, so a pin author can compare the
// fingerprint a new image expects against the marker the last bootstrap wrote
// (see #11506). Values come only from graph.SchemaApplicationForBackend, the
// same source the startup fence compares against.
const graphSchemaInventoryFlag = "--graph-schema-inventory"

// graphSchemaInventoryBackends fixes the stdout order: neo4j first, then
// nornicdb. The mode never reads ESHU_GRAPH_BACKEND; it always prints both.
var graphSchemaInventoryBackends = []graph.SchemaBackend{
	graph.SchemaBackendNeo4j,
	graph.SchemaBackendNornicDB,
}

type graphSchemaInventory struct {
	FormatVersion int                           `json:"format_version"`
	Backends      []graphSchemaInventoryBackend `json:"backends"`
}

type graphSchemaInventoryBackend struct {
	Backend                string   `json:"backend"`
	Fingerprint            string   `json:"fingerprint"`
	StatementCount         int      `json:"statement_count"`
	CompatibleFingerprints []string `json:"compatible_fingerprints"`
}

// printGraphSchemaInventoryFlag handles the offline mode. It runs after the
// sole-argument shape check in printInventoryFlag, so reaching here means no
// store was touched and none will be.
func printGraphSchemaInventoryFlag(output io.Writer) error {
	inventory, err := buildGraphSchemaInventory()
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(inventory)
}

func buildGraphSchemaInventory() (graphSchemaInventory, error) {
	backends := make([]graphSchemaInventoryBackend, 0, len(graphSchemaInventoryBackends))
	for _, backend := range graphSchemaInventoryBackends {
		application, err := graph.SchemaApplicationForBackend(backend)
		if err != nil {
			return graphSchemaInventory{}, fmt.Errorf("graph schema inventory for backend %q: %w", backend, err)
		}
		compatible := application.CompatibleFingerprints
		if compatible == nil {
			compatible = []string{}
		}
		backends = append(backends, graphSchemaInventoryBackend{
			Backend:                string(application.Backend),
			Fingerprint:            application.Fingerprint,
			StatementCount:         application.StatementCount,
			CompatibleFingerprints: compatible,
		})
	}
	return graphSchemaInventory{
		FormatVersion: 1,
		Backends:      backends,
	}, nil
}
