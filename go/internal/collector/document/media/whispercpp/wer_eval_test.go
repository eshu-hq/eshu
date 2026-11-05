// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build whispercpp_sandbox

package whispercpp

// WER eval over the media transcript engine (#10073). It runs only inside
// the media sandbox image with ESHU_WER_EVAL=1, which
// scripts/eval-media-wer.sh sets. Synthetic fixtures are report-only:
// base.en already mishears synthetic speech (README "Sandbox proof"), so
// they prove the harness, not the model. The recorded-speech corpus (BLOCKER
// on #10073: mic/human session) is picked up automatically from
// ESHU_WHISPERCPP_RECORDED_DIR as *.wav files with sibling *.txt ground
// truths; those rows stay report-only until the band is ratified. The
// safety invariant is gate-blocking everywhere: a canary-bearing
// transcript must never reach the facts.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

// mediaWERCase is one scored fixture: audio plus its ground-truth words.
type mediaWERCase struct {
	name      string // evidence fixture label
	audioPath string
	level     string // quiet-synthetic, noisy-10db-synthetic, quiet-recorded, noisy-10db-recorded
	hasCanary bool   // leak check applies only to canary-bearing fixtures
}

// mediaWEREvidence is the per-fixture JSON line the eval script keeps.
type mediaWEREvidence struct {
	Fixture     string  `json:"fixture"`
	Level       string  `json:"level"`
	RefWords    int     `json:"ref_words"`
	Sub         int     `json:"s"`
	Del         int     `json:"d"`
	Ins         int     `json:"i"`
	WER         float64 `json:"wer"`
	WERRaw      float64 `json:"wer_raw"`
	Redacted    int     `json:"redacted"`
	CanaryLeak  bool    `json:"canary_leak"`
	WallSeconds float64 `json:"wall_seconds"`
}

// TestSandboxWEREval transcribes each eval fixture once, scores word-level
// WER against its sibling .txt, and proves the canary still does not reach
// the facts.
func TestSandboxWEREval(t *testing.T) {
	if os.Getenv("ESHU_WER_EVAL") != "1" {
		t.Skip("set ESHU_WER_EVAL=1 inside the media sandbox image (scripts/eval-media-wer.sh)")
	}
	engine := sandboxEngine(t, Limits{Timeout: 2 * time.Minute, MaxAddressSpaceBytes: 6 << 30})
	speech := os.Getenv("ESHU_WHISPERCPP_SPEECH_FIXTURE")
	if speech == "" {
		t.Fatal("sandbox eval needs ESHU_WHISPERCPP_SPEECH_FIXTURE")
	}
	dir := filepath.Dir(speech)
	cases := []mediaWERCase{
		{name: "speech.wav", audioPath: speech, level: "quiet-synthetic", hasCanary: true},
		{name: "speech-noisy10db.wav", audioPath: filepath.Join(dir, "speech-noisy10db.wav"), level: "noisy-10db-synthetic", hasCanary: true},
	}
	for _, recorded := range recordedWERCorpus(t) {
		cases = append(cases, recorded)
	}
	if len(cases) == 0 {
		t.Fatal("no WER eval fixtures found")
	}
	for _, tc := range cases {
		body, err := os.ReadFile(tc.audioPath)
		if err != nil {
			t.Fatalf("read %s: %v", tc.name, err)
		}
		reference := readMediaWERReference(t, tc.audioPath)
		started := time.Now()
		result, err := engine.Transcribe(context.Background(), docmedia.Media{SourceName: "fixture/" + tc.name, Format: "wav", Body: body})
		wall := time.Since(started).Seconds()
		if err != nil {
			t.Fatalf("%s Transcribe: %v", tc.name, err)
		}
		hyp := make([]string, 0, len(result.Segments))
		for _, segment := range result.Segments {
			if segment.EndMillis < segment.StartMillis || segment.StartMillis < 0 {
				t.Fatalf("%s: segment has invalid timing: %d-%d", tc.name, segment.StartMillis, segment.EndMillis)
			}
			hyp = append(hyp, segment.Text)
		}
		score := scoreWER(reference, strings.Join(hyp, " "))
		evidence := mediaWEREvidence{
			Fixture: tc.name, Level: tc.level,
			RefWords: score.RefWords, Sub: score.Sub, Del: score.Del, Ins: score.Ins,
			WER: score.WER, WERRaw: score.WERRaw, WallSeconds: wall,
		}
		extracted, err := docmedia.Extract(context.Background(), docmedia.Request{
			SourceName: "fixture/" + tc.name,
			Body:       body,
			Engine:     engine,
		})
		if err != nil {
			t.Fatalf("%s Extract: %v", tc.name, err)
		}
		for _, section := range extracted.Sections {
			if section.SourceMetadata["redacted"] == "true" {
				evidence.Redacted++
			}
		}
		encoded, err := json.Marshal(extracted.Envelopes)
		if err != nil {
			t.Fatalf("%s marshal envelopes: %v", tc.name, err)
		}
		lowered := strings.ToLower(string(encoded))
		evidence.CanaryLeak = tc.hasCanary && strings.Contains(lowered, "swordfish")
		line, _ := json.Marshal(evidence)
		t.Logf("MEDIA_WER_EVIDENCE %s", line)
		if evidence.CanaryLeak {
			t.Fatalf("%s: canary reached the facts", tc.name)
		}
	}
}

// readMediaWERReference loads the sibling .txt ground truth for an eval
// fixture. It holds the words read aloud, never recognition output.
func readMediaWERReference(t *testing.T, audioPath string) string {
	t.Helper()
	raw, err := os.ReadFile(strings.TrimSuffix(audioPath, ".wav") + ".txt")
	if err != nil {
		t.Fatalf("read ground truth for %s: %v", filepath.Base(audioPath), err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		t.Fatalf("ground truth for %s is empty", filepath.Base(audioPath))
	}
	return string(raw)
}

// recordedWERCorpus picks up the real recorded-speech corpus when the
// operator mounts it at ESHU_WHISPERCPP_RECORDED_DIR: every *.wav with a
// sibling *.txt. Recorded utterances carry no canaries by discipline, so no
// fixture here sets hasCanary. Absent dir means corpus not recorded yet.
func recordedWERCorpus(t *testing.T) []mediaWERCase {
	t.Helper()
	dir := os.Getenv("ESHU_WHISPERCPP_RECORDED_DIR")
	if dir == "" {
		t.Logf("MEDIA_WER_EVIDENCE recorded_corpus=absent (BLOCKER on #10073: no mic/human session yet)")
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.wav"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("recorded corpus dir %s has no wav files", dir)
	}
	var cases []mediaWERCase
	for _, match := range matches {
		level := "quiet-recorded"
		if strings.Contains(strings.ToLower(filepath.Base(match)), "noisy") {
			level = "noisy-10db-recorded"
		}
		cases = append(cases, mediaWERCase{
			name:      "recorded/" + filepath.Base(match),
			audioPath: match,
			level:     level,
		})
	}
	return cases
}
