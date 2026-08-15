// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

const (
	// imageTruthMaxFiles bounds how many manifest files one scan reads.
	imageTruthMaxFiles = 2000
	// imageTruthMaxFileBytes bounds how much of one manifest is read. A file
	// past the bound is not partially parsed -- it marks the scan incomplete.
	imageTruthMaxFileBytes = 512 * 1024
)

// errImageTruthLimitReached stops the manifest walk at imageTruthMaxFiles. It
// marks the scan incomplete rather than failing the run.
var errImageTruthLimitReached = errors.New("image truth file limit reached")

// LocalContainerImageResolver builds the resolver that checks a documented
// container image reference against the image references written in the
// workspace's own manifests. It returns nil when no workspace root resolves.
//
// The scan is lazy and runs at most once per resolver: the first claim triggers
// it, later claims reuse the result. When the scan is incomplete (file limit
// hit, an oversized manifest, an unreadable file) an unmatched reference
// reports unsupported rather than contradicted, so a bounded scan never
// manufactures a false contradiction.
func LocalContainerImageResolver(verifyPath string) doctruth.ContainerImageResolver {
	root, ok := TruthRoot(verifyPath)
	if !ok {
		return nil
	}
	var once sync.Once
	var refs map[string]struct{}
	var complete bool
	return func(_ doctruth.DocumentInput, imageRef string) doctruth.ContainerImageResolution {
		normalized := doctruth.NormalizeContainerImageRefClaim(imageRef)
		if normalized == "" {
			return doctruth.ContainerImageResolution{}
		}
		once.Do(func() {
			refs, complete = containerImageTruth(root)
		})
		if _, ok := refs[normalized]; ok {
			return doctruth.ContainerImageResolution{Supported: true, Exists: true}
		}
		if !complete {
			return doctruth.ContainerImageResolution{}
		}
		return doctruth.ContainerImageResolution{Supported: true, Exists: false}
	}
}

// containerImageTruth walks root collecting every container image reference
// found in Dockerfiles and YAML/JSON/TOML manifests. The second return reports
// whether the scan saw everything it wanted to; false means an unmatched
// reference must not be called contradicted.
func containerImageTruth(root string) (map[string]struct{}, bool) {
	refs := map[string]struct{}{}
	files := 0
	complete := true
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			complete = false
			return nil
		}
		if entry.IsDir() {
			if shouldSkipImageTruthDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isImageTruthFile(path) {
			return nil
		}
		files++
		if files > imageTruthMaxFiles {
			return errImageTruthLimitReached
		}
		imageRefs, ok := imageRefsFromFile(path)
		if !ok {
			complete = false
		}
		for _, imageRef := range imageRefs {
			refs[imageRef] = struct{}{}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errImageTruthLimitReached) {
		complete = false
	}
	if errors.Is(err, errImageTruthLimitReached) {
		complete = false
	}
	return refs, complete
}

// shouldSkipImageTruthDir reports the directories the manifest scan does not
// descend into: version control, sibling worktrees, and dependency or build
// output trees whose images are not this workspace's truth.
func shouldSkipImageTruthDir(name string) bool {
	switch name {
	case ".git", ".worktrees", "node_modules", "vendor", "dist", "build", "site":
		return true
	default:
		return false
	}
}

// isImageTruthFile reports whether path is a file kind that can carry a
// container image reference: a Dockerfile, or a YAML/JSON/TOML manifest.
func isImageTruthFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if base == "dockerfile" || strings.HasSuffix(base, ".dockerfile") {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json", ".toml":
		return true
	default:
		return false
	}
}

// imageRefsFromFile reads one manifest and extracts its image references. The
// second return is false when the file could not be opened or exceeds
// imageTruthMaxFileBytes; the caller turns that into an incomplete scan.
func imageRefsFromFile(path string) ([]string, bool) {
	file, err := os.Open(path) // #nosec G304 -- path is a local config/manifest file discovered by the program from the scan target directory, not an HTTP request param
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, imageTruthMaxFileBytes+1))
	if err != nil || len(content) > imageTruthMaxFileBytes {
		return nil, false
	}
	return doctruth.ContainerImageRefsFromText(string(content)), true
}
