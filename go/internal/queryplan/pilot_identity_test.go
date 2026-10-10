// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPilotComparisonIdentityRejectsInvalidAndStaleCandidate(t *testing.T) {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(output))
	identity := PilotComparisonIdentity{
		Version: 1, Event: "local", Mode: "feature_merge_base",
		Target: strings.Repeat("c", 40), Base: strings.Repeat("b", 40), Candidate: head,
	}
	path := filepath.Join(t.TempDir(), "identity.json")
	write := func(value PilotComparisonIdentity) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(identity)
	loaded, err := LoadPilotComparisonIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.VerifyCandidateCheckout(); err != nil {
		t.Fatal(err)
	}
	identity.Candidate = strings.Repeat("a", 40)
	write(identity)
	loaded, err = LoadPilotComparisonIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.VerifyCandidateCheckout(); err == nil {
		t.Fatal("stale candidate identity passed current checkout")
	}
	identity.Candidate = identity.Base
	write(identity)
	if _, err := LoadPilotComparisonIdentity(path); err == nil {
		t.Fatal("comparison identity accepted equal base and candidate")
	}
	if _, err := LoadPilotComparisonIdentity(""); err == nil {
		t.Fatal("comparison identity accepted missing path")
	}
}
