// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

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
	docsVerifyImageTruthMaxFiles     = 2000
	docsVerifyImageTruthMaxFileBytes = 512 * 1024
)

var errDocsVerifyImageTruthLimitReached = errors.New("image truth file limit reached")

func docsVerifyContainerImageResolver(cmd remoteFlagReader, opts docsVerifyOptions) doctruth.ContainerImageResolver {
	if opts.ImageTruth == "api" {
		return docsVerifyContainerImageAPIResolver(apiClientFromRemoteFlags(cmd))
	}
	return docsVerifyLocalContainerImageResolver(opts.Path)
}

func effectiveDocsVerifyImageTruth(cmd remoteFlagReader, mode string) string {
	mode = normalizedDocsVerifyImageTruth(mode)
	if mode == "auto" {
		if docsVerifyRemoteImageTruthConfigured(cmd) {
			return "api"
		}
		return "local"
	}
	return mode
}

func docsVerifyLocalContainerImageResolver(verifyPath string) doctruth.ContainerImageResolver {
	root, ok := docsVerifyTruthRoot(verifyPath)
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
			refs, complete = docsVerifyContainerImageTruth(root)
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

func docsVerifyContainerImageTruth(root string) (map[string]struct{}, bool) {
	refs := map[string]struct{}{}
	files := 0
	complete := true
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			complete = false
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDocsVerifyImageTruthDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isDocsVerifyImageTruthFile(path) {
			return nil
		}
		files++
		if files > docsVerifyImageTruthMaxFiles {
			return errDocsVerifyImageTruthLimitReached
		}
		imageRefs, ok := docsVerifyImageRefsFromFile(path)
		if !ok {
			complete = false
		}
		for _, imageRef := range imageRefs {
			refs[imageRef] = struct{}{}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDocsVerifyImageTruthLimitReached) {
		complete = false
	}
	if errors.Is(err, errDocsVerifyImageTruthLimitReached) {
		complete = false
	}
	return refs, complete
}

func shouldSkipDocsVerifyImageTruthDir(name string) bool {
	switch name {
	case ".git", ".worktrees", "node_modules", "vendor", "dist", "build", "site":
		return true
	default:
		return false
	}
}

func isDocsVerifyImageTruthFile(path string) bool {
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

func docsVerifyImageRefsFromFile(path string) ([]string, bool) {
	file, err := os.Open(path) // #nosec G304 -- path is a local config/manifest file discovered by the program from the scan target directory, not an HTTP request param
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, docsVerifyImageTruthMaxFileBytes+1))
	if err != nil || len(content) > docsVerifyImageTruthMaxFileBytes {
		return nil, false
	}
	return doctruth.ContainerImageRefsFromText(string(content)), true
}
