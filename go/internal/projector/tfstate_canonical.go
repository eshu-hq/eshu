// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// terraformStateCanonicalStage is the bounded telemetry stage label the
// projector's terraform_state canonical extractor reports on
// eshu_dp_projector_input_invalid_facts_total.
const terraformStateCanonicalStage = "terraform_state_canonical"

// extractTerraformStateRows projects committed terraform_state fact envelopes
// into canonical resource/module/output rows on mat, decoding each fact
// through the typed factschema seam. A fact missing a required identity field
// is QUARANTINED per-fact (returned in the []quarantinedFact slice) rather
// than producing a graph identity from an empty-string segment: that one
// fact is skipped while every valid fact — terraform_state and non-terraform_state
// — still projects. The caller (buildCanonicalMaterialization) records the
// quarantined facts as visible input_invalid dead-letters via
// recordProjectorQuarantinedFacts. A present-but-empty identity field is a
// valid decode that the row builders' own identity gate still drops,
// byte-identical to the pre-typing behavior.
//
// terraform_state_candidate, terraform_state_provider_binding, and
// terraform_state_warning are intentionally not consumed here
// (typed-but-deferred, no projector read site today), so no case handles
// them.
func extractTerraformStateRows(mat *CanonicalMaterialization, envelopes []facts.Envelope) []quarantinedFact {
	if mat == nil || len(envelopes) == 0 {
		return nil
	}

	var quarantined []quarantinedFact
	snapshot, snapshotEnvelope, snapshotErr := terraformStateSnapshot(envelopes)
	if snapshotErr != nil {
		if q, isQuarantine, fatal := partitionProjectorDecodeFailures(snapshotEnvelope, snapshotErr); fatal == nil && isQuarantine {
			// See the extractor's fatal-branch comment below: an unsupported
			// schema major is unreachable here on the production path because
			// runtime.go's validateFactSchemaVersion rejects it upstream.
			quarantined = append(quarantined, q)
		}
	}
	tagHashesByResource, tagQuarantined := terraformStateTagHashesByResource(envelopes)
	quarantined = append(quarantined, tagQuarantined...)
	moduleRows := []TerraformStateModuleRow{}
	for _, envelope := range envelopes {
		var err error
		switch envelope.FactKind {
		case facts.TerraformStateResourceFactKind:
			if row, ok, rowErr := terraformStateResourceRow(
				mat.ScopeID,
				snapshot,
				tagHashesByResource,
				envelope,
			); ok {
				mat.TerraformStateResources = append(mat.TerraformStateResources, row)
			} else {
				err = rowErr
			}
		case facts.TerraformStateModuleFactKind:
			if row, ok, rowErr := terraformStateModuleRow(mat.ScopeID, snapshot, envelope); ok {
				moduleRows = append(moduleRows, row)
			} else {
				err = rowErr
			}
		case facts.TerraformStateOutputFactKind:
			if row, ok, rowErr := terraformStateOutputRow(mat.ScopeID, snapshot, envelope); ok {
				mat.TerraformStateOutputs = append(mat.TerraformStateOutputs, row)
			} else {
				err = rowErr
			}
		default:
			continue
		}
		if err == nil {
			continue
		}
		q, isQuarantine, fatal := partitionProjectorDecodeFailures(envelope, err)
		if fatal != nil {
			// The only fatal decode error is an unsupported schema major, which
			// the projector's schema-version admission (validateFactSchemaVersion
			// in runtime.go) already rejects for the whole work item BEFORE this
			// extractor runs, so a fatal here is unreachable on the production
			// path. Dropping it matches the pre-typing extractor's behavior for a
			// fact it could not read, and never fails the whole repository
			// projection over one fact.
			continue
		}
		if isQuarantine {
			quarantined = append(quarantined, q)
		}
	}
	mat.TerraformStateModules = append(mat.TerraformStateModules, aggregateTerraformStateModuleRows(moduleRows)...)
	return quarantined
}

// terraformStateSnapshot decodes the first terraform_state_snapshot envelope
// (if any) through the typed factschema seam. Snapshot has no required field
// (see tfstatev1.Snapshot), so a decode error here is never a missing-identity
// input_invalid; it is either a payload-shape/type-mismatch input_invalid
// (quarantined per-fact, like every other kind) or an unsupported schema major
// (fatal, unreachable past the projector's schema-version admission). The
// caller routes it through partitionProjectorDecodeFailures with the real fact
// identity, matching every other terraform_state decode site. It
// returns the matched envelope alongside the error so the caller can route the
// error through partitionProjectorDecodeFailures with the real fact identity.
// The zero-value snapshot context (and a zero-value envelope, nil error) is
// returned when no snapshot fact is present, exactly matching the pre-typing
// fallback.
func terraformStateSnapshot(envelopes []facts.Envelope) (terraformStateSnapshotContext, facts.Envelope, error) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.TerraformStateSnapshotFactKind {
			continue
		}
		snapshot, err := decodeTerraformStateSnapshot(envelope)
		if err != nil {
			return terraformStateSnapshotContext{}, envelope, err
		}
		lineage := tfstateDerefString(snapshot.Lineage)
		backendKind := tfstateDerefString(snapshot.BackendKind)
		locatorHash := tfstateDerefString(snapshot.LocatorHash)
		return terraformStateSnapshotContext{
			Lineage:     lineage,
			Serial:      tfstateDerefInt64(snapshot.Serial),
			BackendKind: backendKind,
			LocatorHash: locatorHash,
			StatePath:   terraformStatePath(backendKind, locatorHash, envelope.ScopeID),
		}, envelope, nil
	}
	return terraformStateSnapshotContext{}, facts.Envelope{}, nil
}

func terraformStateResourceRow(
	scopeID string,
	snapshot terraformStateSnapshotContext,
	tagHashesByResource map[string][]string,
	envelope facts.Envelope,
) (TerraformStateResourceRow, bool, error) {
	resource, err := decodeTerraformStateResource(envelope)
	if err != nil {
		return TerraformStateResourceRow{}, false, err
	}
	address := strings.TrimSpace(resource.Address)
	if address == "" {
		// Present-but-empty (or whitespace-only) address is a valid decode,
		// distinct from an absent required key (which the decode seam already
		// dead-lettered). Trim before the gate so a whitespace-only identity is
		// dropped as non-materializable exactly as the pre-typing payloadString
		// path did, never keying a row on an empty-after-trim graph identity.
		return TerraformStateResourceRow{}, false, nil
	}
	sourceSystem := terraformStateSourceSystem(envelope)
	return TerraformStateResourceRow{
		UID:                terraformStateUID("resource", scopeID, snapshot.Lineage, address),
		Address:            address,
		Mode:               tfstateDerefString(resource.Mode),
		ResourceType:       tfstateDerefString(resource.ResourceType),
		Name:               tfstateDerefString(resource.Name),
		ModuleAddress:      tfstateDerefString(resource.Module),
		ProviderAddress:    tfstateDerefString(resource.Provider),
		Lineage:            snapshot.Lineage,
		Serial:             snapshot.Serial,
		BackendKind:        snapshot.BackendKind,
		LocatorHash:        snapshot.LocatorHash,
		StatePath:          snapshot.StatePath,
		SourceFactID:       envelope.FactID,
		StableFactKey:      envelope.StableFactKey,
		SourceSystem:       sourceSystem,
		SourceRecordID:     envelope.SourceRef.SourceRecordID,
		SourceConfidence:   envelope.SourceConfidence,
		CollectorKind:      envelope.CollectorKind,
		CorrelationAnchors: terraformStateCorrelationAnchors(resource.CorrelationAnchors),
		TagKeyHashes:       tagHashesByResource[address],
		Attributes:         terraformStateResourceAttributes(resource.Attributes),
		ObservedAt:         envelope.ObservedAt,
	}, true, nil
}

func terraformStateModuleRow(
	scopeID string,
	snapshot terraformStateSnapshotContext,
	envelope facts.Envelope,
) (TerraformStateModuleRow, bool, error) {
	module, err := decodeTerraformStateModule(envelope)
	if err != nil {
		return TerraformStateModuleRow{}, false, err
	}
	moduleAddress := strings.TrimSpace(module.ModuleAddress)
	if moduleAddress == "" {
		// Whitespace-only identity drops the row as non-materializable, matching
		// the pre-typing payloadString trim (see terraformStateResourceRow).
		return TerraformStateModuleRow{}, false, nil
	}
	sourceSystem := terraformStateSourceSystem(envelope)
	return TerraformStateModuleRow{
		UID:              terraformStateUID("module", scopeID, snapshot.Lineage, moduleAddress),
		ModuleAddress:    moduleAddress,
		ResourceCount:    tfstateDerefInt64(module.ResourceCount),
		Lineage:          snapshot.Lineage,
		Serial:           snapshot.Serial,
		BackendKind:      snapshot.BackendKind,
		LocatorHash:      snapshot.LocatorHash,
		StatePath:        snapshot.StatePath,
		SourceFactID:     envelope.FactID,
		StableFactKey:    envelope.StableFactKey,
		SourceSystem:     sourceSystem,
		SourceRecordID:   envelope.SourceRef.SourceRecordID,
		SourceConfidence: envelope.SourceConfidence,
		CollectorKind:    envelope.CollectorKind,
		ObservedAt:       envelope.ObservedAt,
	}, true, nil
}

func aggregateTerraformStateModuleRows(rows []TerraformStateModuleRow) []TerraformStateModuleRow {
	if len(rows) == 0 {
		return nil
	}
	byModule := make(map[string]TerraformStateModuleRow, len(rows))
	for _, row := range rows {
		existing, ok := byModule[row.ModuleAddress]
		if !ok {
			byModule[row.ModuleAddress] = row
			continue
		}
		existing.ResourceCount += row.ResourceCount
		if row.ObservedAt.After(existing.ObservedAt) {
			existing.SourceFactID = row.SourceFactID
			existing.StableFactKey = row.StableFactKey
			existing.SourceRecordID = row.SourceRecordID
			existing.ObservedAt = row.ObservedAt
		}
		byModule[row.ModuleAddress] = existing
	}
	moduleAddresses := make([]string, 0, len(byModule))
	for moduleAddress := range byModule {
		moduleAddresses = append(moduleAddresses, moduleAddress)
	}
	sort.Strings(moduleAddresses)
	aggregated := make([]TerraformStateModuleRow, 0, len(moduleAddresses))
	for _, moduleAddress := range moduleAddresses {
		aggregated = append(aggregated, byModule[moduleAddress])
	}
	return aggregated
}

func terraformStateOutputRow(
	scopeID string,
	snapshot terraformStateSnapshotContext,
	envelope facts.Envelope,
) (TerraformStateOutputRow, bool, error) {
	output, err := decodeTerraformStateOutput(envelope)
	if err != nil {
		return TerraformStateOutputRow{}, false, err
	}
	name := strings.TrimSpace(output.Name)
	if name == "" {
		// Present-but-empty (or whitespace-only) name is a valid decode, distinct
		// from an absent required key (which the decode seam already
		// dead-lettered). See terraformStateResourceRow.
		return TerraformStateOutputRow{}, false, nil
	}
	sensitive := tfstateDerefBool(output.Sensitive)
	valueShape := tfstateDerefString(output.ValueShape)
	// The raw "value" payload key is intentionally not modeled on the typed
	// Output struct (see tfstatev1.Output's doc comment): it is read directly
	// off the envelope's raw payload here only to check PRESENCE for the
	// fallback value-shape derivation, matching the pre-typing
	// payloadHasKey(payload, "value") behavior byte-for-byte.
	if valueShape == "" && payloadHasKey(envelope.Payload, "value") {
		valueShape = "scalar"
		if sensitive {
			valueShape = "redacted_scalar"
		}
	}
	sourceSystem := terraformStateSourceSystem(envelope)
	return TerraformStateOutputRow{
		UID:              terraformStateUID("output", scopeID, snapshot.Lineage, name),
		Name:             name,
		Sensitive:        sensitive,
		ValueShape:       valueShape,
		Lineage:          snapshot.Lineage,
		Serial:           snapshot.Serial,
		BackendKind:      snapshot.BackendKind,
		LocatorHash:      snapshot.LocatorHash,
		StatePath:        snapshot.StatePath,
		SourceFactID:     envelope.FactID,
		StableFactKey:    envelope.StableFactKey,
		SourceSystem:     sourceSystem,
		SourceRecordID:   envelope.SourceRef.SourceRecordID,
		SourceConfidence: envelope.SourceConfidence,
		CollectorKind:    envelope.CollectorKind,
		ObservedAt:       envelope.ObservedAt,
	}, true, nil
}

func terraformStateSourceSystem(envelope facts.Envelope) string {
	if sourceSystem := strings.TrimSpace(envelope.SourceRef.SourceSystem); sourceSystem != "" {
		return sourceSystem
	}
	return strings.TrimSpace(envelope.CollectorKind)
}

func terraformStatePath(backendKind, locatorHash, scopeID string) string {
	backendKind = strings.TrimSpace(backendKind)
	locatorHash = strings.TrimSpace(locatorHash)
	if backendKind != "" && locatorHash != "" {
		return "tfstate://" + backendKind + "/" + locatorHash
	}
	return "tfstate://" + strings.TrimSpace(scopeID)
}

func terraformStateUID(kind, scopeID, lineage, key string) string {
	return facts.StableID("TerraformStateCanonicalNode", map[string]any{
		"kind":    kind,
		"scope":   scopeID,
		"lineage": lineage,
		"key":     key,
	})
}

// terraformStateCorrelationAnchors folds the typed Resource.CorrelationAnchors
// (each element an untyped {anchor_kind, value_hash} object — see
// tfstatev1.Resource's doc comment for why the element itself stays
// unmodeled) into the sorted "kind:hash" string list the resource row
// carries, preserving the pre-typing raw-payload read byte-for-byte.
func terraformStateCorrelationAnchors(rawAnchors []map[string]any) []string {
	if len(rawAnchors) == 0 {
		return nil
	}
	anchors := make([]string, 0, len(rawAnchors))
	for _, entry := range rawAnchors {
		kind, _ := payloadString(entry, "anchor_kind")
		hash, _ := payloadString(entry, "value_hash")
		if kind == "" || hash == "" {
			continue
		}
		anchors = append(anchors, kind+":"+hash)
	}
	sort.Strings(anchors)
	if len(anchors) == 0 {
		return nil
	}
	return anchors
}

// terraformStateTagHashesByResource decodes every terraform_state_tag_observation
// envelope through the typed factschema seam and joins each valid observation
// to its resource by (ResourceAddress, TagKeyHash). A fact missing either
// required join key is QUARANTINED per-fact (returned in the []quarantinedFact
// slice) rather than silently dropped, mirroring every other terraform_state
// decode site's per-fact isolation contract; every other valid tag
// observation still joins.
func terraformStateTagHashesByResource(envelopes []facts.Envelope) (map[string][]string, []quarantinedFact) {
	tagHashes := map[string][]string{}
	seen := map[string]struct{}{}
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.TerraformStateTagObservationFactKind {
			continue
		}
		observation, err := decodeTerraformStateTagObservation(envelope)
		if err != nil {
			if q, isQuarantine, fatal := partitionProjectorDecodeFailures(envelope, err); fatal == nil && isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		address := strings.TrimSpace(observation.ResourceAddress)
		hash := strings.TrimSpace(observation.TagKeyHash)
		if address == "" || hash == "" {
			// Whitespace-only join key is a valid decode dropped as
			// non-materializable, matching the pre-typing payloadString trim
			// (see terraformStateResourceRow).
			continue
		}
		key := address + "\x00" + hash
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tagHashes[address] = append(tagHashes[address], hash)
	}
	for address := range tagHashes {
		sort.Strings(tagHashes[address])
	}
	return tagHashes, quarantined
}
