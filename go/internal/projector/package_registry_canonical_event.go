// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// PackageRegistryEventRow carries one source-reported registry lifecycle
// event (publish, yank, unyank, deprecate, delete, unlist, or a generic
// registry-reported mutation) scoped to a package version, for canonical
// graph projection. This is the #5458 yank/deprecate TIMELINE the epic names:
// before this row existed, package_registry.registry_event facts were
// collected and read by nothing, so a package's publish/yank/deprecate
// history had no graph representation at all.
type PackageRegistryEventRow struct {
	UID                 string
	PackageID           string
	VersionID           string
	Version             string
	Ecosystem           string
	Registry            string
	EventKey            string
	EventType           string
	ArtifactKey         string
	Actor               string
	Message             string
	OccurredAt          time.Time
	SourceFactID        string
	StableFactKey       string
	SourceSystem        string
	SourceRecordID      string
	SourceConfidence    string
	CollectorKind       string
	CorrelationAnchors  []string
	CollectorInstanceID string
	ObservedAt          time.Time
}

// packageRegistryEventRow decodes one package_registry.registry_event
// envelope through the typed factschema seam and builds its canonical row. A
// missing required event_key or event_type dead-letters via the returned
// error (both are schema-required, see
// sdk/go/factschema/schema/package_registry.registry_event.v1.schema.json).
//
// package_id and version_id are schema-OPTIONAL (a registry can report an
// event that is not scoped to one version, or even one package), so a
// present-but-absent value for either is a VALID decode -- not a dead-letter
// -- that this row builder's own identity gate still drops: this row exists
// to project a per-VERSION yank/deprecate timeline, and an event with no
// version to anchor on has nothing to attach a graph edge to. That mirrors
// packageRegistryArtifactRow's package_id/version_id/artifact_key gate, one
// step further: version_id is schema-nullable there too, but every emitted
// package_artifact fact carries one (an artifact always belongs to a
// specific version), so the gate is de facto never exercised for artifacts.
// Registry-wide events are real and reachable, so this row explicitly
// documents (and tests) that path rather than assuming it away.
func packageRegistryEventRow(envelope facts.Envelope) (PackageRegistryEventRow, bool, error) {
	if envelope.IsTombstone {
		return PackageRegistryEventRow{}, false, nil
	}
	event, err := decodePackageRegistryRegistryEvent(envelope)
	if err != nil {
		return PackageRegistryEventRow{}, false, err
	}
	packageID := strings.TrimSpace(packageRegistryDerefString(event.PackageID))
	versionID := strings.TrimSpace(packageRegistryDerefString(event.VersionID))
	if packageID == "" || versionID == "" {
		// Present-but-empty (or absent) identity is a valid decode, distinct
		// from an absent required key. See packageRegistryArtifactRow and this
		// function's doc comment for why version_id gates materialization here.
		return PackageRegistryEventRow{}, false, nil
	}
	stableFactKey := strings.TrimSpace(envelope.StableFactKey)
	if stableFactKey == "" {
		return PackageRegistryEventRow{}, false, nil
	}
	return PackageRegistryEventRow{
		UID:                 stableFactKey,
		PackageID:           packageID,
		VersionID:           versionID,
		Version:             packageRegistryDerefString(event.Version),
		Ecosystem:           packageRegistryDerefString(event.Ecosystem),
		Registry:            packageRegistryDerefString(event.Registry),
		EventKey:            event.EventKey,
		EventType:           event.EventType,
		ArtifactKey:         packageRegistryDerefString(event.ArtifactKey),
		Actor:               packageRegistryDerefString(event.Actor),
		Message:             packageRegistryDerefString(event.Message),
		OccurredAt:          packageRegistryParsedPublishedAt(event.OccurredAt),
		SourceFactID:        envelope.FactID,
		StableFactKey:       stableFactKey,
		SourceSystem:        packageRegistrySourceSystem(envelope),
		SourceRecordID:      envelope.SourceRef.SourceRecordID,
		SourceConfidence:    envelope.SourceConfidence,
		CollectorKind:       envelope.CollectorKind,
		CorrelationAnchors:  packageRegistrySortedStrings(event.CorrelationAnchors),
		CollectorInstanceID: packageRegistryDerefString(event.CollectorInstanceID),
		ObservedAt:          envelope.ObservedAt,
	}, true, nil
}
