// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

// minKeyBodyRegions is the fewest body regions the key-body.png layout must
// yield. The label window covers two, so a fixture that reads as fewer proves
// nothing about a body that runs past it.
const minKeyBodyRegions = 6

// liveKeyBody proves the multi-line rule end to end (#9799). It reads
// key-body.png, a PEM private key whose body is nine lines, takes the body
// regions before the END armor line from the real tesseract output, and checks that
// none of their text reaches the persisted facts. It compares what tesseract
// read, not the text the fixture was drawn from, so an OCR misread cannot hide a
// leak. It logs only indexes and counts, never region text.
func liveKeyBody(t *testing.T, engine *Engine, fixtures string) {
	t.Helper()
	const name = "key-body.png"
	body, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	evidence := liveRecognize(t, engine, name, body, "Deploy the Checkout Service to staging")
	evidence.Fixture = name
	recognized, err := engine.Recognize(context.Background(), ocr.Image{Body: body})
	if err != nil {
		t.Fatalf("%s Recognize: %v", name, err)
	}
	// The body is every region between the first line and the END armor line.
	// Tesseract drops the BEGIN armor line from this image, so the proof does not
	// rely on it: the body must be withheld on its own shape.
	const begin = 0
	end := -1
	for index, region := range recognized.Regions {
		if index > begin && strings.Contains(region.Text, "END") {
			end = index
			break
		}
	}
	if end-begin-1 < minKeyBodyRegions {
		t.Fatalf("%s: END armor at region %d, want at least %d body regions before it; the fixture no longer reproduces the layout",
			name, end, minKeyBodyRegions)
	}
	result := liveExtract(t, engine, name, body, &evidence)
	persisted, _ := json.Marshal(result.Envelopes)
	leaked := 0
	for _, region := range recognized.Regions[begin+1 : end] {
		if text := strings.TrimSpace(region.Text); text != "" && textInPersisted(persisted, text) {
			leaked++
		}
	}
	evidence.CanaryLeak = evidence.CanaryLeak || leaked > 0
	line, _ := json.Marshal(evidence)
	t.Logf("OCR_SANDBOX_EVIDENCE %s body_regions=%d body_regions_in_clear=%d", line, end-begin-1, leaked)
	if !evidence.Deterministic || !evidence.CleanLineMatch || evidence.CanaryLeak {
		t.Fatalf("%s failed sandbox evidence: %s", name, line)
	}
	if evidence.ExtractStatus != "completed" {
		t.Fatalf("%s: status %q, want completed", name, evidence.ExtractStatus)
	}
	const kept = "Ask the platform team for access"
	keptFound := false
	for _, section := range result.Sections {
		keptFound = keptFound || section.Content == kept
	}
	if !keptFound {
		t.Fatalf("%s: an ordinary line past the key was not kept", name)
	}
}

// textInPersisted reports whether text appears in the JSON-encoded persisted
// envelopes. The envelopes were encoded by encoding/json, which escapes <, >, &,
// ", and \, so the raw text of a line that holds one of them would never match
// the bytes. The text is encoded the same way before the search.
func textInPersisted(persisted []byte, text string) bool {
	encoded, err := json.Marshal(text)
	if err != nil {
		return true
	}
	return bytes.Contains(persisted, encoded[1:len(encoded)-1])
}
