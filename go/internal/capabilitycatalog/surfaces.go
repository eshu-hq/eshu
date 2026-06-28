// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"encoding/json"
	"fmt"
	"sort"
)

// SurfaceCategory classifies one of the six platform surface families the
// generated surface inventory tracks. Every category is enumerable from live
// code, specs, or the source tree so the inventory is generated, never
// hand-maintained.
type SurfaceCategory string

const (
	// SurfaceCommand is a built command binary under go/cmd.
	SurfaceCommand SurfaceCategory = "command"
	// SurfaceCollector is a collector family from scope.AllCollectorKinds.
	SurfaceCollector SurfaceCategory = "collector"
	// SurfaceReducerDomain is a reducer domain from reducer.AllDomains.
	SurfaceReducerDomain SurfaceCategory = "reducer_domain"
	// SurfaceAPIRoute is an HTTP API route from the OpenAPI spec paths.
	SurfaceAPIRoute SurfaceCategory = "api_route"
	// SurfaceMCPTool is an MCP tool from the read-only MCP registry.
	SurfaceMCPTool SurfaceCategory = "mcp_tool"
	// SurfaceConsolePage is a console page component under apps/console.
	SurfaceConsolePage SurfaceCategory = "console_page"
)

// surfaceCategories is the closed, ordered set of surface categories.
var surfaceCategories = []SurfaceCategory{
	SurfaceCommand,
	SurfaceCollector,
	SurfaceReducerDomain,
	SurfaceAPIRoute,
	SurfaceMCPTool,
	SurfaceConsolePage,
}

// AllSurfaceCategories returns every surface category in a stable order.
func AllSurfaceCategories() []SurfaceCategory {
	return append([]SurfaceCategory(nil), surfaceCategories...)
}

// SurfaceRecord is one reconciled row of the surface inventory: a single live
// surface annotated with its readiness lane and editorial metadata.
type SurfaceRecord struct {
	// Category is the surface family.
	Category SurfaceCategory `json:"category"`
	// Name is the surface identifier within its category (binary name, collector
	// kind, reducer domain, route, tool name, or page component).
	Name string `json:"name"`
	// Readiness is the declared readiness lane (overlay override or category
	// default).
	Readiness ReadinessLane `json:"readiness"`
	// Owner is the owning Go import path or source location, when declared.
	Owner string `json:"owner,omitempty"`
	// Proof references the promotion proof for an implemented surface.
	Proof string `json:"proof,omitempty"`
	// Docs lists doc paths describing the surface.
	Docs []string `json:"docs,omitempty"`
	// Notes is optional editorial context.
	Notes string `json:"notes,omitempty"`
	// CollectorContract records the source-to-read-surface provenance contract
	// for collector surfaces. It is omitted for non-collector surfaces.
	CollectorContract *CollectorContract `json:"collector_contract,omitempty"`
}

// SurfaceInventory is the generated, reconciled inventory of every platform
// surface. It is committed as data/surface-inventory.generated.json and a golden
// drift test keeps it in lockstep with live code so no surface can appear or
// disappear silently.
type SurfaceInventory struct {
	// Version mirrors the overlay schema version.
	Version string `json:"version"`
	// Surfaces are the records sorted by category then name.
	Surfaces []SurfaceRecord `json:"surfaces"`
}

// LiveSurfaces carries the surfaces enumerated from live code, specs, and the
// source tree, keyed by category. The generator builds it; the catalog package
// stays free of mcp, query, reducer, scope, and filesystem dependencies.
type LiveSurfaces struct {
	// Surfaces maps a category to the live surface names in that category.
	Surfaces map[SurfaceCategory][]string
	// CollectorFactKinds maps collector kind to live core fact kinds that the
	// generated inventory must cover with a collector contract.
	CollectorFactKinds map[string][]string
}

// SurfaceOverlay is the editorial overlay for the surface inventory. It carries
// only the readiness lane, owner, proof, docs, and notes that cannot be derived
// from code, keyed by category and name. Surfaces with the category default lane
// need no overlay row, so the overlay stays small and DRY.
type SurfaceOverlay struct {
	// Version is the overlay schema version.
	Version string
	// Surfaces holds the per-surface editorial records.
	Surfaces []SurfaceOverlayRecord
}

// SurfaceOverlayRecord is the editorial overlay for one surface.
type SurfaceOverlayRecord struct {
	// Category is the surface family the record annotates.
	Category SurfaceCategory
	// Name is the surface identifier within its category.
	Name string
	// Readiness is the declared readiness lane.
	Readiness ReadinessLane
	// Owner is the owning Go import path or source location.
	Owner string
	// Proof references the promotion proof for an implemented surface.
	Proof string
	// Docs lists doc paths describing the surface.
	Docs []string
	// Notes is optional editorial context.
	Notes string
	// CollectorContract is the source-to-read-surface contract for collector
	// records, when the surface is a collector.
	CollectorContract CollectorContract
}

// CollectorContract maps one collector family to the facts it emits and the
// projection/read surfaces that consume those facts. The inventory uses this to
// make collector extraction and API/MCP provenance reviewable before any
// collector leaves the monorepo.
type CollectorContract struct {
	// FactKinds are the core fact kinds this collector family emits.
	FactKinds []string `json:"fact_kinds,omitempty"`
	// ProjectionSurfaces names reducer domains, projectors, writers, or read
	// models that consume the fact kinds.
	ProjectionSurfaces []string `json:"projection_surfaces,omitempty"`
	// ReadSurfaces names API routes, MCP tools, or console pages that expose the
	// resulting truth or source evidence.
	ReadSurfaces []string `json:"read_surfaces,omitempty"`
	// ProofGates lists tests, verifiers, or generated-artifact gates required
	// for this collector contract.
	ProofGates []string `json:"proof_gates,omitempty"`
	// FixtureRefs lists cassettes, golden fixtures, or fixture directories that
	// prove the contract.
	FixtureRefs []string `json:"fixture_refs,omitempty"`
	// TruthProfile distinguishes deterministic evidence from optional semantic
	// or provider-gated output.
	TruthProfile string `json:"truth_profile,omitempty"`
}

// FindingUnclassifiedCollector is a live collector with no declared readiness
// lane. Collectors must be explicitly classified because their lane drives
// production-readiness claims; defaulting one to implemented would over-claim.
const FindingUnclassifiedCollector FindingKind = "unclassified_collector"

// FindingStaleSurfaceOverlay is an overlay record whose surface is absent from
// live code: a surface was removed or renamed but its overlay row lingers.
const FindingStaleSurfaceOverlay FindingKind = "stale_surface_overlay"

// FindingImplementedWithoutProof is a collector declared implemented with no
// promotion proof reference. The implemented lane asserts production readiness,
// so it must link proof.
const FindingImplementedWithoutProof FindingKind = "implemented_without_proof"

// FindingInvalidReadinessLane is an overlay record with a readiness value that
// is not one of the closed readiness lanes.
const FindingInvalidReadinessLane FindingKind = "invalid_readiness_lane"

// FindingDuplicateOverlayRow is more than one overlay record for the same
// category and name. Duplicates are dangerous because the second silently wins,
// which could downgrade a collector's readiness without any signal.
const FindingDuplicateOverlayRow FindingKind = "duplicate_overlay_row"

// FindingCollectorFactKindUnmapped is a live collector fact kind that has no
// matching entry in that collector's manifest contract.
const FindingCollectorFactKindUnmapped FindingKind = "collector_fact_kind_unmapped"

// defaultReadiness returns the readiness lane assigned to a surface in the given
// category when no overlay row classifies it. Commands, API routes, MCP tools,
// and console pages exist because they are built and served, so they default to
// implemented. Collectors have no default: their lane must be declared
// explicitly so an unclassified collector is flagged rather than over-claimed.
func defaultReadiness(category SurfaceCategory) (ReadinessLane, bool) {
	switch category {
	case SurfaceCollector:
		return "", false
	default:
		return ReadinessImplemented, true
	}
}

// BuildSurfaceInventory reconciles the live surfaces with the editorial overlay
// into a deterministic surface inventory plus reconciliation findings. The
// inventory is always returned best-effort; an empty findings slice means the
// inventory is fully reconciled. A non-empty slice is a drift-gate failure.
func BuildSurfaceInventory(live LiveSurfaces, overlay SurfaceOverlay) (SurfaceInventory, []Finding) {
	overlayByKey := map[string]SurfaceOverlayRecord{}
	var findings []Finding
	for _, rec := range overlay.Surfaces {
		key := surfaceKey(rec.Category, rec.Name)
		if _, dup := overlayByKey[key]; dup {
			findings = append(findings, Finding{
				Kind:    FindingDuplicateOverlayRow,
				Subject: rec.Name,
				Detail:  fmt.Sprintf("overlay has more than one record for %s %q", rec.Category, rec.Name),
			})
		}
		overlayByKey[key] = rec
	}

	liveKeys := map[string]struct{}{}
	var records []SurfaceRecord

	for _, category := range surfaceCategories {
		names := append([]string(nil), live.Surfaces[category]...)
		sort.Strings(names)
		for _, name := range names {
			key := surfaceKey(category, name)
			liveKeys[key] = struct{}{}
			rec, recFindings := buildSurfaceRecord(category, name, overlayByKey[key], live.CollectorFactKinds[name])
			records = append(records, rec)
			findings = append(findings, recFindings...)
		}
	}

	findings = append(findings, staleOverlayFindings(overlay, liveKeys)...)

	sort.Slice(records, func(i, j int) bool {
		if records[i].Category != records[j].Category {
			return records[i].Category < records[j].Category
		}
		return records[i].Name < records[j].Name
	})
	sortFindings(findings)
	return SurfaceInventory{Version: overlay.Version, Surfaces: records}, findings
}

// buildSurfaceRecord reconciles one live surface against its optional overlay row
// and returns the record plus any findings (unclassified collector, invalid
// lane, implemented-without-proof).
func buildSurfaceRecord(category SurfaceCategory, name string, overlay SurfaceOverlayRecord, liveFactKinds []string) (SurfaceRecord, []Finding) {
	rec := SurfaceRecord{Category: category, Name: name}
	var findings []Finding

	hasOverlay := overlay.Category == category && overlay.Name == name
	switch {
	case hasOverlay && overlay.Readiness != "":
		rec.Readiness = overlay.Readiness
		if !overlay.Readiness.Valid() {
			findings = append(findings, Finding{
				Kind:    FindingInvalidReadinessLane,
				Subject: name,
				Detail:  fmt.Sprintf("%s surface %q declares invalid readiness lane %q", category, name, overlay.Readiness),
			})
		}
	default:
		lane, ok := defaultReadiness(category)
		if !ok {
			findings = append(findings, Finding{
				Kind:    FindingUnclassifiedCollector,
				Subject: name,
				Detail:  fmt.Sprintf("collector %q has no declared readiness lane in the surface overlay", name),
			})
		}
		rec.Readiness = lane
	}

	if hasOverlay {
		rec.Owner = overlay.Owner
		rec.Proof = overlay.Proof
		rec.Docs = append([]string(nil), overlay.Docs...)
		rec.Notes = overlay.Notes
		if category == SurfaceCollector && !overlay.CollectorContract.empty() {
			contract := overlay.CollectorContract.normalized()
			rec.CollectorContract = &contract
		}
	}

	// An implemented collector asserts production readiness and must link proof.
	if category == SurfaceCollector && rec.Readiness.RequiresPromotionProof() && rec.Proof == "" {
		findings = append(findings, Finding{
			Kind:    FindingImplementedWithoutProof,
			Subject: name,
			Detail:  fmt.Sprintf("collector %q is declared implemented but links no promotion proof", name),
		})
	}
	if category == SurfaceCollector {
		findings = append(findings, collectorFactKindFindings(name, liveFactKinds, rec.CollectorContract)...)
	}
	return rec, findings
}

func collectorFactKindFindings(name string, liveFactKinds []string, contract *CollectorContract) []Finding {
	if len(liveFactKinds) == 0 {
		return nil
	}
	mapped := map[string]struct{}{}
	if contract != nil {
		for _, kind := range contract.FactKinds {
			mapped[kind] = struct{}{}
		}
	}
	live := append([]string(nil), liveFactKinds...)
	sort.Strings(live)
	live = compactStrings(live)
	var findings []Finding
	for _, kind := range live {
		if _, ok := mapped[kind]; ok {
			continue
		}
		findings = append(findings, Finding{
			Kind:    FindingCollectorFactKindUnmapped,
			Subject: name + ":" + kind,
			Detail:  fmt.Sprintf("collector %q emits fact kind %q but its collector_contract.fact_kinds omits it", name, kind),
		})
	}
	return findings
}

func (c CollectorContract) empty() bool {
	return len(c.FactKinds) == 0 &&
		len(c.ProjectionSurfaces) == 0 &&
		len(c.ReadSurfaces) == 0 &&
		len(c.ProofGates) == 0 &&
		len(c.FixtureRefs) == 0 &&
		c.TruthProfile == ""
}

func (c CollectorContract) normalized() CollectorContract {
	return CollectorContract{
		FactKinds:          sortedStrings(c.FactKinds),
		ProjectionSurfaces: sortedStrings(c.ProjectionSurfaces),
		ReadSurfaces:       sortedStrings(c.ReadSurfaces),
		ProofGates:         sortedStrings(c.ProofGates),
		FixtureRefs:        sortedStrings(c.FixtureRefs),
		TruthProfile:       c.TruthProfile,
	}
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return compactStrings(out)
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	var previous string
	for i, value := range values {
		if value == "" || (i > 0 && value == previous) {
			continue
		}
		out = append(out, value)
		previous = value
	}
	return out
}

// staleOverlayFindings reports overlay records whose surface is absent from live
// code, catching a surface that was removed or renamed without updating the
// overlay.
func staleOverlayFindings(overlay SurfaceOverlay, liveKeys map[string]struct{}) []Finding {
	var findings []Finding
	for _, rec := range overlay.Surfaces {
		if _, ok := liveKeys[surfaceKey(rec.Category, rec.Name)]; ok {
			continue
		}
		findings = append(findings, Finding{
			Kind:    FindingStaleSurfaceOverlay,
			Subject: rec.Name,
			Detail:  fmt.Sprintf("overlay record for %s %q has no matching live surface", rec.Category, rec.Name),
		})
	}
	return findings
}

func surfaceKey(category SurfaceCategory, name string) string {
	return string(category) + "\x1f" + name
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Subject < findings[j].Subject
	})
}

// MarshalSurfaceInventory renders the inventory as deterministic, indented JSON
// with a trailing newline, suitable for committing as the generated artifact.
func MarshalSurfaceInventory(inv SurfaceInventory) ([]byte, error) {
	payload, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal surface inventory: %w", err)
	}
	return append(payload, '\n'), nil
}
