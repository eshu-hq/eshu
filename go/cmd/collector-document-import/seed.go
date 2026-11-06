// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
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

// Seed identity: the component this caller seeds into a satellite
// activation. The seed path stamps a digest-pinned image into the installed
// manifest copy and refuses to stamp any other component's manifest.
const (
	componentID      = "dev.eshu.collector.document_import"
	componentVersion = "0.1.0"
	// seedImageRepo is the only repository the seed stamp accepts. A
	// well-formed digest from any other repo is refused: the installed
	// manifest must name the bytes kubelet actually pulled for this
	// component, never a foreign image.
	seedImageRepo = "ghcr.io/eshu-hq/eshu-collector-document-import"
	// installedManifestPath is the manifest copy the seed image carries.
	// The seed subcommand copies it into the activation destination with
	// the built image digest stamped in.
	installedManifestPath = "/manifest.yaml"
)

// parseSeedArgs parses `collector seed --dest <dir> --image-ref
// <repo@sha256:…>`. Both flags are required and no positional arguments are
// admitted, so a miswired seed init container fails with the usage instead
// of writing a half-configured destination.
func parseSeedArgs(args []string) (dest string, imageRef string, err error) {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	destFlag := flags.String("dest", "", "destination directory for the collector binary and manifest copy")
	refFlag := flags.String("image-ref", "", "digest-pinned seed image reference (repo@sha256:<64 hex>)")
	if err := flags.Parse(args); err != nil {
		return "", "", fmt.Errorf("collector seed --dest <dir> --image-ref <repo@sha256:…>: %w", err)
	}
	if len(flags.Args()) != 0 {
		return "", "", fmt.Errorf("collector seed --dest <dir> --image-ref <repo@sha256:…>: unexpected arguments %q", flags.Args())
	}
	dest = strings.TrimSpace(*destFlag)
	imageRef = strings.TrimSpace(*refFlag)
	if dest == "" {
		return "", "", fmt.Errorf("collector seed --dest <dir> --image-ref <repo@sha256:…>: --dest is required")
	}
	if imageRef == "" {
		return "", "", fmt.Errorf("collector seed --dest <dir> --image-ref <repo@sha256:…>: --image-ref is required")
	}
	return dest, imageRef, nil
}

// seedComponent copies the caller binary and the installed manifest into
// dest, stamping imageRef into the manifest copy's artifact for platform.
// The reference must name this component's own repository and pass the
// shared component artifact gate before anything is written, so a foreign
// or non-digest ref leaves dest untouched. The manifest edit preserves the
// source document's comments by rewriting only the image scalar; sibling
// platform slots are left byte-identical.
func seedComponent(dest, imageRef, collectorSrc, manifestSrc, platform string) error {
	raw, err := os.ReadFile(manifestSrc)
	if err != nil {
		return fmt.Errorf("read seed manifest %q: %w", manifestSrc, err)
	}
	var manifest component.Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("decode seed manifest %q: %w", manifestSrc, err)
	}
	if manifest.Metadata.ID != componentID {
		return fmt.Errorf("seed manifest %q is component %q, refusing to stamp %q", manifestSrc, manifest.Metadata.ID, componentID)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode seed manifest %q: %w", manifestSrc, err)
	}
	artifact, err := seedArtifactNodeForPlatform(&document, platform)
	if err != nil {
		return fmt.Errorf("seed manifest %q: %w", manifestSrc, err)
	}
	image := seedMappingValue(artifact, "image")
	if image == nil || image.Kind != yaml.ScalarNode {
		return fmt.Errorf("seed manifest %q declares no spec.artifacts image for platform %q to stamp", manifestSrc, platform)
	}
	if repo := seedImageRepoOfRef(imageRef); repo != seedImageRepo {
		return fmt.Errorf("collector seed --image-ref %q: repo %q is not this component's seed repository %q", imageRef, repo, seedImageRepo)
	}
	candidate := component.Artifact{Platform: platform, Image: imageRef}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("collector seed --image-ref %q: %w", imageRef, err)
	}
	image.Value = imageRef
	image.Tag = "!!str"

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create seed destination %q: %w", dest, err)
	}
	if err := copySeedFile(collectorSrc, filepath.Join(dest, "collector"), 0o755); err != nil {
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
	manifestDest := filepath.Join(dest, "manifest.yaml")
	if err := os.WriteFile(manifestDest, rewritten.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write seed manifest %q: %w", manifestDest, err)
	}
	stamped, err := os.ReadFile(manifestDest)
	if err != nil {
		return fmt.Errorf("re-read seed manifest %q: %w", manifestDest, err)
	}
	var verified component.Manifest
	if err := yaml.Unmarshal(stamped, &verified); err != nil {
		return fmt.Errorf("decode stamped manifest %q: %w", manifestDest, err)
	}
	if len(verified.Spec.Artifacts) == 0 {
		return fmt.Errorf("stamped manifest %q declares no artifacts", manifestDest)
	}
	if verified.Metadata.ID != componentID {
		return fmt.Errorf("stamped manifest %q is component %q, want %q", manifestDest, verified.Metadata.ID, componentID)
	}
	matched := false
	for _, entry := range verified.Spec.Artifacts {
		if entry.Platform == platform {
			matched = true
			if err := entry.Validate(); err != nil {
				return fmt.Errorf("stamped manifest %q: %w", manifestDest, err)
			}
			if entry.Image != imageRef {
				return fmt.Errorf("stamped manifest %q image = %q, want %q", manifestDest, entry.Image, imageRef)
			}
			continue
		}
		if want := seedSourceImage(manifest, entry.Platform); entry.Image != want {
			return fmt.Errorf("stamped manifest %q moved sibling platform %q image = %q, want %q", manifestDest, entry.Platform, entry.Image, want)
		}
	}
	if !matched {
		return fmt.Errorf("stamped manifest %q declares no artifact for platform %q", manifestDest, platform)
	}
	return nil
}

// seedImageRepoOfRef returns the repository half of a repo@digest reference,
// or "" when the reference has no digest separator.
func seedImageRepoOfRef(imageRef string) string {
	repo, _, found := strings.Cut(imageRef, "@")
	if !found {
		return ""
	}
	return strings.TrimSpace(repo)
}

// seedSourceImage returns the image the source manifest pins for platform,
// or "" when the source declares none.
func seedSourceImage(manifest component.Manifest, platform string) string {
	for _, entry := range manifest.Spec.Artifacts {
		if entry.Platform == platform {
			return entry.Image
		}
	}
	return ""
}

// seedArtifactNodeForPlatform locates the spec.artifacts entry for platform
// in a decoded manifest document for the seed stamp. Every real reader is
// platform-matched (the host OCI picker, the helm contract test), so a
// positional first-slot stamp would write one arch's digest into another
// arch's slot.
func seedArtifactNodeForPlatform(document *yaml.Node, platform string) (*yaml.Node, error) {
	if document == nil || len(document.Content) == 0 {
		return nil, fmt.Errorf("manifest document is empty")
	}
	root := document.Content[0]
	spec := seedMappingValue(root, "spec")
	if spec == nil {
		return nil, fmt.Errorf("manifest declares no spec to stamp")
	}
	artifacts := seedMappingValue(spec, "artifacts")
	if artifacts == nil || artifacts.Kind != yaml.SequenceNode || len(artifacts.Content) == 0 {
		return nil, fmt.Errorf("manifest declares no spec.artifacts to stamp")
	}
	for _, entry := range artifacts.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		if seedScalar(entry, "platform") == platform {
			return entry, nil
		}
	}
	return nil, fmt.Errorf("manifest declares no spec.artifacts entry for platform %q", platform)
}

// seedMappingValue returns the value for key in a mapping node, or nil.
func seedMappingValue(node *yaml.Node, key string) *yaml.Node {
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

// seedScalar returns the scalar value for key in a mapping node, or "".
func seedScalar(node *yaml.Node, key string) string {
	value := seedMappingValue(node, key)
	if value == nil {
		return ""
	}
	return value.Value
}

// runSeedCommand runs `collector seed --dest <dir> --image-ref
// <repo@sha256:…>` against the running binary and the installed manifest.
// It is the entry the satellite seed init container invokes. The platform
// is the seed runner's own arch, so tests pin each arch deterministically
// by calling seedComponent directly.
func runSeedCommand(args []string) error {
	dest, imageRef, err := parseSeedArgs(args)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("collector seed resolves the caller binary: %w", err)
	}
	return seedComponent(dest, imageRef, self, installedManifestPath, runtime.GOOS+"/"+runtime.GOARCH)
}

// copySeedFile copies one file with the given mode, failing closed when the
// source cannot be read in full.
func copySeedFile(src, dest string, mode os.FileMode) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read seed source %q: %w", src, err)
	}
	if err := os.WriteFile(dest, body, mode); err != nil {
		return fmt.Errorf("write seed destination %q: %w", dest, err)
	}
	return nil
}
