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

// TestConformanceEmpty proves an empty source emits an authoritative complete
// snapshot with zero records, retiring stale evidence instead of leaving it
// current. Unchanged freshness stays a separate ResultUnchanged outcome.
func TestConformanceEmpty(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	result := mustCollect(t, "empty.json", testClaim(), testObservedAt(), "")
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete", result.State)
	}
	if len(result.Facts) != 1 || result.Facts[0].Kind != FactKindSnapshot {
		t.Fatalf("Facts = %#v, want one snapshot fact", result.Facts)
	}
	report := conformance.Run(conformance.Request{Manifest: manifest, Fixtures: []sdk.Result{result}, Mode: conformance.ModeFixture})
	if !report.OK() {
		t.Fatalf("empty snapshot findings = %#v, want passed", report.Findings)
	}
}

// TestSourceURIRefusal proves credential-bearing source URIs fail before any
// fact is built, including hierarchical userinfo the naive "@" check misses.
func TestSourceURIRefusal(t *testing.T) {
	t.Parallel()

	for _, uri := range []string{
		"https://user:secret@example.invalid/source",
		"https://user:password@example.invalid/source",
		"svc:SECRET@example.invalid/x",
		"https://example.invalid/source?token=abc",
		"https://example.invalid/source?password=hunter2",
		"",
	} {
		_, err := Collect(testClaim(), mustLoadReport(t, "complete.json"), CollectOptions{
			ObservedAt: testObservedAt(),
			SourceURI:  uri,
		})
		if err == nil {
			t.Fatalf("Collect(SourceURI=%q) error = nil, want refusal", uri)
		}
	}
	if _, err := Collect(testClaim(), mustLoadReport(t, "complete.json"), CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://example.invalid/source/template",
	}); err != nil {
		t.Fatalf("Collect(good URI) error = %v, want nil", err)
	}
}

// TestClaimBounds proves record, payload, and deadline bounds fail terminal
// instead of allocating without bound.
func TestClaimBounds(t *testing.T) {
	t.Parallel()

	overRecords, err := Collect(testClaim(), mustLoadReport(t, "complete.json"), CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://example.invalid/source/template",
		Limits:     ResourceUse{MaxRecordsPerClaim: 1, MaxPayloadBytes: 65536, ClaimTimeoutSeconds: 300},
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if overRecords.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal for record-budget breach", overRecords.State)
	}

	overPayload, err := Collect(testClaim(), mustLoadReport(t, "complete.json"), CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://example.invalid/source/template",
		Limits:     ResourceUse{MaxRecordsPerClaim: 5000, MaxPayloadBytes: 10, ClaimTimeoutSeconds: 300},
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if overPayload.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal for payload-budget breach", overPayload.State)
	}

	expired := testClaim()
	expired.Deadline = testObservedAt().Add(-time.Minute)
	pastDeadline, err := Collect(expired, mustLoadReport(t, "complete.json"), CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://example.invalid/source/template",
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if pastDeadline.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal past the claim deadline", pastDeadline.State)
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
	foundReason := false
	for _, fact := range partial.Facts {
		if fact.Kind == FactKindWarning && fact.Payload["reason"] == "source-degraded" {
			foundReason = true
		}
	}
	if !foundReason {
		t.Fatalf("partial result missing warning fact carrying the cause: %#v", partial.Facts)
	}
	withheld, err := CollectPartial(testClaim(), mustLoadReport(t, "partial.json"),
		CollectOptions{ObservedAt: testObservedAt(), SourceURI: "https://example.invalid/source/template"}, "password=hunter2")
	if err != nil {
		t.Fatalf("CollectPartial() error = %v", err)
	}
	for _, fact := range withheld.Facts {
		raw, _ := json.Marshal(fact.Payload)
		if strings.Contains(string(raw), "hunter2") {
			t.Fatalf("partial warning leaks unshareable cause: %s", raw)
		}
	}
}

// TestSnapshotPayloadBound proves an unbounded source string cannot slip
// past the payload budget inside the snapshot fact.
func TestSnapshotPayloadBound(t *testing.T) {
	t.Parallel()

	report := mustLoadReport(t, "complete.json")
	report.Source = strings.Repeat("s", 70000)
	result, err := Collect(testClaim(), report, CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://example.invalid/source/template",
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal for snapshot-budget breach", result.State)
	}
}

// TestPartialLimitsKeepCustomValues proves Collect defaults Limits per
// field: a partially customized Limits keeps the caller's values (gaps fall
// back to DefaultResourceUse) instead of discarding the whole struct. The
// complete fixture holds two records, so a custom MaxRecordsPerClaim of 1
// must go terminal while defaults would emit complete.
func TestPartialLimitsKeepCustomValues(t *testing.T) {
	t.Parallel()

	report := mustLoadReport(t, "complete.json")
	if len(report.Records) != 2 {
		t.Fatalf("complete.json holds %d records, want 2 for the custom-bound discriminator", len(report.Records))
	}
	result, err := Collect(testClaim(), report, CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://example.invalid/source/template",
		Limits:     ResourceUse{MaxRecordsPerClaim: 1},
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal: custom MaxRecordsPerClaim 1 lost", result.State)
	}
	if len(result.Statuses) != 1 || result.Statuses[0].FailureClass != "record-budget-exceeded" {
		t.Fatalf("Statuses = %#v, want record-budget-exceeded", result.Statuses)
	}

	result, err = Collect(testClaim(), report, CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://example.invalid/source/template",
		Limits:     ResourceUse{MaxPayloadBytes: 8},
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal: custom MaxPayloadBytes 8 lost", result.State)
	}
	if len(result.Statuses) != 1 || result.Statuses[0].FailureClass != "payload-budget-exceeded" {
		t.Fatalf("Statuses = %#v, want payload-budget-exceeded", result.Statuses)
	}
}

// TestLoadReportRefusesOversizedInput proves source documents past the read
// cap fail instead of decoding unbounded input.
func TestLoadReportRefusesOversizedInput(t *testing.T) {
	t.Parallel()

	oversized := strings.NewReader(`{"source":"x","records":[` + strings.Repeat(" ", maxReportBytes+1) + `]}`)
	if _, err := LoadReport(oversized); err == nil {
		t.Fatal("LoadReport(oversized) error = nil, want failure")
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
