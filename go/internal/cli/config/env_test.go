// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetValuePersistsAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvVar, home)

	if err := SetValue("ESHU_API_KEY", "local-compose-token"); err != nil {
		t.Fatalf("SetValue() error = %v, want nil", err)
	}

	got := ResolveValue("ESHU_API_KEY", "")
	if got != "local-compose-token" {
		t.Fatalf("ResolveValue() = %q, want %q", got, "local-compose-token")
	}

	envBytes, err := os.ReadFile(filepath.Join(home, envFileName))
	if err != nil {
		t.Fatalf("ReadFile() error = %v, want nil", err)
	}
	if !strings.Contains(string(envBytes), "ESHU_API_KEY=local-compose-token") {
		t.Fatalf(".env = %q, want persisted token", string(envBytes))
	}
}

func TestSetValueCreatesHomeDirectory(t *testing.T) {
	// SetValue is the only writer that creates the config directory; Reset
	// deliberately does not, so a reset before any set is a no-op failure
	// rather than a directory-creating side effect.
	home := filepath.Join(t.TempDir(), "nested", "eshu")
	t.Setenv(homeEnvVar, home)

	if err := SetValue("ESHU_API_KEY", "token"); err != nil {
		t.Fatalf("SetValue() into a missing directory error = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(home, envFileName)); err != nil {
		t.Fatalf("Stat(.env) error = %v, want the file created", err)
	}
}

func TestLoadMissingFileReturnsEmptyMap(t *testing.T) {
	t.Setenv(homeEnvVar, t.TempDir())

	got := Load()
	if got == nil {
		t.Fatal("Load() = nil, want a non-nil empty map")
	}
	if len(got) != 0 {
		t.Fatalf("Load() = %v, want empty", got)
	}
}

func TestLoadSkipsCommentsBlanksAndMalformedLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvVar, home)
	body := strings.Join([]string{
		"# a comment",
		"",
		"   ",
		"NOEQUALS",
		"  ESHU_A = 1  ",
		"ESHU_B=x=y",
	}, "\n")
	writeTestEnvFile(t, filepath.Join(home, envFileName), body)

	got := Load()
	if len(got) != 2 {
		t.Fatalf("Load() = %v, want exactly ESHU_A and ESHU_B", got)
	}
	if got["ESHU_A"] != "1" {
		t.Errorf("ESHU_A = %q, want 1 (key and value trimmed)", got["ESHU_A"])
	}
	if got["ESHU_B"] != "x=y" {
		t.Errorf("ESHU_B = %q, want x=y (only the first = splits)", got["ESHU_B"])
	}
}

func TestResolveValueProfileKeyWinsOverBase(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvVar, home)
	writeTestEnvFile(t, filepath.Join(home, envFileName),
		"ESHU_SERVICE_URL=http://base.test\nESHU_SERVICE_URL_STAGING=http://staging.test\n")

	// The profile suffix is upper-cased before lookup, so the lower-case
	// profile name "staging" still finds the ESHU_SERVICE_URL_STAGING key.
	if got := ResolveValue("ESHU_SERVICE_URL", "staging"); got != "http://staging.test" {
		t.Errorf("ResolveValue(profile=staging) = %q, want the profile value", got)
	}
	if got := ResolveValue("ESHU_SERVICE_URL", ""); got != "http://base.test" {
		t.Errorf("ResolveValue(profile=\"\") = %q, want the base value", got)
	}
}

func TestResolveValueFallsBackWhenProfileValueIsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvVar, home)
	writeTestEnvFile(t, filepath.Join(home, envFileName),
		"ESHU_SERVICE_URL=http://base.test\nESHU_SERVICE_URL_PROD=\n")

	if got := ResolveValue("ESHU_SERVICE_URL", "prod"); got != "http://base.test" {
		t.Errorf("ResolveValue() = %q, want the base value when the profile value is empty", got)
	}
}

func TestResolveValueUnknownKeyReturnsEmpty(t *testing.T) {
	t.Setenv(homeEnvVar, t.TempDir())

	if got := ResolveValue("ESHU_NOT_SET", "prod"); got != "" {
		t.Errorf("ResolveValue() = %q, want empty for an unset key", got)
	}
}

func TestHomeExpandsTildePrefix(t *testing.T) {
	t.Setenv(homeEnvVar, "~/scratch-eshu-home")

	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("os.UserHomeDir() error = %v; no home directory to expand against", err)
	}
	want := filepath.Join(userHome, "scratch-eshu-home")
	if got := Home(); got != want {
		t.Fatalf("Home() = %q, want %q", got, want)
	}
}

func TestHomeDefaultsToDotEshuUnderUserHome(t *testing.T) {
	t.Setenv(homeEnvVar, "")

	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("os.UserHomeDir() error = %v; no home directory to join against", err)
	}
	want := filepath.Join(userHome, homeDirname)
	if got := Home(); got != want {
		t.Fatalf("Home() = %q, want %q", got, want)
	}
}

func TestEnvFilePathIsEnvFileUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvVar, home)

	if got, want := EnvFilePath(), filepath.Join(home, envFileName); got != want {
		t.Fatalf("EnvFilePath() = %q, want %q", got, want)
	}
}

func TestSetValueWritesKeysInSortedOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvVar, home)

	for _, key := range []string{"ESHU_Z", "ESHU_A", "ESHU_M"} {
		if err := SetValue(key, "v"); err != nil {
			t.Fatalf("SetValue(%q) error = %v, want nil", key, err)
		}
	}

	body := readTestEnvFile(t, filepath.Join(home, envFileName))
	want := "ESHU_A=v\nESHU_M=v\nESHU_Z=v\n"
	if body != want {
		t.Fatalf(".env = %q, want %q (stable sorted output)", body, want)
	}
}

func TestResetClearsEveryValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvVar, home)
	if err := SetValue("ESHU_API_KEY", "token"); err != nil {
		t.Fatalf("SetValue() error = %v, want nil", err)
	}

	if err := Reset(); err != nil {
		t.Fatalf("Reset() error = %v, want nil", err)
	}

	if got := Load(); len(got) != 0 {
		t.Fatalf("Load() after Reset() = %v, want empty", got)
	}
	// Reset writes an empty file body (a lone newline), not a deleted file:
	// operators and `config show` both still expect the path to exist.
	if body := readTestEnvFile(t, filepath.Join(home, envFileName)); body != "\n" {
		t.Fatalf(".env after Reset() = %q, want a single newline", body)
	}
}

func writeTestEnvFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}

func readTestEnvFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v, want nil", path, err)
	}
	return string(body)
}
