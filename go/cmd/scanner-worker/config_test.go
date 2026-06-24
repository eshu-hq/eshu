// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability/osruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestLoadRuntimeConfigSelectsScannerWorkerInstance(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"scanner-worker-source",
			"collector_kind":"scanner_worker",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{"analyzer":"source_analysis"}
		}]`,
	}

	config, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.CollectorKind, scope.CollectorScannerWorker; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := config.Analyzer, scannerworker.AnalyzerSourceAnalysis; got != want {
		t.Fatalf("Analyzer = %q, want %q", got, want)
	}
	if got, want := config.Limits.MemoryBytes, int64(4<<30); got != want {
		t.Fatalf("MemoryBytes = %d, want %d", got, want)
	}
}

func TestLoadRuntimeConfigRejectsReducerOwnedAnalyzer(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"scanner-worker-source",
			"collector_kind":"scanner_worker",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{"analyzer":"vulnerability_matching"}
		}]`,
	}

	_, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want reducer analyzer rejection")
	}
	if got, want := err.Error(), "reducer"; !strings.Contains(got, want) {
		t.Fatalf("loadRuntimeConfig() error = %q, want substring %q", got, want)
	}
}

func TestLoadRuntimeConfigSelectsSBOMGenerationAnalyzer(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"scanner-worker-sbom",
			"collector_kind":"scanner_worker",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{"analyzer":"sbom_generation"}
		}]`,
	}

	config, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Analyzer, scannerworker.AnalyzerSBOMGeneration; got != want {
		t.Fatalf("Analyzer = %q, want %q", got, want)
	}
	if got, want := config.Limits.MemoryBytes, int64(8<<30); got != want {
		t.Fatalf("MemoryBytes = %d, want %d (sbom_generation default)", got, want)
	}
	if got, want := config.Limits.MaxFacts, 50000; got != want {
		t.Fatalf("MaxFacts = %d, want %d (sbom_generation default)", got, want)
	}
}

func TestLoadRuntimeConfigParsesSBOMTargets(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"scanner-worker-sbom",
			"collector_kind":"scanner_worker",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"analyzer":"sbom_generation",
				"sbom_targets":[{
					"scope_id":"scanner-worker://repository/repo-private-name",
					"root_path":"/var/lib/eshu/scanner/repositories/repo-private-name",
					"subject_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111"
				}]
			}
		}]`,
	}

	config, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := len(config.SBOMTargets), 1; got != want {
		t.Fatalf("SBOMTargets len = %d, want %d", got, want)
	}
	if got, want := config.SBOMTargets[0].SubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111"; got != want {
		t.Fatalf("SubjectDigest = %q, want %q", got, want)
	}
}

func TestBuildAnalyzerUsesSBOMGenerationFallbackWarning(t *testing.T) {
	t.Parallel()

	analyzer, err := buildAnalyzer(runtimeConfig{Analyzer: scannerworker.AnalyzerSBOMGeneration})
	if err != nil {
		t.Fatalf("buildAnalyzer(sbom_generation) error = %v, want nil", err)
	}
	warning, ok := analyzer.(scannerworker.WarningAnalyzer)
	if !ok {
		t.Fatalf("buildAnalyzer(sbom_generation) = %T, want WarningAnalyzer until concrete source ships", analyzer)
	}
	if got, want := warning.Reason, "sbom_generator_source_not_configured"; got != want {
		t.Fatalf("WarningAnalyzer.Reason = %q, want %q", got, want)
	}
}

func TestLoadRuntimeConfigParsesResourceOverrides(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"scanner-worker-image",
			"collector_kind":"scanner_worker",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{"analyzer":"image_unpacking"}
		}]`,
		envCPUMillis:     "7000",
		envMemoryBytes:   "17179869184",
		envTimeout:       "20m",
		envMaxInputBytes: "8589934592",
		envMaxFiles:      "300000",
		envMaxFacts:      "60000",
	}

	config, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if config.Limits.CPUMillis != 7000 || config.Limits.MemoryBytes != 16<<30 {
		t.Fatalf("Limits = %#v, want overridden CPU and memory", config.Limits)
	}
	if config.Limits.Timeout != 20*time.Minute || config.Limits.MaxInputBytes != 8<<30 {
		t.Fatalf("Limits = %#v, want overridden timeout and input bytes", config.Limits)
	}
	if config.Limits.MaxFiles != 300000 || config.Limits.MaxFacts != 60000 {
		t.Fatalf("Limits = %#v, want overridden cardinality", config.Limits)
	}
}

func TestLoadRuntimeConfigParsesOSPackageTargets(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"scanner-worker-os",
			"collector_kind":"scanner_worker",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"analyzer":"os_package_extraction",
				"os_package_targets":[{
					"scope_id":"image://registry.example/team/app@sha256:deadbeef",
					"rootfs_path":"/var/lib/eshu/scanner/rootfs/deadbeef",
					"source_uri":"oci://registry.example/team/app@sha256:deadbeef",
					"source_record_id":"sha256:deadbeef",
					"distro":"alpine",
					"distro_version":"3.19.1"
				}]
			}
		}]`,
	}

	config, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Analyzer, scannerworker.AnalyzerOSPackageExtraction; got != want {
		t.Fatalf("Analyzer = %q, want %q", got, want)
	}
	if got, want := len(config.OSPackageTargets), 1; got != want {
		t.Fatalf("OSPackageTargets len = %d, want %d", got, want)
	}
	if got, want := config.OSPackageTargets[0].Distro, osruntime.DistroAlpine; got != want {
		t.Fatalf("Distro = %q, want %q", got, want)
	}
}

func TestLoadRuntimeConfigParsesImageAnalyzerTargets(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"scanner-worker-image",
			"collector_kind":"scanner_worker",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"analyzer":"image_unpacking",
				"image_targets":[{
					"scope_id":"image://registry.example/team/app@sha256:deadbeef",
					"rootfs_path":"/var/lib/eshu/scanner/rootfs/deadbeef",
					"layer_paths":["/var/lib/eshu/scanner/layers/deadbeef.tar.gz"],
					"source_uri":"oci://registry.example/team/app@sha256:deadbeef",
					"source_record_id":"sha256:deadbeef",
					"image_reference":"registry.example/team/app:1.2.3",
					"image_digest":"sha256:deadbeef",
					"distro":"alpine",
					"distro_version":"3.19.1"
				}]
			}
		}]`,
	}

	config, err := loadRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Analyzer, scannerworker.AnalyzerImageUnpacking; got != want {
		t.Fatalf("Analyzer = %q, want %q", got, want)
	}
	if got, want := len(config.ImageTargets), 1; got != want {
		t.Fatalf("ImageTargets len = %d, want %d", got, want)
	}
	if got, want := config.ImageTargets[0].ImageReference, "registry.example/team/app:1.2.3"; got != want {
		t.Fatalf("ImageReference = %q, want %q", got, want)
	}
}
