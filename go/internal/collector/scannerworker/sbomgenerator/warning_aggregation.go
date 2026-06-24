// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomgenerator

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const componentWarningSampleLimit = 5

type componentWarningAggregator struct {
	groups map[componentWarningKey]*componentWarningGroup
}

type componentWarningKey struct {
	category         string
	identityHint     string
	name             string
	version          string
	componentType    string
	ecosystem        string
	evidenceSource   string
	lockfilePath     string
	extractionReason string
	tool             string
}

type componentWarningGroup struct {
	key     componentWarningKey
	count   int
	samples []int
}

func newComponentWarningAggregator() *componentWarningAggregator {
	return &componentWarningAggregator{
		groups: map[componentWarningKey]*componentWarningGroup{},
	}
}

func (a *componentWarningAggregator) addMissingIdentity(index int, comp Component, tool string) {
	a.add(index, componentWarningKey{
		category:         "missing_identity",
		name:             strings.TrimSpace(comp.Name),
		version:          strings.TrimSpace(comp.Version),
		componentType:    strings.TrimSpace(comp.Type),
		ecosystem:        strings.TrimSpace(comp.Ecosystem),
		evidenceSource:   strings.TrimSpace(comp.EvidenceSource),
		lockfilePath:     strings.TrimSpace(comp.LockfilePath),
		extractionReason: strings.TrimSpace(comp.ExtractionReason),
		tool:             strings.TrimSpace(tool),
	})
}

func (a *componentWarningAggregator) addDuplicateIdentity(index int, comp Component, identity string, tool string) {
	a.add(index, componentWarningKey{
		category:         "duplicate_identity",
		identityHint:     strings.TrimSpace(identity),
		name:             strings.TrimSpace(comp.Name),
		version:          strings.TrimSpace(comp.Version),
		componentType:    strings.TrimSpace(comp.Type),
		ecosystem:        strings.TrimSpace(comp.Ecosystem),
		evidenceSource:   strings.TrimSpace(comp.EvidenceSource),
		lockfilePath:     strings.TrimSpace(comp.LockfilePath),
		extractionReason: strings.TrimSpace(comp.ExtractionReason),
		tool:             strings.TrimSpace(tool),
	})
}

func (a *componentWarningAggregator) add(index int, key componentWarningKey) {
	group, ok := a.groups[key]
	if !ok {
		group = &componentWarningGroup{key: key}
		a.groups[key] = group
	}
	group.count++
	if len(group.samples) < componentWarningSampleLimit {
		group.samples = append(group.samples, index)
	}
}

func (a *componentWarningAggregator) factCount() int {
	return len(a.groups)
}

func (a *componentWarningAggregator) facts(
	input scannerworker.ClaimInput,
	observedAt time.Time,
	documentID string,
) []facts.Envelope {
	if len(a.groups) == 0 {
		return nil
	}
	groups := make([]*componentWarningGroup, 0, len(a.groups))
	for _, group := range a.groups {
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].key.sortKey() < groups[j].key.sortKey()
	})
	out := make([]facts.Envelope, 0, len(groups))
	for _, group := range groups {
		out = append(out, newComponentWarningFact(input, observedAt, documentID, group))
	}
	return out
}

func newComponentWarningFact(
	input scannerworker.ClaimInput,
	observedAt time.Time,
	documentID string,
	group *componentWarningGroup,
) facts.Envelope {
	warningKey := group.key.stableKey()
	payload := map[string]any{
		"document_id":              documentID,
		"reason":                   WarningReasonComponentMissingIdentity,
		"summary":                  group.summary(),
		"warning_key":              warningKey,
		"component_warning_kind":   group.key.category,
		"occurrence_count":         group.count,
		"sample_component_indexes": append([]int(nil), group.samples...),
	}
	addWarningPayloadString(payload, "component_identity_hint", group.key.identityHint)
	addWarningPayloadString(payload, "name", group.key.name)
	addWarningPayloadString(payload, "version", group.key.version)
	addWarningPayloadString(payload, "type", group.key.componentType)
	addWarningPayloadString(payload, "ecosystem", group.key.ecosystem)
	addWarningPayloadString(payload, "evidence_source", group.key.evidenceSource)
	addWarningPayloadString(payload, "lockfile_path", group.key.lockfilePath)
	addWarningPayloadString(payload, "extraction_reason", group.key.extractionReason)
	addWarningPayloadString(payload, "created_by_tool", group.key.tool)
	stableKey := facts.StableID(facts.SBOMWarningFactKind, map[string]any{
		"document_id":   documentID,
		"generation_id": input.GenerationID,
		"reason":        WarningReasonComponentMissingIdentity,
		"warning_key":   warningKey,
	})
	return newEnvelope(input, observedAt, facts.SBOMWarningFactKind, stableKey, payload)
}

func addWarningPayloadString(payload map[string]any, key string, value string) {
	if value == "" {
		return
	}
	payload[key] = value
}

func (g *componentWarningGroup) summary() string {
	samples := componentIndexSummary(g.samples)
	switch g.key.category {
	case "duplicate_identity":
		return fmt.Sprintf("%d duplicate components collapsed into existing identity %q (samples: %s)", g.count, g.key.identityHint, samples)
	default:
		return fmt.Sprintf("%d components missing purl and name+version identity (samples: %s)", g.count, samples)
	}
}

func componentIndexSummary(indexes []int) string {
	parts := make([]string, 0, len(indexes))
	for _, index := range indexes {
		parts = append(parts, fmt.Sprintf("component[%d]", index))
	}
	return strings.Join(parts, ", ")
}

func (k componentWarningKey) stableKey() string {
	return facts.StableID(facts.SBOMWarningFactKind+":component", map[string]any{
		"category":          k.category,
		"component_type":    k.componentType,
		"ecosystem":         k.ecosystem,
		"evidence_source":   k.evidenceSource,
		"extraction_reason": k.extractionReason,
		"identity_hint":     k.identityHint,
		"lockfile_path":     k.lockfilePath,
		"name":              k.name,
		"tool":              k.tool,
		"version":           k.version,
	})
}

func (k componentWarningKey) sortKey() string {
	return strings.Join([]string{
		k.category,
		k.identityHint,
		k.name,
		k.version,
		k.componentType,
		k.ecosystem,
		k.evidenceSource,
		k.lockfilePath,
		k.extractionReason,
		k.tool,
	}, "\x00")
}
