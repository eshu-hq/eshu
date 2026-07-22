// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// defaultIgnoredDirs lists directories that are always pruned during discovery.
// This matches the Python-era default exclusion list plus additional coverage
// for every language the parser registry supports.
var defaultIgnoredDirs = []string{
	// VCS
	".git",
	".svn",
	".hg",
	// Eshu repo-local configuration is operator metadata, not source input.
	".eshu",
	// Infrastructure / IaC caches
	".terraform",
	".terragrunt-cache",
	".tox",
	".mypy_cache",
	".pytest_cache",
	".aws-sam",
	"cdk.out",
	".serverless",
	// JavaScript / TypeScript
	"node_modules",
	"bower_components",
	"jspm_packages",
	// Yarn Berry stores package-manager bundles and caches under .yarn.
	// These generated artifacts can dwarf real application source.
	".yarn",
	".next",
	".nuxt",
	// Python
	"site-packages",
	"dist-packages",
	"__pypackages__",
	"__pycache__",
	".venv",
	"venv",
	".eggs",
	// PHP
	"vendor",
	"wp-admin",
	"wp-includes",
	// Go
	// (vendor already listed under PHP)
	// Ruby
	"bundle",
	// Elixir
	"deps",
	"_build",
	// Swift ecosystem
	"Pods",
	".build",
	"Carthage",
	// Java / Kotlin / Scala / Groovy
	".gradle",
	".m2",
	".ivy2",
	// Rust
	// (target already listed below under build output)
	// Haskell
	".stack-work",
	".cabal-sandbox",
	"dist-newstyle",
	// Dart
	".dart_tool",
	".pub-cache",
	// Perl
	"blib",
	"local",
	// C / C#
	"obj",
	"bin",
	// Ansible
	".ansible",
	"ansible_collections",
	// Jenkins
	".jenkins",
	// Common build and distribution output
	"dist",
	"build",
	"target",
	"out",
	// Coverage and test output
	"coverage",
	".nyc_output",
	"htmlcov",
}

// defaultIgnoredExtensions lists file suffixes that are always skipped during
// discovery. These cover log/output artifacts, minified/bundled assets, and
// other non-source files that should never be parsed.
var defaultIgnoredExtensions = []string{
	// Logs and output
	".log",
	".out",
	// Minified and bundled assets
	".min.js",
	".min.mjs",
	".min.css",
	".bundle.js",
	".chunk.js",
	".min.map",
	// Source maps
	".map",
	// Yarn Berry Plug'n'Play loader files are generated dependency metadata.
	".pnp.cjs",
	".pnp.loader.mjs",
	// Compiled / binary artifacts commonly checked in
	".pyc",
	".pyo",
	".class",
	".dll",
	".so",
	".dylib",
	".exe",
	".o",
	".a",
	".wasm",
}

func (s NativeRepositorySnapshotter) discoveryOptions() discovery.Options {
	opts := defaultNativeDiscoveryOptions()
	opts.IgnoredDirs = append(opts.IgnoredDirs, s.DiscoveryOptions.IgnoredDirs...)
	opts.IgnoredExtensions = append(opts.IgnoredExtensions, s.DiscoveryOptions.IgnoredExtensions...)
	opts.IgnoreHidden = s.DiscoveryOptions.IgnoreHidden
	opts.PreservedHiddenPrefixes = append(opts.PreservedHiddenPrefixes, s.DiscoveryOptions.PreservedHiddenPrefixes...)
	opts.HonorGitignore = true
	opts.HonorEshuIgnore = true
	opts.IgnoredPathGlobs = append(opts.IgnoredPathGlobs, s.DiscoveryOptions.IgnoredPathGlobs...)
	opts.PreservedPathGlobs = append(opts.PreservedPathGlobs, s.DiscoveryOptions.PreservedPathGlobs...)
	return opts
}

func defaultNativeDiscoveryOptions() discovery.Options {
	return discovery.Options{
		IgnoredDirs:       defaultIgnoredDirs,
		IgnoredExtensions: defaultIgnoredExtensions,
		HonorGitignore:    true,
		HonorEshuIgnore:   true,
	}
}

func (s NativeRepositorySnapshotter) logDiscoveryStats(ctx context.Context, repoPath string, stats discovery.DiscoveryStats) {
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}

	attrs := []any{
		log.RepoPath(filepath.Base(repoPath)),
		slog.Int("dirs_skipped_total", stats.TotalDirsSkipped()),
		slog.Int("files_skipped_total", stats.TotalFilesSkipped()),
	}

	// Log per-directory-name breakdown for operator tuning visibility.
	for name, count := range stats.DirsSkippedByName {
		attrs = append(attrs, slog.Int("dirs_skipped."+name, count))
	}
	for ext, count := range stats.FilesSkippedByExtension {
		attrs = append(attrs, slog.Int("files_skipped.ext"+ext, count))
	}
	for reason, count := range stats.FilesSkippedByContent {
		attrs = append(attrs, slog.Int("files_skipped.content."+reason, count))
	}
	for reason, count := range stats.DirsSkippedByUser {
		attrs = append(attrs, slog.Int("dirs_skipped.user."+reason, count))
	}
	for reason, count := range stats.FilesSkippedByUser {
		attrs = append(attrs, slog.Int("files_skipped.user."+reason, count))
	}
	if stats.FilesSkippedHidden > 0 {
		attrs = append(attrs, slog.Int("files_skipped.hidden", stats.FilesSkippedHidden))
	}
	if stats.FilesSkippedGitignore > 0 {
		attrs = append(attrs, slog.Int("files_skipped.gitignore", stats.FilesSkippedGitignore))
	}
	if stats.FilesSkippedEshuIgnore > 0 {
		attrs = append(attrs, slog.Int("files_skipped.eshuignore", stats.FilesSkippedEshuIgnore))
	}

	logger.InfoContext(ctx, "discovery stats", attrs...)
}

func (s NativeRepositorySnapshotter) recordDiscoveryMetrics(ctx context.Context, stats discovery.DiscoveryStats) {
	if s.Instruments == nil {
		return
	}

	for name, count := range stats.DirsSkippedByName {
		s.Instruments.DiscoveryDirsSkipped.Add(
			ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason(name)),
		)
	}
	for ext, count := range stats.FilesSkippedByExtension {
		s.Instruments.DiscoveryFilesSkipped.Add(
			ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason("ext:"+ext)),
		)
	}
	for reason, count := range stats.FilesSkippedByContent {
		s.Instruments.DiscoveryFilesSkipped.Add(
			ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason("content:"+reason)),
		)
	}
	for reason, count := range stats.DirsSkippedByUser {
		s.Instruments.DiscoveryDirsSkipped.Add(
			ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason("user:"+reason)),
		)
	}
	for reason, count := range stats.FilesSkippedByUser {
		s.Instruments.DiscoveryFilesSkipped.Add(
			ctx, int64(count),
			metric.WithAttributes(telemetry.AttrSkipReason("user:"+reason)),
		)
	}
	if stats.FilesSkippedHidden > 0 {
		s.Instruments.DiscoveryFilesSkipped.Add(
			ctx, int64(stats.FilesSkippedHidden),
			metric.WithAttributes(telemetry.AttrSkipReason("hidden")),
		)
	}
	if stats.FilesSkippedGitignore > 0 {
		s.Instruments.DiscoveryFilesSkipped.Add(
			ctx, int64(stats.FilesSkippedGitignore),
			metric.WithAttributes(telemetry.AttrSkipReason("gitignore")),
		)
	}
	if stats.FilesSkippedEshuIgnore > 0 {
		s.Instruments.DiscoveryFilesSkipped.Add(
			ctx, int64(stats.FilesSkippedEshuIgnore),
			metric.WithAttributes(telemetry.AttrSkipReason("eshuignore")),
		)
	}
}

func (s NativeRepositorySnapshotter) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func resolveNativeSnapshotFileSet(
	repoPath string,
	registry parser.Registry,
	opts discovery.Options,
) (discovery.RepoFileSet, discovery.DiscoveryStats, error) {
	opts, err := discoveryOptionsWithRepoDiscoveryConfig(repoPath, opts)
	if err != nil {
		return discovery.RepoFileSet{}, discovery.DiscoveryStats{}, err
	}
	stats, fileSets, err := discovery.ResolveRepositoryFileSetsWithStats(
		repoPath,
		func(path string) bool {
			if isTerraformStateCandidateName(filepath.Base(path)) {
				return true
			}
			_, ok := registry.LookupByPath(path)
			if ok || isGitDocumentationPath(path) {
				return true
			}
			return isGitmodulesCandidatePath(repoPath, path) || isCodeownersCandidatePath(repoPath, path)
		},
		opts,
	)
	if err != nil {
		return discovery.RepoFileSet{}, stats, fmt.Errorf("resolve repository file sets: %w", err)
	}
	for _, fileSet := range fileSets {
		if fileSet.RepoRoot == repoPath {
			fileSet.Files = filterGeneratedNativeSnapshotFiles(fileSet.Files, &stats)
			return fileSet, stats, nil
		}
	}
	return discovery.RepoFileSet{RepoRoot: repoPath}, stats, nil
}
