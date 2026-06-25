// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
)

type rootFSUnsupportedReader struct {
	root           string
	remainingBytes int64
	peakBytes      int64
}

func readRootFSUnsupportedSnapshot(
	ctx context.Context,
	target TargetConfig,
	limits scannerworker.ResourceLimits,
	now func() time.Time,
) (Snapshot, scannerworker.ResourceUsage, bool) {
	reader := rootFSUnsupportedReader{
		root:           target.RootFSPath,
		remainingBytes: limits.MaxInputBytes,
	}
	osRelease, ok, err := reader.readOptional(ctx, osReleasePath)
	if err != nil || !ok {
		return Snapshot{}, reader.usage(), false
	}
	fields := parseOSRelease(osRelease)
	distro := firstNonBlankDistro(target.Distro, ospackagevulnerability.Distro(strings.TrimSpace(fields["ID"])))
	manager := target.PackageManager
	if manager == "" {
		switch distro {
		case ospackagevulnerability.DistroAlpine:
			manager = ospackagevulnerability.PackageManagerAPK
		case ospackagevulnerability.DistroDebian:
			manager = ospackagevulnerability.PackageManagerDPKG
		}
	}
	if distro == "" && manager == "" {
		return Snapshot{}, reader.usage(), false
	}
	return Snapshot{
		Distro:             distro,
		DistroVersion:      firstNonBlank(target.DistroVersion, fields["VERSION_ID"], fields["VERSION"]),
		PackageManager:     manager,
		Repositories:       target.Repositories,
		ThirdPartyPackages: target.ThirdPartyPackages,
		SourceURI:          target.SourceURI,
		SourceRecordID:     target.SourceRecordID,
		ImageReference:     target.ImageReference,
		ImageDigest:        target.ImageDigest,
		EvidenceSource:     EvidenceSourceRootFS,
		ExtractionReason:   extractionUnsupported,
		ObservedAt:         now().UTC(),
	}, reader.usage(), true
}

func (r *rootFSUnsupportedReader) readOptional(
	ctx context.Context,
	rel string,
) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	path, err := safeRootFSMetadataPath(r.root, rel)
	if err != nil {
		return nil, false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if info.IsDir() {
		return nil, false, fmt.Errorf("%w: rootfs metadata is directory", errUnsupportedTarget)
	}
	if info.Size() > r.remainingBytes {
		return nil, false, fmt.Errorf("%w: rootfs metadata byte budget exceeded", errInputLimitExceeded)
	}
	body, err := os.ReadFile(path) // #nosec G304 -- reads rootfs metadata file at a path constructed from the scanner-controlled rootfs extraction directory
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > r.remainingBytes {
		return nil, false, fmt.Errorf("%w: rootfs metadata byte budget exceeded", errInputLimitExceeded)
	}
	r.remainingBytes -= int64(len(body))
	if int64(len(body)) > r.peakBytes {
		r.peakBytes = int64(len(body))
	}
	return body, true, nil
}

func (r rootFSUnsupportedReader) usage() scannerworker.ResourceUsage {
	return scannerworker.ResourceUsage{PeakMemoryBytes: r.peakBytes}
}

func safeRootFSMetadataPath(root string, rel string) (string, error) {
	cleanRoot := filepath.Clean(root)
	if cleanRoot == "." || cleanRoot == "" {
		return "", fmt.Errorf("%w: rootfs path is required", errUnsupportedTarget)
	}
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(cleanRel) || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: unsafe rootfs relative path", errUnsupportedTarget)
	}
	absRoot, err := filepath.Abs(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("%w: resolve rootfs path", errUnsupportedTarget)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	current := realRoot
	parts := strings.Split(cleanRel, string(filepath.Separator))
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				candidate := filepath.Join(realRoot, cleanRel)
				if !metadataPathWithinRoot(realRoot, candidate) {
					return "", fmt.Errorf("%w: rootfs path escaped root", errUnsupportedTarget)
				}
				return candidate, nil
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: rootfs path crosses symlink", errUnsupportedTarget)
		}
		current = next
	}
	if !metadataPathWithinRoot(realRoot, current) {
		return "", fmt.Errorf("%w: rootfs path escaped root", errUnsupportedTarget)
	}
	return current, nil
}

func metadataPathWithinRoot(root string, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
