// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/component"
)

const (
	mediaComponentID = "dev.eshu.collector.media_import"
	// mediaSeedImageRepo is the only repository the seed stamp accepts. A
	// well-formed digest from any other repo is refused: the installed
	// manifest must name the bytes kubelet actually pulled for this
	// component, never a foreign image.
	mediaSeedImageRepo = "ghcr.io/eshu-hq/eshu-collector-media-import"
	// The seed image carries the manifest copy at /manifest.yaml; the seed
	// subcommand stamps the embedded identical bytes instead, so the
	// payload always matches the binary that ships it.
)

// runSeedCommand parses the seed subcommand flags and materializes the seed
// payload: collector seed --dest <dir> --image-ref <repo@sha256:…>. Both
// flags are required and no positional arguments are admitted, so a miswired
// seed init container fails with the usage instead of writing a
// half-configured destination.
func runSeedCommand(args []string) error {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dest := flags.String("dest", "", "destination directory for the seed payload")
	imageRef := flags.String("image-ref", "", "digest-pinned seed image reference (repo@sha256:<64hex>)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("collector seed --dest <dir> --image-ref <repo@sha256:…>: %w", err)
	}
	if len(flags.Args()) != 0 {
		return fmt.Errorf("collector seed --dest <dir> --image-ref <repo@sha256:…>: unexpected arguments %q", flags.Args())
	}
	if strings.TrimSpace(*dest) == "" {
		return fmt.Errorf("collector seed --dest <dir> --image-ref <repo@sha256:…>: --dest is required")
	}
	if strings.TrimSpace(*imageRef) == "" {
		return fmt.Errorf("collector seed --dest <dir> --image-ref <repo@sha256:…>: --image-ref is required")
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("collector seed resolves the caller binary: %w", err)
	}
	return runSeed(strings.TrimSpace(*dest), strings.TrimSpace(*imageRef), self, runtime.GOOS+"/"+runtime.GOARCH)
}

// embeddedManifest carries the authored component manifest inside the caller
// binary so the seed dispatch materializes exactly the manifest the binary
// was built with, without reading the source tree at seed time.
//
//go:embed manifest.yaml
var embeddedManifest []byte

// runSeed validates the image reference and materializes the seed payload
// into dest: the caller binary under the satellite path name plus the
// embedded component manifest with imageRef stamped into the artifact entry
// for platform. The reference must name this component's own repository and
// pass the shared component artifact gate before anything is written, so a
// foreign or non-digest ref leaves dest untouched. Sibling platform slots
// are left byte-identical.
func runSeed(dest, imageRef, selfPath, platform string) error {
	if strings.TrimSpace(dest) == "" {
		return fmt.Errorf("seed destination directory is required")
	}
	if strings.TrimSpace(selfPath) == "" {
		return fmt.Errorf("seed source binary path is required")
	}
	var manifest component.Manifest
	if err := yaml.Unmarshal(embeddedManifest, &manifest); err != nil {
		return fmt.Errorf("decode embedded seed manifest: %w", err)
	}
	if manifest.Metadata.ID != mediaComponentID {
		return fmt.Errorf("embedded seed manifest is component %q, refusing to stamp %q", manifest.Metadata.ID, mediaComponentID)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(embeddedManifest, &document); err != nil {
		return fmt.Errorf("decode embedded seed manifest: %w", err)
	}
	artifact, err := mediaSeedArtifactNodeForPlatform(&document, platform)
	if err != nil {
		return fmt.Errorf("embedded seed manifest: %w", err)
	}
	image := mediaSeedMappingValue(artifact, "image")
	if image == nil || image.Kind != yaml.ScalarNode {
		return fmt.Errorf("embedded seed manifest declares no spec.artifacts image for platform %q to stamp", platform)
	}
	if repo := mediaSeedImageRepoOfRef(imageRef); repo != mediaSeedImageRepo {
		return fmt.Errorf("collector seed --image-ref %q: repo %q is not this component's seed repository %q", imageRef, repo, mediaSeedImageRepo)
	}
	candidate := component.Artifact{Platform: platform, Image: imageRef}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("collector seed --image-ref %q: %w", imageRef, err)
	}
	image.Value = imageRef
	image.Tag = "!!str"

	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("create seed destination: %w", err)
	}
	if err := copySeedFile(selfPath, filepath.Join(dest, "collector"), 0o555); err != nil {
		return err
	}
	var rewritten bytes.Buffer
	encoder := yaml.NewEncoder(&rewritten)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		_ = encoder.Close()
		return fmt.Errorf("encode stamped manifest: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("encode stamped manifest: %w", err)
	}
	manifestPath := filepath.Join(dest, "manifest.yaml")
	// #nosec G306 -- the seed manifest carries no secrets (image digests, fact kinds, schema versions only); read-only mode protects the stamped payload in the shared seed dir.
	if err := os.WriteFile(manifestPath, rewritten.Bytes(), 0o444); err != nil {
		return fmt.Errorf("write seed manifest: %w", err)
	}
	// #nosec G304 -- manifestPath joins the operator --dest dir with the fixed manifest.yaml name, verified by re-read immediately below.
	stamped, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("re-read seed manifest: %w", err)
	}
	var verified component.Manifest
	if err := yaml.Unmarshal(stamped, &verified); err != nil {
		return fmt.Errorf("decode stamped manifest: %w", err)
	}
	if verified.Metadata.ID != mediaComponentID {
		return fmt.Errorf("stamped manifest is component %q, want %q", verified.Metadata.ID, mediaComponentID)
	}
	matched := false
	for _, entry := range verified.Spec.Artifacts {
		if entry.Platform == platform {
			matched = true
			if err := entry.Validate(); err != nil {
				return fmt.Errorf("stamped manifest: %w", err)
			}
			if entry.Image != imageRef {
				return fmt.Errorf("stamped manifest image = %q, want %q", entry.Image, imageRef)
			}
			continue
		}
		if want := mediaSeedSourceImage(manifest, entry.Platform); entry.Image != want {
			return fmt.Errorf("stamped manifest moved sibling platform %q image = %q, want %q", entry.Platform, entry.Image, want)
		}
	}
	if !matched {
		return fmt.Errorf("stamped manifest declares no artifact for platform %q", platform)
	}
	return nil
}

// mediaSeedImageRepoOfRef returns the repository half of a repo@digest
// reference, or "" when the reference has no digest separator.
func mediaSeedImageRepoOfRef(imageRef string) string {
	repo, _, found := strings.Cut(imageRef, "@")
	if !found {
		return ""
	}
	return strings.TrimSpace(repo)
}

// mediaSeedSourceImage returns the image the embedded manifest pins for
// platform, or "" when it declares none.
func mediaSeedSourceImage(manifest component.Manifest, platform string) string {
	for _, entry := range manifest.Spec.Artifacts {
		if entry.Platform == platform {
			return entry.Image
		}
	}
	return ""
}

// mediaSeedArtifactNodeForPlatform locates the spec.artifacts entry for
// platform in a decoded manifest document for the seed stamp.
func mediaSeedArtifactNodeForPlatform(document *yaml.Node, platform string) (*yaml.Node, error) {
	if document == nil || len(document.Content) == 0 {
		return nil, fmt.Errorf("manifest document is empty")
	}
	root := document.Content[0]
	spec := mediaSeedMappingValue(root, "spec")
	if spec == nil {
		return nil, fmt.Errorf("manifest declares no spec to stamp")
	}
	artifacts := mediaSeedMappingValue(spec, "artifacts")
	if artifacts == nil || artifacts.Kind != yaml.SequenceNode || len(artifacts.Content) == 0 {
		return nil, fmt.Errorf("manifest declares no spec.artifacts to stamp")
	}
	for _, entry := range artifacts.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		if mediaSeedScalar(entry, "platform") == platform {
			return entry, nil
		}
	}
	return nil, fmt.Errorf("manifest declares no spec.artifacts entry for platform %q", platform)
}

// mediaSeedMappingValue returns the value for key in a mapping node, or nil.
func mediaSeedMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// mediaSeedScalar returns the scalar value for key in a mapping node, or "".
func mediaSeedScalar(node *yaml.Node, key string) string {
	value := mediaSeedMappingValue(node, key)
	if value == nil {
		return ""
	}
	return value.Value
}

// copySeedFile copies one regular file to a destination path with the given
// permission bits.
func copySeedFile(src, dst string, perm os.FileMode) error {
	// #nosec G304 -- src is the caller binary (os.Executable) or a test fixture; dst joins the operator --dest dir with fixed collector/manifest.yaml names.
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read seed source: %w", err)
	}
	// #nosec G304,G703 -- dst joins the operator --dest dir (0750) with the fixed collector/manifest.yaml names; no traversal beyond the designated seed dir, and the stamp is verified by re-read.
	if err := os.WriteFile(dst, body, perm); err != nil {
		return fmt.Errorf("write seed payload: %w", err)
	}
	return nil
}
