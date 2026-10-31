// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/metrics"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/distribution"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/imageanalyzer"
)

const (
	registryStagingBufferBytes int64 = 64 << 10
	imageManifestMediaType           = "application/vnd.oci.image.manifest.v1+json"
	dockerManifestMediaType          = "application/vnd.docker.distribution.manifest.v2+json"
	imageConfigMediaType             = "application/vnd.oci.image.config.v1+json"
	dockerConfigMediaType            = "application/vnd.docker.container.image.v1+json"
	imageGzipLayerMediaType          = "application/vnd.oci.image.layer.v1.tar+gzip"
	dockerGzipLayerMediaType         = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	imageTarLayerMediaType           = "application/vnd.oci.image.layer.v1.tar"
)

var (
	errExpandedLayerLimit   = errors.New("expanded image layer limit exceeded")
	errExpandedLayerStaging = errors.New("expanded image layer staging failed")
)

// registryImageResolver resolves one fenced claim to durable target metadata
// and revalidated registry authority. It must not return a stale or unauthorized
// target. The client carries credentials in memory only.
type registryImageResolver interface {
	ResolveImage(context.Context, scannerworker.ClaimInput) (registryImageTarget, error)
}

type registryImageTarget struct {
	ScanTargetID      string
	Client            *distribution.Client
	Repository        string
	ManifestDigest    string
	RuntimeDigest     string
	RuntimeDigestKind string
}

type registryImageAnalyzer struct {
	resolver            registryImageResolver
	collectorInstanceID string
}

type imageManifestDescriptors struct {
	Config imageLayerDescriptor
	Layers []imageLayerDescriptor
}

type imageLayerDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

func (a registryImageAnalyzer) Analyze(ctx context.Context, input scannerworker.ClaimInput) (result scannerworker.AnalyzerResult, resultErr error) {
	startCPU := registryUserCPUSeconds()
	var trackedStageBytes int64
	defer func() {
		usage := result.Usage
		var failure scannerworker.AnalyzerFailure
		if errors.As(resultErr, &failure) {
			usage = failure.ResourceUsage()
		}
		if cpu := registryUserCPUSeconds() - startCPU; cpu > usage.CPUSeconds {
			usage.CPUSeconds = cpu
		}
		usage.PeakMemoryBytes += trackedStageBytes
		if resultErr == nil {
			result.Usage = usage
		} else if errors.As(resultErr, &failure) {
			if failure.Retryable() {
				resultErr = scannerworker.NewRetryableAnalyzerFailure(failure.FailureClass(), usage, resultErr)
			} else {
				resultErr = scannerworker.NewTerminalAnalyzerFailure(failure.FailureClass(), usage, resultErr)
			}
		}
	}()
	if input.Target.Kind != scannerworker.TargetImage {
		return scannerworker.AnalyzerResult{}, registryImageTerminal("registry image target kind invalid")
	}
	if a.resolver == nil || strings.TrimSpace(a.collectorInstanceID) == "" {
		return scannerworker.AnalyzerResult{}, scannerworker.NewTerminalAnalyzerFailure(
			scannerworker.FailureClassAnalyzerFailed, scannerworker.ResourceUsage{}, errors.New("registry image resolver unavailable"))
	}
	target, err := a.resolver.ResolveImage(ctx, input)
	if err != nil {
		if errors.Is(err, errRegistryImageUnauthorized) {
			return scannerworker.AnalyzerResult{}, registryImageTerminal("registry image target unauthorized")
		}
		return scannerworker.AnalyzerResult{}, registryImageRetryable("registry image target resolution failed")
	}
	if target.ScanTargetID != input.Target.ScopeID || target.Client == nil || !validImageRepository(target.Repository) ||
		!validImageDigest(target.ManifestDigest) || !validImageDigest(target.RuntimeDigest) {
		return scannerworker.AnalyzerResult{}, registryImageTerminal("registry image target invalid")
	}
	if target.RuntimeDigestKind != "manifest" && target.RuntimeDigestKind != "config" {
		return scannerworker.AnalyzerResult{}, registryImageTerminal("registry image digest kind unsupported")
	}
	if target.RuntimeDigestKind == "manifest" && target.RuntimeDigest != target.ManifestDigest {
		return scannerworker.AnalyzerResult{}, registryImageTerminal("registry image manifest identity mismatch")
	}
	manifest, err := target.Client.GetVerifiedManifest(ctx, target.Repository, target.ManifestDigest, maxScannerManifestBytes)
	if err != nil {
		return scannerworker.AnalyzerResult{}, registryImageRetryable("registry image manifest unavailable")
	}
	trackedStageBytes = int64(len(manifest.Body))
	descriptors, err := decodeImageManifest(ctx, manifest.Body, target)
	if err != nil {
		if ctx.Err() != nil {
			return scannerworker.AnalyzerResult{}, registryImageRetryable("registry image manifest decode canceled")
		}
		return scannerworker.AnalyzerResult{}, registryImageTerminal("registry image manifest unsupported")
	}
	if int64(len(descriptors.Layers)) > input.Limits.MaxFiles {
		return scannerworker.AnalyzerResult{}, registryImageLimit(scannerworker.FailureClassFileLimitExceeded)
	}
	stageDir, err := os.MkdirTemp("", "eshu-registry-image-*")
	if err != nil {
		return scannerworker.AnalyzerResult{}, registryImageRetryable("registry image staging unavailable")
	}
	defer func() {
		if err := os.RemoveAll(stageDir); err != nil {
			result = scannerworker.AnalyzerResult{}
			resultErr = registryImageRetryable("registry image staging cleanup failed")
		}
	}()
	compressedRemaining := input.Limits.MaxInputBytes
	if descriptors.Config.Size <= 0 || descriptors.Config.Size > compressedRemaining {
		return scannerworker.AnalyzerResult{}, registryImageLimit(scannerworker.FailureClassInputLimitExceeded)
	}
	trackedStageBytes += registryStagingBufferBytes
	if _, err := target.Client.StreamBlob(ctx, target.Repository, descriptors.Config.Digest, descriptors.Config.Size, compressedRemaining, io.Discard); err != nil {
		return scannerworker.AnalyzerResult{}, registryImageRetryable("registry image config unavailable")
	}
	compressedRemaining -= descriptors.Config.Size
	expandedRemaining := input.Limits.MaxInputBytes
	paths := make([]string, 0, len(descriptors.Layers))
	for index, descriptor := range descriptors.Layers {
		if descriptor.Size <= 0 || descriptor.Size > compressedRemaining {
			return scannerworker.AnalyzerResult{}, registryImageLimit(scannerworker.FailureClassInputLimitExceeded)
		}
		compressedPath := filepath.Join(stageDir, fmt.Sprintf("compressed-%d", index))
		// #nosec G304 -- compressedPath joins an os.MkdirTemp staging dir with an integer-derived layer index name and is created O_EXCL|0o600, so no external path or symlink can redirect it.
		compressedFile, err := os.OpenFile(compressedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return scannerworker.AnalyzerResult{}, registryImageRetryable("registry image staging unavailable")
		}
		_, streamErr := target.Client.StreamBlob(ctx, target.Repository, descriptor.Digest, descriptor.Size, compressedRemaining, compressedFile)
		closeErr := compressedFile.Close()
		if streamErr != nil || closeErr != nil {
			return scannerworker.AnalyzerResult{}, registryImageRetryable("registry image layer unavailable")
		}
		compressedRemaining -= descriptor.Size
		if descriptor.MediaType == imageTarLayerMediaType {
			if descriptor.Size > expandedRemaining {
				return scannerworker.AnalyzerResult{}, registryImageLimit(scannerworker.FailureClassInputLimitExceeded)
			}
			expandedRemaining -= descriptor.Size
			paths = append(paths, compressedPath)
			continue
		}
		expandedPath := filepath.Join(stageDir, fmt.Sprintf("expanded-%d", index))
		size, err := expandGzipLayer(ctx, compressedPath, expandedPath, expandedRemaining)
		if err != nil {
			if errors.Is(err, errExpandedLayerLimit) {
				return scannerworker.AnalyzerResult{}, registryImageLimit(scannerworker.FailureClassInputLimitExceeded)
			}
			if errors.Is(err, errExpandedLayerStaging) || errors.Is(err, context.Canceled) {
				return scannerworker.AnalyzerResult{}, registryImageRetryable("registry image layer staging unavailable")
			}
			return scannerworker.AnalyzerResult{}, registryImageTerminal("registry image layer unsupported")
		}
		expandedRemaining -= size
		paths = append(paths, expandedPath)
	}
	analyzer, err := imageanalyzer.NewAnalyzer(imageanalyzer.AnalyzerConfig{
		CollectorInstanceID: a.collectorInstanceID,
		Targets: []imageanalyzer.TargetConfig{{
			ScopeID:        target.ScanTargetID,
			LayerPaths:     paths,
			ImageReference: target.ManifestDigest,
			ImageDigest:    target.ManifestDigest,
			SourceURI:      "oci://" + target.ManifestDigest,
			SourceRecordID: target.ScanTargetID,
		}},
	})
	if err != nil {
		return scannerworker.AnalyzerResult{}, registryImageTerminal("registry image analyzer target invalid")
	}
	return analyzer.Analyze(ctx, input)
}

func decodeImageManifest(ctx context.Context, raw []byte, target registryImageTarget) (imageManifestDescriptors, error) {
	if err := validateUniqueManifestJSONKeys(ctx, raw); err != nil {
		return imageManifestDescriptors{}, err
	}
	var manifest struct {
		SchemaVersion int                    `json:"schemaVersion"`
		MediaType     string                 `json:"mediaType"`
		Config        imageLayerDescriptor   `json:"config"`
		Layers        []imageLayerDescriptor `json:"layers"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return imageManifestDescriptors{}, err
	}
	if err := ctx.Err(); err != nil {
		return imageManifestDescriptors{}, err
	}
	if manifest.SchemaVersion != 2 || (manifest.MediaType != imageManifestMediaType && manifest.MediaType != dockerManifestMediaType) || len(manifest.Layers) == 0 {
		return imageManifestDescriptors{}, errors.New("unsupported image manifest")
	}
	if (manifest.Config.MediaType != imageConfigMediaType && manifest.Config.MediaType != dockerConfigMediaType) ||
		!validImageDigest(manifest.Config.Digest) || manifest.Config.Size <= 0 {
		return imageManifestDescriptors{}, errors.New("invalid image config descriptor")
	}
	if target.RuntimeDigestKind == "config" && target.RuntimeDigest != manifest.Config.Digest {
		return imageManifestDescriptors{}, errors.New("runtime config digest mismatch")
	}
	for _, layer := range manifest.Layers {
		if !validImageDigest(layer.Digest) || layer.Size <= 0 {
			return imageManifestDescriptors{}, errors.New("invalid image layer descriptor")
		}
		if layer.MediaType != imageGzipLayerMediaType && layer.MediaType != dockerGzipLayerMediaType && layer.MediaType != imageTarLayerMediaType {
			return imageManifestDescriptors{}, errors.New("unsupported image layer")
		}
	}
	return imageManifestDescriptors{Config: manifest.Config, Layers: manifest.Layers}, nil
}

func expandGzipLayer(ctx context.Context, compressedPath, expandedPath string, capBytes int64) (int64, error) {
	// #nosec G304 -- compressedPath is the analyzer's own O_EXCL-created staging file under an os.MkdirTemp dir with an integer-derived name, not external input.
	input, err := os.Open(compressedPath)
	if err != nil {
		return 0, errExpandedLayerStaging
	}
	defer func() { _ = input.Close() }()
	gzipReader, err := gzip.NewReader(input)
	if err != nil {
		return 0, err
	}
	defer func() { _ = gzipReader.Close() }()
	// #nosec G304 -- expandedPath joins an os.MkdirTemp staging dir with an integer-derived layer index name and is created O_EXCL|0o600, so no external path or symlink can redirect it.
	output, err := os.OpenFile(expandedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, errExpandedLayerStaging
	}
	defer func() { _ = output.Close() }()
	buffer := make([]byte, registryStagingBufferBytes)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := gzipReader.Read(buffer)
		if count > 0 {
			if int64(count) > capBytes-total {
				return total, errExpandedLayerLimit
			}
			written, err := output.Write(buffer[:count])
			if err != nil || written != count {
				return total, errExpandedLayerStaging
			}
			total += int64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return total, readErr
		}
	}
	if err := output.Close(); err != nil {
		return total, errExpandedLayerStaging
	}
	return total, nil
}

func registryUserCPUSeconds() float64 {
	samples := []metrics.Sample{{Name: "/cpu/classes/user:cpu-seconds"}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindFloat64 {
		return 0
	}
	return samples[0].Value.Float64()
}

func registryImageRetryable(message string) error {
	return scannerworker.NewRetryableAnalyzerFailure(scannerworker.FailureClassSourceUnavailable, scannerworker.ResourceUsage{}, errors.New(message))
}

func registryImageTerminal(message string) error {
	return scannerworker.NewTerminalAnalyzerFailure(scannerworker.FailureClassUnsupportedTarget, scannerworker.ResourceUsage{}, errors.New(message))
}

func validImageDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

func validImageRepository(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, char := range segment {
			if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
				continue
			}
			return false
		}
	}
	return true
}

func registryImageLimit(class scannerworker.FailureClass) error {
	return scannerworker.NewTerminalAnalyzerFailure(class, scannerworker.ResourceUsage{}, errors.New("registry image resource limit exceeded"))
}
