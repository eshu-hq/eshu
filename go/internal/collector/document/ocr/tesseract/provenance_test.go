// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

func TestNewRejectsVersionTheBinaryDoesNotReport(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	cfg := fx.config()
	cfg.EngineVersion = "5.4.1"

	engine, err := New(cfg)
	if engine != nil {
		t.Fatal("New accepted a version the binary does not report")
	}
	assertOutcome(t, err, OutcomeConfigInvalid)
	if !strings.Contains(err.Error(), "version") {
		t.Fatalf("error does not name the version check: %v", err)
	}
}

func TestNewRejectsPageSegModesThatLoadOSD(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	for _, psm := range []int{-1, 1, 2, 12, 14} {
		cfg := fx.config()
		cfg.PageSegMode = psm
		if _, err := New(cfg); OutcomeOf(err) != OutcomeConfigInvalid {
			t.Fatalf("psm %d: outcome = %q, want config_invalid (err=%v)", psm, OutcomeOf(err), err)
		}
	}
	cfg := fx.config()
	cfg.PageSegMode = 6
	if _, err := New(cfg); err != nil {
		t.Fatalf("psm 6: %v", err)
	}
}

func TestNewPinsRuntimeLibraries(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)

	missing := fx.config()
	missing.Libraries = nil
	if _, err := New(missing); OutcomeOf(err) != OutcomeConfigInvalid {
		t.Fatalf("no libraries: outcome = %q (err=%v)", OutcomeOf(err), err)
	}

	mismatch := fx.config()
	mismatch.Libraries[0].SHA256 = strings.Repeat("0", 64)
	if _, err := New(mismatch); OutcomeOf(err) != OutcomeDigestMismatch {
		t.Fatalf("library mismatch: outcome = %q (err=%v)", OutcomeOf(err), err)
	}

	engine, err := New(fx.config())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pattern := regexp.MustCompile(`^5\.5\.0; binary=sha256:[0-9a-f]{64}; libs=sha256:[0-9a-f]{64} \(2 files\); eng\.traineddata=sha256:[0-9a-f]{64}$`)
	if !pattern.MatchString(engine.EngineVersion()) {
		t.Fatalf("EngineVersion %q does not record the library set", engine.EngineVersion())
	}

	reordered := fx.config()
	reordered.Libraries[0], reordered.Libraries[1] = reordered.Libraries[1], reordered.Libraries[0]
	again, err := New(reordered)
	if err != nil {
		t.Fatalf("New reordered: %v", err)
	}
	if again.EngineVersion() != engine.EngineVersion() {
		t.Fatal("library digest depends on configuration order")
	}

	if err := os.WriteFile(fx.libraries[1].Path, []byte("swapped library"), 0o600); err != nil {
		t.Fatalf("swap library: %v", err)
	}
	_, err = engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "ok")})
	assertOutcome(t, err, OutcomeDigestMismatch)
}

func TestRecognizeUsesNoTessdataConfigFile(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	result, err := engine.Recognize(context.Background(), ocr.Image{Body: fakePNG(t, 20, 10, "args")})
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	args := make([]string, 0, len(result.Regions))
	for _, region := range result.Regions {
		args = append(args, region.Text)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-c tessedit_create_tsv=1") || !strings.Contains(joined, "-c tessedit_create_txt=0") {
		t.Fatalf("args %q do not select TSV through -c", joined)
	}
	for _, arg := range args {
		if arg == "tsv" {
			t.Fatalf("args %q still name the unpinned tessdata/configs/tsv file", joined)
		}
	}
}

func TestRecognizeRejectsDecodedSizeOverCapBeforeExec(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	// 6000x6000 is 36 MP, under the 50 MP pixel cap, but 4 bytes per pixel
	// is 144 MB, over the 128 MiB decoded-bytes cap of design 1737.
	_, err := engine.Recognize(context.Background(), ocr.Image{Body: pngHeader(6000, 6000)})
	assertOutcome(t, err, OutcomeInputRejected)
	if !strings.Contains(err.Error(), "decoded") {
		t.Fatalf("rejection does not name the decoded cap: %v", err)
	}
	if got := engine.execCount.Load(); got != 0 {
		t.Fatalf("exec count = %d, want 0", got)
	}
	if _, err := engine.Recognize(context.Background(), ocr.Image{Body: pngHeader(5000, 5000)}); OutcomeOf(err) == OutcomeInputRejected {
		t.Fatalf("25 MP (100 MB decoded) was rejected: %v", err)
	}
}

func TestExtractClassifiesPreflightFailure(t *testing.T) {
	t.Parallel()
	engine, h := newTelemetryEngine(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := engine.Extract(ctx, ocr.Request{
		ScopeID: "scope-ocr", GenerationID: "gen-1", SourceName: "docs/runbook.png",
		Body: fakePNG(t, 20, 10, "ok"),
	})
	if err == nil {
		t.Fatal("Extract succeeded with a canceled context")
	}
	if !strings.Contains(h.logs.String(), `"failure_class":"preflight_error"`) {
		t.Fatalf("preflight failure not classified: %s", h.logs.String())
	}
	if got := h.counter(t, "eshu_dp_ocr_extractions_total", map[string]string{"status": "skipped"}); got != 1 {
		t.Fatalf("skipped extractions = %d, want 1", got)
	}
}

// TestExtractLeavesValueOnlyLineUnredacted pins a known residual: line
// grouping protects a secret only when tesseract puts its label on the same
// line. In a form or column layout the value can land on its own line, and
// ocr.Extract's marker list does not match a bare value. Tracked for the
// caller lane in #9782.
func TestExtractLeavesValueOnlyLineUnredacted(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, nil)

	result, err := engine.Extract(context.Background(), ocr.Request{
		ScopeID: "scope-ocr", GenerationID: "gen-1", SourceName: "docs/form.png",
		Body: fakePNG(t, 200, 100, "formlayout"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(result.Sections))
	}
	if result.Sections[0].SourceMetadata["redacted"] != "true" {
		t.Fatal("label line was not redacted")
	}
	if result.Sections[1].Content != canaryPassword {
		t.Fatalf("value-only line content = %q; if this now redacts, the residual is fixed: update this test, the README, and the issue", result.Sections[1].Content)
	}
}
