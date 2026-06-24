// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	goruntime "runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
)

const ociConfigTestDigest = "ghcr.io/eshu-hq/examples/scorecard-collector@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func ociAdapterManifest(platform string) component.Manifest {
	return component.Manifest{
		Metadata: component.Metadata{ID: "dev.eshu.examples.scorecard"},
		Spec: component.Spec{
			Runtime:   component.RuntimeContract{Adapter: component.RuntimeAdapterOCI},
			Artifacts: []component.Artifact{{Platform: platform, Image: ociConfigTestDigest}},
		},
	}
}

func TestRunnerForAdapterBuildsOCIRunnerFromManifestArtifact(t *testing.T) {
	t.Parallel()

	manifest := ociAdapterManifest(goruntime.GOOS + "/" + goruntime.GOARCH)
	runner, err := runnerForAdapter(manifest, componentRuntimeFile{
		OCI: ociRuntimeConfig{Network: "scorecard-egress", Env: []string{"TOKEN=x"}},
	})
	if err != nil {
		t.Fatalf("runnerForAdapter() error = %v, want nil", err)
	}
	ociRunner, ok := runner.(extensionhost.OCIRunner)
	if !ok {
		t.Fatalf("runner = %T, want extensionhost.OCIRunner", runner)
	}
	if ociRunner.ImageRef != ociConfigTestDigest {
		t.Fatalf("ImageRef = %q, want manifest digest %q", ociRunner.ImageRef, ociConfigTestDigest)
	}
	if ociRunner.Network != "scorecard-egress" {
		t.Fatalf("Network = %q, want operator override", ociRunner.Network)
	}
}

func TestRunnerForAdapterRejectsUnknownAdapter(t *testing.T) {
	t.Parallel()

	manifest := component.Manifest{Spec: component.Spec{Runtime: component.RuntimeContract{Adapter: "wasm"}}}
	if _, err := runnerForAdapter(manifest, componentRuntimeFile{}); err == nil {
		t.Fatal("runnerForAdapter() error = nil, want unsupported-adapter error")
	}
}

func TestOCIArtifactImagePrefersPlatformMatch(t *testing.T) {
	t.Parallel()

	platform := goruntime.GOOS + "/" + goruntime.GOARCH
	manifest := component.Manifest{
		Metadata: component.Metadata{ID: "dev.eshu.examples.scorecard"},
		Spec: component.Spec{Artifacts: []component.Artifact{
			{Platform: "other/arch", Image: "ghcr.io/x@sha256:" + repeat64('c')},
			{Platform: platform, Image: ociConfigTestDigest},
		}},
	}
	image, err := ociArtifactImage(manifest)
	if err != nil {
		t.Fatalf("ociArtifactImage() error = %v", err)
	}
	if image != ociConfigTestDigest {
		t.Fatalf("image = %q, want platform-matched %q", image, ociConfigTestDigest)
	}
}

func TestOCIArtifactImageErrorsWhenNoPlatformMatch(t *testing.T) {
	t.Parallel()

	manifest := component.Manifest{
		Metadata: component.Metadata{ID: "dev.eshu.examples.scorecard"},
		Spec: component.Spec{Artifacts: []component.Artifact{
			{Platform: "other/arch", Image: "ghcr.io/x@sha256:" + repeat64('c')},
			{Platform: "another/arch", Image: "ghcr.io/y@sha256:" + repeat64('d')},
		}},
	}
	if _, err := ociArtifactImage(manifest); err == nil {
		t.Fatal("ociArtifactImage() error = nil, want no-platform-match error")
	}
}

func repeat64(b byte) string {
	out := make([]byte, 64)
	for i := range out {
		out[i] = b
	}
	return string(out)
}
