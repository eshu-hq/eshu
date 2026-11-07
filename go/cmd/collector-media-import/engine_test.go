// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// fixtureEngineFile writes body under dir/name and returns the source path
// plus its hex SHA-256, so tests build exact pin sets without a whisper.cpp
// installation.
func fixtureEngineFile(t *testing.T, dir, name string, body []byte) (string, string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write fixture engine file: %v", err)
	}
	sum := sha256.Sum256(body)
	return path, hex.EncodeToString(sum[:])
}

// fixtureMediaEnginePayload builds a small pinned engine payload with the
// production target layout (bin/ + models/) over fixture files.
func fixtureMediaEnginePayload(t *testing.T) engineSeedPayload {
	t.Helper()
	src := t.TempDir()
	bin, binSHA := fixtureEngineFile(t, src, "whisper-cli", []byte("fake-whisper-binary"))
	model, modelSHA := fixtureEngineFile(t, src, "ggml-base.en.bin", []byte("fake-model-bytes"))
	return engineSeedPayload{
		engine:   "whisper.cpp",
		version:  "v1.9.5",
		platform: "linux/test",
		files: []engineSeedFile{
			{source: bin, target: "bin/whisper-cli", sha256: binSHA, executable: true},
			{source: model, target: "models/ggml-base.en.bin", sha256: modelSHA},
		},
	}
}

// TestSeedEnginePayloadCopiesPinnedSet proves the seed materializes the
// exact pinned engine set: every file lands at its layout path with
// identical bytes without a whole-file read, the binary stays executable,
// lib-path names the seeded lib dir, and the pin manifest records exactly
// the pinned set.
func TestSeedEnginePayloadCopiesPinnedSet(t *testing.T) {
	t.Parallel()

	payload := fixtureMediaEnginePayload(t)
	dest := filepath.Join(t.TempDir(), "dest")
	if err := seedEnginePayload(dest, payload); err != nil {
		t.Fatalf("seedEnginePayload: %v", err)
	}
	root := filepath.Join(dest, "engine")
	var found []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found = append(found, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk seeded engine dir: %v", err)
	}
	want := []string{
		"bin/whisper-cli",
		"models/ggml-base.en.bin",
		"pin-manifest.yaml",
		"lib-path",
	}
	if len(found) != len(want) {
		t.Fatalf("seeded engine files = %q, want exactly %q", found, want)
	}
	seen := map[string]bool{}
	for _, rel := range found {
		seen[rel] = true
	}
	for _, rel := range want {
		if !seen[rel] {
			t.Fatalf("seeded engine files = %q, missing %q", found, rel)
		}
	}
	for _, file := range payload.files {
		got, err := os.ReadFile(filepath.Join(root, file.target))
		if err != nil {
			t.Fatalf("read seeded %q: %v", file.target, err)
		}
		// #nosec G304 -- source paths are test fixtures created above.
		original, err := os.ReadFile(file.source)
		if err != nil {
			t.Fatalf("read fixture %q: %v", file.source, err)
		}
		if !bytes.Equal(got, original) {
			t.Fatalf("seeded %q differs from the pinned source bytes", file.target)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "bin/whisper-cli")); err != nil {
		t.Fatalf("stat seeded binary: %v", err)
	} else if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("seeded binary mode = %v, want an executable bit", info.Mode())
	}
	libDir, err := filepath.Abs(filepath.Join(root, "lib"))
	if err != nil {
		t.Fatalf("absolute lib dir: %v", err)
	}
	rawPath, err := os.ReadFile(filepath.Join(root, "lib-path"))
	if err != nil {
		t.Fatalf("read seeded lib-path: %v", err)
	}
	if strings.TrimSpace(string(rawPath)) != libDir {
		t.Fatalf("seeded lib-path = %q, want the seeded engine lib dir %q", rawPath, libDir)
	}
	rawManifest, err := os.ReadFile(filepath.Join(root, "pin-manifest.yaml"))
	if err != nil {
		t.Fatalf("read seeded pin manifest: %v", err)
	}
	var manifest enginePinManifest
	if err := yaml.Unmarshal(rawManifest, &manifest); err != nil {
		t.Fatalf("decode seeded pin manifest: %v", err)
	}
	if manifest.Engine != payload.engine || manifest.Version != payload.version || manifest.Platform != payload.platform {
		t.Fatalf("pin manifest identity = %+v, want engine %q version %q platform %q",
			manifest, payload.engine, payload.version, payload.platform)
	}
	if len(manifest.Files) != len(payload.files) {
		t.Fatalf("pin manifest files = %d, want %d (no seeded file unpinned, no pin unseeded)",
			len(manifest.Files), len(payload.files))
	}
	for i, entry := range manifest.Files {
		if entry.Path != payload.files[i].target || entry.SHA256 != payload.files[i].sha256 {
			t.Fatalf("pin manifest entry %d = %+v, want target %q sha %q",
				i, entry, payload.files[i].target, payload.files[i].sha256)
		}
	}
}

// TestSeedEnginePayloadRefusesTamperedFile proves a source file whose bytes
// no longer match the pin fails closed with no partial engine payload left
// behind.
func TestSeedEnginePayloadRefusesTamperedFile(t *testing.T) {
	t.Parallel()

	payload := fixtureMediaEnginePayload(t)
	tampered := payload.files[1].source
	fh, err := os.OpenFile(tampered, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open fixture for tampering: %v", err)
	}
	if _, err := fh.WriteString("tampered"); err != nil {
		_ = fh.Close()
		t.Fatalf("tamper fixture: %v", err)
	}
	_ = fh.Close()

	dest := filepath.Join(t.TempDir(), "dest")
	err = seedEnginePayload(dest, payload)
	if err == nil {
		t.Fatal("seedEnginePayload with a tampered model: got nil error, want a digest failure")
	}
	if !strings.Contains(err.Error(), "models/ggml-base.en.bin") {
		t.Fatalf("tamper error = %q, want it to name the mismatched file", err)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "engine")); !os.IsNotExist(statErr) {
		t.Fatalf("tampered seed left an engine dir behind, want no partial payload")
	}
}

// TestSeedEnginePayloadCleansUpPartialCopy proves a failure midway through
// the copy removes the files already written, so a missing source never
// leaves a partial engine payload.
func TestSeedEnginePayloadCleansUpPartialCopy(t *testing.T) {
	t.Parallel()

	payload := fixtureMediaEnginePayload(t)
	payload.files[0].source = filepath.Join(t.TempDir(), "missing-whisper-cli")

	dest := filepath.Join(t.TempDir(), "dest")
	if err := seedEnginePayload(dest, payload); err == nil {
		t.Fatal("seedEnginePayload with a missing binary: got nil error, want a failure")
	}
	if _, statErr := os.Stat(filepath.Join(dest, "engine")); !os.IsNotExist(statErr) {
		t.Fatalf("failed seed left an engine dir behind, want no partial payload")
	}
}

// TestEnginePayloadForPlatformRefusesUnknown proves an unpinned platform
// fails before anything is written.
func TestEnginePayloadForPlatformRefusesUnknown(t *testing.T) {
	t.Parallel()

	if _, err := enginePayloadForPlatform("linux/riscv64"); err == nil {
		t.Fatal("enginePayloadForPlatform(riscv64): got nil error, want a failure")
	} else if !strings.Contains(err.Error(), "linux/riscv64") {
		t.Fatalf("unknown-platform error = %q, want it to name the platform", err)
	}
}

// satellitePinFile mirrors specs/satellite-pins/transcription.<arch>.json
// for the drift test below.
type satellitePinFile struct {
	Engine struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"engine"`
	Binary struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"binary"`
	Model struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"model"`
}

// TestEnginePayloadPinsMatchSatellitePins proves the embedded pin tables are
// exactly the reviewed satellite pins: the seed copies the pinned set, no
// seeded file unpinned and no pin unseeded.
func TestEnginePayloadPinsMatchSatellitePins(t *testing.T) {
	t.Parallel()

	for _, arch := range []string{"amd64", "arm64"} {
		platform := "linux/" + arch
		payload, err := enginePayloadForPlatform(platform)
		if err != nil {
			t.Fatalf("enginePayloadForPlatform(%q): %v", platform, err)
		}
		// #nosec G304 -- test-only read of the committed satellite pins.
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "specs", "satellite-pins", "transcription."+arch+".json"))
		if err != nil {
			t.Fatalf("read satellite pins for %s: %v", arch, err)
		}
		var pins satellitePinFile
		if err := json.Unmarshal(raw, &pins); err != nil {
			t.Fatalf("decode satellite pins for %s: %v", arch, err)
		}
		if payload.engine != pins.Engine.Name || payload.version != pins.Engine.Version {
			t.Fatalf("%s: payload engine = %q %q, pins = %q %q",
				arch, payload.engine, payload.version, pins.Engine.Name, pins.Engine.Version)
		}
		if len(payload.files) != 2 {
			t.Fatalf("%s: payload files = %d, want 2 (binary + model)", arch, len(payload.files))
		}
		bySource := map[string]engineSeedFile{}
		for _, file := range payload.files {
			bySource[file.source] = file
		}
		bin, ok := bySource[pins.Binary.Path]
		if !ok {
			t.Fatalf("%s: payload omits pinned binary %q (a pin unseeded)", arch, pins.Binary.Path)
		}
		if bin.sha256 != pins.Binary.SHA256 || bin.target != "bin/whisper-cli" || !bin.executable {
			t.Fatalf("%s: binary entry = %+v, want target bin/whisper-cli sha %q executable",
				arch, bin, pins.Binary.SHA256)
		}
		model, ok := bySource[pins.Model.Path]
		if !ok {
			t.Fatalf("%s: payload omits pinned model %q (a pin unseeded)", arch, pins.Model.Path)
		}
		if model.sha256 != pins.Model.SHA256 || model.target != "models/ggml-base.en.bin" || model.executable {
			t.Fatalf("%s: model entry = %+v, want target models/ggml-base.en.bin sha %q non-executable",
				arch, model, pins.Model.SHA256)
		}
	}
}

// TestSeedWritesEnginePayload proves the full seed path writes the caller,
// the stamped manifest, and the engine payload together, and that a missing
// engine source removes the whole destination.
func TestSeedWritesEnginePayload(t *testing.T) {
	t.Parallel()

	self := filepath.Join(t.TempDir(), "collector-fixture")
	if err := os.WriteFile(self, []byte("fake-collector-binary"), 0o755); err != nil {
		t.Fatalf("write fixture binary: %v", err)
	}
	engine := fixtureMediaEnginePayload(t)
	engine.platform = "linux/amd64"
	dest := t.TempDir()

	if err := runSeed(dest, mediaSeedTestRef, self, "linux/amd64", engine); err != nil {
		t.Fatalf("runSeed: %v", err)
	}
	for _, rel := range []string{
		"collector",
		"manifest.yaml",
		"engine/bin/whisper-cli",
		"engine/models/ggml-base.en.bin",
		"engine/pin-manifest.yaml",
		"engine/lib-path",
	} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Fatalf("seed dest misses %q: %v", rel, err)
		}
	}

	broken := fixtureMediaEnginePayload(t)
	// Reach the engine copy: the platform guard would otherwise reject this
	// fixture ("linux/test") before any copy, and the test would pass
	// without exercising engine-failure propagation or dest cleanup.
	broken.platform = "linux/amd64"
	broken.files[0].source = filepath.Join(t.TempDir(), "missing-whisper-cli")
	badSelf := filepath.Join(t.TempDir(), "collector-fixture")
	if err := os.WriteFile(badSelf, []byte("fake-collector-binary"), 0o755); err != nil {
		t.Fatalf("write fixture binary: %v", err)
	}
	badDest := filepath.Join(t.TempDir(), "dest")
	seedErr := runSeed(badDest, mediaSeedTestRef, badSelf, "linux/amd64", broken)
	if seedErr == nil {
		t.Fatal("runSeed with a missing engine binary: got nil error, want a failure")
	}
	if !strings.Contains(seedErr.Error(), "bin/whisper-cli") {
		t.Fatalf("missing-source error = %q, want it to name the engine target", seedErr)
	}
	if _, statErr := os.Stat(badDest); !os.IsNotExist(statErr) {
		t.Fatalf("failed seed left dest behind, want no partial payload")
	}
}
