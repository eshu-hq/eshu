// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"gopkg.in/yaml.v3"
)

// DepthRequirementsFileName is the depth-requirement taxonomy spec inside the
// specs directory. It declares the applicability classes mirrored from graph
// retraction registries (retractable graph node and edge types) plus the reducer
// drain; the projection/shared-projection/collector classes are derived in code
// from the already-loaded registries.
const DepthRequirementsFileName = "replay-depth-requirements.v1.yaml"

// Depth-applicability registries. These enumerate the surfaces that C-13 (#4366)
// requires a depth scenario_type for, beyond the C-1 breadth surfaces.
const (
	// RegistryRetractableType is a retractable graph node type (delta_tombstone).
	RegistryRetractableType Registry = "retractable_type"
	// RegistryRetractableEdgeType is a retractable graph edge type (delta_tombstone).
	RegistryRetractableEdgeType Registry = "retractable_edge_type"
	// RegistryProjection is a reducer projection = a distinct reducer_domain
	// (cost, plus ordering when the projection is shared-conflict-key).
	RegistryProjection Registry = "projection"
	// RegistryReducerDrain is the reducer projection drain path (crash).
	RegistryReducerDrain Registry = "reducer_drain"
)

// DepthRequirements is the parsed depth-requirement taxonomy spec.
type DepthRequirements struct {
	// Version is the spec schema version.
	Version string
	// RetractableNodeTypes are the graph node labels the canonical retract phase
	// can tombstone; each requires a delta_tombstone scenario. Kept byte-equal to
	// cypher.RetractableNodeEntityLabels() by a lockstep test.
	RetractableNodeTypes []string
	// RetractableEdgeTypes are the graph relationship types the static canonical
	// and reducer edge retract paths can remove; each requires a delta_tombstone
	// scenario.
	// Kept byte-equal to cypher.RetractableEdgeTypes() by a lockstep test.
	RetractableEdgeTypes []string
	// ReducerDrain is the single reducer projection drain surface; it requires a
	// crash scenario.
	ReducerDrain ReducerDrainSurface
	// Exemptions are depth (surface, scenario_type) pairs deliberately not required,
	// each with a reason.
	Exemptions []DepthExemption
}

// ReducerDrainSurface describes the single reducer projection drain.
type ReducerDrainSurface struct {
	// Surface is the drain's coverage-key suffix ("reducer_drain:<Surface>").
	Surface string
	// Detail is a short human description for the coverage report.
	Detail string
}

// DepthExemption records a depth (surface, scenario_type) pair deliberately not
// required to have a replay scenario, with an auditable reason.
type DepthExemption struct {
	// Surface is the full depth surface key (e.g. "retractable_node:Foo").
	Surface string
	// ScenarioType is the depth class being exempted for Surface.
	ScenarioType DepthScenarioType
	// Reason explains why the pair needs no replay scenario.
	Reason string
}

type depthRequirementsFile struct {
	Version              string               `yaml:"version"`
	RetractableNodeTypes []string             `yaml:"retractable_node_types"`
	RetractableEdgeTypes []string             `yaml:"retractable_edge_types"`
	ReducerDrain         depthDrainFile       `yaml:"reducer_drain"`
	Exemptions           []depthExemptionFile `yaml:"exemptions"`
}

type depthDrainFile struct {
	Surface string `yaml:"surface"`
	Detail  string `yaml:"detail"`
}

type depthExemptionFile struct {
	Surface      string `yaml:"surface"`
	ScenarioType string `yaml:"scenario_type"`
	Reason       string `yaml:"reason"`
}

// LoadDepthRequirements reads the depth-requirement taxonomy spec from path. A
// missing file is an error: unlike the coverage manifest, an absent depth spec
// would silently drop every depth requirement (the exact #4186 blindness this
// gate exists to remove), so the gate must fail loudly instead. The loader
// rejects a blank/duplicate node or edge type, a blank drain surface, and an
// exemption with a blank surface, an invalid scenario_type, or a blank reason.
func LoadDepthRequirements(path string) (DepthRequirements, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator-configured depth spec under specs/, not external input
	if err != nil {
		return DepthRequirements{}, fmt.Errorf("read depth requirements %s: %w", path, err)
	}
	var parsed depthRequirementsFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return DepthRequirements{}, fmt.Errorf("parse depth requirements %s: %w", path, err)
	}

	dr := DepthRequirements{Version: parsed.Version}
	seen := map[string]struct{}{}
	for _, rawType := range parsed.RetractableNodeTypes {
		label := strings.TrimSpace(rawType)
		if label == "" {
			return DepthRequirements{}, fmt.Errorf("depth requirements %s: blank retractable_node_types entry", path)
		}
		if _, dup := seen[label]; dup {
			return DepthRequirements{}, fmt.Errorf("depth requirements %s: retractable node type %q declared twice", path, label)
		}
		seen[label] = struct{}{}
		dr.RetractableNodeTypes = append(dr.RetractableNodeTypes, label)
	}
	if len(dr.RetractableNodeTypes) == 0 {
		return DepthRequirements{}, fmt.Errorf("depth requirements %s: no retractable_node_types declared", path)
	}

	seen = map[string]struct{}{}
	for _, rawType := range parsed.RetractableEdgeTypes {
		edgeType := strings.TrimSpace(rawType)
		if edgeType == "" {
			return DepthRequirements{}, fmt.Errorf("depth requirements %s: blank retractable_edge_types entry", path)
		}
		if _, dup := seen[edgeType]; dup {
			return DepthRequirements{}, fmt.Errorf("depth requirements %s: retractable edge type %q declared twice", path, edgeType)
		}
		seen[edgeType] = struct{}{}
		dr.RetractableEdgeTypes = append(dr.RetractableEdgeTypes, edgeType)
	}
	if len(dr.RetractableEdgeTypes) == 0 {
		return DepthRequirements{}, fmt.Errorf("depth requirements %s: no retractable_edge_types declared", path)
	}

	drainSurface := strings.TrimSpace(parsed.ReducerDrain.Surface)
	if drainSurface == "" {
		return DepthRequirements{}, fmt.Errorf("depth requirements %s: reducer_drain.surface is blank", path)
	}
	dr.ReducerDrain = ReducerDrainSurface{Surface: drainSurface, Detail: strings.TrimSpace(parsed.ReducerDrain.Detail)}

	for _, ex := range parsed.Exemptions {
		surface := strings.TrimSpace(ex.Surface)
		scenarioType := DepthScenarioType(strings.TrimSpace(ex.ScenarioType))
		reason := strings.TrimSpace(ex.Reason)
		if surface == "" {
			return DepthRequirements{}, fmt.Errorf("depth requirements %s: depth exemption has blank surface", path)
		}
		if _, ok := validDepthScenarioTypes[scenarioType]; !ok {
			return DepthRequirements{}, fmt.Errorf("depth requirements %s: depth exemption %q has invalid scenario_type %q", path, surface, ex.ScenarioType)
		}
		if reason == "" {
			return DepthRequirements{}, fmt.Errorf("depth requirements %s: depth exemption %q has blank reason", path, surface)
		}
		dr.Exemptions = append(dr.Exemptions, DepthExemption{Surface: surface, ScenarioType: scenarioType, Reason: reason})
	}
	return dr, nil
}

// retractableNodeSurfaceKey is the coverage key for a retractable graph node type.
func retractableNodeSurfaceKey(label string) string { return "retractable_node:" + label }

// retractableEdgeSurfaceKey is the coverage key for a retractable graph edge type.
func retractableEdgeSurfaceKey(edgeType string) string { return "retractable_edge:" + edgeType }

// projectionSurfaceKey is the coverage key for a reducer projection.
func projectionSurfaceKey(domain string) string { return "projection:" + domain }

// reducerDrainSurfaceKey is the coverage key for the reducer drain surface.
func reducerDrainSurfaceKey(surface string) string { return "reducer_drain:" + surface }

// EnumerateDepthSurfaces returns the depth-only surfaces C-13 requires a depth
// scenario_type for: a retractable_node surface per retractable graph node type,
// a retractable_edge surface per retractable graph relationship type, a
// projection surface per distinct reducer_domain, and the single reducer_drain
// surface. Collector-boundary fault applies to the existing collector surfaces
// and is added in DeriveRequirements, not here. The result is sorted by registry
// then key so report output stays byte-stable.
func EnumerateDepthSurfaces(dr DepthRequirements, factKinds []facts.FactKindRegistryEntry) []SupportedSurface {
	var out []SupportedSurface
	for _, label := range dr.RetractableNodeTypes {
		out = append(out, SupportedSurface{
			Registry: RegistryRetractableType,
			Key:      retractableNodeSurfaceKey(label),
			Detail:   fmt.Sprintf("retractable graph node type %q", label),
		})
	}
	for _, edgeType := range dr.RetractableEdgeTypes {
		out = append(out, SupportedSurface{
			Registry: RegistryRetractableEdgeType,
			Key:      retractableEdgeSurfaceKey(edgeType),
			Detail:   fmt.Sprintf("retractable graph edge type %q", edgeType),
		})
	}
	for _, domain := range projectionDomains(factKinds) {
		out = append(out, SupportedSurface{
			Registry: RegistryProjection,
			Key:      projectionSurfaceKey(domain),
			Detail:   fmt.Sprintf("reducer projection %q", domain),
		})
	}
	out = append(out, SupportedSurface{
		Registry: RegistryReducerDrain,
		Key:      reducerDrainSurfaceKey(dr.ReducerDrain.Surface),
		Detail:   reducerDrainDetail(dr),
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Registry != out[j].Registry {
			return out[i].Registry < out[j].Registry
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func reducerDrainDetail(dr DepthRequirements) string {
	if dr.ReducerDrain.Detail != "" {
		return dr.ReducerDrain.Detail
	}
	return fmt.Sprintf("reducer drain %q", dr.ReducerDrain.Surface)
}

// DeriveRequirements computes the depth scenario_types required per applicable
// surface, the core of C-13: requirements are derived per surface, not opted into
// one at a time. It returns one ScenarioRequirement per surface that needs a
// non-default requirement:
//
//   - collector:<name>        -> baseline + fault (every collector boundary),
//   - retractable_node:<L>    -> delta_tombstone (every retractable node type),
//   - retractable_edge:<T>    -> delta_tombstone (every retractable edge type),
//   - projection:<domain>     -> cost, plus ordering when the projection is
//     shared-conflict-key (written by >=2 distinct projection hooks),
//   - reducer_drain:<surface> -> crash (the reducer drain path).
//
// The caller unions these with the manifest's explicit scenario_requirements and
// reconciles them; a derived requirement for a surface that has no covering
// manifest entry is reported UNCOVERED, which is the C-14 backfill worklist.
func DeriveRequirements(supported []SupportedSurface, dr DepthRequirements, factKinds []facts.FactKindRegistryEntry) []ScenarioRequirement {
	shared := sharedConflictKeyProjections(factKinds)
	var reqs []ScenarioRequirement
	for _, s := range supported {
		switch {
		case s.Registry == RegistrySurfaceInventory && strings.HasPrefix(s.Key, "collector:"):
			reqs = append(reqs, ScenarioRequirement{Surface: s.Key, ScenarioTypes: []DepthScenarioType{ScenarioTypeBaseline, ScenarioTypeFault}})
		case s.Registry == RegistryRetractableType:
			reqs = append(reqs, ScenarioRequirement{Surface: s.Key, ScenarioTypes: []DepthScenarioType{ScenarioTypeDeltaTombstone}})
		case s.Registry == RegistryRetractableEdgeType:
			reqs = append(reqs, ScenarioRequirement{Surface: s.Key, ScenarioTypes: []DepthScenarioType{ScenarioTypeDeltaTombstone}})
		case s.Registry == RegistryProjection:
			types := []DepthScenarioType{ScenarioTypeCost}
			if _, ok := shared[strings.TrimPrefix(s.Key, "projection:")]; ok {
				types = append(types, ScenarioTypeOrdering)
			}
			reqs = append(reqs, ScenarioRequirement{Surface: s.Key, ScenarioTypes: types})
		case s.Registry == RegistryReducerDrain:
			reqs = append(reqs, ScenarioRequirement{Surface: s.Key, ScenarioTypes: []DepthScenarioType{ScenarioTypeCrash}})
		}
	}
	return reqs
}

// projectionDomains returns the sorted, de-duplicated set of reducer projection
// domains (each distinct reducer_domain is one projection that requires a cost
// scenario).
func projectionDomains(factKinds []facts.FactKindRegistryEntry) []string {
	seen := map[string]struct{}{}
	for _, e := range factKinds {
		domain := strings.TrimSpace(e.ReducerDomain)
		if domain == "" {
			continue
		}
		seen[domain] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for domain := range seen {
		out = append(out, domain)
	}
	sort.Strings(out)
	return out
}

// sharedConflictKeyProjections returns the reducer projection domains written by
// more than one distinct projection hook. Two distinct projection hooks landing
// on the same reducer_domain contend on the same projection conflict key, so the
// projection must prove deterministic behavior under alternate ordering (R-13).
func sharedConflictKeyProjections(factKinds []facts.FactKindRegistryEntry) map[string]struct{} {
	hooksByDomain := map[string]map[string]struct{}{}
	for _, e := range factKinds {
		domain := strings.TrimSpace(e.ReducerDomain)
		hook := strings.TrimSpace(e.ProjectionHook)
		if domain == "" || hook == "" {
			continue
		}
		if hooksByDomain[domain] == nil {
			hooksByDomain[domain] = map[string]struct{}{}
		}
		hooksByDomain[domain][hook] = struct{}{}
	}
	shared := map[string]struct{}{}
	for domain, hooks := range hooksByDomain {
		if len(hooks) >= 2 {
			shared[domain] = struct{}{}
		}
	}
	return shared
}

// depthExemptions renders the depth exemptions as a map from "surface|type"
// coverage key to reason, so the reconciler can mark those pairs exempt (with the
// reason) rather than uncovered.
func depthExemptions(dr DepthRequirements) map[string]string {
	out := make(map[string]string, len(dr.Exemptions))
	for _, ex := range dr.Exemptions {
		out[manifestCoverageKey(ex.Surface, ex.ScenarioType)] = ex.Reason
	}
	return out
}

// unionRequirements merges two requirement lists by surface, unioning their
// scenario types. It is how the derived per-surface requirements (DeriveRequirements)
// combine with the manifest's explicit scenario_requirements: a surface required
// by both keeps the union of both type sets. Output is deterministic (surfaces
// sorted, types in canonical depth order).
func unionRequirements(a, b []ScenarioRequirement) []ScenarioRequirement {
	bySurface := map[string]map[DepthScenarioType]struct{}{}
	add := func(reqs []ScenarioRequirement) {
		for _, req := range reqs {
			if bySurface[req.Surface] == nil {
				bySurface[req.Surface] = map[DepthScenarioType]struct{}{}
			}
			for _, t := range req.ScenarioTypes {
				bySurface[req.Surface][t] = struct{}{}
			}
		}
	}
	add(a)
	add(b)
	surfaces := make([]string, 0, len(bySurface))
	for surface := range bySurface {
		surfaces = append(surfaces, surface)
	}
	sort.Strings(surfaces)
	out := make([]ScenarioRequirement, 0, len(surfaces))
	for _, surface := range surfaces {
		types := make([]DepthScenarioType, 0, len(bySurface[surface]))
		for _, t := range canonicalDepthOrder {
			if _, ok := bySurface[surface][t]; ok {
				types = append(types, t)
			}
		}
		out = append(out, ScenarioRequirement{Surface: surface, ScenarioTypes: types})
	}
	return out
}

// canonicalDepthOrder is the stable order depth scenario types are emitted in so
// requirement lists and reports are byte-stable.
var canonicalDepthOrder = []DepthScenarioType{
	ScenarioTypeBaseline,
	ScenarioTypeDeltaTombstone,
	ScenarioTypeFault,
	ScenarioTypeOrdering,
	ScenarioTypeCrash,
	ScenarioTypeCost,
}

// applyDepthExemptions converts uncovered depth rows whose (surface, scenario_type)
// pair is declared exempt into StatusExempt rows carrying the exemption reason, so
// a deliberately-unprovable depth surface is audited (not silently a gap).
func applyDepthExemptions(cov Coverage, exemptions map[string]string) Coverage {
	if len(exemptions) == 0 {
		return cov
	}
	for i := range cov.Surfaces {
		sc := &cov.Surfaces[i]
		if sc.Status != StatusUncovered {
			continue
		}
		if reason, ok := exemptions[manifestCoverageKey(sc.Surface.Key, sc.ScenarioType)]; ok {
			sc.Status = StatusExempt
			sc.Detail = reason
		}
	}
	return cov
}

// sortSupportedSurfaces orders surfaces by registry then key, the byte-stable
// order EnumerateSupported produces, so appending the depth surfaces keeps gate
// output and the coverage report deterministic.
func sortSupportedSurfaces(surfaces []SupportedSurface) {
	sort.Slice(surfaces, func(i, j int) bool {
		if surfaces[i].Registry != surfaces[j].Registry {
			return surfaces[i].Registry < surfaces[j].Registry
		}
		return surfaces[i].Key < surfaces[j].Key
	})
}
