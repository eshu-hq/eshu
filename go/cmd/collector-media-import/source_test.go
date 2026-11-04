// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// stubTranscriptEngine is a scripted media.Engine. Tests inject it so no test
// depends on a whisper.cpp installation; the pinned backend is wired only in
// main.
type stubTranscriptEngine struct {
	mu              sync.Mutex
	extractCalls    int
	transcribeCalls int
	result          docmedia.EngineResult
	err             error
	received        []docmedia.Media
}

func (s *stubTranscriptEngine) Transcribe(_ context.Context, media docmedia.Media) (docmedia.EngineResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcribeCalls++
	s.received = append(s.received, media)
	return s.result, s.err
}

// Extract mirrors the pinned backend's wrapper contract: it installs the
// engine on the request and runs the shared extraction path (preflight plus
// redaction), so the canary tests observe production behavior with a
// controlled engine result.
func (s *stubTranscriptEngine) Extract(ctx context.Context, req docmedia.Request) (docmedia.Result, error) {
	s.mu.Lock()
	s.extractCalls++
	s.mu.Unlock()
	req.Engine = s
	return docmedia.Extract(ctx, req)
}

func (s *stubTranscriptEngine) extractCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.extractCalls
}

func (s *stubTranscriptEngine) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transcribeCalls
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

// encodeWAV builds a minimal PCM WAV body that media preflight accepts as a
// real audio source, so Extract reaches the injected engine.
func encodeWAV(t *testing.T) []byte {
	t.Helper()
	const sampleRate, channels, bits, frames = 16000, 1, 16, 16000
	blockAlign := channels * bits / 8
	dataSize := frames * blockAlign
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bits))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataSize))
	for range frames {
		_ = binary.Write(&buf, binary.LittleEndian, int16(0))
	}
	return buf.Bytes()
}

func benignResult() docmedia.EngineResult {
	return docmedia.EngineResult{
		EngineName:    "stub",
		EngineVersion: "test",
		Language:      "en",
		Segments: []docmedia.Segment{
			{SegmentID: "s1", Text: "Restart the payments worker.", StartMillis: 0, EndMillis: 1500, Confidence: 0.9},
		},
	}
}

func testSourceOptions(engine extractor) sourceOptions {
	return sourceOptions{
		engine:        engine,
		sourceSystem:  "media_import_test",
		scopePrefix:   "media-import-transcript-test",
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
		testSourceOptions(&stubTranscriptEngine{}),
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
	if generations, envelopes := drain(t, t.TempDir(), &stubTranscriptEngine{}, committer); generations != 0 || envelopes != 0 {
		t.Fatalf("empty dir: got (%d, %d), want (0, 0)", generations, envelopes)
	}
}

// TestSourceHappyPathCommitsEnvelopes proves one audio artifact produces one
// generation whose envelopes flow unchanged to the committer.
func TestSourceHappyPathCommitsEnvelopes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "standup.wav", encodeWAV(t))
	engine := &stubTranscriptEngine{result: benignResult()}
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
	if joined := fmt.Sprintf("%v", committer.envelopes); !strings.Contains(joined, "Restart the payments worker") {
		t.Fatalf("committed envelopes do not carry the extracted section text")
	}
}

// TestSourceWithholdsPasswordCanary proves sensitive speech that reaches the
// engine never lands in committed envelopes on the full caller path.
func TestSourceWithholdsPasswordCanary(t *testing.T) {
	t.Parallel()

	const canary = "hunter2-canary-9zqx"
	dir := t.TempDir()
	writeArtifact(t, dir, "standup.wav", encodeWAV(t))
	engine := &stubTranscriptEngine{
		result: docmedia.EngineResult{
			EngineName:    "stub",
			EngineVersion: "test",
			Language:      "en",
			Segments: []docmedia.Segment{
				{SegmentID: "s1", Text: "Restart the payments worker.", StartMillis: 0, EndMillis: 1500, Confidence: 0.9},
				{SegmentID: "s2", Text: "My password is " + canary, StartMillis: 1500, EndMillis: 3000, Confidence: 0.9},
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

// TestSourceWithholdsEmailCanary proves a spoken contact address is withheld
// the same way on the full caller path.
func TestSourceWithholdsEmailCanary(t *testing.T) {
	t.Parallel()

	const canary = "alice.canary@example.com"
	dir := t.TempDir()
	writeArtifact(t, dir, "standup.wav", encodeWAV(t))
	engine := &stubTranscriptEngine{
		result: docmedia.EngineResult{
			EngineName:    "stub",
			EngineVersion: "test",
			Language:      "en",
			Segments: []docmedia.Segment{
				{SegmentID: "s1", Text: "Restart the payments worker.", StartMillis: 0, EndMillis: 1500, Confidence: 0.9},
				{SegmentID: "s2", Text: "Mail " + canary + " today", StartMillis: 1500, EndMillis: 3000, Confidence: 0.9},
			},
		},
	}
	committer := &fakeCommitter{}
	if generations, envelopes := drain(t, dir, engine, committer); generations != 1 || envelopes == 0 {
		t.Fatalf("email canary run: got (%d, %d), want (1, >0)", generations, envelopes)
	}
	if joined := fmt.Sprintf("%v", committer.envelopes); strings.Contains(joined, canary) {
		t.Fatalf("committed envelopes contain the email canary")
	}
}

// TestSourceNonAudioCommitsSkippedRecord proves a non-audio file is an honest
// skipped observation, not a crash and not a transcript attempt: default-off
// formats stay off, so only WAV reaches the engine.
func TestSourceNonAudioCommitsSkippedRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeArtifact(t, dir, "notes.txt", []byte("just some text, not audio"))
	engine := &stubTranscriptEngine{}
	committer := &fakeCommitter{}
	if generations, _ := drain(t, dir, engine, committer); generations != 1 {
		t.Fatalf("non-audio: generations = %d, want 1 skipped record", generations)
	}
	if engine.callCount() != 0 {
		t.Fatalf("non-audio: engine calls = %d, want 0 (preflight skips first)", engine.callCount())
	}
	if joined := fmt.Sprintf("%v", committer.envelopes); !strings.Contains(joined, "skipped") {
		t.Fatalf("non-audio: envelopes %v do not record a skipped status", joined)
	}
}

// TestSourceSkipsSymlink proves a symlink inside the artifact directory is
// skipped, closing the path-escape hostile case.
func TestSourceSkipsSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outside := t.TempDir()
	writeArtifact(t, outside, "secret.wav", encodeWAV(t))
	if err := os.Symlink(filepath.Join(outside, "secret.wav"), filepath.Join(dir, "link.wav")); err != nil {
		t.Fatalf("symlink fixture: %v", err)
	}
	committer := &fakeCommitter{}
	if generations, _ := drain(t, dir, &stubTranscriptEngine{}, committer); generations != 0 {
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
	writeArtifact(t, dir, "big.wav", big)
	opts := testSourceOptions(&stubTranscriptEngine{})
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
	body := encodeWAV(t)
	writeArtifact(t, dir, "a.wav", body)
	writeArtifact(t, dir, "b.wav", body)
	engine := &stubTranscriptEngine{result: benignResult()}
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
	writeArtifact(t, dir, "a.wav", encodeWAV(t))
	src, err := newDirSource(dir, testSourceOptions(&stubTranscriptEngine{}), slog.Default())
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
	writeArtifact(t, dir, "a.wav", encodeWAV(t))
	engine := &stubTranscriptEngine{result: benignResult()}
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
	writeArtifact(t, dir, "ok.wav", encodeWAV(t))
	writeArtifact(t, dir, "skip.txt", []byte("not audio"))
	engine := &stubTranscriptEngine{result: benignResult()}
	committer := &fakeCommitter{}
	if generations, _ := drain(t, dir, engine, committer); generations != 2 {
		t.Fatalf("wrapper route: generations = %d, want 2 (audio plus skipped record)", generations)
	}
	if got := engine.extractCount(); got != 2 {
		t.Fatalf("wrapper route: Extract calls = %d, want 2", got)
	}
	if got := engine.callCount(); got != 1 {
		t.Fatalf("wrapper route: engine Transcribe calls = %d, want 1 (preflight skips the text file)", got)
	}
}

// TestSourceRespectsArtifactCap proves the per-run bound holds with
// streaming residency: the first artifact drains, the rest count over cap,
// and the engine never sees them.
func TestSourceRespectsArtifactCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := encodeWAV(t)
	second := append(append([]byte(nil), first...), 0x00)
	writeArtifact(t, dir, "a.wav", first)
	writeArtifact(t, dir, "b.wav", second)
	opts := testSourceOptions(&stubTranscriptEngine{result: benignResult()})
	opts.maxArtifacts = 1
	engine := opts.engine.(*stubTranscriptEngine)
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
	writeArtifact(t, dir, "a.wav", encodeWAV(t))
	engine := &stubTranscriptEngine{result: benignResult()}
	src, err := newDirSource(dir, testSourceOptions(engine), slog.Default())
	if err != nil {
		t.Fatalf("newDirSource: %v", err)
	}
	replacement := encodeWAV(t)
	replacement[60] ^= 0xff
	writeArtifact(t, dir, "a.wav", replacement)
	committer := &fakeCommitter{}
	if _, _, err := drainOnce(context.Background(), src, committer, slog.Default()); err == nil {
		t.Fatalf("changed bytes: got nil error, want a stale-identity failure")
	}
	if committer.generations() != 0 {
		t.Fatalf("changed bytes: %d generations committed, want 0", committer.generations())
	}
}
