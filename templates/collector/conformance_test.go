// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

// TestConformancePositive proves the representative collector passes the
// public fixture harness using only the published SDK modules.
func TestConformancePositive(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	result := mustCollect(t, "complete.json", testClaim(), testObservedAt(), "")
	report := conformance.Run(conformance.Request{Manifest: manifest, Fixtures: []sdk.Result{result}, Mode: conformance.ModeFixture})
	if !report.OK() {
		t.Fatalf("conformance findings = %#v, want passed", report.Findings)
	}
	if report.Summary.FactCount != 3 {
		t.Fatalf("FactCount = %d, want 3 (2 records + snapshot)", report.Summary.FactCount)
	}
	if !report.Summary.IdempotentReemissionChecked {
		t.Fatal("IdempotentReemissionChecked = false, want true")
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal(report) error = %v", err)
	}
	for _, want := range []string{`"schema_version":"eshu.extension.conformance.v1"`, `"status":"passed"`, `"component_id":"` + ComponentID + `"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("conformance report JSON missing %q: %s", want, raw)
		}
	}
}

// TestConformanceEmpty proves empty source completes unchanged with no facts.
func TestConformanceEmpty(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	result := mustCollect(t, "empty.json", testClaim(), testObservedAt(), "")
	if result.State != sdk.ResultUnchanged {
		t.Fatalf("State = %q, want unchanged", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("FactCount = %d, want 0", len(result.Facts))
	}
	report := conformance.Run(conformance.Request{Manifest: manifest, Fixtures: []sdk.Result{result}, Mode: conformance.ModeFixture})
	if !report.OK() {
		t.Fatalf("empty unchanged findings = %#v, want passed", report.Findings)
	}
}

// TestConformanceStale proves an unchanged digest completes unchanged.
func TestConformanceStale(t *testing.T) {
	t.Parallel()

	report := mustLoadReport(t, "complete.json")
	digest := Digest(report)
	result := mustCollect(t, "complete.json", testClaim(), testObservedAt(), digest)
	if result.State != sdk.ResultUnchanged {
		t.Fatalf("State = %q, want unchanged for stale digest", result.State)
	}
}

// TestConformanceDuplicate proves duplicate delivery emits one fact per key.
func TestConformanceDuplicate(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	complete := mustCollect(t, "complete.json", testClaim(), testObservedAt(), "")
	duplicate := mustCollect(t, "duplicate.json", testClaim(), testObservedAt(), "")
	if len(duplicate.Facts) != len(complete.Facts) {
		t.Fatalf("duplicate Facts = %d, want %d", len(duplicate.Facts), len(complete.Facts))
	}
	report := conformance.Run(conformance.Request{Manifest: manifest, Fixtures: []sdk.Result{duplicate}, Mode: conformance.ModeFixture})
	if !report.OK() {
		t.Fatalf("duplicate findings = %#v, want passed", report.Findings)
	}
}

// TestIdentitiesAndScope proves stable keys, scope binding, and generation.
func TestIdentitiesAndScope(t *testing.T) {
	t.Parallel()

	claim := testClaim()
	claim.Scope = sdk.Scope{ID: "component:scoped-1", Kind: "component"}
	claim.GenerationID = "generation-9"
	result := mustCollect(t, "complete.json", claim, testObservedAt(), "")
	seen := map[string]bool{}
	for _, fact := range result.Facts {
		if strings.TrimSpace(fact.StableKey) == "" {
			t.Fatal("fact StableKey empty")
		}
		if seen[fact.StableKey] {
			t.Fatalf("duplicate StableKey %q", fact.StableKey)
		}
		seen[fact.StableKey] = true
		if fact.SourceRef.ScopeID != claim.Scope.ID {
			t.Fatalf("ScopeID = %q, want %q", fact.SourceRef.ScopeID, claim.Scope.ID)
		}
		if fact.SourceRef.GenerationID != claim.GenerationID {
			t.Fatalf("GenerationID = %q, want %q", fact.SourceRef.GenerationID, claim.GenerationID)
		}
		if strings.Contains(fact.SourceRef.URI, "@") && !strings.Contains(fact.SourceRef.URI, "://") {
			t.Fatalf("SourceRef URI embeds credentials: %q", fact.SourceRef.URI)
		}
	}
}

// TestRedaction proves secrets never enter payloads and are recorded.
func TestRedaction(t *testing.T) {
	t.Parallel()

	report := mustLoadReport(t, "complete.json")
	report.Records[0].Secret = "super-secret-value"
	result, err := Collect(testClaim(), report, CollectOptions{ObservedAt: testObservedAt(), SourceURI: "https://example.invalid/source/template"})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, fact := range result.Facts {
		raw, _ := json.Marshal(fact.Payload)
		if strings.Contains(string(raw), "super-secret-value") {
			t.Fatalf("payload leaks secret: %s", raw)
		}
	}
	redacted := false
	for _, fact := range result.Facts {
		for _, redaction := range fact.Redactions {
			if redaction.Field == "secret" {
				redacted = true
			}
		}
	}
	if !redacted {
		t.Fatal("no secret redaction recorded")
	}
}

// TestRetryAndPartial proves bounded-retry and partial-failure shapes.
func TestRetryAndPartial(t *testing.T) {
	t.Parallel()

	retryable := CollectRetryable(testClaim(), testObservedAt(), 30)
	if retryable.State != sdk.ResultRetryable {
		t.Fatalf("State = %q, want retryable", retryable.State)
	}
	partial, err := CollectPartial(testClaim(), mustLoadReport(t, "partial.json"),
		CollectOptions{ObservedAt: testObservedAt(), SourceURI: "https://example.invalid/source/template"}, "source-degraded")
	if err != nil {
		t.Fatalf("CollectPartial() error = %v", err)
	}
	if partial.State != sdk.ResultPartial {
		t.Fatalf("State = %q, want partial", partial.State)
	}
	foundWarning := false
	for _, status := range partial.Statuses {
		if status.Class == sdk.StatusWarning && status.Partial {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatal("partial result missing warning status")
	}
}

func testObservedAt() time.Time {
	return time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
}

func testClaim() sdk.Claim {
	observedAt := testObservedAt()
	return sdk.Claim{
		ComponentID:   ComponentID,
		InstanceID:    "template-primary",
		CollectorKind: CollectorKind,
		SourceSystem:  SourceSystem,
		Scope:         sdk.Scope{ID: "component:template-primary", Kind: "component"},
		SourceRunID:   "run-1",
		GenerationID:  "generation-1",
		WorkItemID:    "work-1",
		FencingToken:  "fence-1",
		Attempt:       1,
		Deadline:      observedAt.Add(time.Hour),
		ConfigHandle:  "component-config:template",
	}
}

func mustLoadReport(t *testing.T, name string) Report {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", name, err)
	}
	var report Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", name, err)
	}
	return report
}

func mustCollect(t *testing.T, fixture string, claim sdk.Claim, observedAt time.Time, previousDigest string) sdk.Result {
	t.Helper()
	result, err := Collect(claim, mustLoadReport(t, fixture), CollectOptions{
		ObservedAt:     observedAt,
		SourceURI:      "https://example.invalid/source/template",
		PreviousDigest: previousDigest,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return result
}

func loadManifest(t *testing.T) conformance.Manifest {
	t.Helper()
	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatalf("os.ReadFile(manifest.yaml) error = %v", err)
	}
	var manifest conformance.Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal(manifest.yaml) error = %v", err)
	}
	return manifest
}

func hasFinding(report conformance.Report, code conformance.FindingCode) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
