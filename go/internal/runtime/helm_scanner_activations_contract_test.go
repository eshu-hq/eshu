// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	scannercomponent "github.com/eshu-hq/eshu/go/internal/collector/scannerworker/component"
)

// scannerActivationValues carries the reviewed eshu-side activation contract
// for the three pinned scanner-<name> images (Refs #8878). Each entry mirrors
// one `componentExtensionCollector.activations` item in the eshu-deploy chart
// (`helm/eshu/values.yaml`): identity, digest-only seed image, and the exact
// producer grants the component needs. The chart-side copy is applied in the
// deploy step; these entries are what it must match.
//
// Deliberately environment-specific fields are absent: host scope, process
// command, and config targets come from the QA environment at deploy time.
// The scanner binaries take no CLI arguments (stdin/stdout only, plus
// --version), so no process args are declared here.
const scannerActivationValues = `
- instanceId: scanner-image
  componentId: eshu.scanner.image
  componentVersion: 0.1.0
  mode: scheduled
  seed:
    image: ghcr.io/eshu-hq/scanner-image@sha256:d2967a1ab642747ec5bafed484b4a5384839b90b59c9562ee38e67b3fa547d49
    pullPolicy: IfNotPresent
  grants:
    - kind: scanner_worker.analysis
      schemaVersions: ["1.0.0"]
      scope: scanner_worker
      expiresIn: 720h
    - kind: scanner_worker.warning
      schemaVersions: ["1.0.0"]
      scope: scanner_worker
      expiresIn: 720h
    - kind: vulnerability.os_package
      schemaVersions: ["1.0.0"]
      scope: scanner_worker
      expiresIn: 720h
    - kind: vulnerability.warning
      schemaVersions: ["1.0.0"]
      scope: scanner_worker
      expiresIn: 720h
- instanceId: scanner-sbom
  componentId: eshu.scanner.sbom
  componentVersion: 0.1.0
  mode: scheduled
  seed:
    image: ghcr.io/eshu-hq/scanner-sbom@sha256:cbafeeeb2bae99902838e5159bd4003e210f5cfe41891b50b0caf502b2d99dec
    pullPolicy: IfNotPresent
  grants:
    - kind: sbom.document
      schemaVersions: ["1.1.0"]
      scope: scanner_worker
      expiresIn: 720h
    - kind: sbom.component
      schemaVersions: ["1.1.0"]
      scope: scanner_worker
      expiresIn: 720h
    - kind: sbom.warning
      schemaVersions: ["1.1.0"]
      scope: scanner_worker
      expiresIn: 720h
- instanceId: scanner-osruntime
  componentId: eshu.scanner.osruntime
  componentVersion: 0.1.0
  mode: scheduled
  seed:
    image: ghcr.io/eshu-hq/scanner-osruntime@sha256:e302cf4e398f23974210f69988a3c53510738100ae7525ead6d11d992d7be3a8
    pullPolicy: IfNotPresent
  grants:
    - kind: vulnerability.os_package
      schemaVersions: ["1.0.0"]
      scope: scanner_worker
      expiresIn: 720h
    - kind: vulnerability.warning
      schemaVersions: ["1.0.0"]
      scope: scanner_worker
      expiresIn: 720h
`

type scannerActivationGrant struct {
	Kind           string   `yaml:"kind"`
	SchemaVersions []string `yaml:"schemaVersions"`
	Scope          string   `yaml:"scope"`
	ExpiresIn      string   `yaml:"expiresIn"`
}

type scannerActivationSeed struct {
	Image      string `yaml:"image"`
	PullPolicy string `yaml:"pullPolicy"`
}

type scannerActivation struct {
	InstanceID       string                   `yaml:"instanceId"`
	ComponentID      string                   `yaml:"componentId"`
	ComponentVersion string                   `yaml:"componentVersion"`
	Mode             string                   `yaml:"mode"`
	Seed             scannerActivationSeed    `yaml:"seed"`
	Grants           []scannerActivationGrant `yaml:"grants"`
}

var (
	scannerSeedImagePattern  = regexp.MustCompile(`^ghcr\.io/eshu-hq/scanner-[a-z]+@sha256:[0-9a-f]{64}$`)
	scannerInstanceIDPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	scannerModePattern       = regexp.MustCompile(`^[a-z0-9_-]+$`)
)

func parseScannerActivations(t *testing.T) []scannerActivation {
	t.Helper()

	var activations []scannerActivation
	if err := yaml.Unmarshal([]byte(scannerActivationValues), &activations); err != nil {
		t.Fatalf("parse scanner activation values: %v", err)
	}
	return activations
}

// TestScannerActivationSeedImagesMatchPinnedManifests keeps the reviewed
// activation entries pointed at the exact published images: every seed image
// must equal the digest-pinned artifact ManifestFor builds from
// artifactDigests. A republish that moves the code pin without updating the
// entry (or vice versa) fails here instead of pulling stale analyzers in QA.
func TestScannerActivationSeedImagesMatchPinnedManifests(t *testing.T) {
	t.Parallel()

	activations := parseScannerActivations(t)
	for _, activation := range activations {
		t.Run(activation.ComponentID, func(t *testing.T) {
			t.Parallel()

			if !scannerSeedImagePattern.MatchString(activation.Seed.Image) {
				t.Fatalf("seed image %q is not a digest-only scanner image ref", activation.Seed.Image)
			}
			kind, err := scannercomponent.AnalyzerFor(activation.ComponentID)
			if err != nil {
				t.Fatalf("resolve analyzer for %q: %v", activation.ComponentID, err)
			}
			manifest, err := scannercomponent.ManifestFor(kind)
			if err != nil {
				t.Fatalf("build manifest for %q: %v", kind, err)
			}
			if len(manifest.Spec.Artifacts) != 1 {
				t.Fatalf("manifest for %q declares %d artifacts, want exactly 1", kind, len(manifest.Spec.Artifacts))
			}
			if got := strings.TrimSpace(manifest.Spec.Artifacts[0].Image); got != activation.Seed.Image {
				t.Fatalf("seed image = %q, want pinned manifest artifact %q", activation.Seed.Image, got)
			}
		})
	}
}

// TestScannerActivationGrantsMatchRequiredGrants keeps every example grant
// honest: the kinds, schema versions, and scope must equal the RequiredGrants
// set the host enforces, and every entry must carry a positive grant lifetime
// (`eshu component grant` refuses unknown kinds, leaving the reconcile init
// container in Init:Error).
func TestScannerActivationGrantsMatchRequiredGrants(t *testing.T) {
	t.Parallel()

	activations := parseScannerActivations(t)
	for _, activation := range activations {
		t.Run(activation.ComponentID, func(t *testing.T) {
			t.Parallel()

			kind, err := scannercomponent.AnalyzerFor(activation.ComponentID)
			if err != nil {
				t.Fatalf("resolve analyzer for %q: %v", activation.ComponentID, err)
			}
			manifest, err := scannercomponent.ManifestFor(kind)
			if err != nil {
				t.Fatalf("build manifest for %q: %v", kind, err)
			}
			want := map[string]string{}
			for _, grant := range scannercomponent.RequiredGrants(manifest) {
				want[grant.Kind] = strings.Join(grant.SchemaVersions, ",") + "\x00" + grant.Scope
			}
			if len(activation.Grants) == 0 {
				t.Fatalf("activation %q declares no grants", activation.ComponentID)
			}
			seen := map[string]bool{}
			for _, grant := range activation.Grants {
				if seen[grant.Kind] {
					t.Errorf("duplicate grant %q in activation %q", grant.Kind, activation.ComponentID)
				}
				got := strings.Join(grant.SchemaVersions, ",") + "\x00" + grant.Scope
				if want[grant.Kind] != got {
					t.Errorf("grant %q = %q, want RequiredGrants %q", grant.Kind, got, want[grant.Kind])
				}
				lifetime, err := time.ParseDuration(strings.TrimSpace(grant.ExpiresIn))
				if err != nil || lifetime <= 0 {
					t.Errorf("grant %q expiresIn = %q, want a positive duration", grant.Kind, grant.ExpiresIn)
				}
				seen[grant.Kind] = true
			}
			for kind := range want {
				if !seen[kind] {
					t.Errorf("RequiredGrants kind %q has no activation grant entry", kind)
				}
			}
		})
	}
}

// TestScannerActivationsValuesShape pins the chart-consumable shape: three
// entries, unique DNS-safe instance IDs, the three component identities at the
// current manifest version, and a well-formed mode on each.
func TestScannerActivationsValuesShape(t *testing.T) {
	t.Parallel()

	activations := parseScannerActivations(t)
	if len(activations) != 3 {
		t.Fatalf("got %d scanner activations, want 3", len(activations))
	}
	wantIDs := map[string]bool{
		scannercomponent.IDImage:     false,
		scannercomponent.IDSBOM:      false,
		scannercomponent.IDOSRuntime: false,
	}
	seenInstances := map[string]bool{}
	for _, activation := range activations {
		if _, ok := wantIDs[activation.ComponentID]; !ok {
			t.Errorf("unexpected componentId %q", activation.ComponentID)
			continue
		}
		wantIDs[activation.ComponentID] = true
		if len(activation.InstanceID) > 30 || !scannerInstanceIDPattern.MatchString(activation.InstanceID) {
			t.Errorf("instanceId %q is not a DNS label of at most 30 chars", activation.InstanceID)
		}
		if seenInstances[activation.InstanceID] {
			t.Errorf("duplicate instanceId %q", activation.InstanceID)
		}
		seenInstances[activation.InstanceID] = true
		if activation.ComponentVersion != scannercomponent.Version {
			t.Errorf("componentVersion = %q, want manifest version %q", activation.ComponentVersion, scannercomponent.Version)
		}
		if !scannerModePattern.MatchString(activation.Mode) {
			t.Errorf("mode = %q, want a chart-schema mode string", activation.Mode)
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Errorf("componentId %q has no activation entry", id)
		}
	}
}
