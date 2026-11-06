// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// stdioTestEnv returns operator configuration for the stdio adapter with a
// valid shape: the adapter runs the same startup verification as the
// one-shot drain, while tests inject a stub extractor so no test needs a
// tesseract installation.
func stdioTestEnv(t *testing.T, artifactDir string) map[string]string {
	t.Helper()
	return map[string]string{
		envEnabled:       "true",
		envTier:          "qa",
		envArtifactDir:   artifactDir,
		envBinaryPath:    "/usr/bin/tesseract",
		envBinarySHA256:  strings.Repeat("a", 64),
		envLibraries:     "/usr/lib/libtesseract.so.5=" + strings.Repeat("b", 64),
		envTessdataDir:   "/usr/share/tessdata",
		envModelSHA256:   strings.Repeat("c", 64),
		envEngineVersion: "5.5.0",
	}
}

// stdioTestClaim builds one valid host claim for the document collector.
// The adapter echoes it verbatim; the SDK validator requires every field.
func stdioTestClaim() sdkcollector.Claim {
	return sdkcollector.Claim{
		ComponentID:   componentID,
		InstanceID:    "document-import-qa-0",
		CollectorKind: "documentation",
		SourceSystem:  "document_import",
		Scope:         sdkcollector.Scope{ID: "document-import-ocr-test-scope", Kind: "documentation_source"},
		SourceRunID:   "run-test",
		GenerationID:  "generation-test",
		WorkItemID:    "work-test",
		FencingToken:  "fence-test",
		Attempt:       1,
		Deadline:      time.Now().UTC().Add(5 * time.Minute),
		ConfigHandle:  "config://test/document-import",
	}
}

// stdioTestRequest marshals one host request carrying the claim.
func stdioTestRequest(t *testing.T, claim sdkcollector.Claim) []byte {
	t.Helper()
	raw, err := json.Marshal(extensionhost.Request{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Contract:        sdkcollector.Contract{ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1},
	})
	if err != nil {
		t.Fatalf("marshal test request: %v", err)
	}
	return raw
}

// recordDocGrants authorizes the test component for both documentation
// families, scoped to the documentation collector kind the host recheck
// uses. It mirrors the activation grant flow without a live backend.
func recordDocGrants(t *testing.T, home string) {
	t.Helper()
	registry := component.NewRegistry(home)
	for _, grant := range []component.ProducerGrant{
		{
			ProducerID:     componentID,
			Version:        componentVersion,
			Kind:           "documentation_document",
			SchemaVersions: []string{"1.0.0"},
			Scope:          "documentation",
			ExpiresAt:      time.Now().UTC().Add(24 * time.Hour),
		},
		{
			ProducerID:     componentID,
			Version:        componentVersion,
			Kind:           "documentation_section",
			SchemaVersions: []string{"1.1.0"},
			Scope:          "documentation",
			ExpiresAt:      time.Now().UTC().Add(24 * time.Hour),
		},
	} {
		if err := registry.RecordGrant(grant); err != nil {
			t.Fatalf("record grant for %s: %v", grant.Kind, err)
		}
	}
}

// stdioTestEngine returns a stub extractor emitting one OCR text region,
// proving the suite exercises the production extraction path.
func stdioTestEngine() *stubEngine {
	return &stubEngine{
		result: ocr.EngineResult{
			EngineName:    "stub",
			EngineVersion: "test",
			Regions: []ocr.Region{
				{RegionID: "r1", Text: "office supplies", Bounds: ocr.Bounds{Width: 0.5, Height: 0.1}, Confidence: 0.99},
			},
		},
	}
}

// decodeStdioResult decodes one adapter ack from the captured stdout.
func decodeStdioResult(t *testing.T, stdout *bytes.Buffer) sdkcollector.Result {
	t.Helper()
	var result sdkcollector.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode stdio result: %v\n%s", err, stdout.String())
	}
	return result
}

// TestSDKStdioClaimDrainAck proves one claimed generation drains the
// activation artifact dir through the existing drain and acks a complete
// SDK result carrying the drained documentation facts.
func TestSDKStdioClaimDrainAck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "receipt.png", decodePNG(t))
	work := t.TempDir()
	grantHome := t.TempDir()
	recordDocGrants(t, grantHome)
	engine := stdioTestEngine()

	var stdout bytes.Buffer
	err := serveSDKStdio(context.Background(), bytes.NewReader(stdioTestRequest(t, stdioTestClaim())), &stdout, sdkStdioDeps{
		getenv:       func(key string) string { return stdioTestEnv(t, dir)[key] },
		manifestPath: seedTestManifest(t, filepath.Join(work, "manifest.yaml")),
		grantHome:    grantHome,
		newExtractor: func(Config) (extractor, error) { return engine, nil },
	})
	if err != nil {
		t.Fatalf("serveSDKStdio: %v", err)
	}
	result := decodeStdioResult(t, &stdout)
	if result.State != sdkcollector.ResultComplete {
		t.Fatalf("result state = %q, want complete", result.State)
	}
	if result.Claim.GenerationID != "generation-test" {
		t.Fatalf("result claim generation = %q, want the echoed claim", result.Claim.GenerationID)
	}
	if result.Generation.ID != result.Claim.GenerationID {
		t.Fatalf("result generation = %q, want the claim generation %q", result.Generation.ID, result.Claim.GenerationID)
	}
	if len(result.Facts) == 0 {
		t.Fatal("result carries zero facts, want the drained documentation facts")
	}
	for _, fact := range result.Facts {
		if fact.Kind != "documentation_document" && fact.Kind != "documentation_section" {
			t.Fatalf("fact kind = %q, want a declared documentation family", fact.Kind)
		}
	}
	if len(result.Statuses) != 1 || result.Statuses[0].Class != sdkcollector.StatusComplete {
		t.Fatalf("result statuses = %+v, want one complete row", result.Statuses)
	}
	if result.Statuses[0].FactCount != len(result.Facts) {
		t.Fatalf("status fact count = %d, want %d", result.Statuses[0].FactCount, len(result.Facts))
	}
}

// TestSDKStdioEmptyDrainAcksUnchanged proves an empty artifact dir acks
// unchanged with zero facts instead of an error.
func TestSDKStdioEmptyDrainAcksUnchanged(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	grantHome := t.TempDir()
	recordDocGrants(t, grantHome)

	var stdout bytes.Buffer
	err := serveSDKStdio(context.Background(), bytes.NewReader(stdioTestRequest(t, stdioTestClaim())), &stdout, sdkStdioDeps{
		getenv:       func(key string) string { return stdioTestEnv(t, t.TempDir())[key] },
		manifestPath: seedTestManifest(t, filepath.Join(work, "manifest.yaml")),
		grantHome:    grantHome,
		newExtractor: func(Config) (extractor, error) { return stdioTestEngine(), nil },
	})
	if err != nil {
		t.Fatalf("serveSDKStdio: %v", err)
	}
	result := decodeStdioResult(t, &stdout)
	if result.State != sdkcollector.ResultUnchanged {
		t.Fatalf("result state = %q, want unchanged", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("result carries %d facts, want zero", len(result.Facts))
	}
}

// TestSDKStdioGrantDeniedAcksTerminal proves a grant-less activation acks a
// terminal denial without a live backend: core-owned documentation kinds
// never emit without a live producer grant.
func TestSDKStdioGrantDeniedAcksTerminal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "receipt.png", decodePNG(t))
	work := t.TempDir()

	var stdout bytes.Buffer
	err := serveSDKStdio(context.Background(), bytes.NewReader(stdioTestRequest(t, stdioTestClaim())), &stdout, sdkStdioDeps{
		getenv:       func(key string) string { return stdioTestEnv(t, dir)[key] },
		manifestPath: seedTestManifest(t, filepath.Join(work, "manifest.yaml")),
		grantHome:    t.TempDir(),
		newExtractor: func(Config) (extractor, error) { return stdioTestEngine(), nil },
	})
	if err != nil {
		t.Fatalf("serveSDKStdio on a denied grant: %v (want a terminal ack, not a process failure)", err)
	}
	result := decodeStdioResult(t, &stdout)
	if result.State != sdkcollector.ResultTerminal {
		t.Fatalf("result state = %q, want terminal", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("denied result carries %d facts, want zero", len(result.Facts))
	}
	denied := false
	for _, status := range result.Statuses {
		if status.Class == sdkcollector.StatusFailure && status.FailureClass == grantDeniedFailureClass {
			denied = true
		}
	}
	if !denied {
		t.Fatalf("result statuses = %+v, want a grant_denied failure row", result.Statuses)
	}
	if result.Claim.GenerationID != "generation-test" {
		t.Fatalf("denied result drops the claim echo, want the claim carried for host accounting")
	}
}

// TestSDKStdioRejectsBadInput proves corrupt requests, unsupported
// protocols, and a disabled collector fail the process instead of acking.
func TestSDKStdioRejectsBadInput(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	grantHome := t.TempDir()
	recordDocGrants(t, grantHome)
	deps := func() sdkStdioDeps {
		return sdkStdioDeps{
			getenv:       func(key string) string { return stdioTestEnv(t, t.TempDir())[key] },
			manifestPath: seedTestManifest(t, filepath.Join(work, "manifest.yaml")),
			grantHome:    grantHome,
			newExtractor: func(Config) (extractor, error) { return stdioTestEngine(), nil },
		}
	}
	badProtocol, err := json.Marshal(extensionhost.Request{
		ProtocolVersion: "collector-sdk/v9",
		Claim:           stdioTestClaim(),
	})
	if err != nil {
		t.Fatalf("marshal bad-protocol request: %v", err)
	}
	cases := map[string][]byte{
		"malformed json": []byte("{not json"),
		"empty":          {},
		"bad protocol":   badProtocol,
		"trailing data":  append(stdioTestRequest(t, stdioTestClaim()), []byte("{}")...),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			if err := serveSDKStdio(context.Background(), bytes.NewReader(input), &stdout, deps()); err == nil {
				t.Fatalf("serveSDKStdio(%s): got nil error, want a failure", name)
			}
		})
	}
	t.Run("disabled collector", func(t *testing.T) {
		t.Parallel()
		d := deps()
		d.getenv = func(string) string { return "" }
		var stdout bytes.Buffer
		if err := serveSDKStdio(context.Background(), bytes.NewReader(stdioTestRequest(t, stdioTestClaim())), &stdout, d); err == nil {
			t.Fatal("serveSDKStdio with a disabled collector: got nil error, want the startup verification failure")
		}
	})
}
