// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

// liveFixtureLines are the lines scripts/ocr-sandbox/Dockerfile renders into
// every fixture image. The first is clean mixed-case proportional text; the
// other two carry synthetic PII canaries that Extract must redact.
var liveFixtureLines = []string{
	"Deploy the Checkout Service to staging",
	"password: " + canaryPassword,
	"Contact " + canaryEmail + " today",
}

// liveEvidence is the per-fixture JSON line the sandbox proof script keeps.
type liveEvidence struct {
	Fixture        string  `json:"fixture"`
	Format         string  `json:"format"`
	Runs           int     `json:"runs"`
	OutputSHA256   string  `json:"output_sha256"`
	TextSHA256     string  `json:"text_sha256"`
	Deterministic  bool    `json:"deterministic"`
	CleanLineMatch bool    `json:"clean_line_match"`
	ExtractStatus  string  `json:"extract_status"`
	Sections       int     `json:"sections"`
	Redacted       int     `json:"redacted"`
	CanaryLeak     bool    `json:"canary_leak"`
	PeakRSSBytes   int64   `json:"peak_rss_bytes"`
	WallSeconds    float64 `json:"wall_seconds"`
}

// TestSandboxRealTesseract drives the real pinned tesseract binary. It runs
// only inside the sandbox container that scripts/prove-ocr-sandbox.sh
// starts with --network none, --read-only, --cap-drop ALL, and a non-root
// user; everywhere else it skips.
func TestSandboxRealTesseract(t *testing.T) {
	if os.Getenv("ESHU_OCR_SANDBOX_PROOF") != "1" {
		t.Skip("set ESHU_OCR_SANDBOX_PROOF=1 inside the OCR sandbox image (scripts/prove-ocr-sandbox.sh)")
	}
	cfg := Config{
		BinaryPath:    os.Getenv("ESHU_OCR_TESSERACT_BIN"),
		BinarySHA256:  os.Getenv("ESHU_OCR_TESSERACT_SHA256"),
		TessdataDir:   os.Getenv("ESHU_OCR_TESSDATA_DIR"),
		ModelSHA256:   os.Getenv("ESHU_OCR_MODEL_SHA256"),
		EngineVersion: os.Getenv("ESHU_OCR_ENGINE_VERSION"),
		ScratchDir:    os.Getenv("ESHU_OCR_SCRATCH_DIR"),
		Timeout:       20 * time.Second,
	}
	fixtures := os.Getenv("ESHU_OCR_FIXTURE_DIR")
	engine, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, name := range []string{"fixture.png", "fixture.jpg", "fixture.gif", "fixture.webp"} {
		body, err := os.ReadFile(filepath.Join(fixtures, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		evidence := liveRecognize(t, engine, name, body)
		evidence.Fixture = name
		liveExtract(t, engine, name, body, &evidence)
		line, _ := json.Marshal(evidence)
		t.Logf("OCR_SANDBOX_EVIDENCE %s", line)
		if !evidence.Deterministic || !evidence.CleanLineMatch || evidence.CanaryLeak {
			t.Fatalf("%s failed sandbox evidence: %s", name, line)
		}
	}
	liveBounds(t, cfg, fixtures)
}

func liveRecognize(t *testing.T, engine *Engine, name string, body []byte) liveEvidence {
	t.Helper()
	const runs = 5
	var first ocr.EngineResult
	evidence := liveEvidence{Runs: runs, Deterministic: true}
	start := time.Now()
	for run := 0; run < runs; run++ {
		stats := recognizeStats{}
		result, err := engine.recognize(context.Background(), ocr.Image{Body: body}, &stats)
		if err != nil {
			t.Fatalf("%s run %d: %v", name, run, err)
		}
		evidence.Format = stats.format
		evidence.PeakRSSBytes = max(evidence.PeakRSSBytes, stats.peakRSS)
		if run == 0 {
			first = result
			continue
		}
		if !reflect.DeepEqual(first, result) {
			evidence.Deterministic = false
		}
	}
	evidence.WallSeconds = time.Since(start).Seconds() / runs
	encoded, _ := json.Marshal(first)
	sum := sha256.Sum256(encoded)
	evidence.OutputSHA256 = hex.EncodeToString(sum[:])
	texts := make([]string, 0, len(first.Regions))
	for _, region := range first.Regions {
		texts = append(texts, region.Text)
	}
	textSum := sha256.Sum256([]byte(strings.Join(texts, "\n")))
	evidence.TextSHA256 = hex.EncodeToString(textSum[:])
	evidence.CleanLineMatch = len(first.Regions) > 0 && first.Regions[0].Text == liveFixtureLines[0]
	return evidence
}

func liveExtract(t *testing.T, engine *Engine, name string, body []byte, evidence *liveEvidence) {
	t.Helper()
	result, err := engine.Extract(context.Background(), ocr.Request{
		ScopeID: "scope-ocr-sandbox", GenerationID: "gen-sandbox",
		ObservedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		SourceName: "docs/" + name, DocumentID: "doc:" + name,
		Body: body,
	})
	if err != nil {
		t.Fatalf("%s Extract: %v", name, err)
	}
	evidence.ExtractStatus = result.Document.SourceMetadata["ocr_status"]
	evidence.Sections = len(result.Sections)
	for _, section := range result.Sections {
		if section.SourceMetadata["redacted"] == "true" {
			evidence.Redacted++
		}
	}
	persisted, _ := json.Marshal(result.Envelopes)
	evidence.CanaryLeak = bytes.Contains(persisted, []byte("Hunter2")) || bytes.Contains(persisted, []byte("alice.canary"))
}

// liveBounds proves the caps fail closed against the real binary.
func liveBounds(t *testing.T, base Config, fixtures string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(fixtures, "fixture.png"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Config)
		image  []byte
		want   []Outcome
	}{
		{name: "timeout", mutate: func(c *Config) { c.Timeout = 5 * time.Millisecond }, image: body, want: []Outcome{OutcomeTimeout}},
		{name: "address_space_cap", mutate: func(c *Config) { c.MaxAddressSpaceBytes = 48 << 20 }, image: body, want: []Outcome{OutcomeExecFailed, OutcomeSignaled}},
		{name: "stdout_cap", mutate: func(c *Config) { c.MaxOutputBytes = 64 }, image: body, want: []Outcome{OutcomeOutputOversize}},
		{name: "pixel_cap", mutate: func(c *Config) { c.MaxPixels = 1000 }, image: body, want: []Outcome{OutcomeInputRejected}},
		{name: "corrupt_body", image: append(append([]byte{}, body[:64]...), bytes.Repeat([]byte{0xA5}, 4096)...), want: []Outcome{OutcomeExecFailed, OutcomeNoText, OutcomeSignaled}},
		{name: "hostile_text", image: []byte("<svg onload=alert(1)>" + canaryPassword), want: []Outcome{OutcomeInputRejected}},
	}
	emulated := os.Getenv("ESHU_OCR_SANDBOX_EMULATED") == "1"
	for _, tc := range cases {
		if emulated && tc.name == "address_space_cap" {
			// Under binfmt emulation RLIMIT_AS lands on the qemu
			// interpreter, not a native tesseract, so it proves nothing.
			t.Logf("OCR_SANDBOX_BOUND %s outcome=not_asserted_under_emulation", tc.name)
			continue
		}
		cfg := base
		if tc.mutate != nil {
			tc.mutate(&cfg)
		}
		engine, err := New(cfg)
		if err != nil {
			t.Fatalf("%s New: %v", tc.name, err)
		}
		result, err := engine.Recognize(context.Background(), ocr.Image{Body: tc.image})
		got := outcomeOfResult(result, err)
		t.Logf("OCR_SANDBOX_BOUND %s outcome=%s", tc.name, got)
		if !containsOutcome(tc.want, got) {
			t.Fatalf("%s outcome = %q, want one of %v (err=%v)", tc.name, got, tc.want, err)
		}
		if err != nil && strings.Contains(err.Error(), "Hunter2") {
			t.Fatalf("%s error leaks canary", tc.name)
		}
	}
}

func containsOutcome(set []Outcome, value Outcome) bool {
	for _, item := range set {
		if item == value {
			return true
		}
	}
	return false
}
