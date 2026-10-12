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

func TestLoadPilotComparisonIdentityRejectsMalformedSidecar(t *testing.T) {
	valid := map[string]any{
		"version": 1, "event": "local", "mode": "feature_merge_base",
		"target": strings.Repeat("c", 40), "base": strings.Repeat("b", 40),
		"candidate": strings.Repeat("a", 40),
	}
	for _, test := range []struct {
		name     string
		field    string
		value    any
		trailing string
	}{
		{name: "trailing JSON", trailing: " {}"},
		{name: "unknown field", field: "unexpected", value: true},
		{name: "unsupported version", field: "version", value: 2},
		{name: "unsupported event", field: "event", value: "other"},
		{name: "mismatched event mode", field: "event", value: "pull_request"},
		{name: "unsupported mode", field: "mode", value: "other"},
		{name: "uppercase target", field: "target", value: "C" + strings.Repeat("c", 39)},
		{name: "nonhex target", field: "target", value: "g" + strings.Repeat("c", 39)},
		{name: "uppercase base", field: "base", value: "B" + strings.Repeat("b", 39)},
		{name: "nonhex base", field: "base", value: "g" + strings.Repeat("b", 39)},
		{name: "uppercase candidate", field: "candidate", value: "A" + strings.Repeat("a", 39)},
		{name: "nonhex candidate", field: "candidate", value: "g" + strings.Repeat("a", 39)},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := make(map[string]any, len(valid)+1)
			for key, value := range valid {
				payload[key] = value
			}
			if test.field != "" {
				payload[test.field] = test.value
			}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "identity.json")
			if err := os.WriteFile(path, append(data, test.trailing...), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadPilotComparisonIdentity(path); err == nil {
				t.Fatal("malformed identity was accepted")
			}
		})
	}
}
