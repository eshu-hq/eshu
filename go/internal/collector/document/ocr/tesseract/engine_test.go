// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

func newTestEngine(t *testing.T, mutate func(*Config)) (*Engine, fixture) {
	t.Helper()
	fx := newFixture(t)
	cfg := fx.config()
	if mutate != nil {
		mutate(&cfg)
	}
	engine, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return engine, fx
}

func assertOutcome(t *testing.T, err error, want Outcome) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error, got nil", want)
	}
	if got := OutcomeOf(err); got != want {
		t.Fatalf("OutcomeOf(err) = %q, want %q (err=%v)", got, want, err)
	}
	for _, canary := range []string{canaryPassword, canaryEmail} {
		if strings.Contains(err.Error(), canary) {
			t.Fatalf("error leaks OCR canary %q: %v", canary, err)
		}
	}
}

func TestRecognizeGroupsWordsIntoLineRegions(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	result, err := engine.Recognize(context.Background(), ocr.Image{Format: "png", Body: fakePNG(t, 200, 100, "ok")})
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if result.EngineName != EngineName {
		t.Fatalf("EngineName = %q, want %q", result.EngineName, EngineName)
	}
	for _, want := range []string{"5.5.0", "binary=sha256:", "eng.traineddata=sha256:"} {
		if !strings.Contains(result.EngineVersion, want) {
			t.Fatalf("EngineVersion %q missing %q", result.EngineVersion, want)
		}
	}
	if result.Language != "en" {
		t.Fatalf("Language = %q, want en", result.Language)
	}
	gotText := make([]string, 0, len(result.Regions))
	for _, region := range result.Regions {
		gotText = append(gotText, region.Text)
	}
	wantText := []string{"Deploy the Checkout", "password: " + canaryPassword, "Contact " + canaryEmail}
	if !reflect.DeepEqual(gotText, wantText) {
		t.Fatalf("region text = %q, want %q", gotText, wantText)
	}
	first := result.Regions[0]
	if first.Bounds.X != 0.05 || first.Bounds.Y != 0.1 || first.Bounds.Width != 0.9 || first.Bounds.Height != 0.2 {
		t.Fatalf("first region bounds = %+v, want normalized line union", first.Bounds)
	}
	if first.Confidence < 0.95 || first.Confidence > 0.96 {
		t.Fatalf("first region confidence = %v, want mean word conf / 100", first.Confidence)
	}
	if first.RegionID != "tess-0001" {
		t.Fatalf("first RegionID = %q", first.RegionID)
	}
}

func TestRecognizeAcceptsWebP(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	result, err := engine.Recognize(context.Background(), ocr.Image{Format: "webp", Body: fakeWebP(200, 100, "ok")})
	if err != nil {
		t.Fatalf("Recognize webp: %v", err)
	}
	if len(result.Regions) != 3 {
		t.Fatalf("webp regions = %d, want 3", len(result.Regions))
	}
}

func TestRecognizeNoTextReturnsEmptyRegions(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	result, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "empty")})
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(result.Regions) != 0 {
		t.Fatalf("regions = %d, want 0", len(result.Regions))
	}
}

func TestRecognizeIsDeterministicAcrossRuns(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)
	body := fakePNG(t, 200, 100, "ok")

	first, err := engine.Recognize(context.Background(), ocr.Image{Body: body})
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	for run := 0; run < 8; run++ {
		next, err := engine.Recognize(context.Background(), ocr.Image{Body: body})
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("run %d differs:\nfirst=%+v\nnext=%+v", run, first, next)
		}
	}
}

func TestRecognizeTimesOutAndKillsProcess(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, func(cfg *Config) { cfg.Timeout = 300 * time.Millisecond })

	start := time.Now()
	_, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "sleep")})
	assertOutcome(t, err, OutcomeTimeout)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("timeout took %s, want bounded near 300ms", elapsed)
	}
}

func TestRecognizeHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	_, err := engine.Recognize(ctx, ocr.Image{Body: fakePNG(t, 20, 10, "sleep")})
	assertOutcome(t, err, OutcomeCanceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false: %v", err)
	}
}

func TestRecognizeRejectsAlreadyCanceledContextBeforeExec(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := engine.Recognize(ctx, ocr.Image{Body: fakePNG(t, 20, 10, "ok")})
	assertOutcome(t, err, OutcomeCanceled)
	if got := engine.execCount.Load(); got != 0 {
		t.Fatalf("exec count = %d, want 0", got)
	}
}

func TestRecognizeFailsClosedOnOversizeStdout(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, func(cfg *Config) { cfg.MaxOutputBytes = 64 << 10 })

	_, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "flood")})
	assertOutcome(t, err, OutcomeOutputOversize)
}

func TestRecognizeFailsClosedOnOversizeStderr(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, func(cfg *Config) { cfg.MaxStderrBytes = 4 << 10 })

	_, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "stderrflood")})
	assertOutcome(t, err, OutcomeOutputOversize)
}

func TestRecognizeFailsClosedOnNonZeroExit(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	result, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "exit3")})
	assertOutcome(t, err, OutcomeExecFailed)
	if len(result.Regions) != 0 {
		t.Fatalf("non-zero exit returned %d regions, want none", len(result.Regions))
	}
}

func TestRecognizeFailsClosedOnMalformedOutput(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	for _, mode := range []string{"garbage", "badrow"} {
		_, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, mode)})
		assertOutcome(t, err, OutcomeParseFailed)
	}
}

func TestRecognizeFailsClosedPastRegionCap(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, func(cfg *Config) { cfg.MaxRegions = 10 })

	_, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 100, "manylines")})
	assertOutcome(t, err, OutcomeRegionCapExceeded)
}

func TestRecognizeScrubsEnvironmentAndUsesPrivateScratchDir(t *testing.T) {
	t.Setenv("ESHU_OCR_PARENT_CANARY", "parent-env-must-not-leak")
	engine, fx := newTestEngine(t, nil)

	result, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "env")})
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	env := map[string]string{}
	cwd := ""
	for _, region := range result.Regions {
		if after, ok := strings.CutPrefix(region.Text, "CWD="); ok {
			cwd = after
			continue
		}
		key, value, _ := strings.Cut(region.Text, "=")
		env[key] = value
	}
	if _, leaked := env["ESHU_OCR_PARENT_CANARY"]; leaked {
		t.Fatal("parent environment leaked into the OCR subprocess")
	}
	if env["OMP_THREAD_LIMIT"] != "1" {
		t.Fatalf("OMP_THREAD_LIMIT = %q, want 1", env["OMP_THREAD_LIMIT"])
	}
	if _, ok := env["PATH"]; ok {
		t.Fatal("PATH must not be passed to the OCR subprocess")
	}
	if !strings.HasPrefix(cwd, fx.scratch+string(os.PathSeparator)) {
		t.Fatalf("subprocess cwd %q not under scratch dir %q", cwd, fx.scratch)
	}
	if _, err := os.Stat(cwd); !os.IsNotExist(err) {
		t.Fatalf("per-run scratch dir %q not removed after recognition (stat err=%v)", cwd, err)
	}
	entries, err := os.ReadDir(fx.scratch)
	if err != nil {
		t.Fatalf("read scratch: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("scratch dir keeps %d entries after recognition", len(entries))
	}
}

func TestRecognizeDetectsBinarySwapAfterConstruction(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	dir := t.TempDir()
	copyPath := filepath.Join(dir, "tesseract")
	body, err := os.ReadFile(fx.binary)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if err := os.WriteFile(copyPath, body, 0o700); err != nil {
		t.Fatalf("write binary copy: %v", err)
	}
	cfg := fx.config()
	cfg.BinaryPath = copyPath
	engine, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.WriteFile(copyPath, append(body, 0), 0o700); err != nil {
		t.Fatalf("swap binary: %v", err)
	}

	_, err = engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "ok")})
	assertOutcome(t, err, OutcomeDigestMismatch)
	if got := engine.execCount.Load(); got != 0 {
		t.Fatalf("exec count = %d after swap, want 0", got)
	}
}
