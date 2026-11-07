// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/component"
)

const mediaSeedTestRef = "ghcr.io/eshu-hq/eshu-collector-media-import@sha256:9b766668cec1b7fe5901716e2fd4df14b98dbb2a049c646674370ff7f56bb495"

// TestSeedStampsCollectorAndManifest proves the seed dispatch materializes
// the seed payload: the caller binary under the satellite path name plus the
// embedded component manifest with the passed digest stamped into the
// artifact entry for the seed runner's platform. Sibling slots stay
// byte-identical.
func TestSeedStampsCollectorAndManifest(t *testing.T) {
	t.Parallel()

	self := filepath.Join(t.TempDir(), "collector-fixture")
	if err := os.WriteFile(self, []byte("fake-collector-binary"), 0o755); err != nil {
		t.Fatalf("write fixture binary: %v", err)
	}
	engine := fixtureMediaEnginePayload(t)
	engine.platform = "linux/amd64"
	dest := t.TempDir()
	if err := runSeed(dest, mediaSeedTestRef, self, "linux/amd64", engine); err != nil {
		t.Fatalf("runSeed: %v", err)
	}
	collector, err := os.ReadFile(filepath.Join(dest, "collector"))
	if err != nil {
		t.Fatalf("read dest collector: %v", err)
	}
	if string(collector) != "fake-collector-binary" {
		t.Fatalf("dest collector carries %q, want the fixture binary bytes", collector)
	}
	if info, err := os.Stat(filepath.Join(dest, "collector")); err != nil {
		t.Fatalf("stat dest collector: %v", err)
	} else if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("dest collector mode %o is not executable", info.Mode().Perm())
	}
	stamped, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read dest manifest: %v", err)
	}
	var embedded component.Manifest
	if err := yaml.Unmarshal(embeddedManifest, &embedded); err != nil {
		t.Fatalf("decode embedded manifest: %v", err)
	}
	var got component.Manifest
	if err := yaml.Unmarshal(stamped, &got); err != nil {
		t.Fatalf("decode dest manifest: %v", err)
	}
	if len(got.Spec.Artifacts) != len(embedded.Spec.Artifacts) {
		t.Fatalf("dest artifacts = %d, want %d (embedded count)", len(got.Spec.Artifacts), len(embedded.Spec.Artifacts))
	}
	for _, entry := range got.Spec.Artifacts {
		if entry.Platform == "linux/amd64" {
			if entry.Image != mediaSeedTestRef {
				t.Fatalf("dest amd64 image = %q, want the stamped ref %q", entry.Image, mediaSeedTestRef)
			}
			continue
		}
		want := ""
		for _, src := range embedded.Spec.Artifacts {
			if src.Platform == entry.Platform {
				want = src.Image
			}
		}
		if entry.Image != want {
			t.Fatalf("dest sibling platform %q image = %q, want embedded %q", entry.Platform, entry.Image, want)
		}
	}
}

// TestSeedRejectsNonDigestRef proves the seed dispatch fails closed on any
// image reference that is not a digest-pinned ref in this component's own
// repository, and leaves no partial payload behind.
func TestSeedRejectsNonDigestRef(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{
		"",
		"ghcr.io/eshu-hq/eshu-collector-media-import:latest",
		"ghcr.io/eshu-hq/eshu-collector-media-import",
		"ghcr.io/eshu-hq/eshu-collector-media-import@sha256:xyz",
		"ghcr.io/eshu-hq/eshu-collector-media-import@sha256:" + strings.Repeat("0", 63),
		"ghcr.io/eshu-hq/eshu-collector-media-import@sha256:" + strings.Repeat("0", 64) + "1",
		"@sha256:" + strings.Repeat("0", 64),
		"ghcr.io/eshu-hq/eshu-collector-media-import@sha512:" + strings.Repeat("0", 128),
		"ghcr.io/eshu-hq/eshu-collector-document-import@sha256:" + strings.Repeat("0", 64),
	} {
		self := filepath.Join(t.TempDir(), "collector-fixture")
		if err := os.WriteFile(self, []byte("fake-collector-binary"), 0o755); err != nil {
			t.Fatalf("write fixture binary: %v", err)
		}
		engine := fixtureMediaEnginePayload(t)
		engine.platform = "linux/amd64"
		dest := t.TempDir()
		if err := runSeed(dest, ref, self, "linux/amd64", engine); err == nil {
			t.Fatalf("ref %q: got nil error, want a digest-pin failure", ref)
		} else if !strings.Contains(err.Error(), "--image-ref") {
			t.Fatalf("ref %q: error %q does not name --image-ref", ref, err)
		}
		if entries, _ := os.ReadDir(dest); len(entries) != 0 {
			t.Fatalf("ref %q: dest holds %d files, want none (no partial payload)", ref, len(entries))
		}
	}
}

// TestSeedRejectsMissingPlatform proves a seed run for a platform the
// manifest does not declare fails closed with an empty destination, instead
// of stamping the digest into another arch's slot.
func TestSeedRejectsMissingPlatform(t *testing.T) {
	t.Parallel()

	self := filepath.Join(t.TempDir(), "collector-fixture")
	if err := os.WriteFile(self, []byte("fake-collector-binary"), 0o755); err != nil {
		t.Fatalf("write fixture binary: %v", err)
	}
	engine := fixtureMediaEnginePayload(t)
	engine.platform = "linux/riscv64"
	dest := t.TempDir()
	if err := runSeed(dest, mediaSeedTestRef, self, "linux/riscv64", engine); err == nil {
		t.Fatal("runSeed for a missing platform: got nil error, want a failure")
	} else if !strings.Contains(err.Error(), "linux/riscv64") {
		t.Fatalf("runSeed error = %q, want it to name the missing platform", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("runSeed wrote %d dest entries, want a closed failure with none", len(entries))
	}
}

// TestSeedCommandRejectsPositionals proves the seed subcommand admits no
// positional arguments, so a miswired seed init container fails with the
// usage instead of writing a half-configured destination.
func TestSeedCommandRejectsPositionals(t *testing.T) {
	t.Parallel()

	err := runSeedCommand([]string{"--dest", t.TempDir(), "--image-ref", mediaSeedTestRef, "extra"})
	if err == nil {
		t.Fatal("runSeedCommand with a positional: got nil error, want a usage failure")
	} else if !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("runSeedCommand error = %q, want it to name the unexpected arguments", err)
	}
}
