package main

import (
	"strings"
	"testing"
	"time"

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
