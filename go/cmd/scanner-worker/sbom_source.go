// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/metrics"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
)

const repositorySBOMSourceTool = "eshu_repository_manifest_sbom 0.1.0"

var errNoSBOMTargets = errors.New("no SBOM generation targets configured")

type sbomTargetConfig struct {
	ScopeID       string `json:"scope_id"`
	RootPath      string `json:"root_path"`
	SubjectDigest string `json:"subject_digest"`
	SourceTool    string `json:"source_tool"`
}

type repositorySBOMSource struct {
	targets map[string]sbomTargetConfig
}

func newRepositorySBOMSource(targets []sbomTargetConfig) (*repositorySBOMSource, error) {
	if len(targets) == 0 {
		return nil, errNoSBOMTargets
	}
	byScope := make(map[string]sbomTargetConfig, len(targets))
	for i, target := range targets {
		validated, err := target.validate()
		if err != nil {
			return nil, fmt.Errorf("sbom target %d: %w", i, err)
		}
		if _, exists := byScope[validated.ScopeID]; exists {
			return nil, fmt.Errorf("duplicate SBOM target scope_id")
		}
		byScope[validated.ScopeID] = validated
	}
	return &repositorySBOMSource{targets: byScope}, nil
}

func (s *repositorySBOMSource) Collect(
	ctx context.Context,
	input scannerworker.ClaimInput,
) (sbomgenerator.Inventory, error) {
	target, ok := s.targets[strings.TrimSpace(input.Target.ScopeID)]
	if !ok {
		return sbomgenerator.Inventory{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassUnsupportedTarget,
			scannerworker.ResourceUsage{},
			sbomgenerator.ErrUnsupportedTarget,
		)
	}
	reader := repositoryManifestReader{
		root:            target.RootPath,
		remainingBytes:  input.Limits.MaxInputBytes,
		maxFiles:        input.Limits.MaxFiles,
		startCPUSeconds: currentScannerCPUSeconds(),
	}
	components, warnings, err := reader.collect(ctx)
	usage := reader.usage()
	if err != nil {
		return sbomgenerator.Inventory{}, err
	}
	tool := strings.TrimSpace(target.SourceTool)
	if tool == "" {
		tool = repositorySBOMSourceTool
	}
	return sbomgenerator.Inventory{
		SubjectDigest: target.SubjectDigest,
		SourceTool:    tool,
		FileCount:     reader.filesSeen,
		InputBytes:    reader.inputBytes,
		Components:    components,
		Warnings:      warnings,
		ResourceUsage: usage,
	}, nil
}

func (t sbomTargetConfig) validate() (sbomTargetConfig, error) {
	t.ScopeID = strings.TrimSpace(t.ScopeID)
	t.RootPath = strings.TrimSpace(t.RootPath)
	t.SubjectDigest = strings.TrimSpace(t.SubjectDigest)
	t.SourceTool = strings.TrimSpace(t.SourceTool)
	if t.ScopeID == "" {
		return sbomTargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if t.RootPath == "" {
		return sbomTargetConfig{}, fmt.Errorf("root_path is required")
	}
	return t, nil
}

type repositoryManifestReader struct {
	root            string
	remainingBytes  int64
	maxFiles        int64
	filesSeen       int64
	inputBytes      int64
	peakBytes       int64
	startCPUSeconds float64
}

func (r *repositoryManifestReader) collect(ctx context.Context) ([]sbomgenerator.Component, []sbomgenerator.Warning, error) {
	root, err := secureRepositoryRoot(r.root)
	if err != nil {
		return nil, nil, scannerworker.NewRetryableAnalyzerFailure(
			scannerworker.FailureClassTargetUnavailable,
			r.usage(),
			err,
		)
	}
	components := make([]sbomgenerator.Component, 0)
	warnings := make([]sbomgenerator.Warning, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return scannerworker.NewRetryableAnalyzerFailure(
				scannerworker.FailureClassTargetUnavailable,
				r.usage(),
				walkErr,
			)
		}
		if err := ctx.Err(); err != nil {
			return scannerworker.NewRetryableAnalyzerFailure(
				scannerworker.FailureClassSourceUnavailable,
				r.usage(),
				err,
			)
		}
		if entry.IsDir() {
			if shouldSkipRepositoryDir(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		if !isSupportedManifestName(entry.Name()) {
			return nil
		}
		r.filesSeen++
		if r.filesSeen > r.maxFiles {
			return scannerworker.NewTerminalAnalyzerFailure(
				scannerworker.FailureClassFileLimitExceeded,
				r.usage(),
				nil,
			)
		}
		body, err := r.readManifest(path)
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			relativePath = entry.Name()
		}
		relativePath = filepath.ToSlash(relativePath)
		parsed, parsedWarnings := parseRepositoryManifest(relativePath, entry.Name(), body)
		components = append(components, parsed...)
		warnings = append(warnings, parsedWarnings...)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return components, warnings, nil
}

func (r *repositoryManifestReader) readManifest(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, scannerworker.NewRetryableAnalyzerFailure(
			scannerworker.FailureClassTargetUnavailable,
			r.usage(),
			err,
		)
	}
	if info.Size() > r.remainingBytes {
		return nil, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassInputLimitExceeded,
			r.usage(),
			nil,
		)
	}
	body, err := os.ReadFile(path) // #nosec G304 -- path is produced by filepath.WalkDir over an indexed repository working tree, not from untrusted external input
	if err != nil {
		return nil, scannerworker.NewRetryableAnalyzerFailure(
			scannerworker.FailureClassTargetUnavailable,
			r.usage(),
			err,
		)
	}
	if int64(len(body)) > r.remainingBytes {
		return nil, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassInputLimitExceeded,
			r.usage(),
			nil,
		)
	}
	r.remainingBytes -= int64(len(body))
	r.inputBytes += int64(len(body))
	if int64(len(body)) > r.peakBytes {
		r.peakBytes = int64(len(body))
	}
	return body, nil
}

func (r repositoryManifestReader) usage() scannerworker.ResourceUsage {
	cpuSeconds := currentScannerCPUSeconds() - r.startCPUSeconds
	if cpuSeconds < 0 {
		cpuSeconds = 0
	}
	return scannerworker.ResourceUsage{
		CPUSeconds:      cpuSeconds,
		PeakMemoryBytes: r.peakBytes,
	}
}

func secureRepositoryRoot(root string) (string, error) {
	cleanRoot := filepath.Clean(root)
	if cleanRoot == "." || cleanRoot == "" {
		return "", fmt.Errorf("repository root path is required")
	}
	absRoot, err := filepath.Abs(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root")
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root")
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return "", fmt.Errorf("stat repository root")
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root is not a directory")
	}
	return realRoot, nil
}

func shouldSkipRepositoryDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".terraform", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func currentScannerCPUSeconds() float64 {
	samples := []metrics.Sample{{Name: "/cpu/classes/user:cpu-seconds"}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindFloat64 {
		return 0
	}
	return samples[0].Value.Float64()
}
