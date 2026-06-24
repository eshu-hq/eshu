// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package skillgen

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// DefaultCatalogPath is the path S2 reads at gen time to derive the
// enabled collector list. The path is relative to the working directory
// of the `eshu skillgen gen|check` invocation; running from the repo root
// resolves the catalog under specs/.
//
// The catalog is the editorial overlay at specs/surface-inventory.v1.yaml
// and is the single source of truth for the collector set a skill can
// mention. The S1 design says per-collector MCP tools are enumerated
// from the live capability catalog, not from a static prose list, and
// this file is that contract.
const DefaultCatalogPath = "specs/surface-inventory.v1.yaml"

// CatalogSurface is the editorial overlay row from the surface
// inventory. Only `category: collector` and `readiness: implemented`
// rows are returned by LoadDefaultCatalog; other categories and
// readiness states are filtered out.
type CatalogSurface struct {
	Category  string `yaml:"category"`
	Name      string `yaml:"name"`
	Readiness string `yaml:"readiness"`
}

// catalogFile is the on-disk shape of specs/surface-inventory.v1.yaml.
// The file is small (under 200 lines today) and the shape is stable.
type catalogFile struct {
	Version  string           `yaml:"version"`
	Surfaces []CatalogSurface `yaml:"surfaces"`
}

// LoadDefaultCatalog reads DefaultCatalogPath and returns the sorted list
// of implemented collector names. A missing file is an error (fail
// closed) because the S1 design says the catalog is the single source of
// truth for the per-collector matrix; a partial or stale list would
// teach agents an incomplete surface.
//
// The function is read-only and does not write the file.
func LoadDefaultCatalog() ([]string, error) {
	return loadCatalogFrom(DefaultCatalogPath)
}

// loadCatalogFrom reads the catalog at path and returns the sorted list of
// implemented collector names. Exposed for tests so they can point at a
// fixture file without touching the repo-root path.
func loadCatalogFrom(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("catalog %s: file missing (the S1 design requires the editorial overlay as the single source of truth for collectors)", path)
		}
		return nil, fmt.Errorf("read catalog %s: %w", path, err)
	}
	var doc catalogFile
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse catalog %s: %w", path, err)
	}
	names := make([]string, 0, len(doc.Surfaces))
	for _, s := range doc.Surfaces {
		if s.Category != "collector" {
			continue
		}
		if s.Readiness != "implemented" {
			continue
		}
		names = append(names, s.Name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("catalog %s: no implemented collector surfaces found", path)
	}
	// Sort for deterministic output; the catalog author can add rows in
	// any order and the rendered skill remains stable.
	sort.Strings(names)
	return names, nil
}
