// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"strings"
	"testing"
	"time"
)

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadReadConfigDefaultsOffWithTheRuledStaleAfter(t *testing.T) {
	t.Parallel()

	cfg, err := LoadReadConfig(envOf(nil))
	if err != nil {
		t.Fatalf("LoadReadConfig() error = %v", err)
	}
	if cfg.Enabled {
		t.Fatal("the reader flag defaults to on; it ships default off")
	}
	// 3 x the 10 s writer interval + the 2.09 s measured replay lag p95 = 32.09 s.
	if cfg.StaleAfter != 33*time.Second || DefaultStaleAfter != 33*time.Second {
		t.Fatalf("StaleAfter = %v (default %v), want 33s", cfg.StaleAfter, DefaultStaleAfter)
	}
}

func TestLoadReadConfigParsesTheFlagLikeTheWriter(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]bool{
		"true": true, "TRUE": true, " 1 ": true, "yes": true, "on": true,
		"false": false, "0": false, "off": false, "": false, "garbage": false,
	} {
		cfg, err := LoadReadConfig(envOf(map[string]string{ReadEnabledEnv: raw}))
		if err != nil {
			t.Fatalf("LoadReadConfig(%q) error = %v", raw, err)
		}
		if cfg.Enabled != want {
			t.Fatalf("LoadReadConfig(%q).Enabled = %v, want %v", raw, cfg.Enabled, want)
		}
	}
}

func TestLoadReadConfigStaleAfterValidationAppliesWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg, err := LoadReadConfig(envOf(map[string]string{ReadEnabledEnv: "true", StaleAfterEnv: " 45s "}))
	if err != nil || cfg.StaleAfter != 45*time.Second {
		t.Fatalf("LoadReadConfig(45s) = %+v, %v; want StaleAfter 45s", cfg, err)
	}
	cfg, err = LoadReadConfig(envOf(map[string]string{ReadEnabledEnv: "true", StaleAfterEnv: MinStaleAfter.String()}))
	if err != nil || cfg.StaleAfter != MinStaleAfter {
		t.Fatalf("LoadReadConfig(min) = %+v, %v; want the floor accepted", cfg, err)
	}
	for _, raw := range []string{"soon", "33", "0s", "-5s", "9s", "4.9s"} {
		_, err := LoadReadConfig(envOf(map[string]string{ReadEnabledEnv: "true", StaleAfterEnv: raw}))
		if err == nil || !strings.Contains(err.Error(), StaleAfterEnv) {
			t.Fatalf("LoadReadConfig(stale=%q) error = %v, want a startup error naming %s", raw, err, StaleAfterEnv)
		}
	}
}

func TestLoadReadConfigIgnoresStaleAfterWhileDisabled(t *testing.T) {
	t.Parallel()

	// A typo in a knob the reader does not use must not break status reads
	// for a deployment that never turned the reader on.
	cfg, err := LoadReadConfig(envOf(map[string]string{StaleAfterEnv: "soon"}))
	if err != nil {
		t.Fatalf("LoadReadConfig() error = %v, want none while disabled", err)
	}
	if cfg.Enabled || cfg.StaleAfter != DefaultStaleAfter {
		t.Fatalf("cfg = %+v, want disabled with the default stale_after", cfg)
	}
}
