// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

// expectedArgs is the pinned whisper-cli argument vector from the README
// decision record: stdin input, JSON-full on stdout, no prints, no GPU,
// greedy decoding (beam 1, best-of 1), temperature 0, and no temperature
// fallback.
func expectedArgs(model, language string, threads int) []string {
	return []string{
		"-m", model,
		"-f", "-",
		"-of", "-",
		"-ojf",
		"-np",
		"-ng",
		"-l", language,
		"-t", strconv.Itoa(threads),
		"-bs", "1",
		"-bo", "1",
		"-tp", "0",
		"-nf",
	}
}

func wavMedia(t *testing.T) docmedia.Media {
	t.Helper()
	body := encodeWAV(t, 8000, 2, 16, 8000*4)
	return docmedia.Media{SourceName: "docs/standup.wav", Format: "wav", DurationMillis: 4000, Body: body}
}

func TestTranscribeReturnsTimedSegmentsAndModelIdentity(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "success", Limits{})

	result, err := engine.Transcribe(context.Background(), wavMedia(t))
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if result.EngineName != EngineName || result.EngineVersion != "v1.9.5" {
		t.Fatalf("engine identity = %q/%q", result.EngineName, result.EngineVersion)
	}
	if result.ModelName != "ggml-fake.bin" {
		t.Fatalf("ModelName = %q, want ggml-fake.bin", result.ModelName)
	}
	if !strings.HasPrefix(result.ModelDigest, "sha256:") || len(result.ModelDigest) != len("sha256:")+64 {
		t.Fatalf("ModelDigest = %q, want sha256:<64 hex>", result.ModelDigest)
	}
	if result.Language != "en" {
		t.Fatalf("Language = %q, want en", result.Language)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("segments = %d, want 2 (non-speech annotation dropped)", len(result.Segments))
	}
	first := result.Segments[0]
	if first.Text != "Restart the payments worker." || first.StartMillis != 0 || first.EndMillis != 1500 {
		t.Fatalf("first segment = %+v", first)
	}
	if first.Confidence < 0.849 || first.Confidence > 0.851 {
		t.Fatalf("first confidence = %v, want mean of text tokens 0.85", first.Confidence)
	}
	if first.SegmentID != "seg-0001" || result.Segments[1].SegmentID != "seg-0002" {
		t.Fatalf("segment IDs = %q, %q", first.SegmentID, result.Segments[1].SegmentID)
	}
	if first.SpeakerLabel != "" || len(first.MentionHints) != 0 {
		t.Fatalf("engine must not emit speaker labels or mention hints: %+v", first)
	}
}

func TestTranscribeToleratesRawControlBytesInEngineJSON(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "control", Limits{})
	result, err := engine.Transcribe(context.Background(), wavMedia(t))
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if len(result.Segments) != 1 || result.Segments[0].Text != "Page the  on-call." {
		t.Fatalf("segments = %+v", result.Segments)
	}
}

func TestTranscribeClassifiesFailuresWithoutLeakingText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mode   string
		limits Limits
		media  func(*testing.T) docmedia.Media
		class  ErrorClass
	}{
		{name: "no speech annotations only", mode: "nospeech", class: ClassNoSpeech},
		{name: "empty transcription", mode: "empty", class: ClassNoSpeech},
		{name: "missing transcription", mode: "missing", class: ClassMalformedOutput},
		{name: "truncated json", mode: "badjson", class: ClassMalformedOutput},
		{name: "inverted offsets", mode: "hostile", class: ClassMalformedOutput},
		{name: "non-zero exit", mode: "exit", class: ClassEngineFailed},
		{name: "timeout", mode: "sleep", limits: Limits{Timeout: 300 * time.Millisecond}, class: ClassTimeout},
		{name: "stdout flood", mode: "flood", limits: Limits{MaxStdoutBytes: 1 << 20}, class: ClassResourceLimit},
		{name: "stderr flood", mode: "stderrflood", limits: Limits{MaxStderrBytes: 4 << 10}, class: ClassResourceLimit},
		{name: "segment too long", mode: "long", limits: Limits{MaxSegmentChars: 100}, class: ClassResourceLimit},
		{name: "too many segments", mode: "many", limits: Limits{MaxSegments: 10}, class: ClassResourceLimit},
		{name: "input over byte cap", mode: "fail-if-called", limits: Limits{MaxInputBytes: 1024}, class: ClassResourceLimit},
		{name: "audio over duration cap", mode: "fail-if-called", limits: Limits{MaxDurationMillis: 1000}, class: ClassResourceLimit},
		{
			name: "non-wav format", mode: "fail-if-called", class: ClassUnsupportedFormat,
			media: func(t *testing.T) docmedia.Media {
				m := wavMedia(t)
				m.Format = "mp3"
				return m
			},
		},
		{
			name: "garbage bytes", mode: "fail-if-called", class: ClassMalformedMedia,
			media: func(*testing.T) docmedia.Media {
				return docmedia.Media{Format: "wav", Body: []byte("RIFF\xff\xff\xff\xffWAVEjunk " + canaryPassword)}
			},
		},
		{
			name: "empty body", mode: "fail-if-called", class: ClassMalformedMedia,
			media: func(*testing.T) docmedia.Media { return docmedia.Media{Format: "wav"} },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine := newFakeEngine(t, tc.mode, tc.limits)
			input := wavMedia(t)
			if tc.media != nil {
				input = tc.media(t)
			}
			result, err := engine.Transcribe(context.Background(), input)
			if err == nil {
				t.Fatalf("Transcribe() error = nil, want class %s (result %+v)", tc.class, result)
			}
			if got := ClassOf(err); got != tc.class {
				t.Fatalf("ClassOf(%v) = %q, want %q", err, got, tc.class)
			}
			if len(result.Segments) != 0 {
				t.Fatalf("failed run returned %d segments", len(result.Segments))
			}
			for _, canary := range []string{canaryPassword, canaryEmail, canaryStderr} {
				if strings.Contains(err.Error(), canary) {
					t.Fatalf("error string leaked transcript canary: %q", err.Error())
				}
			}
			if tc.class == ClassNoSpeech && !errors.Is(err, docmedia.ErrNoSpeech) {
				t.Fatalf("no-speech error must wrap media.ErrNoSpeech: %v", err)
			}
		})
	}
}

// TestTranscribeKillsEngineOnCancellation cancels only after the fake engine
// reports it has read its input, and times from that cancel. Resampling runs in
// process before exec and is slow under -race, so its cost stays outside the
// kill bound, and a cancel cannot land before there is an engine to kill.
func TestTranscribeKillsEngineOnCancellation(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "sleep", Limits{})
	marker := filepath.Join(filepath.Dir(engine.cfg.ModelPath), engineStartedFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	canceledAt := make(chan time.Time, 1)
	go func() {
		waitForFile(ctx, marker)
		canceledAt <- time.Now()
		cancel()
	}()
	_, err := engine.Transcribe(ctx, wavMedia(t))
	killTime := time.Since(<-canceledAt)
	if got := ClassOf(err); got != ClassCanceled {
		t.Fatalf("ClassOf(%v) = %q, want canceled", err, got)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("engine never started, so this run proved no kill: %v", statErr)
	}
	if killTime > 5*time.Second {
		t.Fatalf("Transcribe returned %s after cancel, want the engine killed promptly", killTime)
	}
}

// waitForFile returns once path exists, ctx ends, or helperStartupDeadline
// passes.
func waitForFile(ctx context.Context, path string) {
	ctx, cancel := context.WithTimeout(ctx, helperStartupDeadline)
	defer cancel()
	for ctx.Err() == nil {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestTranscribeRejectsAlreadyCanceledContextBeforeExec(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "fail-if-called", Limits{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := engine.Transcribe(ctx, wavMedia(t))
	if got := ClassOf(err); got != ClassCanceled {
		t.Fatalf("ClassOf(%v) = %q, want canceled", err, got)
	}
}

func TestTranscribeIsDeterministicAcrossRunsAndGoroutines(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "success", Limits{})
	input := wavMedia(t)
	want, err := engine.Transcribe(context.Background(), input)
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	const runs = 8
	results := make([]docmedia.EngineResult, runs)
	errs := make([]error, runs)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Go(func() {
			results[i], errs[i] = engine.Transcribe(context.Background(), input)
		})
	}
	wg.Wait()
	for i := range runs {
		if errs[i] != nil {
			t.Fatalf("run %d error = %v", i, errs[i])
		}
		if !reflect.DeepEqual(results[i], want) {
			t.Fatalf("run %d result differs:\n got %+v\nwant %+v", i, results[i], want)
		}
	}
}

func TestTranscribeEnforcesAddressSpaceCap(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("address-space cap uses Linux prlimit")
	}
	if raceEnabled {
		// The fake engine is this test binary; under -race its sanitizer
		// shadow mappings count against RLIMIT_AS on some architectures,
		// so the control run is not meaningful. The sandbox proof covers
		// the cap against the real engine.
		t.Skip("address-space cap is not measurable with a race-instrumented fake engine")
	}
	limits := Limits{MaxAddressSpaceBytes: 3 << 30}
	// The same cap must admit a normal run, so the failure below is the cap
	// stopping the 8 GiB allocation, not the cap breaking process start.
	if _, err := newFakeEngine(t, "success", limits).Transcribe(context.Background(), wavMedia(t)); err != nil {
		t.Fatalf("success run under a 3 GiB cap error = %v", err)
	}
	engine := newFakeEngine(t, "memory", limits)
	_, err := engine.Transcribe(context.Background(), wavMedia(t))
	if got := ClassOf(err); got != ClassEngineFailed {
		t.Fatalf("ClassOf(%v) = %q, want engine_failed under a 3 GiB address-space cap", err, got)
	}
}

// TestDefaultLimitsStayWithinDesignCeilings pins the design 1737 section 6
// audio/video limits: 250 MiB source, 30 minutes, 10 minutes wall time,
// 1,000 transcript chunks, and 16 KiB per stored chunk (4,096 runes at most
// 4 bytes each).
func TestDefaultLimitsStayWithinDesignCeilings(t *testing.T) {
	t.Parallel()
	got := normalizeLimits(Limits{})
	if got.MaxInputBytes > 250<<20 || got.MaxDurationMillis > 30*60*1000 || got.Timeout > 10*time.Minute ||
		got.MaxSegments > 1000 || got.MaxSegmentChars > 4096 || got.MaxStdoutBytes > 512<<20 {
		t.Fatalf("default limits exceed design 1737 section 6: %+v", got)
	}
}

func TestNewVerifiesPinnedDigestsAndConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "model digest mismatch", mutate: func(c *Config) { c.ModelSHA256 = strings.Repeat("0", 64) }},
		{name: "binary digest mismatch", mutate: func(c *Config) { c.BinarySHA256 = strings.Repeat("f", 64) }},
		{name: "malformed digest", mutate: func(c *Config) { c.ModelSHA256 = "not-a-digest" }},
		{name: "relative binary path", mutate: func(c *Config) { c.BinaryPath = "whisper-cli" }},
		{name: "missing model", mutate: func(c *Config) { c.ModelPath += ".missing" }},
		{name: "missing engine version", mutate: func(c *Config) { c.EngineVersion = "" }},
		{name: "bad language", mutate: func(c *Config) { c.Language = "en; rm -rf /" }},
		{name: "negative threads", mutate: func(c *Config) { c.Threads = -1 }},
		{name: "input cap above design ceiling", mutate: func(c *Config) { c.Limits.MaxInputBytes = 250<<20 + 1 }},
		{name: "duration cap above design ceiling", mutate: func(c *Config) { c.Limits.MaxDurationMillis = 30*60*1000 + 1 }},
		{name: "timeout above design ceiling", mutate: func(c *Config) { c.Limits.Timeout = 10*time.Minute + time.Second }},
		{name: "segment cap above design ceiling", mutate: func(c *Config) { c.Limits.MaxSegments = 1001 }},
		{name: "segment text above design ceiling", mutate: func(c *Config) { c.Limits.MaxSegmentChars = 4097 }},
		{name: "stdout cap above design ceiling", mutate: func(c *Config) { c.Limits.MaxStdoutBytes = 512<<20 + 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := fakeConfig(t, "success")
			tc.mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatalf("New() error = nil, want rejection")
			}
		})
	}
	cfg := fakeConfig(t, "success")
	cfg.ModelSHA256 = strings.Repeat("0", 64)
	if _, err := New(cfg); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("New() error = %v, want ErrDigestMismatch", err)
	}
}

// TestNewRejectsBadLimitsBeforeDigestIO keeps config checks ahead of the
// pinned-file hashing, which reads a model of about 150 MB. The missing model
// shows the order: reaching the digest step reports the open failure instead.
func TestNewRejectsBadLimitsBeforeDigestIO(t *testing.T) {
	t.Parallel()
	cfg := fakeConfig(t, "success")
	cfg.ModelPath += ".missing"
	cfg.Limits.MaxSegments = ceilingSegments + 1
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "design ceiling") {
		t.Fatalf("New() error = %v, want the design ceiling rejection", err)
	}
}

// TestNewErrorsCarryNoPaths holds the rule that error strings never carry
// file paths, for every way a pinned file can fail verification. Returning the
// *fs.PathError from os.Open would put the configured path in the message and
// in the error chain.
func TestNewErrorsCarryNoPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "missing model", mutate: func(c *Config) { c.ModelPath += ".missing" }, want: "whisper.cpp model not found"},
		{name: "missing binary", mutate: func(c *Config) { c.BinaryPath += ".missing" }, want: "whisper.cpp binary not found"},
		{
			// ENOTDIR fails open for every user, including root, unlike a
			// permission bit.
			name: "unreadable model", want: "whisper.cpp model unreadable",
			mutate: func(c *Config) { c.ModelPath = filepath.Join(c.ModelPath, "child") },
		},
		{
			name: "binary is a directory", want: "whisper.cpp binary must be a regular file",
			mutate: func(c *Config) { c.BinaryPath = filepath.Dir(c.BinaryPath) },
		},
		{
			name: "model digest mismatch", want: ErrDigestMismatch.Error(),
			mutate: func(c *Config) { c.ModelSHA256 = strings.Repeat("0", 64) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := fakeConfig(t, "success")
			dir := filepath.Dir(cfg.ModelPath)
			tc.mutate(&cfg)
			_, err := New(cfg)
			if err == nil {
				t.Fatal("New() error = nil, want rejection")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New() error = %q, want it to contain %q", err, tc.want)
			}
			if strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "ggml-fake") {
				t.Fatalf("New() error carries a path: %q", err)
			}
			var pathErr *fs.PathError
			if errors.As(err, &pathErr) {
				t.Fatalf("New() error chain exposes the path %q", pathErr.Path)
			}
		})
	}
}

// TestRunProcessClassifiesStartAfterCancellation is the regression for a
// race seen under -race: cancellation landing between the entry check and
// cmd.Start made Start fail with the context error, which was reported as
// engine_failed instead of canceled or timeout.
func TestRunProcessClassifiesStartAfterCancellation(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "fail-if-called", Limits{})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.runProcess(canceled, nil, &runStats{}); ClassOf(err) != ClassCanceled {
		t.Fatalf("canceled start: ClassOf(%v) = %q, want canceled", err, ClassOf(err))
	}
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if _, err := engine.runProcess(expired, nil, &runStats{}); ClassOf(err) != ClassTimeout {
		t.Fatalf("expired start: ClassOf(%v) = %q, want timeout", err, ClassOf(err))
	}
}

// TestRunProcessClassifiesExpiredWallClockCap is the regression for a cap that
// expires before exec. Start then fails with the run context's deadline error
// while the caller's context is still live, so checking only the caller left
// the run reported as engine_failed instead of timeout.
func TestRunProcessClassifiesExpiredWallClockCap(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "fail-if-called", Limits{Timeout: time.Nanosecond})
	for name, run := range map[string]func() error{
		"runProcess": func() error {
			_, err := engine.runProcess(context.Background(), nil, &runStats{})
			return err
		},
		"Transcribe": func() error {
			_, err := engine.Transcribe(context.Background(), wavMedia(t))
			return err
		},
	} {
		err := run()
		if got := ClassOf(err); got != ClassTimeout {
			t.Fatalf("%s: ClassOf(%v) = %q, want %q", name, err, got, ClassTimeout)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: error %v does not wrap context.DeadlineExceeded", name, err)
		}
	}
}
