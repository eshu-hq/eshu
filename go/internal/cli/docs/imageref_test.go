// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

func TestContainerImageTruthMarksOversizedManifestIncomplete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "deployment.yaml"),
		bytes.Repeat([]byte("x"), imageTruthMaxFileBytes+1),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(deployment.yaml) error = %v, want nil", err)
	}

	_, complete := containerImageTruth(root)
	if complete {
		t.Fatal("containerImageTruth complete = true, want false for oversized manifest")
	}
}

func TestLocalContainerImageResolverScansLazily(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "deployment.yaml"),
		[]byte("image: ghcr.io/acme/api:1.2.3\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(deployment.yaml) error = %v, want nil", err)
	}

	resolver := LocalContainerImageResolver(root)
	if resolver == nil {
		t.Fatal("LocalContainerImageResolver() = nil, want resolver")
	}
	if err := os.WriteFile(
		filepath.Join(root, "deployment.yaml"),
		[]byte("image: ghcr.io/acme/api:2.0.0\n"),
		0o600,
	); err != nil {
		t.Fatalf("rewrite deployment.yaml error = %v, want nil", err)
	}

	resolution := resolver(doctruth.DocumentInput{}, "ghcr.io/acme/api:2.0.0")
	if !resolution.Supported || !resolution.Exists {
		t.Fatalf("resolution = %#v, want lazy scan to see rewritten manifest", resolution)
	}
}
