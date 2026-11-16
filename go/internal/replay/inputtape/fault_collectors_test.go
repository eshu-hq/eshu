// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape_test

// Collector fault-injection coverage (C-14, #4367, epic #4172).
//
// The replay-coverage gate requires a `fault` depth scenario for every
// implemented collector boundary. When a case exists it drives a REAL
// collector's poll with the inputtape timeout fault injected at its HTTP
// boundary, then asserts the collector surfaces the fault as a classified
// timeout rather than swallowing it, mis-classifying it, or hanging.
//
// The table is empty today: every collector boundary moved to an extracted
// satellite (pagerduty #8863, grafana #8857, prometheus-mimir #8864,
// security-alert/oci-registry/package-registry eshu-hq/eshu#10620,
// vulnerability-intelligence eshu-hq/eshu#9999, sbom-attestation
// eshu-hq/eshu#10450), and each satellite owns its client fault behavior.
// The runner and TestCollectorFaultManifestBinding stay: the cases are
// table-driven off collectorFaultCases as the single source of truth, and
// the binding fails if a manifest row is added without a case (or vice
// versa) under the go-test-race gate.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// concurrency-deadlock-rigor.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// collectorFaultCase drives one real collector's poll into an injected boundary
// timeout. surface is the replay-coverage surface id it proves (the single
// source of truth the manifest binding checks).
type collectorFaultCase struct {
	surface string
	collect func() error
}

// collectorFaultCases is the authoritative list of collector boundaries proven
// against a timeout fault. Adding a collector here without a matching manifest
// row (or vice versa) fails TestCollectorFaultManifestBinding.
func collectorFaultCases() []collectorFaultCase {
	return []collectorFaultCase{
		// The Grafana and Prometheus-Mimir boundaries moved to the extracted
		// satellites (eshu-hq/eshu-collector-grafana cutover #8857,
		// eshu-hq/eshu-collector-prometheus-mimir cutover #8864): core owns
		// no Grafana or Prometheus/Mimir HTTP client to fault-probe anymore,
		// so this table carries no grafana or prometheus_mimir case.
		// The PagerDuty boundary moved to the extracted satellite
		// (eshu-hq/eshu-collector-pagerduty, cutover #8863): core owns no
		// PagerDuty HTTP client to fault-probe anymore, so this table
		// carries no pagerduty case.
		// The security-alert, OCI-registry, and package-registry boundaries
		// moved to the extracted satellites with eshu-hq/eshu#10620: core
		// owns no provider HTTP client to fault-probe anymore, so this
		// table carries no case for them. Each satellite owns its client
		// fault behavior.
		// The vulnerability intelligence boundary moved to the extracted
		// satellite (eshu-hq/eshu#9999): core owns no OSV HTTP client to
		// fault-probe anymore, so this table carries no vuln case. The
		// satellite owns its client fault behavior.
		// The SBOM attestation boundary moved to the extracted
		// satellite (eshu-hq/eshu#10450): core owns no document HTTP
		// client to fault-probe anymore, so this table carries no sbom
		// case. The satellite owns its client fault behavior.
	}
}

// TestCollectorsSurfaceInjectedTimeout runs every collector fault case: the real
// collector is driven into an injected boundary timeout and must surface a
// classified timeout on both paths real SDKs gate retries on — the
// context.DeadlineExceeded sentinel and the net.Error Timeout() interface. A
// collector that returns nil (swallows the fault) or an unclassified error fails.
func TestCollectorsSurfaceInjectedTimeout(t *testing.T) {
	t.Parallel()

	for _, tc := range collectorFaultCases() {
		t.Run(tc.surface, func(t *testing.T) {
			t.Parallel()

			err := tc.collect()
			if err == nil {
				t.Fatalf("collector swallowed the injected timeout; want a surfaced error")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("collector error not classified as context.DeadlineExceeded: %v", err)
			}
			var timeout interface{ Timeout() bool }
			if !errors.As(err, &timeout) || !timeout.Timeout() {
				t.Fatalf("collector error not reachable as a net timeout: %v", err)
			}
		})
	}
}

// TestCollectorFaultManifestBinding binds the collectorFaultCases surface set to
// the replay-coverage manifest's collector fault rows that point at this file.
// It fails if a case is deleted or renamed without dropping its manifest row (or
// a manifest row is added without a case), closing the os.Stat-only ref gap
// where the fault dashboard could stay green after a specific collector's proof
// was silently removed.
func TestCollectorFaultManifestBinding(t *testing.T) {
	t.Parallel()

	const manifestRel = "../../../../specs/replay-coverage-manifest.v1.yaml"
	const thisFileRef = "go/internal/replay/inputtape/fault_collectors_test.go"

	raw, err := os.ReadFile(filepath.Clean(manifestRel))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Coverage []struct {
			Surface      string `yaml:"surface"`
			ScenarioType string `yaml:"scenario_type"`
			Ref          string `yaml:"ref"`
		} `yaml:"coverage"`
	}
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	manifestSurfaces := map[string]struct{}{}
	for _, entry := range manifest.Coverage {
		if entry.ScenarioType != "fault" || !strings.HasPrefix(entry.Surface, "collector:") {
			continue
		}
		if entry.Ref != thisFileRef {
			continue
		}
		manifestSurfaces[entry.Surface] = struct{}{}
	}

	caseSurfaces := map[string]struct{}{}
	for _, tc := range collectorFaultCases() {
		if _, dup := caseSurfaces[tc.surface]; dup {
			t.Fatalf("duplicate collector fault case surface %q", tc.surface)
		}
		caseSurfaces[tc.surface] = struct{}{}
	}

	for surface := range manifestSurfaces {
		if _, ok := caseSurfaces[surface]; !ok {
			t.Errorf("manifest row %q (fault -> %s) has no collectorFaultCases entry; the dashboard would count a collector with no running test", surface, thisFileRef)
		}
	}
	for surface := range caseSurfaces {
		if _, ok := manifestSurfaces[surface]; !ok {
			t.Errorf("collectorFaultCases entry %q has no fault manifest row referencing %s", surface, thisFileRef)
		}
	}
}
