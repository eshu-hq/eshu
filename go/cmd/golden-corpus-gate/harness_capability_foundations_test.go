// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenHarnessIntegratesCapabilityFoundationsInOrder(t *testing.T) {
	t.Parallel()

	scriptPath := filepath.Join("..", "..", "..", "scripts", "verify-golden-corpus-gate.sh")
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	script := string(body)
	for _, want := range []string{
		"semantic-extraction-cassette:collector-prometheus-mimir:semanticextraction",
		"golden_service_changed_since_capture_prior",
		"golden_service_changed_since_mutate_owner",
		"golden_service_changed_since_validate_current",
		"golden_service_changed_since_compose_snapshot",
		"golden_changed_since_capture_prior",
		"golden_changed_since_mutate_fixture",
		"golden_changed_since_validate_current",
		"golden_changed_since_compose_snapshot",
		"export ESHU_EMIT_DATAFLOW=true",
		"build_bin mock-prometheus-mimir",
		"golden_metrics_source_start",
		"GATE_PROMETHEUS_SOURCE_PORT",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("golden harness missing %q", want)
		}
	}
	priorAt := strings.Index(script, "\ngolden_service_changed_since_capture_prior\n")
	repositoryPriorAt := strings.Index(script, "\ngolden_changed_since_capture_prior\n")
	mutateAt := strings.Index(script, "\ngolden_service_changed_since_mutate_owner\n")
	repositoryMutateAt := strings.Index(script, "\ngolden_changed_since_mutate_fixture\n")
	maintenanceAt := strings.Index(script, "\nrun_maintenance_drain_cycles\n")
	validateAt := strings.Index(script, "\ngolden_service_changed_since_validate_current\n")
	repositoryValidateAt := strings.Index(script, "\ngolden_changed_since_validate_current\n")
	metricsAt := strings.Index(script, "\ngolden_metrics_source_start\n")
	apiAt := strings.Index(script, "\nstart_bg api api_pid")
	if priorAt < 0 || repositoryPriorAt <= priorAt || mutateAt <= repositoryPriorAt || repositoryMutateAt <= mutateAt ||
		maintenanceAt <= repositoryMutateAt || validateAt <= maintenanceAt || repositoryValidateAt <= validateAt ||
		metricsAt <= repositoryValidateAt || apiAt <= metricsAt {
		t.Fatalf("integration order service-prior=%d repository-prior=%d service-mutate=%d repository-mutate=%d maintenance=%d service-validate=%d repository-validate=%d metrics=%d api=%d", priorAt, repositoryPriorAt, mutateAt, repositoryMutateAt, maintenanceAt, validateAt, repositoryValidateAt, metricsAt, apiAt)
	}
}

func TestGoldenHarnessRuntimeSnapshotTransformsCompose(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	suppressionBody, err := os.ReadFile(filepath.Join(root, "scripts", "lib", "golden-corpus-vulnerability-suppression.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(suppressionBody), `local input_snapshot="$1"`) {
		t.Fatal("suppression runtime transform must consume the caller's input snapshot")
	}
	metricsBody, err := os.ReadFile(filepath.Join(root, "scripts", "lib", "golden-corpus-metrics-source.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ESHU_COLLECTOR_INSTANCES_JSON", "ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID"} {
		if !strings.Contains(string(metricsBody), want) {
			t.Errorf("metrics source helper missing %q", want)
		}
	}
	scriptBody, err := os.ReadFile(filepath.Join(root, "scripts", "verify-golden-corpus-gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBody)
	composeAt := strings.Index(script, "golden_service_changed_since_compose_snapshot")
	suppressionAt := -1
	if composeAt >= 0 {
		suppressionAt = strings.Index(script[composeAt:], "${golden_suppression_runtime_snapshot}")
	}
	if composeAt < 0 || suppressionAt < 0 {
		t.Fatal("service changed-since transform must consume the suppression-composed snapshot")
	}
	repositoryComposeAt := strings.Index(script, "golden_changed_since_compose_snapshot")
	serviceSnapshotAt := -1
	if repositoryComposeAt >= 0 {
		serviceSnapshotAt = strings.Index(script[repositoryComposeAt:], "${golden_service_runtime_snapshot}")
	}
	if repositoryComposeAt < 0 || serviceSnapshotAt < 0 {
		t.Fatal("repository changed-since transform must consume the service-composed snapshot")
	}
}
