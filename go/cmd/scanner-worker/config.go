// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability/osruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/imageanalyzer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_SCANNER_WORKER_INSTANCE_ID"
	envAnalyzer            = "ESHU_SCANNER_WORKER_ANALYZER"
	envPollInterval        = "ESHU_SCANNER_WORKER_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_SCANNER_WORKER_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_SCANNER_WORKER_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_SCANNER_WORKER_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
	envCPUMillis           = "ESHU_SCANNER_WORKER_CPU_MILLIS"
	envMemoryBytes         = "ESHU_SCANNER_WORKER_MEMORY_BYTES"
	envTimeout             = "ESHU_SCANNER_WORKER_TIMEOUT"
	envMaxInputBytes       = "ESHU_SCANNER_WORKER_MAX_INPUT_BYTES"
	envMaxFiles            = "ESHU_SCANNER_WORKER_MAX_FILES"
	envMaxFacts            = "ESHU_SCANNER_WORKER_MAX_FACTS"
)

type runtimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Analyzer          scannerworker.AnalyzerKind
	Limits            scannerworker.ResourceLimits
	SBOMTargets       []sbomTargetConfig
	OSPackageTargets  []osruntime.TargetConfig
	ImageTargets      []imageanalyzer.TargetConfig
}

type scannerInstanceConfig struct {
	Analyzer         string                       `json:"analyzer"`
	ResourceLimits   resourceLimitsJSON           `json:"resource_limits"`
	SBOMTargets      []sbomTargetConfig           `json:"sbom_targets"`
	OSPackageTargets []osruntime.TargetConfig     `json:"os_package_targets"`
	ImageTargets     []imageanalyzer.TargetConfig `json:"image_targets"`
}

type resourceLimitsJSON struct {
	CPUMillis     int64  `json:"cpu_millis"`
	MemoryBytes   int64  `json:"memory_bytes"`
	Timeout       string `json:"timeout"`
	MaxInputBytes int64  `json:"max_input_bytes"`
	MaxFiles      int64  `json:"max_files"`
	MaxFacts      int    `json:"max_facts"`
}

func loadRuntimeConfig(getenv func(string) string) (runtimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectScannerWorkerInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return runtimeConfig{}, err
	}
	if err := validateScannerWorkerInstance(instance); err != nil {
		return runtimeConfig{}, err
	}
	decoded, err := parseScannerInstanceConfig(instance)
	if err != nil {
		return runtimeConfig{}, err
	}
	analyzer := scannerworker.AnalyzerKind(firstNonBlank(getenv(envAnalyzer), decoded.Analyzer, string(scannerworker.AnalyzerSourceAnalysis)))
	limits, err := scannerworker.DefaultResourceLimits(analyzer)
	if err != nil {
		return runtimeConfig{}, err
	}
	limits, err = mergeConfiguredLimits(limits, decoded.ResourceLimits)
	if err != nil {
		return runtimeConfig{}, err
	}
	limits, err = applyResourceEnvOverrides(getenv, limits)
	if err != nil {
		return runtimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, envPollInterval, time.Second)
	if err != nil {
		return runtimeConfig{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, envClaimLeaseTTL, workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return runtimeConfig{}, err
	}
	heartbeatInterval, err := envDuration(getenv, envHeartbeatInterval, workflow.DefaultHeartbeatInterval())
	if err != nil {
		return runtimeConfig{}, err
	}
	if heartbeatInterval >= claimLeaseTTL {
		return runtimeConfig{}, fmt.Errorf("scanner-worker heartbeat interval must be less than claim lease TTL")
	}
	return runtimeConfig{
		Instance:          instance,
		OwnerID:           ownerID(getenv),
		PollInterval:      pollInterval,
		ClaimLeaseTTL:     claimLeaseTTL,
		HeartbeatInterval: heartbeatInterval,
		Analyzer:          analyzer,
		Limits:            limits,
		SBOMTargets:       decoded.SBOMTargets,
		OSPackageTargets:  decoded.OSPackageTargets,
		ImageTargets:      decoded.ImageTargets,
	}, nil
}

func selectScannerWorkerInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorScannerWorker {
			continue
		}
		if requestedInstanceID != "" && instance.InstanceID != requestedInstanceID {
			continue
		}
		matches = append(matches, instance)
	}
	switch len(matches) {
	case 0:
		if requestedInstanceID != "" {
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("scanner-worker instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no scanner-worker instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple scanner-worker instances configured; set %s", envCollectorInstanceID)
	}
}

func validateScannerWorkerInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("scanner-worker instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorScannerWorker {
		return fmt.Errorf("scanner-worker requires collector_kind %q", scope.CollectorScannerWorker)
	}
	if !instance.Enabled {
		return fmt.Errorf("scanner-worker requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("scanner-worker requires claim-enabled collector instance")
	}
	return nil
}

func parseScannerInstanceConfig(instance workflow.DesiredCollectorInstance) (scannerInstanceConfig, error) {
	var decoded scannerInstanceConfig
	if strings.TrimSpace(instance.Configuration) == "" {
		return decoded, nil
	}
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return scannerInstanceConfig{}, fmt.Errorf("decode scanner-worker configuration: %w", err)
	}
	return decoded, nil
}

func mergeConfiguredLimits(limits scannerworker.ResourceLimits, configured resourceLimitsJSON) (scannerworker.ResourceLimits, error) {
	if configured.CPUMillis > 0 {
		limits.CPUMillis = configured.CPUMillis
	}
	if configured.MemoryBytes > 0 {
		limits.MemoryBytes = configured.MemoryBytes
	}
	if strings.TrimSpace(configured.Timeout) != "" {
		parsed, err := time.ParseDuration(configured.Timeout)
		if err != nil {
			return scannerworker.ResourceLimits{}, fmt.Errorf("parse configured timeout: %w", err)
		}
		limits.Timeout = parsed
	}
	if configured.MaxInputBytes > 0 {
		limits.MaxInputBytes = configured.MaxInputBytes
	}
	if configured.MaxFiles > 0 {
		limits.MaxFiles = configured.MaxFiles
	}
	if configured.MaxFacts > 0 {
		limits.MaxFacts = configured.MaxFacts
	}
	return limits, nil
}

func applyResourceEnvOverrides(getenv func(string) string, limits scannerworker.ResourceLimits) (scannerworker.ResourceLimits, error) {
	var err error
	if limits.CPUMillis, err = envInt64(getenv, envCPUMillis, limits.CPUMillis); err != nil {
		return scannerworker.ResourceLimits{}, err
	}
	if limits.MemoryBytes, err = envInt64(getenv, envMemoryBytes, limits.MemoryBytes); err != nil {
		return scannerworker.ResourceLimits{}, err
	}
	if limits.Timeout, err = envDuration(getenv, envTimeout, limits.Timeout); err != nil {
		return scannerworker.ResourceLimits{}, err
	}
	if limits.MaxInputBytes, err = envInt64(getenv, envMaxInputBytes, limits.MaxInputBytes); err != nil {
		return scannerworker.ResourceLimits{}, err
	}
	if limits.MaxFiles, err = envInt64(getenv, envMaxFiles, limits.MaxFiles); err != nil {
		return scannerworker.ResourceLimits{}, err
	}
	if limits.MaxFacts, err = envInt(getenv, envMaxFacts, limits.MaxFacts); err != nil {
		return scannerworker.ResourceLimits{}, err
	}
	return limits, nil
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return value, nil
}

func envInt64(getenv func(string) string, key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return value, nil
}

func envInt(getenv func(string) string, key string, fallback int) (int, error) {
	value, err := envInt64(getenv, key, int64(fallback))
	if err != nil {
		return 0, err
	}
	return int(value), nil
}

func ownerID(getenv func(string) string) string {
	for _, key := range []string{envOwnerID, "HOSTNAME"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return "scanner-worker"
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
