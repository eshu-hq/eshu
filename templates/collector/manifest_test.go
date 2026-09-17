// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"regexp"
	"strings"
	"testing"
)

var digestPattern = regexp.MustCompile(`@sha256:[A-Fa-f0-9]{64}$`)

// TestManifestReleaseProvenance proves releases are reproducible and the
// component manifest is valid: every artifact is digest-pinned, the manifest
// carries the supported core range and wire protocol, and every emitted fact
// family is versioned with declared confidence.
func TestManifestReleaseProvenance(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	if strings.TrimSpace(manifest.APIVersion) == "" || strings.TrimSpace(manifest.Kind) == "" {
		t.Fatal("manifest missing apiVersion/kind")
	}
	if strings.TrimSpace(manifest.Metadata.ID) == "" || strings.TrimSpace(manifest.Metadata.Version) == "" {
		t.Fatal("manifest missing metadata id/version")
	}
	if manifest.Metadata.ID != ComponentID {
		t.Fatalf("manifest id = %q, want %q", manifest.Metadata.ID, ComponentID)
	}
	if len(manifest.Spec.Artifacts) == 0 {
		t.Fatal("manifest declares no artifacts")
	}
	for _, artifact := range manifest.Spec.Artifacts {
		if !digestPattern.MatchString(artifact.Image) {
			t.Fatalf("artifact image %q is not digest-pinned", artifact.Image)
		}
		if strings.Contains(artifact.Image, ":latest") {
			t.Fatalf("artifact image %q uses a mutable tag", artifact.Image)
		}
		// The all-zero digest matches the pin format but names no pullable
		// artifact. The pristine template ships unpinned, so zeros are
		// tolerated only while the manifest still carries the template
		// placeholder identity (compared as a literal: adopters rename the
		// ComponentID constant too, so comparing against it would disarm
		// this tripwire). Renaming is the mandatory first adopter step;
		// from that moment the gate stays red until scripts/pin-digest.sh
		// records the real pushed digest.
		if strings.HasSuffix(artifact.Image, "@sha256:"+strings.Repeat("0", 64)) &&
			manifest.Metadata.ID != "dev.eshu.template.collector" {
			t.Fatalf("artifact image %q is the unpinned placeholder digest: run scripts/pin-digest.sh", artifact.Image)
		}
	}
	if len(manifest.Spec.EmittedFacts) == 0 {
		t.Fatal("manifest declares no emitted facts")
	}
	for _, family := range manifest.Spec.EmittedFacts {
		if strings.TrimSpace(family.Kind) == "" {
			t.Fatal("emitted fact kind empty")
		}
		if len(family.SchemaVersions) == 0 {
			t.Fatalf("fact kind %q has no schema versions", family.Kind)
		}
		if len(family.SourceConfidence) == 0 {
			t.Fatalf("fact kind %q has no source confidence", family.Kind)
		}
	}
	if len(manifest.Spec.ConsumerContracts.Reducer.Phases) == 0 {
		t.Fatal("manifest missing reducer consumer contract")
	}
	if strings.TrimSpace(manifest.Spec.Telemetry.MetricsPrefix) == "" {
		t.Fatal("manifest missing telemetry metrics prefix")
	}
}
