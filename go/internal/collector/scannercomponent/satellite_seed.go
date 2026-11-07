// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannercomponent

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/component"
)

const (
	// SatelliteDocumentImportID is the seed-capable document-import
	// satellite component identity (#8879 Event-B).
	SatelliteDocumentImportID = "dev.eshu.collector.document_import"
	// SatelliteMediaImportID is the seed-capable media-import satellite
	// component identity (#8879 Event-B).
	SatelliteMediaImportID = "dev.eshu.collector.media_import"
)

// satelliteSeedPins is the host-side pin registry for satellite seed images
// (#10285): the published seed-image digest per satellite component ID and
// platform, mirroring the scanner artifactDigests pin source. Each entry
// equals the artifacts[] entry in the matching caller manifest.yaml
// (go/cmd/collector-document-import/manifest.yaml and
// go/cmd/collector-media-import/manifest.yaml); the
// TestSatelliteSeedPinsMatchCallerManifests binding fails when the two
// drift apart, so a re-pin lands in one commit on both sides, the same
// convention the scanner re-pin PRs follow.
//
// Pin semantics match the scanner pins: these are platform-side admission
// truth, not seed-time comparisons. Seed stamps the deploy-time --image-ref
// into the installed manifest copy; VerifySatelliteSeededArtifact compares
// the installed artifacts against this registry and fails closed on drift,
// before any claim or execution.
var satelliteSeedPins = map[string]map[string]string{
	SatelliteDocumentImportID: {
		"linux/amd64": "ghcr.io/eshu-hq/eshu-collector-document-import@sha256:2e813571b2e23367cbf09e4fb8c1bff6028a81567c8d55001511a8e8f8a40ca5",
		"linux/arm64": "ghcr.io/eshu-hq/eshu-collector-document-import@sha256:e15c3b8f1597537ca2661e6fbf5a26c2515a3c818be77e2f9e0e8770ae0e9d5d",
	},
	SatelliteMediaImportID: {
		"linux/amd64": "ghcr.io/eshu-hq/eshu-collector-media-import@sha256:bae515ec1b7543696a1641eccbfa7c5e6c73220702573c54690f771eee455ce4",
		"linux/arm64": "ghcr.io/eshu-hq/eshu-collector-media-import@sha256:348d7ae76d875e52eb95ebfc25ede0599dc959e7d4fd74a0a2c3efdba98bfb22",
	},
}

// SatelliteSeedPins returns a copy of the host-side satellite seed-image
// pin registry: component ID to platform to digest-pinned image. Callers
// must not mutate the registry; the copy keeps the admission truth stable
// for the process lifetime.
func SatelliteSeedPins() map[string]map[string]string {
	out := make(map[string]map[string]string, len(satelliteSeedPins))
	for id, platforms := range satelliteSeedPins {
		inner := make(map[string]string, len(platforms))
		for platform, image := range platforms {
			inner[platform] = image
		}
		out[id] = inner
	}
	return out
}

// VerifySatelliteSeededArtifact enforces the #10285 satellite admission
// contract for a registry-loaded component manifest: when the manifest
// belongs to a satellite family in the host-side pin registry, its artifact
// set must equal the registry pins for that component, platform for
// platform. Callers on the load-then-claim path (the extension worker's
// selectActivation and the coordinator's activation planner) run this next
// to VerifySeededArtifact after loading the installed manifest — which the
// chart reconcile init container installed from the seeded manifest.yaml —
// and refuse the activation on any mismatch, before any claim or execution.
//
// The check owns only the registry-scoped satellite IDs: any other
// component ID passes through untouched, preserving the scanner and
// third-party pass-through semantics. A registry-scoped identity with no
// artifacts, a missing platform pin, or an artifact whose image differs
// from the pin denies fail-closed. Error text names the component ID (safe
// per the package privacy rule) and the defect, never an image value:
// neither the seeded ref nor the pin is echoed.
func VerifySatelliteSeededArtifact(manifest component.Manifest) error {
	id := manifest.Metadata.ID
	pins, ok := satelliteSeedPins[id]
	if !ok {
		return nil
	}
	if len(manifest.Spec.Artifacts) != len(pins) {
		return fmt.Errorf("satellite component %q declares no pinned seed image artifact set: refusing admission without the pinned platform images", id)
	}
	for _, artifact := range manifest.Spec.Artifacts {
		want, ok := pins[artifact.Platform]
		if !ok || artifact.Image != want {
			return fmt.Errorf("satellite component %q seeded artifact is not the pinned platform image set: refusing admission", id)
		}
	}
	return nil
}
