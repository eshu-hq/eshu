// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannercomponent

import (
	"fmt"
	"strings"

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
// into the installed manifest copy's own platform slot;
// VerifySatelliteSeededArtifact compares that slot against this registry and
// fails closed on drift, before any claim or execution.
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
// contract for a registry-loaded component manifest on one host platform:
// when the manifest belongs to a satellite family in the host-side pin
// registry, its artifact for platform must equal the registry pin for that
// component on that platform. Callers on the load-then-claim path (the
// extension worker's selectActivation and the coordinator's activation
// planner) run this next to VerifySeededArtifact after loading the installed
// manifest — which the chart reconcile init container installed from the
// seeded manifest.yaml — and refuse the activation on any mismatch, before
// any claim or execution. Callers pass their own runtime.GOOS/GOARCH pair:
// only the executing host's slot is ever pulled, so only it is consulted.
//
// The scope is deliberately the single executing platform, not the full
// artifact set. Seed stamps the deploy-time --image-ref into the runner's own
// platform slot and leaves sibling slots byte-identical to the image-embedded
// manifest, which freezes at image build time and goes stale on every repin
// (the image cannot contain its own digest, so a rebuild voids the pin).
// Demanding full-set equality therefore refuses every honestly seeded host
// after a repin (#9877); the sibling slot is never executed here, so
// ignoring it loses no protection: a drifted local seed image still fails.
//
// The check owns only the registry-scoped satellite IDs: any other
// component ID passes through untouched, preserving the scanner and
// third-party pass-through semantics. A registry-scoped identity with no
// artifact for the host platform, a platform the registry does not pin, a
// duplicated host-platform entry, a non-canonical (whitespace-padded) host
// platform spelling, or an image that differs from the pin denies
// fail-closed. Error text names the component ID (safe per the package
// privacy rule) and the defect, never an image value: neither the seeded ref
// nor the pin is echoed.
func VerifySatelliteSeededArtifact(manifest component.Manifest, platform string) error {
	id := manifest.Metadata.ID
	pins, ok := satelliteSeedPins[id]
	if !ok {
		return nil
	}
	want, ok := pins[platform]
	if !ok {
		return fmt.Errorf("satellite component %q declares no seed image for platform %q: refusing admission without the pinned platform image", id, platform)
	}
	// Candidate selection is trim-consistent with the image picker
	// (ociArtifactImage matches on strings.TrimSpace) and with
	// Artifact.Validate, which accepts a whitespace-padded platform: every
	// artifact whose trimmed platform equals the host platform is a
	// candidate, and the single surviving candidate must then be canonical
	// (Platform == platform exactly) and pinned (Image == pins[platform]
	// exactly). A padded host entry therefore denies rather than being
	// silently skipped (#9877 arbiter ruling 2.4).
	var candidate *component.Artifact
	matched := 0
	for i, artifact := range manifest.Spec.Artifacts {
		if strings.TrimSpace(artifact.Platform) != platform {
			continue
		}
		matched++
		candidate = &manifest.Spec.Artifacts[i]
	}
	if matched == 0 {
		return fmt.Errorf("satellite component %q declares no seed image for platform %q: refusing admission without the pinned platform image", id, platform)
	}
	if matched > 1 {
		return fmt.Errorf("satellite component %q declares a duplicate seed image artifact: refusing admission without the pinned platform images", id)
	}
	if candidate.Platform != platform {
		return fmt.Errorf("satellite component %q declares a non-canonical seed image platform: refusing admission without the pinned platform image", id)
	}
	if candidate.Image != want {
		return fmt.Errorf("satellite component %q seeded artifact is not the pinned platform image: refusing admission", id)
	}
	return nil
}
