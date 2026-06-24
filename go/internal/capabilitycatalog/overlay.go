// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Overlay carries the editorial capability metadata that cannot be derived from
// the capability matrix: display names, owning packages, operational maturity
// overrides, known gaps, linked issues, docs, and the exemption lists that
// record intentional reconciliation gaps. It is stored in
// specs/capability-catalog.v1.yaml.
type Overlay struct {
	// Version is the overlay schema version.
	Version string
	// Capabilities holds per-capability editorial entries keyed by capability id.
	Capabilities []OverlayCapability
	// ToolExemptions records MCP tools intentionally not mapped to a capability.
	ToolExemptions []OverlayToolExemption
	// NonMCPSurfaces records declared matrix tools that are API or logical
	// surfaces rather than MCP tools.
	NonMCPSurfaces []OverlayNonMCPSurface
}

// OverlayCapability is the editorial overlay for one capability id.
type OverlayCapability struct {
	// Capability is the capability id this overlay augments.
	Capability string
	// DisplayName overrides the derived human-readable name.
	DisplayName string
	// OwnerPackage is the Go import path (relative to go/) that owns the
	// capability. It is validated to exist during reconciliation.
	OwnerPackage string
	// Maturity, when set, overrides the matrix-derived maturity. Only
	// overlay-only states (gated, degraded) are permitted.
	Maturity Maturity
	// Reason explains a maturity override and is required when Maturity is set.
	Reason string
	// KnownGaps lists tracked gaps for the capability.
	KnownGaps []string
	// LinkedIssues lists GitHub issue numbers driving the capability.
	LinkedIssues []int
	// Docs lists doc paths (relative to docs/) that describe the capability.
	Docs []string
	// Console marks the capability as surfaced in the console capability matrix.
	Console bool
}

// OverlayToolExemption records an MCP tool that the matrix tools field does not
// reference. When Capability is empty the tool is a genuine utility with no
// product capability; when Capability is set the tool is an alias surface of an
// existing capability and is attached to that catalog entry.
type OverlayToolExemption struct {
	// Tool is the MCP tool name.
	Tool string
	// Capability optionally maps the tool to an existing capability id. When
	// set, the tool is attached to that entry as an MCP surface.
	Capability string
	// Reason explains why the tool is exempt or aliased.
	Reason string
	// Issue optionally links a tracking issue.
	Issue int
}

// OverlayNonMCPSurface records a matrix-declared tool that is served through an
// API or logical surface rather than the MCP registry.
type OverlayNonMCPSurface struct {
	// Tool is the matrix-declared surface name.
	Tool string
	// Kind is the surface kind (api or logical).
	Kind SurfaceKind
	// Reason explains the surface classification.
	Reason string
}

// OverlayFileName is the overlay file inside the specs directory.
const OverlayFileName = "capability-catalog.v1.yaml"

type overlayFile struct {
	Version        string                  `yaml:"version"`
	Capabilities   []overlayFileCapability `yaml:"capabilities"`
	ToolExemptions []overlayFileExemption  `yaml:"tool_exemptions"`
	NonMCPSurfaces []overlayFileSurface    `yaml:"non_mcp_surfaces"`
}

type overlayFileCapability struct {
	Capability   string   `yaml:"capability"`
	DisplayName  string   `yaml:"display_name"`
	OwnerPackage string   `yaml:"owner_package"`
	Maturity     string   `yaml:"maturity"`
	Reason       string   `yaml:"maturity_reason"`
	KnownGaps    []string `yaml:"known_gaps"`
	LinkedIssues []int    `yaml:"linked_issues"`
	Docs         []string `yaml:"docs"`
	Console      bool     `yaml:"console"`
}

type overlayFileExemption struct {
	Tool       string `yaml:"tool"`
	Capability string `yaml:"capability"`
	Reason     string `yaml:"reason"`
	Issue      int    `yaml:"issue"`
}

type overlayFileSurface struct {
	Tool   string `yaml:"tool"`
	Kind   string `yaml:"kind"`
	Reason string `yaml:"reason"`
}

// LoadOverlay reads the overlay from path. A missing file yields an empty
// overlay so the catalog can be built from the matrix alone.
func LoadOverlay(path string) (Overlay, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Overlay{}, nil
		}
		return Overlay{}, fmt.Errorf("read overlay %s: %w", path, err)
	}
	var parsed overlayFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return Overlay{}, fmt.Errorf("parse overlay %s: %w", path, err)
	}
	return convertOverlay(parsed), nil
}

func convertOverlay(parsed overlayFile) Overlay {
	overlay := Overlay{Version: parsed.Version}
	for _, raw := range parsed.Capabilities {
		overlay.Capabilities = append(overlay.Capabilities, OverlayCapability{
			Capability:   raw.Capability,
			DisplayName:  raw.DisplayName,
			OwnerPackage: raw.OwnerPackage,
			Maturity:     Maturity(raw.Maturity),
			Reason:       raw.Reason,
			KnownGaps:    raw.KnownGaps,
			LinkedIssues: raw.LinkedIssues,
			Docs:         raw.Docs,
			Console:      raw.Console,
		})
	}
	for _, raw := range parsed.ToolExemptions {
		overlay.ToolExemptions = append(overlay.ToolExemptions, OverlayToolExemption(raw))
	}
	for _, raw := range parsed.NonMCPSurfaces {
		overlay.NonMCPSurfaces = append(overlay.NonMCPSurfaces, OverlayNonMCPSurface{
			Tool:   raw.Tool,
			Kind:   SurfaceKind(raw.Kind),
			Reason: raw.Reason,
		})
	}
	return overlay
}
