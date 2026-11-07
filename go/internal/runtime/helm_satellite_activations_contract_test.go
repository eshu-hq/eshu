// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// satelliteActivationValues carries the reviewed eshu-side activation
// contract for the two Event-B seed-capable satellite images (Refs #8879).
// Each entry mirrors one `componentExtensionCollector.activations` item for
// the eshu-deploy chart: identity, digest-only seed image (linux/amd64, the
// QA node arch), and the exact emitted fact kinds the component needs. The
// chart-side copy is applied in the deploy step; these entries are what it
// must match. Digests are the integration local-proof content digests (both
// seed gates PASS per arch); promotion re-pins them to the pushed digests.
const satelliteActivationValues = `
- instanceId: ocr-cx-qa
  componentId: dev.eshu.collector.document_import
  componentVersion: 0.1.0
  mode: scheduled
  seed:
    image: ghcr.io/eshu-hq/eshu-collector-document-import@sha256:e8d2297bfeacea416d1bb0ec9ccb09d5a27060591042f5572225cf34502efead
    command: ["/usr/local/bin/collector", "seed"]
    pullPolicy: IfNotPresent
  grants:
    - kind: documentation_document
      schemaVersions: ["1.0.0"]
      scope: documentation
    - kind: documentation_section
      schemaVersions: ["1.1.0"]
      scope: documentation
- instanceId: transcription-cx-qa
  componentId: dev.eshu.collector.media_import
  componentVersion: 0.1.0
  mode: scheduled
  seed:
    image: ghcr.io/eshu-hq/eshu-collector-media-import@sha256:68636a8e8bf2c4f3ca2b037505e8d2ee5d28cc0050eae0f29d1554dbd128111a
    command: ["/usr/local/bin/collector", "seed"]
    pullPolicy: IfNotPresent
  grants:
    - kind: documentation_document
      schemaVersions: ["1.0.0"]
      scope: documentation
    - kind: documentation_section
      schemaVersions: ["1.1.0"]
      scope: documentation
`

type satelliteActivationSeed struct {
	Image      string   `yaml:"image"`
	Command    []string `yaml:"command"`
	PullPolicy string   `yaml:"pullPolicy"`
}

type satelliteActivationGrant struct {
	Kind           string   `yaml:"kind"`
	SchemaVersions []string `yaml:"schemaVersions"`
	Scope          string   `yaml:"scope"`
}

type satelliteActivation struct {
	InstanceID       string                     `yaml:"instanceId"`
	ComponentID      string                     `yaml:"componentId"`
	ComponentVersion string                     `yaml:"componentVersion"`
	Mode             string                     `yaml:"mode"`
	Seed             satelliteActivationSeed    `yaml:"seed"`
	Grants           []satelliteActivationGrant `yaml:"grants"`
}

var (
	satelliteSeedImagePattern  = regexp.MustCompile(`^ghcr\.io/eshu-hq/eshu-collector-(document-import|media-import)@sha256:[0-9a-f]{64}$`)
	satelliteInstanceIDPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	satelliteModePattern       = regexp.MustCompile(`^[a-z0-9_-]+$`)
)

// satelliteManifestPaths binds each satellite component id to its authored
// manifest, the arbiter-ruled source of truth (Refs #8879).
var satelliteManifestPaths = map[string]string{
	"dev.eshu.collector.document_import": filepath.Join("..", "..", "cmd", "collector-document-import", "manifest.yaml"),
	"dev.eshu.collector.media_import":    filepath.Join("..", "..", "cmd", "collector-media-import", "manifest.yaml"),
}

func parseSatelliteActivations(t *testing.T) []satelliteActivation {
	t.Helper()

	var activations []satelliteActivation
	if err := yaml.Unmarshal([]byte(satelliteActivationValues), &activations); err != nil {
		t.Fatalf("parse satellite activation values: %v", err)
	}
	return activations
}

func loadSatelliteManifest(t *testing.T, componentID string) component.Manifest {
	t.Helper()

	path, ok := satelliteManifestPaths[componentID]
	if !ok {
		t.Fatalf("no manifest path for componentId %q", componentID)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var manifest component.Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return manifest
}

// TestSatelliteActivationSeedImagesMatchPinnedManifests keeps the reviewed
// activation entries pointed at the exact seed images: every seed image must
// equal the digest-pinned linux/amd64 manifest artifact. A republish that
// moves the image without updating the entry (or vice versa) fails here
// instead of seeding stale bytes in QA.
func TestSatelliteActivationSeedImagesMatchPinnedManifests(t *testing.T) {
	t.Parallel()

	activations := parseSatelliteActivations(t)
	for _, activation := range activations {
		t.Run(activation.ComponentID, func(t *testing.T) {
			t.Parallel()

			if !satelliteSeedImagePattern.MatchString(activation.Seed.Image) {
				t.Fatalf("seed image %q is not a digest-only satellite image ref", activation.Seed.Image)
			}
			manifest := loadSatelliteManifest(t, activation.ComponentID)
			var want string
			for _, artifact := range manifest.Spec.Artifacts {
				if artifact.Platform == "linux/amd64" {
					want = strings.TrimSpace(artifact.Image)
				}
			}
			if want == "" {
				t.Fatalf("manifest for %q declares no linux/amd64 artifact", activation.ComponentID)
			}
			if activation.Seed.Image != want {
				t.Fatalf("seed image = %q, want pinned manifest artifact %q", activation.Seed.Image, want)
			}
		})
	}
}

// TestSatelliteActivationSeedCommandsMatchImageBinaries pins the seed init
// container command each activation runs: the canonical in-image collector
// path plus the `seed` subcommand. The chart appends --dest and --image-ref;
// an entry pointing at a relocated binary would seed nothing.
func TestSatelliteActivationSeedCommandsMatchImageBinaries(t *testing.T) {
	t.Parallel()

	activations := parseSatelliteActivations(t)
	for _, activation := range activations {
		t.Run(activation.ComponentID, func(t *testing.T) {
			t.Parallel()

			want := []string{"/usr/local/bin/collector", "seed"}
			if len(activation.Seed.Command) != len(want) {
				t.Fatalf("seed command = %q, want %q", activation.Seed.Command, want)
			}
			for i := range want {
				if activation.Seed.Command[i] != want[i] {
					t.Fatalf("seed command = %q, want %q", activation.Seed.Command, want)
				}
			}
		})
	}
}

// TestSatelliteActivationGrantsMatchManifestEmittedFacts keeps every grant
// honest: the kinds and schema versions must equal the manifest's declared
// emitted facts, every scope must be a member of the manifest's declared
// collector kinds, and the activation-built grants must pass host admission
// (Manifest.ValidateWithGrants). Grant lifetimes ride the chart default (the
// draft carries no expiresIn); the `eshu component grant` refusal on unknown
// kinds is the host-side gate.
func TestSatelliteActivationGrantsMatchManifestEmittedFacts(t *testing.T) {
	t.Parallel()

	activations := parseSatelliteActivations(t)
	for _, activation := range activations {
		t.Run(activation.ComponentID, func(t *testing.T) {
			t.Parallel()

			manifest := loadSatelliteManifest(t, activation.ComponentID)
			want := map[string]string{}
			for _, family := range manifest.Spec.EmittedFacts {
				want[family.Kind] = strings.Join(family.SchemaVersions, ",")
			}
			if len(activation.Grants) == 0 {
				t.Fatalf("activation %q declares no grants", activation.ComponentID)
			}
			declared := map[string]bool{}
			for _, scope := range manifest.Spec.CollectorKinds {
				if strings.TrimSpace(scope) != "" {
					declared[scope] = true
				}
			}
			seen := map[string]bool{}
			grants := make([]component.ProducerGrant, 0, len(activation.Grants))
			for _, grant := range activation.Grants {
				if seen[grant.Kind] {
					t.Errorf("duplicate grant %q in activation %q", grant.Kind, activation.ComponentID)
				}
				got := strings.Join(grant.SchemaVersions, ",")
				if want[grant.Kind] != got {
					t.Errorf("grant %q versions = %q, want manifest %q", grant.Kind, got, want[grant.Kind])
				}
				if strings.TrimSpace(grant.Scope) == "" {
					t.Errorf("grant %q has no scope", grant.Kind)
				} else if !declared[grant.Scope] {
					t.Errorf("grant %q scope = %q, want a member of manifest collectorKinds %q", grant.Kind, grant.Scope, manifest.Spec.CollectorKinds)
				}
				grants = append(grants, component.ProducerGrant{
					ProducerID:     activation.ComponentID,
					Version:        activation.ComponentVersion,
					Kind:           grant.Kind,
					SchemaVersions: grant.SchemaVersions,
					Scope:          grant.Scope,
					ExpiresAt:      time.Now().UTC().Add(24 * time.Hour),
				})
				seen[grant.Kind] = true
			}
			for kind := range want {
				if !seen[kind] {
					t.Errorf("manifest emitted kind %q has no activation grant entry", kind)
				}
			}
			if err := manifest.ValidateWithGrants(grants); err != nil {
				t.Errorf("activation %q grants refused at host admission: %v", activation.ComponentID, err)
			}
		})
	}
}

// TestSatelliteActivationsValuesShape pins the chart-consumable shape: two
// entries, unique DNS-safe instance IDs, the two satellite identities at
// the current manifest version, and a well-formed mode on each.
func TestSatelliteActivationsValuesShape(t *testing.T) {
	t.Parallel()

	activations := parseSatelliteActivations(t)
	if len(activations) != 2 {
		t.Fatalf("got %d satellite activations, want 2", len(activations))
	}
	wantIDs := map[string]bool{
		"dev.eshu.collector.document_import": false,
		"dev.eshu.collector.media_import":    false,
	}
	seenInstances := map[string]bool{}
	for _, activation := range activations {
		if _, ok := wantIDs[activation.ComponentID]; !ok {
			t.Errorf("unexpected componentId %q", activation.ComponentID)
			continue
		}
		wantIDs[activation.ComponentID] = true
		if len(activation.InstanceID) > 30 || !satelliteInstanceIDPattern.MatchString(activation.InstanceID) {
			t.Errorf("instanceId %q is not a DNS label of at most 30 chars", activation.InstanceID)
		}
		if seenInstances[activation.InstanceID] {
			t.Errorf("duplicate instanceId %q", activation.InstanceID)
		}
		seenInstances[activation.InstanceID] = true
		manifest := loadSatelliteManifest(t, activation.ComponentID)
		if activation.ComponentVersion != manifest.Metadata.Version {
			t.Errorf("componentVersion = %q, want manifest version %q", activation.ComponentVersion, manifest.Metadata.Version)
		}
		if !satelliteModePattern.MatchString(activation.Mode) {
			t.Errorf("mode = %q, want a chart-schema mode string", activation.Mode)
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Errorf("componentId %q has no activation entry", id)
		}
	}
}
