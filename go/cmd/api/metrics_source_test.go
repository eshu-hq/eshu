// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestSelectMetricsCollectorInstanceIgnoresVulnerabilityFixtures pins issue
// #9323's first criterion at the selector level: the B-7 fixture list carries
// vulnerability-intelligence instances alongside the Prometheus source, and
// the metrics selection must keep resolving the Prometheus instance.
func TestSelectMetricsCollectorInstanceIgnoresVulnerabilityFixtures(t *testing.T) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(`[{
	  "instance_id": "golden-prometheus-range",
	  "collector_kind": "prometheus_mimir",
	  "mode": "continuous",
	  "enabled": true,
	  "bootstrap": false,
	  "claims_enabled": false,
	  "display_name": "Golden Prometheus range source",
	  "configuration": {"targets": [
	    {"provider": "prometheus", "base_url": "http://127.0.0.1:19090", "tenant_id": "golden-corpus", "enabled": true}
	  ]}
	}, {
	  "instance_id": "collector-vulnintel-nvd-known",
	  "collector_kind": "vulnerability_intelligence",
	  "mode": "continuous",
	  "enabled": true,
	  "bootstrap": false,
	  "claims_enabled": false,
	  "display_name": "Golden NVD known-CVE fixture",
	  "configuration": {"targets": [
	    {"scope_id": "vulnerability_intelligence:supply-chain-demo:nvd-known",
	     "source": "nvd", "cve_ids": ["CVE-2026-19227"],
	     "feed_url": "https://nvd.supply-chain-demo.test/rest/json/cves/2.0"}
	  ]}
	}]`)
	if err != nil {
		t.Fatalf("ParseDesiredCollectorInstancesJSON() error = %v", err)
	}
	got, ok, err := selectMetricsCollectorInstance(instances, "golden-prometheus-range")
	if err != nil {
		t.Fatalf("selectMetricsCollectorInstance() error = %v", err)
	}
	if !ok {
		t.Fatal("selectMetricsCollectorInstance() ok = false, want the Prometheus instance")
	}
	if got.InstanceID != "golden-prometheus-range" {
		t.Fatalf("selectMetricsCollectorInstance() instance = %q, want golden-prometheus-range", got.InstanceID)
	}
}

// TestSelectMetricsCollectorInstanceAbsentWithoutPrometheus documents that a
// vulnerability-only instance list is not an error: the metrics source is
// simply unconfigured.
func TestSelectMetricsCollectorInstanceAbsentWithoutPrometheus(t *testing.T) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(`[{
	  "instance_id": "collector-vulnintel-nvd-known",
	  "collector_kind": "vulnerability_intelligence",
	  "mode": "continuous",
	  "enabled": true,
	  "bootstrap": false,
	  "claims_enabled": false,
	  "configuration": {"targets": []}
	}]`)
	if err != nil {
		t.Fatalf("ParseDesiredCollectorInstancesJSON() error = %v", err)
	}
	_, ok, err := selectMetricsCollectorInstance(instances, "")
	if err != nil {
		t.Fatalf("selectMetricsCollectorInstance() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("selectMetricsCollectorInstance() ok = true, want false without a Prometheus instance")
	}
}
