// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"sort"
	"strings"
)

// SurfaceKind classifies how a capability tool is exposed.
type SurfaceKind string

const (
	// SurfaceMCP is a tool served through the MCP registry.
	SurfaceMCP SurfaceKind = "mcp"
	// SurfaceAPI is a tool served through an HTTP API route.
	SurfaceAPI SurfaceKind = "api"
	// SurfaceLogical is a documented logical surface name with no single tool.
	SurfaceLogical SurfaceKind = "logical"
	// SurfaceUnknown marks a declared tool with no matching evidence; it always
	// produces a reconciliation finding.
	SurfaceUnknown SurfaceKind = "unknown"
)

// Catalog is the reconciled capability catalog: the deterministic artifact
// consumed by docs, CI, the API, MCP, and the console.
type Catalog struct {
	// Version mirrors the overlay schema version.
	Version string `json:"version"`
	// Authorization is the v1 role, grant, and data-class catalog.
	Authorization AuthorizationCatalog `json:"authorization"`
	// Entries are the catalog entries sorted by capability id.
	Entries []Entry `json:"entries"`
}

// Entry is one capability's reconciled catalog record.
type Entry struct {
	// Capability is the stable capability id.
	Capability string `json:"capability"`
	// DisplayName is the human-readable name (overlay or derived from the id).
	DisplayName string `json:"display_name"`
	// OwnerPackage is the owning Go package import path, when declared.
	OwnerPackage string `json:"owner_package,omitempty"`
	// Maturity is the effective maturity (overlay override or derived).
	Maturity Maturity `json:"maturity"`
	// DerivedMaturity is the matrix-derived maturity before any overlay override.
	DerivedMaturity Maturity `json:"derived_maturity"`
	// MaturityReason explains an overlay maturity override.
	MaturityReason string `json:"maturity_reason,omitempty"`
	// Surfaces are the classified public surfaces for the capability.
	Surfaces []Surface `json:"surfaces"`
	// Profiles maps a runtime profile id to its support summary.
	Profiles map[string]EntryProfile `json:"profiles"`
	// ProofSignals are the deduplicated verification signals across profiles.
	ProofSignals []ProofSignal `json:"proof_signals"`
	// KnownGaps lists tracked gaps from the overlay.
	KnownGaps []string `json:"known_gaps,omitempty"`
	// LinkedIssues lists GitHub issue numbers from the overlay.
	LinkedIssues []int `json:"linked_issues,omitempty"`
	// Docs lists doc paths from the overlay.
	Docs []string `json:"docs,omitempty"`
	// Console reports whether the capability is surfaced in the console matrix.
	Console bool `json:"console"`
	// Authorization records the explicit role/grant/data-class requirement for
	// this capability.
	Authorization CapabilityAuthorization `json:"authorization"`
}

// Surface is one classified public surface for a capability.
type Surface struct {
	// Tool is the declared surface/tool name.
	Tool string `json:"tool"`
	// Kind classifies how the tool is exposed.
	Kind SurfaceKind `json:"kind"`
}

// EntryProfile summarizes a capability's support in one runtime profile.
type EntryProfile struct {
	// Status is supported, experimental, or unsupported.
	Status string `json:"status"`
	// MaxTruthLevel is the highest truth level allowed in the profile.
	MaxTruthLevel string `json:"max_truth_level,omitempty"`
	// RequiredRuntime is the runtime shape required for the profile.
	RequiredRuntime string `json:"required_runtime,omitempty"`
	// P95LatencyMS is the declared p95 latency budget in milliseconds.
	P95LatencyMS *int `json:"p95_latency_ms,omitempty"`
	// MaxScopeSize is the declared maximum supported scope for the profile.
	MaxScopeSize string `json:"max_scope_size,omitempty"`
}

// ProofSignal is one deduplicated verification signal.
type ProofSignal struct {
	// Kind is the proof kind (go_test, integration_test, compose_e2e,
	// remote_validation).
	Kind string `json:"kind"`
	// Ref is the proof reference.
	Ref string `json:"ref"`
}

// Signals carries the live code evidence used to reconcile declared surfaces.
type Signals struct {
	// MCPTools is the set of tool names registered in the MCP server.
	MCPTools map[string]bool
}

// Build reconciles the capability matrix, the editorial overlay, and live code
// signals into a deterministic catalog plus the list of reconciliation
// findings. The catalog is always returned best-effort; an empty findings slice
// means the catalog is fully reconciled. Build does not import the MCP or query
// packages; callers inject their registries through Signals.
func Build(matrix Matrix, overlay Overlay, signals Signals) (Catalog, []Finding) {
	return BuildWithAuthorization(matrix, overlay, AuthorizationCatalog{}, signals)
}

// BuildWithAuthorization reconciles the capability matrix, editorial overlay,
// authorization catalog, and live code signals into a deterministic catalog.
func BuildWithAuthorization(matrix Matrix, overlay Overlay, authorization AuthorizationCatalog, signals Signals) (Catalog, []Finding) {
	overlayByID := map[string]OverlayCapability{}
	for _, oc := range overlay.Capabilities {
		overlayByID[oc.Capability] = oc
	}
	nonMCP := map[string]SurfaceKind{}
	for _, surface := range overlay.NonMCPSurfaces {
		nonMCP[surface.Tool] = surface.Kind
	}

	// aliasSurfaces maps a capability id to the alias MCP tools attached through
	// overlay exemptions so they appear as catalog surfaces.
	aliasSurfaces := map[string][]string{}
	for _, exemption := range overlay.ToolExemptions {
		if exemption.Capability != "" {
			aliasSurfaces[exemption.Capability] = append(aliasSurfaces[exemption.Capability], exemption.Tool)
		}
	}

	authzIndex := newAuthorizationIndex(authorization)
	entries := make([]Entry, 0, len(matrix.Capabilities))
	referencedTools := map[string]bool{}
	for _, capability := range matrix.Capabilities {
		entry := buildEntry(capability, overlayByID[capability.Capability], signals, nonMCP)
		if authz, ok := authzIndex.authorizationFor(capability.Capability); ok {
			entry.Authorization = authz
		}
		entry.Surfaces = appendAliasSurfaces(entry.Surfaces, aliasSurfaces[capability.Capability], signals, nonMCP)
		for _, tool := range capability.Tools {
			referencedTools[tool] = true
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Capability < entries[j].Capability
	})

	catalog := Catalog{Version: overlay.Version, Authorization: authorization, Entries: entries}
	findings := reconcile(matrix, overlay, authorization, signals, overlayByID, referencedTools, nonMCP)
	return catalog, findings
}

func buildEntry(capability MatrixCapability, overlay OverlayCapability, signals Signals, nonMCP map[string]SurfaceKind) Entry {
	derived := deriveMaturity(matrixProfilesToSupport(capability.Profiles))
	maturity := derived
	if overlay.Maturity != "" {
		maturity = overlay.Maturity
	}

	display := overlay.DisplayName
	if display == "" {
		display = deriveDisplayName(capability.Capability)
	}

	return Entry{
		Capability:      capability.Capability,
		DisplayName:     display,
		OwnerPackage:    overlay.OwnerPackage,
		Maturity:        maturity,
		DerivedMaturity: derived,
		MaturityReason:  overlay.Reason,
		Surfaces:        classifySurfaces(capability.Tools, signals, nonMCP),
		Profiles:        entryProfiles(capability.Profiles),
		ProofSignals:    collectProofSignals(capability.Profiles),
		KnownGaps:       append([]string(nil), overlay.KnownGaps...),
		LinkedIssues:    append([]int(nil), overlay.LinkedIssues...),
		Docs:            append([]string(nil), overlay.Docs...),
		Console:         overlay.Console,
	}
}

func matrixProfilesToSupport(profiles map[string]MatrixProfile) map[string]ProfileSupport {
	out := make(map[string]ProfileSupport, len(profiles))
	for id, profile := range profiles {
		out[id] = ProfileSupport{Status: effectiveStatus(profile)}
	}
	return out
}

func entryProfiles(profiles map[string]MatrixProfile) map[string]EntryProfile {
	out := make(map[string]EntryProfile, len(profiles))
	for id, profile := range profiles {
		out[id] = EntryProfile{
			Status:          effectiveStatus(profile),
			MaxTruthLevel:   profile.MaxTruthLevel,
			RequiredRuntime: profile.RequiredRuntime,
			P95LatencyMS:    profile.P95LatencyMS,
			MaxScopeSize:    profile.MaxScopeSize,
		}
	}
	return out
}

// effectiveStatus resolves a profile's support status. Some matrix rows omit the
// status field and declare only a truth ceiling; for those the status is
// inferred as supported unless the ceiling itself is unsupported or absent.
func effectiveStatus(profile MatrixProfile) string {
	if profile.Status != "" {
		return profile.Status
	}
	if profile.MaxTruthLevel == "" || profile.MaxTruthLevel == statusUnsupported {
		return statusUnsupported
	}
	return statusSupported
}

// classifySurfaces resolves each declared tool to a surface kind. MCP-registry
// tools become SurfaceMCP, overlay-declared non-MCP tools take their declared
// kind, and everything else is SurfaceUnknown (which reconcile flags).
func classifySurfaces(tools []string, signals Signals, nonMCP map[string]SurfaceKind) []Surface {
	surfaces := make([]Surface, 0, len(tools))
	for _, tool := range tools {
		kind := SurfaceUnknown
		switch {
		case signals.MCPTools[tool]:
			kind = SurfaceMCP
		case nonMCP[tool] != "":
			kind = nonMCP[tool]
		}
		surfaces = append(surfaces, Surface{Tool: tool, Kind: kind})
	}
	sort.Slice(surfaces, func(i, j int) bool { return surfaces[i].Tool < surfaces[j].Tool })
	return surfaces
}

// appendAliasSurfaces adds overlay-aliased tools to a capability's surfaces,
// classifying and re-sorting them and skipping any already present.
func appendAliasSurfaces(surfaces []Surface, aliases []string, signals Signals, nonMCP map[string]SurfaceKind) []Surface {
	if len(aliases) == 0 {
		return surfaces
	}
	present := map[string]bool{}
	for _, surface := range surfaces {
		present[surface.Tool] = true
	}
	for _, tool := range aliases {
		if present[tool] {
			continue
		}
		present[tool] = true
		surfaces = append(surfaces, classifySurfaces([]string{tool}, signals, nonMCP)...)
	}
	sort.Slice(surfaces, func(i, j int) bool { return surfaces[i].Tool < surfaces[j].Tool })
	return surfaces
}

// collectProofSignals deduplicates verification signals across all profiles and
// returns them sorted by kind then ref for deterministic output.
func collectProofSignals(profiles map[string]MatrixProfile) []ProofSignal {
	seen := map[ProofSignal]struct{}{}
	for _, profile := range profiles {
		for _, verification := range profile.Verification {
			seen[ProofSignal(verification)] = struct{}{}
		}
	}
	out := make([]ProofSignal, 0, len(seen))
	for signal := range seen {
		out = append(out, signal)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

// deriveDisplayName turns a capability id such as code_search.exact_symbol into
// a title-cased name such as "Code Search Exact Symbol".
func deriveDisplayName(capability string) string {
	replacer := strings.NewReplacer(".", " ", "_", " ")
	fields := strings.Fields(replacer.Replace(capability))
	for i, field := range fields {
		fields[i] = strings.ToUpper(field[:1]) + field[1:]
	}
	return strings.Join(fields, " ")
}
