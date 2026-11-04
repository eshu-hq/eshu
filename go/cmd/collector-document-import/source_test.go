// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fixturePNG is a 1x1 transparent PNG: the smallest input picture.Preflight
// accepts as a real image, so Extract reaches the injected engine.
const fixturePNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// stubEngine is a scripted ocr.Engine. Tests inject it so no test depends on
// a tesseract installation; the pinned backend is wired only in main.
type stubEngine struct {
	mu           sync.Mutex
	extractCalls int
	recognCalls  int
	result       ocr.EngineResult
	err          error
	received     []ocr.Image
}

func (s *stubEngine) Recognize(_ context.Context, image ocr.Image) (ocr.EngineResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recognCalls++
	s.received = append(s.received, image)
	return s.result, s.err
}

// Extract mirrors the pinned backend's wrapper contract: it installs the
// engine on the request and runs the shared extraction path (preflight plus
// redaction), so the canary tests observe production behavior with a
// controlled engine result.
func (s *stubEngine) Extract(ctx context.Context, req ocr.Request) (ocr.Result, error) {
	s.mu.Lock()
	s.extractCalls++
	s.mu.Unlock()
	req.Engine = s
	return ocr.Extract(ctx, req)
}

func (s *stubEngine) extractCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.extractCalls
}

func (s *stubEngine) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recognCalls
}

// fakeCommitter captures committed generations without a database.
type fakeCommitter struct {
	mu        sync.Mutex
	scopes    []scope.IngestionScope
	gens      []scope.ScopeGeneration
	envelopes [][]facts.Envelope
}

func (f *fakeCommitter) CommitScopeGeneration(
	_ context.Context,
	sc scope.IngestionScope,
	gen scope.ScopeGeneration,
	ch <-chan facts.Envelope,
) error {
	var drained []facts.Envelope
	for env := range ch {
		drained = append(drained, env)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes = append(f.scopes, sc)
	f.gens = append(f.gens, gen)
	f.envelopes = append(f.envelopes, drained)
	return nil
}

func (f *fakeCommitter) generations() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.gens)
}

func writeArtifact(t *testing.T, dir, name string, body []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
		t.Fatalf("write artifact %s: %v", name, err)
	}
}

func decodePNG(t *testing.T) []byte {
	t.Helper()
	body, err := base64.StdEncoding.DecodeString(fixturePNG)
	if err != nil {
		t.Fatalf("decode fixture PNG: %v", err)
	}
	return body
}

func testSourceOptions(engine extractor) sourceOptions {
	return sourceOptions{
		engine:        engine,
		sourceSystem:  "document_import_test",
		scopePrefix:   "document-import-ocr-test",
		maxInputBytes: 1 << 20,
		maxArtifacts:  1024,
	}
}

func drain(t *testing.T, dir string, engine extractor, committer *fakeCommitter) (int, int) {
	t.Helper()
	src, err := newDirSource(dir, testSourceOptions(engine), slog.Default())
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}
	generations, envelopes, err := drainOnce(context.Background(), src, committer, slog.Default())
	if err != nil {
		t.Fatalf("drainOnce: %v", err)
	}
	return generations, envelopes
}

// TestSourceMissingDirFailsClosed proves an unreadable artifact directory is
// a startup error, not a silent empty drain.
func TestSourceMissingDirFailsClosed(t *testing.T) {
	t.Parallel()

	if _, err := newDirSource(
		filepath.Join(t.TempDir(), "does-not-exist"),
		testSourceOptions(&stubEngine{}),
		slog.Default(),
	); err == nil {
		t.Fatalf("newDirSource on a missing dir: got nil error, want a failure")
	}
}

// TestSourceEmptyDirCommitsNothing proves an empty artifact directory drains
// cleanly with zero commits.
func TestSourceEmptyDirCommitsNothing(t *testing.T) {
	t.Parallel()

	committer := &fakeCommitter{}
	if generations, envelopes := drain(t, t.TempDir(), &stubEngine{}, committer); generations != 0 || envelopes != 0 {
		t.Fatalf("empty dir: got (%d, %d), want (0, 0)", generations, envelopes)
	}
}

// TestSourceHappyPathCommitsEnvelopes proves one image artifact produces one
// generation whose envelopes flow unchanged to the committer.
func TestSourceHappyPathCommitsEnvelopes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "receipt.png", decodePNG(t))
	engine := &stubEngine{
		result: ocr.EngineResult{
			EngineName:    "stub",
			EngineVersion: "test",
			Regions: []ocr.Region{
				{RegionID: "r1", Text: "office supplies", Bounds: ocr.Bounds{Width: 0.5, Height: 0.1}, Confidence: 0.99},
			},
		},
	}
	committer := &fakeCommitter{}
	if generations, envelopes := drain(t, dir, engine, committer); generations != 1 || envelopes == 0 {
		t.Fatalf("happy path: got (%d, %d), want (1, >0)", generations, envelopes)
	}
	if engine.callCount() != 1 {
		t.Fatalf("engine calls = %d, want 1", engine.callCount())
	}
	if got := committer.scopes[0].CollectorKind; got != scope.CollectorDocumentation {
		t.Fatalf("collector kind = %q, want %q", got, scope.CollectorDocumentation)
	}
	// Non-vacuity pin: persisted section text reaches the envelopes, so the
	// canary tests below would observe a leak instead of passing blind.
	if joined := fmt.Sprintf("%v", committer.envelopes); !strings.Contains(joined, "office supplies") {
		t.Fatalf("committed envelopes do not carry the extracted section text")
	}
}

// TestSourceWithholdsPasswordCanary proves a sensitive secret that reaches the
// engine never lands in committed envelopes on the full caller path.
func TestSourceWithholdsPasswordCanary(t *testing.T) {
	t.Parallel()

	const canary = "hunter2-canary-9zqx"
	dir := t.TempDir()
	writeArtifact(t, dir, "note.png", decodePNG(t))
	engine := &stubEngine{
		result: ocr.EngineResult{
			EngineName:    "stub",
			EngineVersion: "test",
			Regions: []ocr.Region{
				{RegionID: "r1", Text: "the password is " + canary, Bounds: ocr.Bounds{Width: 0.5, Height: 0.1}, Confidence: 0.99},
			},
		},
	}
	committer := &fakeCommitter{}
	if _, envelopes := drain(t, dir, engine, committer); envelopes == 0 {
		t.Fatalf("canary run: zero envelopes, want the redacted document record")
	}
	joined := fmt.Sprintf("%v", committer.envelopes)
	if strings.Contains(joined, canary) {
		t.Fatalf("committed envelopes contain the canary secret")
	}
}

// TestSourceWithholdsSpokenAPIKeyCanary proves the spoken form of a credential
// label is withheld the same way on the full caller path.
func TestSourceWithholdsSpokenAPIKeyCanary(t *testing.T) {
	t.Parallel()

	const canary = "ak-canary-7wqm"
	dir := t.TempDir()
	writeArtifact(t, dir, "key.png", decodePNG(t))
	engine := &stubEngine{
		result: ocr.EngineResult{
			EngineName:    "stub",
			EngineVersion: "test",
			Regions: []ocr.Region{
				{RegionID: "r1", Text: "the API key is " + canary, Bounds: ocr.Bounds{Width: 0.5, Height: 0.1}, Confidence: 0.99},
			},
		},
	}
	committer := &fakeCommitter{}
	if generations, envelopes := drain(t, dir, engine, committer); generations != 1 || envelopes == 0 {
		t.Fatalf("spoken canary run: got (%d, %d), want (1, >0)", generations, envelopes)
	}
	if joined := fmt.Sprintf("%v", committer.envelopes); strings.Contains(joined, canary) {
		t.Fatalf("committed envelopes contain the API key canary")
	}
}

// TestSourceNonImageCommitsSkippedRecord proves a non-image file is an honest
// skipped observation, not a crash and not an OCR attempt.
func TestSourceNonImageCommitsSkippedRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "notes.txt", []byte("just some text, not an image"))
	engine := &stubEngine{}
	committer := &fakeCommitter{}
	if generations, _ := drain(t, dir, engine, committer); generations != 1 {
		t.Fatalf("non-image: generations = %d, want 1 skipped record", generations)
	}
	if engine.callCount() != 0 {
		t.Fatalf("non-image: engine calls = %d, want 0 (preflight skips first)", engine.callCount())
	}
	if joined := fmt.Sprintf("%v", committer.envelopes); !strings.Contains(joined, "skipped") {
		t.Fatalf("non-image: envelopes %v do not record a skipped status", joined)
	}
}

// TestSourceSkipsSymlink proves a symlink inside the artifact directory is
// skipped, closing the path-escape hostile case.
func TestSourceSkipsSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outside := t.TempDir()
	writeArtifact(t, outside, "secret.png", decodePNG(t))
	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(dir, "link.png")); err != nil {
		t.Fatalf("symlink fixture: %v", err)
	}
	committer := &fakeCommitter{}
	if generations, _ := drain(t, dir, &stubEngine{}, committer); generations != 0 {
		t.Fatalf("symlink: generations = %d, want 0", generations)
	}
}

// TestSourceSkipsOversize proves a file past the input cap is skipped without
// failing the batch.
func TestSourceSkipsOversize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	big := make([]byte, 2048)
	for i := range big {
		big[i] = byte(i)
	}
	writeArtifact(t, dir, "big.png", big)
	opts := testSourceOptions(&stubEngine{})
	opts.maxInputBytes = 64
	src, err := newDirSource(dir, opts, slog.Default())
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}
	committer := &fakeCommitter{}
	generations, _, err := drainOnce(context.Background(), src, committer, slog.Default())
	if err != nil {
		t.Fatalf("drainOnce: %v", err)
	}
	if generations != 0 {
		t.Fatalf("oversize: generations = %d, want 0", generations)
	}
}

// TestSourceDuplicateContentCommitsOneGeneration proves identical bytes in
// two files produce one generation: identity derives from content.
func TestSourceDuplicateContentCommitsOneGeneration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := decodePNG(t)
	writeArtifact(t, dir, "a.png", body)
	writeArtifact(t, dir, "b.png", body)
	engine := &stubEngine{
		result: ocr.EngineResult{
			EngineName:    "stub",
			EngineVersion: "test",
			Regions: []ocr.Region{
				{RegionID: "r1", Text: "same content", Bounds: ocr.Bounds{Width: 0.5, Height: 0.1}, Confidence: 0.99},
			},
		},
	}
	committer := &fakeCommitter{}
	if generations, _ := drain(t, dir, engine, committer); generations != 1 {
		t.Fatalf("duplicate content: generations = %d, want 1", generations)
	}
	if engine.callCount() != 1 {
		t.Fatalf("duplicate content: engine calls = %d, want 1", engine.callCount())
	}
}

// TestSourceCancelledContextCommitsNothing proves cancellation stops the
// drain before any commit: no partial generation is persisted.
func TestSourceCancelledContextCommitsNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "a.png", decodePNG(t))
	src, err := newDirSource(dir, testSourceOptions(&stubEngine{}), slog.Default())
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	committer := &fakeCommitter{}
	if _, _, err := drainOnce(ctx, src, committer, slog.Default()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled drain: error = %v, want context.Canceled", err)
	}
	if committer.generations() != 0 {
		t.Fatalf("cancelled drain: %d generations committed, want 0", committer.generations())
	}
}

// TestSourceSameContentSameGeneration proves re-runs derive the same
// generation identity: the second drain over identical bytes names the same
// generation, so the store can treat it as already observed.
func TestSourceSameContentSameGeneration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "a.png", decodePNG(t))
	engine := &stubEngine{}
	first := &fakeCommitter{}
	drain(t, dir, engine, first)
	second := &fakeCommitter{}
	drain(t, dir, engine, second)
	if first.gens[0].GenerationID != second.gens[0].GenerationID {
		t.Fatalf("generation IDs differ across runs: %q vs %q",
			first.gens[0].GenerationID, second.gens[0].GenerationID)
	}
	if first.gens[0].ScopeID != second.gens[0].ScopeID {
		t.Fatalf("scope IDs differ across runs: %q vs %q",
			first.gens[0].ScopeID, second.gens[0].ScopeID)
	}
}

// TestSourceImplementsCollectorSource proves the adapter satisfies the shared
// commit boundary: drainOnce accepts it as a collector.Source, and this
// compile-time assertion pins the relationship.
var _ collector.Source = (*dirSource)(nil)

// TestSourceRoutesThroughExtractionWrapper proves the drain uses the
// telemetry-recording extraction entry point, not a bare engine call: every
// drained artifact passes through Extract exactly once, while the engine
// itself runs only when preflight accepts the bytes.
func TestSourceRoutesThroughExtractionWrapper(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "ok.png", decodePNG(t))
	writeArtifact(t, dir, "skip.txt", []byte("not an image"))
	engine := &stubEngine{}
	committer := &fakeCommitter{}
	if generations, _ := drain(t, dir, engine, committer); generations != 2 {
		t.Fatalf("wrapper route: generations = %d, want 2 (image plus skipped record)", generations)
	}
	if got := engine.extractCount(); got != 2 {
		t.Fatalf("wrapper route: Extract calls = %d, want 2", got)
	}
	if got := engine.callCount(); got != 1 {
		t.Fatalf("wrapper route: engine Recognize calls = %d, want 1 (preflight skips the text file)", got)
	}
}

// TestSourceRespectsArtifactCap proves the per-run bound holds with
// streaming residency: the first artifact drains, the rest count over cap,
// and the engine never sees them.
func TestSourceRespectsArtifactCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := decodePNG(t)
	second := append(append([]byte(nil), first...), 0x00)
	writeArtifact(t, dir, "a.png", first)
	writeArtifact(t, dir, "b.png", second)
	opts := testSourceOptions(&stubEngine{})
	opts.maxArtifacts = 1
	engine := opts.engine.(*stubEngine)
	src, err := newDirSource(dir, opts, slog.Default())
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}
	committer := &fakeCommitter{}
	generations, _, err := drainOnce(context.Background(), src, committer, slog.Default())
	if err != nil {
		t.Fatalf("drainOnce: %v", err)
	}
	if generations != 1 {
		t.Fatalf("capped drain: generations = %d, want 1", generations)
	}
	if got := engine.extractCount(); got != 1 {
		t.Fatalf("capped drain: Extract calls = %d, want 1", got)
	}
}

// TestSourceRejectsChangedBytes proves a file rewritten between scan and
// drain fails the run instead of committing under a stale content identity.
func TestSourceRejectsChangedBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "a.png", decodePNG(t))
	engine := &stubEngine{}
	src, err := newDirSource(dir, testSourceOptions(engine), slog.Default())
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}
	replacement := decodePNG(t)
	replacement[20] ^= 0xff
	writeArtifact(t, dir, "a.png", replacement)
	committer := &fakeCommitter{}
	if _, _, err := drainOnce(context.Background(), src, committer, slog.Default()); err == nil {
		t.Fatalf("changed bytes: got nil error, want a stale-identity failure")
	}
	if committer.generations() != 0 {
		t.Fatalf("changed bytes: %d generations committed, want 0", committer.generations())
	}
}
