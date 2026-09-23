// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cassette_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// recordedAWSCassette is the committed real aws-cloud recording (#6965
// Phase 3), made with collector-aws-cloud -mode=record through record-mode
// pseudonymization.
const recordedAWSCassette = "recorded-pseudonymized.json"

// TestCommittedRecordingLoadsAndReplays proves the committed recording is a
// valid, replayable cassette at the shape it was recorded with: it loads
// through the real validator, carries the pseudonym-key fingerprint that
// marks it as recorded, and replays every scope through the cassette
// Source. A re-record that changes the shape updates these numbers in the
// same change.
func TestCommittedRecordingLoadsAndReplays(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "testdata", "cassettes", "awscloud", recordedAWSCassette)
	file, err := cassette.LoadFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", recordedAWSCassette, err)
	}
	if file.PseudonymKeyFingerprint == "" {
		t.Fatalf("%s has no pseudonym_key_fingerprint; a committed recording must carry it", recordedAWSCassette)
	}
	facts := 0
	for _, scope := range file.Scopes {
		facts += len(scope.Facts)
	}
	if len(file.Scopes) != 14 || facts != 517 {
		t.Fatalf("%s: %d scopes, %d facts; want 14 scopes, 517 facts", recordedAWSCassette, len(file.Scopes), facts)
	}
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	replayed, replayedFacts := 0, 0
	for {
		gen, ok, err := src.Next(t.Context())
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if !ok {
			break
		}
		replayed++
		for range gen.Facts {
			replayedFacts++
		}
		if replayed > len(file.Scopes) {
			break
		}
	}
	if replayed != len(file.Scopes) || replayedFacts != 517 {
		t.Fatalf("replayed %d scopes and %d facts, want %d scopes and 517 facts", replayed, replayedFacts, len(file.Scopes))
	}
}
