// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SurfaceOverlayFileName is the surface inventory overlay file inside the specs
// directory.
const SurfaceOverlayFileName = "surface-inventory.v1.yaml"

// embeddedSurfaceInventory is the committed, generated surface inventory
// artifact. It is produced by cmd/capability-inventory and embedded so runtime
// surfaces can read the reconciled inventory without enumerating live code or
// the source tree. A golden drift test keeps it in lockstep with live surfaces.
//
//go:embed data/surface-inventory.generated.json
var embeddedSurfaceInventory []byte

// RawSurfaceArtifact returns the committed surface inventory artifact bytes
// exactly as embedded. The generator's verify mode compares a fresh build
// against these bytes so the drift gate catches any deviation.
func RawSurfaceArtifact() []byte {
	return embeddedSurfaceInventory
}

// LoadSurfaceInventory returns the embedded, generated surface inventory. It is
// the runtime entry point for surfaces that serve the inventory.
func LoadSurfaceInventory() (SurfaceInventory, error) {
	var inv SurfaceInventory
	if err := json.Unmarshal(embeddedSurfaceInventory, &inv); err != nil {
		return SurfaceInventory{}, fmt.Errorf("decode embedded surface inventory: %w", err)
	}
	return inv, nil
}

type surfaceOverlayFile struct {
	Version  string                     `yaml:"version"`
	Surfaces []surfaceOverlayFileRecord `yaml:"surfaces"`
}

type surfaceOverlayFileRecord struct {
	Category  string   `yaml:"category"`
	Name      string   `yaml:"name"`
	Readiness string   `yaml:"readiness"`
	Owner     string   `yaml:"owner"`
	Proof     string   `yaml:"proof"`
	Docs      []string `yaml:"docs"`
	Notes     string   `yaml:"notes"`
}

// LoadSurfaceOverlay reads the surface inventory overlay from path. A missing
// file yields an empty overlay so the inventory can be built from live surfaces
// and category defaults alone (which then flags any unclassified collector).
func LoadSurfaceOverlay(path string) (SurfaceOverlay, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SurfaceOverlay{}, nil
		}
		return SurfaceOverlay{}, fmt.Errorf("read surface overlay %s: %w", path, err)
	}
	var parsed surfaceOverlayFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return SurfaceOverlay{}, fmt.Errorf("parse surface overlay %s: %w", path, err)
	}
	overlay := SurfaceOverlay{Version: parsed.Version}
	for _, rec := range parsed.Surfaces {
		overlay.Surfaces = append(overlay.Surfaces, SurfaceOverlayRecord{
			Category:  SurfaceCategory(rec.Category),
			Name:      rec.Name,
			Readiness: ReadinessLane(rec.Readiness),
			Owner:     rec.Owner,
			Proof:     rec.Proof,
			Docs:      rec.Docs,
			Notes:     rec.Notes,
		})
	}
	return overlay, nil
}
