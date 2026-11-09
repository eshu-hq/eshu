// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
)

// shadowClaim builds the host request the worker sends for one claimed SBOM
// scope: the sbom_document collector family, a bounded deadline, and a
// decimal fencing token. The emitter must return it verbatim.
func shadowClaim() sdkcollector.Claim {
	return sdkcollector.Claim{
		ComponentID:   "dev.eshu.collector.sbom-8877-shadow",
		InstanceID:    "sbom-8877-shadow-instance-1",
		CollectorKind: "sbom_document",
		SourceSystem:  "sbom_document",
		Scope:         sdkcollector.Scope{ID: "sbom://registry.example.com/library/example@sha256:1111", Kind: "image"},
		SourceRunID:   "run-1",
		GenerationID:  "gen-1",
		WorkItemID:    "work-1",
		FencingToken:  "7",
		Attempt:       1,
		Deadline:      time.Date(2026, time.November, 6, 4, 0, 0, 0, time.UTC),
		ConfigHandle:  "sbom-shadow-handle",
	}
}

func shadowRequest() extensionhost.Request {
	return extensionhost.Request{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Claim:           shadowClaim(),
		Contract:        sdkcollector.Contract{ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1},
	}
}

// TestServeReturnsCompleteWithPinnedFacts pins the leaf contract: one request
// in, one complete result out carrying every envelope the pinned lodash
// document yields. The per-kind counts mirror the document package's
// checked-in assertions (1 document with component_count 5, 5 components, 3
// dependency edges, 1 external reference, 3 warnings). The result carries no
// status rows: the single-call commit lane records none.
func TestServeReturnsCompleteWithPinnedFacts(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(shadowRequest())
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout bytes.Buffer
	if err := serve(bytes.NewReader(raw), &stdout); err != nil {
		t.Fatalf("serve() error = %v, want nil", err)
	}
	var result sdkcollector.Result
	decoder := json.NewDecoder(&stdout)
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if decoder.More() {
		t.Fatal("stdout must carry exactly one JSON value")
	}
	if result.State != sdkcollector.ResultComplete {
		t.Fatalf("result state = %q, want %q (unchanged rejects fact-bearing results)", result.State, sdkcollector.ResultComplete)
	}
	if got, want := len(result.Facts), 13; got != want {
		t.Fatalf("facts = %d, want %d (pinned lodash document envelope count)", got, want)
	}
	byKind := map[string]int{}
	for _, fact := range result.Facts {
		byKind[fact.Kind]++
	}
	wantKinds := map[string]int{
		"sbom.document":                1,
		"sbom.component":               5,
		"sbom.dependency_relationship": 3,
		"sbom.external_reference":      1,
		"sbom.warning":                 3,
	}
	for kind, want := range wantKinds {
		if got := byKind[kind]; got != want {
			t.Fatalf("kind %q facts = %d, want %d", kind, got, want)
		}
	}
	doc := findFact(t, result.Facts, "sbom.document")
	assertPayload(t, doc.Payload, "component_count", 5)
	assertPayload(t, doc.Payload, "spec_version", "1.5")
	assertPayload(t, doc.Payload, "parse_status", "parsed")
	assertPayload(t, doc.Payload, "subject_digest", "sha256:1111111111111111111111111111111111111111111111111111111111111111")
	lodash := findComponentByPURL(t, result.Facts, "pkg:npm/lodash@4.17.21")
	assertPayload(t, lodash.Payload, "name", "lodash")
	assertPayload(t, lodash.Payload, "version", "4.17.21")
	assertPayload(t, lodash.Payload, "package_id", "npm://registry.npmjs.org/lodash")
	// The single-call commit lane records no statuses, so the emitter
	// carries none: complete-class markers are admitted and dropped, and
	// zero statuses stay the preferred shape. The count stays derivable
	// from the facts.
	if len(result.Statuses) != 0 {
		t.Fatalf("statuses = %d, want none (commit lane records none)", len(result.Statuses))
	}
	if result.Generation.ID != "gen-1" {
		t.Fatalf("generation id = %q, want the claim generation", result.Generation.ID)
	}
	if result.Generation.ObservedAt.IsZero() {
		t.Fatal("generation observed_at must be set (host validation requires it)")
	}
	if result.Page != nil {
		t.Fatal("page envelope must stay absent: v1 never paginates")
	}
	for _, fact := range result.Facts {
		if fact.SourceRef.ScopeID != shadowClaim().Scope.ID {
			t.Fatalf("fact %q scope_id = %q, want claim scope", fact.Kind, fact.SourceRef.ScopeID)
		}
		if fact.SourceRef.GenerationID != "gen-1" {
			t.Fatalf("fact %q generation_id = %q, want claim generation", fact.Kind, fact.SourceRef.GenerationID)
		}
		if string(fact.SourceConfidence) != "reported" {
			t.Fatalf("fact %q confidence = %q, want reported (parser never verifies)", fact.Kind, fact.SourceConfidence)
		}
	}
}

// TestServeEchoesClaimIdentity proves the emitter returns the host claim
// verbatim: the host terminal-fails any identity mismatch before commit.
func TestServeEchoesClaimIdentity(t *testing.T) {
	t.Parallel()

	want := shadowRequest()
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout bytes.Buffer
	if err := serve(bytes.NewReader(raw), &stdout); err != nil {
		t.Fatalf("serve() error = %v, want nil", err)
	}
	var result sdkcollector.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	got, err := json.Marshal(result.Claim)
	if err != nil {
		t.Fatalf("marshal result claim: %v", err)
	}
	wantRaw, err := json.Marshal(want.Claim)
	if err != nil {
		t.Fatalf("marshal want claim: %v", err)
	}
	if !bytes.Equal(got, wantRaw) {
		t.Fatalf("claim echo mismatch:\n got %s\nwant %s", got, wantRaw)
	}
	if result.ProtocolVersion != want.ProtocolVersion {
		t.Fatalf("protocol = %q, want echoed %q", result.ProtocolVersion, want.ProtocolVersion)
	}
}

// TestServeRejectsMalformedInput proves an undecodable request fails closed
// with no result on stdout.
func TestServeRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if err := serve(strings.NewReader("{not json"), &stdout); err == nil {
		t.Fatal("serve() error = nil for malformed input, want failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %d bytes, want none on failure", stdout.Len())
	}
}

// TestServeRejectsOversizeInput proves stdin is bounded: a request past the
// cap fails closed instead of allocating unbounded input.
func TestServeRejectsOversizeInput(t *testing.T) {
	t.Parallel()

	big := bytes.Repeat([]byte("x"), maxRequestBytes+1)
	var stdout bytes.Buffer
	if err := serve(bytes.NewReader(big), &stdout); err == nil {
		t.Fatal("serve() error = nil for oversize input, want failure")
	}
}

// TestServeRejectsUnparsableFencingToken proves a non-decimal fencing token
// fails closed naming the knob, without echoing the offending value.
func TestServeRejectsUnparsableFencingToken(t *testing.T) {
	t.Parallel()

	request := shadowRequest()
	request.Claim.FencingToken = "not-a-number"
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout bytes.Buffer
	err = serve(bytes.NewReader(raw), &stdout)
	if err == nil {
		t.Fatal("serve() error = nil for unparsable fencing token, want failure")
	}
	if !strings.Contains(err.Error(), "claim.fencing_token") {
		t.Fatalf("error = %q, want it to name the claim.fencing_token knob", err)
	}
	if strings.Contains(err.Error(), "not-a-number") {
		t.Fatalf("error = %q, must not echo the offending value", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %d bytes, want none on failure", stdout.Len())
	}
}

// TestPinnedFixtureMatchesSource proves the baked-in document is a
// byte-identical copy of the lodash fixture behind the document package's
// checked-in assertions: a re-pin is deliberate, never drift.
func TestPinnedFixtureMatchesSource(t *testing.T) {
	t.Parallel()

	pinned, err := os.ReadFile(filepath.Join("testdata", "cyclonedx_image_subject.json"))
	if err != nil {
		t.Fatalf("read pinned fixture: %v", err)
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "internal", "collector", "sbom", "document", "testdata", "cyclonedx_image_subject.json"))
	if err != nil {
		t.Fatalf("read source fixture: %v", err)
	}
	if !bytes.Equal(pinned, source) {
		t.Fatalf("pinned fixture drifted from source: %d vs %d bytes; re-pin deliberately", len(pinned), len(source))
	}
	if !bytes.Equal(pinned, pinnedSBOM) {
		t.Fatal("embedded bytes differ from testdata copy; rebuild after any re-pin")
	}
}

// TestResultPassesHostValidation proves the emitted result clears the SDK
// host validator (the same gate the host runs before commit) under the SBOM
// contract the deployment declares: protocol echo, complete-with-facts
// allowance, claim-matching references, and payload policy.
func TestResultPassesHostValidation(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(shadowRequest())
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout bytes.Buffer
	if err := serve(bytes.NewReader(raw), &stdout); err != nil {
		t.Fatalf("serve() error = %v, want nil", err)
	}
	var result sdkcollector.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	contract := sdkcollector.Contract{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Facts: []sdkcollector.FactDeclaration{
			{Kind: "sbom.document", SchemaVersions: []string{"1.1.0"}, SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceReported}},
			{Kind: "sbom.component", SchemaVersions: []string{"1.1.0"}, SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceReported}},
			{Kind: "sbom.dependency_relationship", SchemaVersions: []string{"1.1.0"}, SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceReported}},
			{Kind: "sbom.external_reference", SchemaVersions: []string{"1.1.0"}, SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceReported}},
			{Kind: "sbom.warning", SchemaVersions: []string{"1.1.0"}, SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceReported}},
		},
	}
	report, err := sdkcollector.NewValidator(contract).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v, want host-admissible result", err)
	}
	if report.FactCount != len(result.Facts) {
		t.Fatalf("report fact count = %d, want %d", report.FactCount, len(result.Facts))
	}
}

func findFact(t *testing.T, facts []sdkcollector.Fact, kind string) sdkcollector.Fact {
	t.Helper()
	var found []sdkcollector.Fact
	for _, fact := range facts {
		if fact.Kind == kind {
			found = append(found, fact)
		}
	}
	if len(found) != 1 {
		t.Fatalf("kind %q facts = %d, want exactly 1 for payload assertions", kind, len(found))
	}
	return found[0]
}

func findComponentByPURL(t *testing.T, facts []sdkcollector.Fact, purl string) sdkcollector.Fact {
	t.Helper()
	for _, fact := range facts {
		if fact.Kind != "sbom.component" {
			continue
		}
		if got, _ := fact.Payload["purl"].(string); got == purl {
			if dup, _ := fact.Payload["is_duplicate"].(bool); !dup {
				return fact
			}
		}
	}
	t.Fatalf("no canonical component with purl %q", purl)
	return sdkcollector.Fact{}
}

// BenchmarkServeComplete measures the per-claim emitter cost: one bounded
// request decode, one pinned-document parse, and one 13-fact result encode.
func BenchmarkServeComplete(b *testing.B) {
	raw, err := json.Marshal(shadowRequest())
	if err != nil {
		b.Fatalf("marshal request: %v", err)
	}
	b.ReportMetric(float64(len(raw)), "input-bytes/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := serve(bytes.NewReader(raw), io.Discard); err != nil {
			b.Fatalf("serve() error = %v", err)
		}
	}
}

// TestServeStampsClaimSourceSystem proves attestation-path facts carry
// the claim's source system: the commit door requires every fact echo
// to match the claim, and the document parser's family default does
// not match an attestation claim. The scope and generation already
// follow the claim; the source system must follow it too.
func TestServeStampsClaimSourceSystem(t *testing.T) {
	t.Parallel()

	req := shadowRequest()
	req.Claim.SourceSystem = "sbom_attestation"
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout bytes.Buffer
	if err := serve(bytes.NewReader(raw), &stdout); err != nil {
		t.Fatalf("serve() error = %v, want nil", err)
	}
	var result sdkcollector.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.Facts) == 0 {
		t.Fatal("no facts, want the pinned emission to check")
	}
	for i, fact := range result.Facts {
		if fact.SourceRef.SourceSystem != "sbom_attestation" {
			t.Errorf("fact %d kind %q source_system = %q, want the claim's sbom_attestation",
				i, fact.Kind, fact.SourceRef.SourceSystem)
		}
	}
}

func assertPayload(t *testing.T, payload map[string]any, key string, want any) {
	t.Helper()
	got, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing key %q", key)
	}
	// Result facts round-trip through stdout JSON, so parser ints arrive
	// as float64. Compare numerically instead of by Go type.
	if wantNum, ok := want.(int); ok {
		gotNum, ok := got.(float64)
		if !ok || gotNum != float64(wantNum) {
			t.Fatalf("payload[%q] = %#v, want %d", key, got, wantNum)
		}
		return
	}
	if got != want {
		t.Fatalf("payload[%q] = %#v, want %#v", key, got, want)
	}
}
