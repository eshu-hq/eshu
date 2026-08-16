// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"fmt"
	"strings"
)

// ConfigureDatabaseBackend persists the graph-backend selection behind
// `eshu config db <backend>` and returns the canonical backend name it wrote.
//
// rawBackend is trimmed and lower-cased first, and "nornic" is accepted as an
// alias for "nornicdb". Three keys move together because the runtime reads all
// three -- ESHU_GRAPH_BACKEND selects the driver while DEFAULT_DATABASE and
// ESHU_NEO4J_DATABASE name the database inside it -- so leaving any of them
// behind would point the CLI at one backend and the driver at another.
//
// An unrecognized backend returns an error naming the normalized value and
// both accepted names, and writes nothing.
func ConfigureDatabaseBackend(rawBackend string) (string, error) {
	backend := strings.ToLower(strings.TrimSpace(rawBackend))
	switch backend {
	case "nornicdb", "nornic":
		if err := SetValue("ESHU_GRAPH_BACKEND", "nornicdb"); err != nil {
			return "", err
		}
		if err := SetValue("DEFAULT_DATABASE", "nornic"); err != nil {
			return "", err
		}
		if err := SetValue("ESHU_NEO4J_DATABASE", "nornic"); err != nil {
			return "", err
		}
		return "nornicdb", nil
	case "neo4j":
		if err := SetValue("ESHU_GRAPH_BACKEND", "neo4j"); err != nil {
			return "", err
		}
		if err := SetValue("DEFAULT_DATABASE", "neo4j"); err != nil {
			return "", err
		}
		if err := SetValue("ESHU_NEO4J_DATABASE", "neo4j"); err != nil {
			return "", err
		}
		return "neo4j", nil
	default:
		return "", fmt.Errorf("invalid backend: %s (must be nornicdb or neo4j)", backend)
	}
}
