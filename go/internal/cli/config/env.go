// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// homeDirname is the per-user Eshu config directory created under the
	// operating-system home directory when ESHU_HOME is unset.
	homeDirname = ".eshu"
	// homeEnvVar overrides the config directory. Home reads it plus the
	// platform home variable via os.UserHomeDir (HOME on Unix, USERPROFILE
	// on Windows, home on Plan 9) -- the package's whole environment
	// surface, per doc.go. Every other ESHU_* value comes out of the .env
	// file, not the process environment.
	homeEnvVar = "ESHU_HOME"
	// envFileName is the key=value file inside the config directory that
	// holds the CLI's persisted settings.
	envFileName = ".env"
)

// Home returns the Eshu config directory: $ESHU_HOME when set (with a leading
// "~" expanded against the user's home directory), otherwise ~/.eshu.
//
// A home-directory lookup failure is swallowed, matching the CLI's behavior of
// degrading to a relative path rather than failing a command outright: the
// subsequent file read or write reports the real problem with a path in it.
func Home() string {
	if v := os.Getenv(homeEnvVar); v != "" {
		if strings.HasPrefix(v, "~") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, v[1:])
		}
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, homeDirname)
}

// EnvFilePath returns the path of the .env settings file inside Home.
func EnvFilePath() string {
	return filepath.Join(Home(), envFileName)
}

// Load reads the .env settings file into a map of key to value.
//
// A missing or unreadable file yields an empty (non-nil) map rather than an
// error: an operator who has never run `eshu config set` has no file, and that
// is the normal first-run state, not a failure. Blank lines, lines starting
// with "#", and lines without an "=" are skipped; keys and values are trimmed.
// Only the first "=" splits a line, so a value may itself contain "=".
func Load() map[string]string {
	config := make(map[string]string)
	f, err := os.Open(EnvFilePath())
	if err != nil {
		return config
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		config[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return config
}

// ResolveValue returns the persisted value for key, preferring the
// profile-scoped variant "<key>_<PROFILE>" when profile is non-empty and that
// variant holds a non-empty value. It falls back to the bare key, and returns
// "" when neither is set.
//
// The profile name is upper-cased for the lookup, so `--profile staging` finds
// the key ESHU_SERVICE_URL_STAGING. An empty profile-scoped value falls through
// to the base key rather than resolving to "", so an operator can leave a
// profile entry blank without blanking the shared default.
func ResolveValue(key, profile string) string {
	config := Load()

	// Try profile-specific key first.
	if profile != "" {
		profileKey := key + "_" + strings.ToUpper(profile)
		if v, ok := config[profileKey]; ok && v != "" {
			return v
		}
	}

	// Fall back to base key.
	if v, ok := config[key]; ok {
		return v
	}
	return ""
}

// SetValue persists one key=value pair into the .env settings file, creating
// the config directory when it does not exist yet and preserving every other
// key already in the file.
func SetValue(key, value string) error {
	dir := Home()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		// Unwrapped on purpose: this error already names the directory it
		// failed on, and `eshu config set` prints it verbatim to the
		// operator. Wrapping it with %w would change that operator-visible
		// text for no added context.
		return err //nolint:wrapcheck // os.MkdirAll's *PathError already names the failing directory; the CLI prints it verbatim.
	}

	config := Load()
	config[key] = value
	return write(config)
}

// Reset clears every persisted setting, leaving an empty .env file behind.
//
// It does not create the config directory: resetting settings that were never
// written is a no-op the operator should hear about as a failed write, not a
// side effect that materializes a directory.
func Reset() error {
	return write(map[string]string{})
}

// write replaces the .env settings file with config, one "key=value" line per
// entry sorted by line so the file's byte content is stable across runs and
// diffable. The file is written 0600 because it holds API keys.
func write(config map[string]string) error {
	var lines []string
	for k, v := range config {
		lines = append(lines, fmt.Sprintf("%s=%s", k, v))
	}
	// Sort for stable output.
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lines[j] < lines[j-1]; j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
	// Unwrapped on purpose, same reason as SetValue's os.MkdirAll: the
	// *PathError already names the .env path the CLI failed to write.
	return os.WriteFile(EnvFilePath(), []byte(strings.Join(lines, "\n")+"\n"), 0o600) //nolint:wrapcheck // os.WriteFile's *PathError already names the .env path; the CLI prints it verbatim.
}
