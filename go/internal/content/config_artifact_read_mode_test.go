// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"testing"
)

func TestParseReadModeAcceptsTheThreeModes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw  string
		want ReadMode
	}{
		{"", ReadModeLive},
		{"live", ReadModeLive},
		{"shadow", ReadModeShadow},
		{"stored", ReadModeStored},
		{"  shadow  ", ReadModeShadow},
		{"LIVE", ReadModeLive},
	} {
		if got, err := ParseReadMode(tc.raw); err != nil || got != tc.want {
			t.Errorf("ParseReadMode(%q) = %v, %v; want %v, nil", tc.raw, got, err, tc.want)
		}
	}
}

func TestParseReadModeRejectsUnknownModes(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"direct", "shadowed", "live,shadow", "2"} {
		if got, err := ParseReadMode(raw); err == nil {
			t.Errorf("ParseReadMode(%q) = %v, nil; want an error", raw, got)
		}
	}
}

func TestLoadConfigArtifactReadModeDefaultsToLive(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfigArtifactReadMode(nil)
	if err != nil {
		t.Fatalf("LoadConfigArtifactReadMode(nil) error = %v", err)
	}
	if cfg.Mode != ReadModeLive {
		t.Fatalf("LoadConfigArtifactReadMode(nil).Mode = %v, want live", cfg.Mode)
	}

	cfg, err = LoadConfigArtifactReadMode(func(string) string { return "" })
	if err != nil {
		t.Fatalf("LoadConfigArtifactReadMode(unset) error = %v", err)
	}
	if cfg.Mode != ReadModeLive {
		t.Fatalf("LoadConfigArtifactReadMode(unset).Mode = %v, want live", cfg.Mode)
	}
}

func TestLoadConfigArtifactReadModeReadsTheVariable(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfigArtifactReadMode(func(key string) string {
		if key == ConfigArtifactReadModeEnv {
			return "shadow"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("LoadConfigArtifactReadMode(shadow) error = %v", err)
	}
	if cfg.Mode != ReadModeShadow {
		t.Fatalf("LoadConfigArtifactReadMode(shadow).Mode = %v, want shadow", cfg.Mode)
	}
}

func TestLoadConfigArtifactReadModeRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	if _, err := LoadConfigArtifactReadMode(func(string) string { return "direct" }); err == nil {
		t.Fatal("LoadConfigArtifactReadMode(direct) = nil error, want an error so the process fails at start")
	}
}
