// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/metrics"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
)

const (
	osReleasePath     = "etc/os-release"
	apkReposPath      = "etc/apk/repositories"
	apkInstalledPath  = "lib/apk/db/installed"
	dpkgStatusPath    = "var/lib/dpkg/status"
	whiteoutPrefix    = ".wh."
	opaqueWhiteout    = ".wh..wh..opq"
	layerRegularFiles = int64(4)
)

type layerSnapshotReader struct {
	remainingBytes  int64
	maxFiles        int64
	filesSeen       int64
	peakBytes       int64
	startCPUSeconds float64
	files           map[string][]byte
	deleted         map[string]struct{}
	opaqueDirs      map[string]struct{}
}

func readLayerSnapshot(
	ctx context.Context,
	target TargetConfig,
	limits scannerworker.ResourceLimits,
	now func() time.Time,
) (Snapshot, scannerworker.ResourceUsage, error) {
	reader := &layerSnapshotReader{
		remainingBytes:  limits.MaxInputBytes,
		maxFiles:        limits.MaxFiles,
		startCPUSeconds: currentUserCPUSeconds(),
		files:           make(map[string][]byte, layerRegularFiles),
		deleted:         make(map[string]struct{}),
		opaqueDirs:      make(map[string]struct{}),
	}
	for _, layerPath := range target.LayerPaths {
		if err := reader.readLayer(ctx, layerPath); err != nil {
			if errors.Is(err, errUnsupportedTarget) {
				return unsupportedLayerSnapshot(target, now, extractionMalformedImage), reader.usage(), err
			}
			return Snapshot{}, reader.usage(), err
		}
	}
	osRelease := reader.files[osReleasePath]
	fields := parseOSRelease(osRelease)
	snapshot := Snapshot{
		Distro:             firstNonBlankDistro(target.Distro, ospackagevulnerability.Distro(strings.TrimSpace(fields["ID"]))),
		DistroVersion:      firstNonBlank(target.DistroVersion, fields["VERSION_ID"], fields["VERSION"]),
		PackageManager:     target.PackageManager,
		Repositories:       target.Repositories,
		ThirdPartyPackages: target.ThirdPartyPackages,
		SourceURI:          target.SourceURI,
		SourceRecordID:     target.SourceRecordID,
		ImageReference:     target.ImageReference,
		ImageDigest:        target.ImageDigest,
		EvidenceSource:     EvidenceSourceLayer,
		ExtractionReason:   extractionUnsupported,
		ObservedAt:         now().UTC(),
	}
	distro, manager, err := resolvePackageDatabase(target, fields, reader.files)
	if err != nil {
		return snapshot, reader.usage(), err
	}
	snapshot.Distro = distro
	snapshot.PackageManager = manager
	snapshot.ExtractionReason = extractionOK
	switch manager {
	case ospackagevulnerability.PackageManagerAPK:
		snapshot.Repositories = alpineRepositories(target.Repositories, reader.files[apkReposPath])
		snapshot.InstalledDB = reader.files[apkInstalledPath]
	case ospackagevulnerability.PackageManagerDPKG:
		snapshot.Status = reader.files[dpkgStatusPath]
	default:
		return Snapshot{}, reader.usage(), fmt.Errorf("%w: unsupported package manager", errUnsupportedTarget)
	}
	return snapshot, reader.usage(), nil
}

func unsupportedLayerSnapshot(target TargetConfig, now func() time.Time, reason string) Snapshot {
	return Snapshot{
		Distro:             target.Distro,
		DistroVersion:      target.DistroVersion,
		PackageManager:     target.PackageManager,
		Repositories:       target.Repositories,
		ThirdPartyPackages: target.ThirdPartyPackages,
		SourceURI:          target.SourceURI,
		SourceRecordID:     target.SourceRecordID,
		ImageReference:     target.ImageReference,
		ImageDigest:        target.ImageDigest,
		EvidenceSource:     EvidenceSourceLayer,
		ExtractionReason:   reason,
		ObservedAt:         now().UTC(),
	}
}

func firstNonBlankDistro(values ...ospackagevulnerability.Distro) ospackagevulnerability.Distro {
	for _, value := range values {
		if strings.TrimSpace(string(value)) != "" {
			return value
		}
	}
	return ""
}

func (r *layerSnapshotReader) readLayer(ctx context.Context, layerPath string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: context canceled", errSourceUnavailable)
	}
	file, err := os.Open(layerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: layer unavailable", errTargetUnavailable)
		}
		return fmt.Errorf("%w: open layer", errTargetUnavailable)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: stat layer", errTargetUnavailable)
	}
	if info.IsDir() {
		return fmt.Errorf("%w: layer path is directory", errUnsupportedTarget)
	}
	if info.Size() > r.remainingBytes {
		return fmt.Errorf("%w: layer byte budget exceeded", errInputLimitExceeded)
	}
	r.remainingBytes -= info.Size()
	if info.Size() > r.peakBytes {
		r.peakBytes = info.Size()
	}
	tarReader, closeFn, err := newLayerTarReader(file)
	if err != nil {
		return fmt.Errorf("%w: unsupported layer archive", errUnsupportedTarget)
	}
	defer closeFn()
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: read layer archive", errUnsupportedTarget)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: context canceled", errSourceUnavailable)
		}
		if err := r.handleEntry(header, tarReader); err != nil {
			return err
		}
	}
}

func newLayerTarReader(file *os.File) (*tar.Reader, func(), error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, func() {}, err
	}
	gzipReader, err := gzip.NewReader(file)
	if err == nil {
		return tar.NewReader(gzipReader), func() { _ = gzipReader.Close() }, nil
	}
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return nil, func() {}, seekErr
	}
	return tar.NewReader(file), func() {}, nil
}

func (r *layerSnapshotReader) handleEntry(header *tar.Header, body io.Reader) error {
	name := cleanLayerName(header.Name)
	if name == "" {
		return nil
	}
	if base := filepath.Base(name); strings.HasPrefix(base, whiteoutPrefix) {
		r.applyWhiteout(name)
		return nil
	}
	if header.Typeflag != tar.TypeReg {
		return nil
	}
	if r.filesSeen >= r.maxFiles {
		return fmt.Errorf("%w: layer file budget exceeded", errFileLimitExceeded)
	}
	r.filesSeen++
	if !isPackageEvidencePath(name) {
		return nil
	}
	if header.Size > r.remainingBytes {
		return fmt.Errorf("%w: layer metadata byte budget exceeded", errInputLimitExceeded)
	}
	content, err := io.ReadAll(io.LimitReader(body, header.Size+1))
	if err != nil {
		return fmt.Errorf("%w: read layer metadata", errTargetUnavailable)
	}
	if int64(len(content)) != header.Size {
		return fmt.Errorf("%w: truncated layer metadata", errTargetUnavailable)
	}
	r.remainingBytes -= int64(len(content))
	if int64(len(content)) > r.peakBytes {
		r.peakBytes = int64(len(content))
	}
	r.files[name] = content
	delete(r.deleted, name)
	return nil
}

func (r *layerSnapshotReader) applyWhiteout(name string) {
	dir := filepath.Dir(name)
	base := filepath.Base(name)
	if base == opaqueWhiteout {
		cleanDir := filepath.ToSlash(filepath.Clean(dir))
		r.opaqueDirs[cleanDir] = struct{}{}
		for existing := range r.files {
			if filepath.Dir(existing) == cleanDir {
				delete(r.files, existing)
			}
		}
		return
	}
	target := filepath.ToSlash(filepath.Join(dir, strings.TrimPrefix(base, whiteoutPrefix)))
	delete(r.files, target)
	r.deleted[target] = struct{}{}
}

func (r layerSnapshotReader) usage() scannerworker.ResourceUsage {
	cpuSeconds := currentUserCPUSeconds() - r.startCPUSeconds
	if cpuSeconds < 0 {
		cpuSeconds = 0
	}
	return scannerworker.ResourceUsage{CPUSeconds: cpuSeconds, PeakMemoryBytes: r.peakBytes}
}

func cleanLayerName(name string) string {
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(strings.TrimSpace(name), "/")))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return ""
	}
	return cleaned
}

func isPackageEvidencePath(name string) bool {
	switch name {
	case osReleasePath, apkReposPath, apkInstalledPath, dpkgStatusPath:
		return true
	default:
		return false
	}
}

func resolvePackageDatabase(
	target TargetConfig,
	fields map[string]string,
	files map[string][]byte,
) (ospackagevulnerability.Distro, ospackagevulnerability.PackageManager, error) {
	distro := target.Distro
	if distro == "" {
		distro = ospackagevulnerability.Distro(strings.TrimSpace(fields["ID"]))
	}
	manager := target.PackageManager
	if manager == "" {
		switch {
		case len(files[apkInstalledPath]) > 0:
			manager = ospackagevulnerability.PackageManagerAPK
		case len(files[dpkgStatusPath]) > 0:
			manager = ospackagevulnerability.PackageManagerDPKG
		case distro == ospackagevulnerability.DistroAlpine:
			manager = ospackagevulnerability.PackageManagerAPK
		case distro == ospackagevulnerability.DistroDebian:
			manager = ospackagevulnerability.PackageManagerDPKG
		}
	}
	if err := validateDistro(distro); err != nil {
		return "", "", err
	}
	if err := validatePackageManager(manager); err != nil {
		return "", "", err
	}
	switch manager {
	case ospackagevulnerability.PackageManagerAPK:
		if distro != ospackagevulnerability.DistroAlpine || len(files[apkInstalledPath]) == 0 {
			return "", "", fmt.Errorf("%w: missing apk installed database", errUnsupportedTarget)
		}
	case ospackagevulnerability.PackageManagerDPKG:
		if distro != ospackagevulnerability.DistroDebian || len(files[dpkgStatusPath]) == 0 {
			return "", "", fmt.Errorf("%w: missing dpkg status database", errUnsupportedTarget)
		}
	default:
		return "", "", fmt.Errorf("%w: missing supported package database", errUnsupportedTarget)
	}
	return distro, manager, nil
}

func alpineRepositories(configured []ospackagevulnerability.Repository, body []byte) []ospackagevulnerability.Repository {
	if len(configured) > 0 {
		return configured
	}
	var repos []ospackagevulnerability.Repository
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		repos = append(repos, ospackagevulnerability.Repository{
			URL:   trimmed,
			Class: ospackagevulnerability.ClassifyAlpineRepositoryURL(trimmed),
		})
	}
	return repos
}

func parseOSRelease(body []byte) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			fields[key] = value
		}
	}
	return fields
}

func currentUserCPUSeconds() float64 {
	samples := []metrics.Sample{{Name: "/cpu/classes/user:cpu-seconds"}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindFloat64 {
		return 0
	}
	return samples[0].Value.Float64()
}
