// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build whispercpp_sandbox

package whispercpp

// The sandbox proof runs only inside the container built by
// scripts/verify-media-transcript-sandbox.sh, which compiles this file with
// -tags whispercpp_sandbox and passes the pinned binary, model, and fixture
// through the environment. It exercises the real whisper.cpp binary.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

func sandboxConfig(t *testing.T) Config {
	t.Helper()
	cfg := Config{
		BinaryPath:    os.Getenv("ESHU_WHISPERCPP_BINARY"),
		BinarySHA256:  os.Getenv("ESHU_WHISPERCPP_BINARY_SHA256"),
		ModelPath:     os.Getenv("ESHU_WHISPERCPP_MODEL"),
		ModelSHA256:   os.Getenv("ESHU_WHISPERCPP_MODEL_SHA256"),
		EngineVersion: os.Getenv("ESHU_WHISPERCPP_VERSION"),
		// Run log lines go to stderr so the proof script can scan them for
		// transcript text and read engine peak RSS.
		Logger: slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	}
	if cfg.BinaryPath == "" || cfg.ModelPath == "" {
		t.Fatal("sandbox proof needs ESHU_WHISPERCPP_BINARY and ESHU_WHISPERCPP_MODEL")
	}
	return cfg
}

func sandboxFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(os.Getenv("ESHU_WHISPERCPP_SPEECH_FIXTURE"))
	if err != nil {
		t.Fatalf("read speech fixture: %v", err)
	}
	return body
}

func sandboxEngine(t *testing.T, limits Limits) *Engine {
	t.Helper()
	cfg := sandboxConfig(t)
	cfg.Limits = limits
	engine, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return engine
}

// TestSandboxRealSpeechRedactedAndDeterministic transcribes the synthesized
// speech fixture three times, requires identical output, and requires the
// password canary to be redacted by the media.Extract path.
func TestSandboxRealSpeechRedactedAndDeterministic(t *testing.T) {
	engine := sandboxEngine(t, Limits{Timeout: 2 * time.Minute, MaxAddressSpaceBytes: 6 << 30})
	body := sandboxFixture(t)
	input := docmedia.Media{SourceName: "fixture/speech.wav", Format: "wav", Body: body}

	var first docmedia.EngineResult
	for run := range 3 {
		started := time.Now()
		result, err := engine.Transcribe(context.Background(), input)
		if err != nil {
			t.Fatalf("run %d Transcribe() error = %v", run, err)
		}
		t.Logf("EVIDENCE run=%d wall_seconds=%.3f segments=%d", run, time.Since(started).Seconds(), len(result.Segments))
		if run == 0 {
			first = result
			continue
		}
		if !reflect.DeepEqual(result, first) {
			t.Fatalf("run %d output differs from run 0", run)
		}
	}
	joined := ""
	for _, segment := range first.Segments {
		if segment.EndMillis < segment.StartMillis || segment.StartMillis < 0 {
			t.Fatalf("segment has invalid timing: %d-%d", segment.StartMillis, segment.EndMillis)
		}
		joined += " " + strings.ToLower(segment.Text)
	}
	// "payments worker" is left out: base.en hears the synthetic espeak-ng
	// phrase as "payment swearcare" after this package's resampler and after
	// sox's very-high-quality resampler alike, so it is a model limit on
	// synthetic speech, not a pipeline defect (README "Sandbox proof").
	for _, word := range []string{"restart", "before", "noon", "password"} {
		if !strings.Contains(joined, word) {
			t.Fatalf("transcript is missing expected word %q", word)
		}
	}
	t.Logf("EVIDENCE determinism=3/3 segments=%d model_digest=%s", len(first.Segments), first.ModelDigest)

	extracted, err := docmedia.Extract(context.Background(), docmedia.Request{
		SourceName: "fixture/speech.wav",
		Body:       body,
		Engine:     engine,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	redacted := 0
	for _, section := range extracted.Sections {
		if section.SourceMetadata["redacted"] == "true" {
			redacted++
		}
		if section.SourceMetadata["transcript_model_digest"] != first.ModelDigest {
			t.Fatalf("section missing pinned model digest")
		}
	}
	encoded, err := json.Marshal(extracted.Envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	if redacted == 0 || strings.Contains(strings.ToLower(string(encoded)), "swordfish") {
		t.Fatalf("password canary was not redacted before facts (redacted sections %d)", redacted)
	}
	t.Logf("EVIDENCE redaction sections=%d redacted=%d canary_in_facts=false", len(extracted.Sections), redacted)
}

// recordingEngine wraps the real engine and keeps the result of every
// Transcribe call, so a test can read the transcript media.Extract consumed
// instead of trusting a second, separate engine run to match it.
type recordingEngine struct {
	inner   docmedia.Engine
	results []docmedia.EngineResult
}

// Transcribe forwards to the wrapped engine and records a successful result.
func (r *recordingEngine) Transcribe(ctx context.Context, media docmedia.Media) (docmedia.EngineResult, error) {
	result, err := r.inner.Transcribe(ctx, media)
	if err == nil {
		r.results = append(r.results, result)
	}
	return result, err
}

// TestSandboxCanaryInOwnSegmentIsRedacted feeds the fixture with a silence
// between the marker and the value and proves two things against the real
// binary: whisper.cpp really does put the value in a later segment than the
// marker, and media.Extract still keeps that value out of every envelope. The
// marker and value positions and the value text come from the transcript that
// media.Extract itself consumed (recorded by a wrapper around the engine), not
// from a separate engine run, so the verdict never depends on two runs agreeing.
// The check compares against the text the engine produced, not only the
// expected canary spelling, so a mishearing cannot make it pass. Failure
// messages carry positions and counts only: transcript text must stay out of
// the proof log.
func TestSandboxCanaryInOwnSegmentIsRedacted(t *testing.T) {
	engine := &recordingEngine{inner: sandboxEngine(t, Limits{Timeout: 2 * time.Minute, MaxAddressSpaceBytes: 6 << 30})}
	body, err := os.ReadFile(os.Getenv("ESHU_WHISPERCPP_SPLIT_SPEECH_FIXTURE"))
	if err != nil {
		t.Fatalf("read split speech fixture: %v", err)
	}
	extracted, err := docmedia.Extract(context.Background(), docmedia.Request{
		SourceName: "fixture/speech-split.wav",
		Body:       body,
		Engine:     engine,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(engine.results) != 1 {
		t.Fatalf("Extract made %d successful Transcribe calls, want exactly 1", len(engine.results))
	}
	transcript := engine.results[0]
	marker, value := -1, -1
	for i, segment := range transcript.Segments {
		switch text := strings.ToLower(segment.Text); {
		case strings.Contains(text, "password"):
			marker, value = i, -1
		case marker >= 0 && value < 0:
			value = i
		}
	}
	if marker < 0 || value < 0 {
		t.Fatalf("fixture did not split: segments=%d marker=%d value=%d", len(transcript.Segments), marker, value)
	}
	valueText := strings.Trim(strings.ToLower(transcript.Segments[value].Text), " .,!?")
	heardCanary := strings.Contains(valueText, "swordfish")
	t.Logf("EVIDENCE split segments=%d marker_segment=%d value_segment=%d value_runes=%d value_is_canary=%t",
		len(transcript.Segments), marker, value, utf8.RuneCountInString(valueText), heardCanary)
	if !heardCanary {
		t.Fatalf("value segment %d is not the canary, so this run proves nothing about it", value)
	}

	redacted := 0
	for _, section := range extracted.Sections {
		if section.SourceMetadata["redacted"] == "true" {
			redacted++
		}
	}
	encoded, err := json.Marshal(extracted.Envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), valueText) {
		t.Fatalf("value segment reached the facts (sections %d, redacted %d)", len(extracted.Sections), redacted)
	}
	if len(extracted.Sections) != 2 || redacted != len(extracted.Sections) {
		t.Fatalf("want 2 sections both redacted, got sections %d redacted %d", len(extracted.Sections), redacted)
	}
	t.Logf("EVIDENCE split-redaction sections=%d redacted=%d canary_in_facts=false", len(extracted.Sections), redacted)
}

// TestSandboxSilenceIsNoSpeech feeds six seconds of digital silence.
func TestSandboxSilenceIsNoSpeech(t *testing.T) {
	engine := sandboxEngine(t, Limits{Timeout: 2 * time.Minute})
	silence := encodeWAV(t, 16000, 1, 16, 0)
	silence = append(silence[:40], 0x00, 0xEE, 0x02, 0x00) // 192000 data bytes = 6 s
	silence = append(silence, make([]byte, 192000)...)
	_, err := engine.Transcribe(context.Background(), docmedia.Media{Format: "wav", Body: silence})
	if ClassOf(err) != ClassNoSpeech {
		t.Fatalf("silence ClassOf = %q (err %v), want no_speech", ClassOf(err), err)
	}
	t.Logf("EVIDENCE silence outcome=no_speech")
}

// TestSandboxBoundsFailClosed proves the wall-clock and output caps kill the
// real engine and that hostile bytes never reach it.
func TestSandboxBoundsFailClosed(t *testing.T) {
	body := sandboxFixture(t)
	cases := []struct {
		name   string
		limits Limits
		body   []byte
		want   ErrorClass
	}{
		{name: "wall-clock cap", limits: Limits{Timeout: 50 * time.Millisecond}, body: body, want: ClassTimeout},
		{name: "stdout cap", limits: Limits{MaxStdoutBytes: 64}, body: body, want: ClassResourceLimit},
		{name: "duration cap", limits: Limits{MaxDurationMillis: 500}, body: body, want: ClassResourceLimit},
		{name: "hostile bytes", body: []byte("RIFF\x10\x00\x00\x00WAVEfmt \xff\xff\xff\x7f"), want: ClassMalformedMedia},
		{name: "address-space cap", limits: Limits{MaxAddressSpaceBytes: 64 << 20}, body: body, want: ClassEngineFailed},
	}
	for _, tc := range cases {
		engine := sandboxEngine(t, tc.limits)
		started := time.Now()
		_, err := engine.Transcribe(context.Background(), docmedia.Media{Format: "wav", Body: tc.body})
		elapsed := time.Since(started)
		if ClassOf(err) != tc.want {
			t.Fatalf("%s: ClassOf = %q (err %v), want %q", tc.name, ClassOf(err), err, tc.want)
		}
		if strings.Contains(strings.ToLower(err.Error()), "swordfish") {
			t.Fatalf("%s: error string leaked transcript text", tc.name)
		}
		t.Logf("EVIDENCE bound=%q outcome=%s wall_seconds=%.3f", tc.name, ClassOf(err), elapsed.Seconds())
	}
}

// TestSandboxProfileIsEnforced records the container profile the engine ran
// under: non-root UID, read-only root filesystem, and no network.
func TestSandboxProfileIsEnforced(t *testing.T) {
	if uid := os.Getuid(); uid == 0 {
		t.Fatalf("proof must not run as root")
	}
	if err := os.WriteFile("/sandbox-write-probe", []byte("x"), 0o600); err == nil {
		t.Fatalf("root filesystem is writable")
	}
	conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 2*time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("network is reachable")
	}
	t.Logf("EVIDENCE profile uid=%d rootfs_write=denied network=unreachable", os.Getuid())
}
