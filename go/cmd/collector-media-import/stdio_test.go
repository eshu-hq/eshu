// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// stdioClaimRequest builds one host claim envelope over stdin: the claim the
// satellite must drain exactly one generation for and echo verbatim in its
// ack.
func stdioClaimRequest(t *testing.T, mutate func(*extensionhost.Request)) []byte {
	t.Helper()
	request := extensionhost.Request{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Contract: sdkcollector.Contract{
			ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
			Facts: []sdkcollector.FactDeclaration{
				{
					Kind:             "documentation_document",
					SchemaVersions:   []string{"1.0.0"},
					SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceObserved},
				},
				{
					Kind:             "documentation_section",
					SchemaVersions:   []string{"1.0.0", "1.1.0"},
					SourceConfidence: []sdkcollector.SourceConfidence{sdkcollector.SourceConfidenceObserved},
				},
			},
		},
		Claim: sdkcollector.Claim{
			ComponentID:   "dev.eshu.collector.media_import",
			InstanceID:    "media-import-seed-1",
			CollectorKind: "documentation",
			SourceSystem:  "media_import",
			Scope:         sdkcollector.Scope{ID: "media-import-transcript-test", Kind: "documentation_source"},
			SourceRunID:   "run-1",
			GenerationID:  "generation-1",
			WorkItemID:    "work-1",
			FencingToken:  "7",
			Attempt:       1,
			Deadline:      time.Now().UTC().Add(time.Hour),
			ConfigHandle:  "component-config:media-import",
		},
	}
	if mutate != nil {
		mutate(&request)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal claim request: %v", err)
	}
	return raw
}

// stdioEnv points the satellite at a temp artifact dir with pinned engine
// fixtures. The engine itself stays stubbed through openSource, so no test
// depends on a whisper.cpp installation; env and pin verification still run
// through the production loader.
func stdioEnv(dir string) map[string]string {
	return map[string]string{
		envEnabled:       "true",
		envTier:          "qa",
		envArtifactDir:   dir,
		envBinaryPath:    "/usr/bin/whisper-cli",
		envBinarySHA256:  strings.Repeat("a", 64),
		envModelPath:     "/models/ggml-base.bin",
		envModelSHA256:   strings.Repeat("b", 64),
		envEngineVersion: "v1.9.5",
	}
}

// stubSource opens the real directory source over the stub transcript
// engine: production scan, identity, and drain logic with a controlled
// engine result. It carries the adapter's own configured source system and
// scope prefix, exactly like the production opener, so the SDK contract
// check sees the same identity the host claim names.
func stubSource(engine *stubTranscriptEngine) func(Config, *slog.Logger) (collector.Source, error) {
	return func(cfg Config, logger *slog.Logger) (collector.Source, error) {
		return newDirSource(cfg.ArtifactDir, sourceOptions{
			engine:        engine,
			sourceSystem:  cfg.SourceSystem,
			scopePrefix:   cfg.ScopePrefix,
			maxInputBytes: cfg.MaxInputBytes,
			maxArtifacts:  cfg.MaxArtifacts,
		}, logger)
	}
}

// decodeStdioResult decodes the satellite's ack from stdout.
func decodeStdioResult(t *testing.T, stdout *bytes.Buffer) sdkcollector.Result {
	t.Helper()
	var result sdkcollector.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode stdio result: %v\nstdout: %s", err, stdout.String())
	}
	return result
}

// TestStdioClaimDrainsToAck proves the thin claim adapter path: one claimed
// generation drains the activation artifact dir through the existing source
// and drain, projects the committed envelopes onto the SDK wire shape, and
// acks with the claim echoed verbatim plus a complete status row.
func TestStdioClaimDrainsToAck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "standup.wav", encodeWAV(t))
	grantHome := t.TempDir()
	recordMediaGrants(t, grantHome)
	engine := &stubTranscriptEngine{result: benignResult()}
	stdin := bytes.NewReader(stdioClaimRequest(t, nil))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(grantedEnv(dir, grantHome)), stubSource(engine)); err != nil {
		t.Fatalf("serveStdioClaim: %v", err)
	}
	result := decodeStdioResult(t, &stdout)
	if result.ProtocolVersion != sdkcollector.ProtocolVersionV1Alpha1 {
		t.Fatalf("protocol = %q, want %q", result.ProtocolVersion, sdkcollector.ProtocolVersionV1Alpha1)
	}
	if result.Claim.GenerationID != "generation-1" {
		t.Fatalf("ack claim generation = %q, want the claimed generation echoed", result.Claim.GenerationID)
	}
	if result.State != sdkcollector.ResultComplete {
		t.Fatalf("state = %q, want complete for a drained generation", result.State)
	}
	if len(result.Facts) == 0 {
		t.Fatal("ack carries zero facts, want the drained transcript facts")
	}
	if joined := strings.Join(factKinds(result), ","); !strings.Contains(joined, "documentation_section") {
		t.Fatalf("ack fact kinds %q do not carry the transcript sections", joined)
	}
	if result.Generation.ID != "generation-1" {
		t.Fatalf("ack generation = %q, want the claimed generation", result.Generation.ID)
	}
	complete := false
	for _, status := range result.Statuses {
		if status.Class == sdkcollector.StatusComplete && status.FactCount == len(result.Facts) {
			complete = true
		}
	}
	if !complete {
		t.Fatalf("ack statuses %+v carry no complete row counting %d facts", result.Statuses, len(result.Facts))
	}
}

func factKinds(result sdkcollector.Result) []string {
	kinds := make([]string, 0, len(result.Facts))
	for _, fact := range result.Facts {
		kinds = append(kinds, fact.Kind)
	}
	return kinds
}

// TestStdioRejectsForeignProtocol proves the adapter fails closed when the
// host speaks anything other than the manifest's pinned SDK protocol.
func TestStdioRejectsForeignProtocol(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stdin := bytes.NewReader(stdioClaimRequest(t, func(request *extensionhost.Request) {
		request.ProtocolVersion = "collector-sdk/v9alpha9"
	}))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(stdioEnv(dir)), stubSource(&stubTranscriptEngine{})); err == nil {
		t.Fatal("foreign protocol: got nil error, want a failure")
	} else if !strings.Contains(err.Error(), "protocol_version") {
		t.Fatalf("foreign protocol: error %q does not name protocol_version", err)
	}
}

// TestStdioRejectsUnnamedGeneration proves the adapter fails closed when the
// claim names no generation: without an identity there is nothing to drain
// and nothing the ack could echo.
func TestStdioRejectsUnnamedGeneration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stdin := bytes.NewReader(stdioClaimRequest(t, func(request *extensionhost.Request) {
		request.Claim.GenerationID = ""
	}))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(stdioEnv(dir)), stubSource(&stubTranscriptEngine{})); err == nil {
		t.Fatal("blank generation: got nil error, want a failure")
	}
}

// TestStdioDisabledFailsClosed proves the satellite stays inert without an
// explicit opt-in, exactly like the one-shot drain: no claim can enable it
// through stdin alone.
func TestStdioDisabledFailsClosed(t *testing.T) {
	t.Parallel()

	stdin := bytes.NewReader(stdioClaimRequest(t, nil))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(map[string]string{envEnabled: "false"}), stubSource(&stubTranscriptEngine{})); err == nil {
		t.Fatal("disabled satellite: got nil error, want a failure")
	}
}

// TestStdioMissingArtifactDirFailsClosed proves a claim cannot redirect the
// drain at an implicit location: without the absolute artifact dir the
// adapter fails closed before any source is opened.
func TestStdioMissingArtifactDirFailsClosed(t *testing.T) {
	t.Parallel()

	pairs := stdioEnv(filepath.Join(t.TempDir(), "does-not-exist"))
	delete(pairs, envArtifactDir)
	stdin := bytes.NewReader(stdioClaimRequest(t, nil))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(pairs), stubSource(&stubTranscriptEngine{})); err == nil {
		t.Fatal("missing artifact dir: got nil error, want a failure")
	} else if !strings.Contains(err.Error(), envArtifactDir) {
		t.Fatalf("missing artifact dir: error %q does not name %s", err, envArtifactDir)
	}
}

// TestStdioMalformedRequestFailsClosed proves trailing garbage on stdin
// fails the claim instead of draining against a partially decoded request.
func TestStdioMalformedRequestFailsClosed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	raw := append(stdioClaimRequest(t, nil), "{}"...)
	stdin := bytes.NewReader(raw)
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(stdioEnv(dir)), stubSource(&stubTranscriptEngine{})); err == nil {
		t.Fatal("trailing JSON: got nil error, want a failure")
	}
}

// TestStdioContractRefusalFailsClosed proves a grant-less activation acks a
// terminal denial instead of draining: core-owned transcript families need a
// live producer grant, so without one the adapter terminal-fails the claim
// with grant_denied and emits zero facts.
func TestStdioContractRefusalFailsClosed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "standup.wav", encodeWAV(t))
	stdin := bytes.NewReader(stdioClaimRequest(t, nil))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(grantedEnv(dir, t.TempDir())), stubSource(&stubTranscriptEngine{result: benignResult()})); err != nil {
		t.Fatalf("serveStdioClaim on a grant-less activation: %v (want a terminal ack, not a process failure)", err)
	}
	result := decodeStdioResult(t, &stdout)
	if result.State != sdkcollector.ResultTerminal {
		t.Fatalf("result state = %q, want terminal for a grant-less activation", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("denied result carries %d facts, want zero", len(result.Facts))
	}
}

// TestStdioEmptyDirAcksUnchanged proves an empty activation dir drains to an
// unchanged ack with zero facts: the claim completes with no fact, reducer,
// or graph delta, mirroring the zero-fact emitter semantics.
func TestStdioEmptyDirAcksUnchanged(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	grantHome := t.TempDir()
	recordMediaGrants(t, grantHome)
	stdin := bytes.NewReader(stdioClaimRequest(t, nil))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(grantedEnv(dir, grantHome)), stubSource(&stubTranscriptEngine{result: benignResult()})); err != nil {
		t.Fatalf("serveStdioClaim on empty dir: %v", err)
	}
	result := decodeStdioResult(t, &stdout)
	if result.State != sdkcollector.ResultUnchanged {
		t.Fatalf("state = %q, want unchanged for an empty drain", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("facts = %d, want zero for an empty drain", len(result.Facts))
	}
}

// TestStdioSourceErrorFailsClosed proves a source-construction failure (here
// a missing artifact directory) fails the claim with no ack on stdout.
func TestStdioSourceErrorFailsClosed(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "does-not-exist")
	grantHome := t.TempDir()
	recordMediaGrants(t, grantHome)
	stdin := bytes.NewReader(stdioClaimRequest(t, nil))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(grantedEnv(dir, grantHome)), stubSource(&stubTranscriptEngine{})); err == nil {
		t.Fatal("missing dir: got nil error, want a failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed claim wrote %d ack bytes, want silence (fail closed, no partial ack)", stdout.Len())
	}
}

// recordMediaGrants authorizes the media test component for both
// documentation families, scoped to the documentation collector kind.
func recordMediaGrants(t *testing.T, home string) {
	t.Helper()
	registry := component.NewRegistry(home)
	for _, grant := range []component.ProducerGrant{
		{
			ProducerID:     mediaComponentID,
			Version:        "0.1.0",
			Kind:           "documentation_document",
			SchemaVersions: []string{"1.0.0"},
			Scope:          "documentation",
			ExpiresAt:      time.Now().UTC().Add(24 * time.Hour),
		},
		{
			ProducerID:     mediaComponentID,
			Version:        "0.1.0",
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

// grantedEnv returns the stdio env with live grants wired through
// ESHU_COMPONENT_HOME.
func grantedEnv(dir, grantHome string) map[string]string {
	pairs := stdioEnv(dir)
	pairs["ESHU_COMPONENT_HOME"] = grantHome
	return pairs
}

// TestStdioGrantDeniedAcksTerminal RED: a grant-less activation must ack a
// terminal denial with grant_denied instead of burning retry budget.
func TestStdioGrantDeniedAcksTerminal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "standup.wav", encodeWAV(t))
	stdin := bytes.NewReader(stdioClaimRequest(t, nil))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(grantedEnv(dir, t.TempDir())), stubSource(&stubTranscriptEngine{result: benignResult()})); err != nil {
		t.Fatalf("serveStdioClaim on a denied grant: %v (want a terminal ack, not a process failure)", err)
	}
	result := decodeStdioResult(t, &stdout)
	if result.State != sdkcollector.ResultTerminal {
		t.Fatalf("result state = %q, want terminal for a revoked grant", result.State)
	}
	denied := false
	for _, status := range result.Statuses {
		if status.Class == sdkcollector.StatusFailure && status.FailureClass == "grant_denied" {
			denied = true
		}
	}
	if !denied {
		t.Fatalf("result statuses = %+v, want a grant_denied failure row", result.Statuses)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("denied result carries %d facts, want zero", len(result.Facts))
	}
}

// TestStdioClaimRekeysSourceSystem RED: the ack must bind every fact to the
// claim's SourceSystem, not the drain default.
func TestStdioClaimRekeysSourceSystem(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "standup.wav", encodeWAV(t))
	grantHome := t.TempDir()
	recordMediaGrants(t, grantHome)
	stdin := bytes.NewReader(stdioClaimRequest(t, func(request *extensionhost.Request) {
		request.Claim.SourceSystem = "claim-system-xyz"
	}))
	var stdout bytes.Buffer
	if err := serveStdioClaim(context.Background(), stdin, &stdout, testGetenv(grantedEnv(dir, grantHome)), stubSource(&stubTranscriptEngine{result: benignResult()})); err != nil {
		t.Fatalf("serveStdioClaim: %v", err)
	}
	result := decodeStdioResult(t, &stdout)
	if len(result.Facts) == 0 {
		t.Fatal("ack carries zero facts, want the drained transcript facts")
	}
	for _, fact := range result.Facts {
		if fact.SourceRef.SourceSystem != "claim-system-xyz" {
			t.Fatalf("fact SourceSystem = %q, want the claim SourceSystem %q", fact.SourceRef.SourceSystem, "claim-system-xyz")
		}
	}
}

// TestRunSDKStdioCommandRejectsExtraArgs RED: --sdk-stdio takes no other
// arguments; extras fail closed instead of serving under surprising flags.
func TestRunSDKStdioCommandRejectsExtraArgs(t *testing.T) {
	t.Parallel()

	if err := runSDKStdioCommand(context.Background(), []string{"--sdk-stdio", "extra"}); err == nil {
		t.Fatal("runSDKStdioCommand with an extra arg: got nil error, want a usage failure")
	}
}
