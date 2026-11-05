// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

// werLevel is one rung of the deterministic degradation ladder baked by
// scripts/ocr-sandbox/Dockerfile: the clean control plus light, medium, and
// heavy. form-columns is a layout probe, not accuracy material, so it has no
// ladder rung.
type werLevel struct {
	name  string // fixture file suffix: "" (clean), "light", "medium", "heavy"
	gated bool   // clean WER 0 is gate-blocking; degraded levels are report-only
}

var werLevels = []werLevel{
	{name: "", gated: true},
	{name: "light"},
	{name: "medium"},
	{name: "heavy"},
}

// werBases are the ladder layouts. Ground truth lives in <base>.txt beside
// the fixtures, written by the Dockerfile from the same -annotate strings.
// The clean gate applies per layout: prose layouts (fixture, form-stacked)
// must score WER 0, but the key-body base64 lines are inherently
// approximate at this glyph size (measured clean WER 0.47 on arm64), so the
// whole key-body ladder is report-only until its band is ratified.
var werBases = []struct {
	name       string
	cleanGated bool
}{
	{name: "fixture", cleanGated: true},
	{name: "form-stacked", cleanGated: true},
	{name: "key-body"},
}

// werEvidence is the per-fixture JSON line the eval script keeps. It mirrors
// liveEvidence with WER fields in place of determinism fields; the proof
// test already proves determinism.
type werEvidence struct {
	Fixture       string  `json:"fixture"`
	Level         string  `json:"level"`
	RefWords      int     `json:"ref_words"`
	Sub           int     `json:"s"`
	Del           int     `json:"d"`
	Ins           int     `json:"i"`
	WER           float64 `json:"wer"`
	WERRaw        float64 `json:"wer_raw"`
	CER           float64 `json:"cer"`
	ExtractStatus string  `json:"extract_status"`
	Redacted      int     `json:"redacted"`
	CanaryLeak    bool    `json:"canary_leak"`
	WallSeconds   float64 `json:"wall_seconds"`
}

// TestSandboxWEREval scores word-level WER of the real pinned tesseract
// binary over the deterministic noise ladder. It runs only inside the OCR
// sandbox image (scripts/prove-ocr-sandbox.sh profile) with ESHU_WER_EVAL=1,
// which scripts/eval-ocr-wer.sh sets. The prose-layout clean controls must
// score WER 0 (the key-body base64 ladder is report-only: base64 glyphs are
// inherently approximate at this size) and every level must withhold the
// canary (the accuracy to safety coupling: a misread canary is still
// redacted, or the whole image is withheld, or the run fails).
// Degraded-level WER is report-only until the band is known.
func TestSandboxWEREval(t *testing.T) {
	if os.Getenv("ESHU_OCR_SANDBOX_PROOF") != "1" || os.Getenv("ESHU_WER_EVAL") != "1" {
		t.Skip("set ESHU_OCR_SANDBOX_PROOF=1 and ESHU_WER_EVAL=1 inside the OCR sandbox image (scripts/eval-ocr-wer.sh)")
	}
	cfg := Config{
		BinaryPath:    os.Getenv("ESHU_OCR_TESSERACT_BIN"),
		BinarySHA256:  os.Getenv("ESHU_OCR_TESSERACT_SHA256"),
		TessdataDir:   os.Getenv("ESHU_OCR_TESSDATA_DIR"),
		ModelSHA256:   os.Getenv("ESHU_OCR_MODEL_SHA256"),
		EngineVersion: os.Getenv("ESHU_OCR_ENGINE_VERSION"),
		ScratchDir:    os.Getenv("ESHU_OCR_SCRATCH_DIR"),
		Libraries:     parseLibraryPins(t, os.Getenv("ESHU_OCR_LIBRARIES")),
		Timeout:       20 * time.Second,
	}
	fixtures := os.Getenv("ESHU_OCR_FIXTURE_DIR")
	engine, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, base := range werBases {
		reference := readWERReference(t, fixtures, base.name)
		for _, level := range werLevels {
			name := base.name + ".png"
			label := "clean"
			if level.name != "" {
				name = base.name + "-" + level.name + ".png"
				label = level.name
			}
			gated := level.gated && base.cleanGated
			body, err := os.ReadFile(filepath.Join(fixtures, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			start := time.Now()
			recognized, err := engine.Recognize(context.Background(), ocr.Image{Body: body})
			if err != nil {
				t.Fatalf("%s Recognize: %v", name, err)
			}
			wall := time.Since(start).Seconds()
			texts := make([]string, 0, len(recognized.Regions))
			for _, region := range recognized.Regions {
				texts = append(texts, region.Text)
			}
			score := scoreWER(reference, strings.Join(texts, "\n"))
			evidence := werEvidence{
				Fixture: base.name, Level: label,
				RefWords: score.RefWords, Sub: score.Sub, Del: score.Del, Ins: score.Ins,
				WER: score.WER, WERRaw: score.WERRaw, CER: score.CER,
				WallSeconds: wall,
			}
			result := liveExtract(t, engine, name, body, &liveEvidence{})
			evidence.ExtractStatus = result.Document.SourceMetadata["ocr_status"]
			for _, section := range result.Sections {
				if section.SourceMetadata["redacted"] == "true" {
					evidence.Redacted++
				}
			}
			persisted, _ := json.Marshal(result.Envelopes)
			evidence.CanaryLeak = strings.Contains(string(persisted), "Hunter2") ||
				strings.Contains(string(persisted), "alice.canary")
			line, _ := json.Marshal(evidence)
			t.Logf("OCR_WER_EVIDENCE %s", line)
			if evidence.CanaryLeak {
				t.Fatalf("%s: canary leaked through redaction under noise", name)
			}
			// completed proves redaction; skipped proves preflight
			// withheld the whole image (zero sections), which also
			// withholds the canary. Heavy noise bytes can trip the
			// byte-pattern metadata heuristic in preflight with no
			// real metadata present — a preflight-package follow-up,
			// not a leak. Any other status fails the run.
			switch evidence.ExtractStatus {
			case "completed":
			case "skipped":
				if len(result.Sections) != 0 {
					t.Fatalf("%s: skipped extract emitted %d sections", name, len(result.Sections))
				}
			default:
				t.Fatalf("%s: extract status %q, want completed or skipped", name, evidence.ExtractStatus)
			}
			if gated && score.WER != 0 {
				t.Fatalf("%s: clean WER = %v (S=%d D=%d I=%d), want 0", name, score.WER, score.Sub, score.Del, score.Ins)
			}
		}
	}
}

// readWERReference loads the Dockerfile-written ground truth for a ladder
// base. It never contains recognition output, only the rendered strings.
func readWERReference(t *testing.T, fixtures, base string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, base+".txt"))
	if err != nil {
		t.Fatalf("read %s.txt ground truth: %v", base, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		t.Fatalf("%s.txt ground truth is empty", base)
	}
	return string(raw)
}
