// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// fixtureEngineFile writes body under dir/name and returns the source path
// plus its hex SHA-256, so tests build exact pin sets without a tesseract
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

// fixtureDocEnginePayload builds a small pinned engine payload with the
// production target layout (bin/ + lib/ + tessdata/) over fixture files.
func fixtureDocEnginePayload(t *testing.T) engineSeedPayload {
	t.Helper()
	src := t.TempDir()
	bin, binSHA := fixtureEngineFile(t, src, "tesseract", []byte("fake-tesseract-binary"))
	lib, libSHA := fixtureEngineFile(t, src, "libtesseract.so.5", []byte("fake-lib-bytes"))
	model, modelSHA := fixtureEngineFile(t, src, "eng.traineddata", []byte("fake-model-bytes"))
	return engineSeedPayload{
		engine:   "tesseract",
		version:  "5.5.0",
		platform: "linux/test",
		files: []engineSeedFile{
			{source: bin, target: "bin/tesseract", sha256: binSHA, executable: true},
			{source: lib, target: "lib/libtesseract.so.5", sha256: libSHA},
			{source: model, target: "tessdata/eng.traineddata", sha256: modelSHA},
		},
	}
}

// engineRootFiles lists the relative paths seedEnginePayload must leave
// behind for the fixture payload: every pinned file plus the pin manifest
// and the musl loader path file, nothing else.
func engineRootFiles() []string {
	return []string{
		"bin/tesseract",
		"lib/libtesseract.so.5",
		"tessdata/eng.traineddata",
		"pin-manifest.yaml",
		"lib-path",
	}
}

// TestSeedEnginePayloadCopiesPinnedSet proves the seed materializes the
// exact pinned engine set: every file lands at its layout path with
// identical bytes, the binary stays executable, lib-path names the seeded
// lib dir, and the pin manifest records exactly the pinned set.
func TestSeedEnginePayloadCopiesPinnedSet(t *testing.T) {
	t.Parallel()

	payload := fixtureDocEnginePayload(t)
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
	want := engineRootFiles()
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
	if info, err := os.Stat(filepath.Join(root, "bin/tesseract")); err != nil {
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

	payload := fixtureDocEnginePayload(t)
	tampered := payload.files[2].source
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
	if !strings.Contains(err.Error(), "tessdata/eng.traineddata") {
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

	payload := fixtureDocEnginePayload(t)
	payload.files[1].source = filepath.Join(t.TempDir(), "missing-lib.so")

	dest := filepath.Join(t.TempDir(), "dest")
	if err := seedEnginePayload(dest, payload); err == nil {
		t.Fatal("seedEnginePayload with a missing library: got nil error, want a failure")
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

// satellitePinFile mirrors specs/satellite-pins/ocr.<arch>.json for the
// drift test below.
type satellitePinFile struct {
	Engine struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"engine"`
	Binary struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"binary"`
	Libraries []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"libraries"`
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
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "specs", "satellite-pins", "ocr."+arch+".json"))
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
		bySource := map[string]engineSeedFile{}
		for _, file := range payload.files {
			bySource[file.source] = file
		}
		bin, ok := bySource[pins.Binary.Path]
		if !ok {
			t.Fatalf("%s: payload omits pinned binary %q (a pin unseeded)", arch, pins.Binary.Path)
		}
		if bin.sha256 != pins.Binary.SHA256 || bin.target != "bin/tesseract" || !bin.executable {
			t.Fatalf("%s: binary entry = %+v, want target bin/tesseract sha %q executable",
				arch, bin, pins.Binary.SHA256)
		}
		model, ok := bySource[pins.Model.Path]
		if !ok {
			t.Fatalf("%s: payload omits pinned model %q (a pin unseeded)", arch, pins.Model.Path)
		}
		if model.sha256 != pins.Model.SHA256 || model.target != "tessdata/eng.traineddata" || model.executable {
			t.Fatalf("%s: model entry = %+v, want target tessdata/eng.traineddata sha %q non-executable",
				arch, model, pins.Model.SHA256)
		}
		if len(payload.files) != len(pins.Libraries)+2 {
			t.Fatalf("%s: payload files = %d, want %d (binary + %d libraries + model)",
				arch, len(payload.files), len(pins.Libraries)+2, len(pins.Libraries))
		}
		bases := map[string]bool{}
		for _, lib := range pins.Libraries {
			entry, ok := bySource[lib.Path]
			if !ok {
				t.Fatalf("%s: payload omits pinned library %q (a pin unseeded)", arch, lib.Path)
			}
			if entry.sha256 != lib.SHA256 || entry.executable {
				t.Fatalf("%s: library %q entry = %+v, want sha %q non-executable",
					arch, lib.Path, entry, lib.SHA256)
			}
			base := filepath.Base(lib.Path)
			if entry.target != "lib/"+base {
				t.Fatalf("%s: library %q target = %q, want flat lib/%q", arch, lib.Path, entry.target, base)
			}
			if bases[base] {
				t.Fatalf("%s: library basename %q collides, the flat lib/ layout is ambiguous", arch, base)
			}
			bases[base] = true
		}
	}
}

// TestSeedComponentWritesEnginePayload proves the full seed path writes the
// collector, the stamped manifest, and the engine payload together, and that
// a tampered engine file removes the whole destination.
func TestSeedComponentWritesEnginePayload(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	binary := seedTestBinary(t, filepath.Join(work, "collector"), []byte("fake-collector-bytes"))
	manifest := seedTestManifest(t, filepath.Join(work, "manifest.yaml"))
	engine := fixtureDocEnginePayload(t)
	engine.platform = "linux/amd64"
	dest := filepath.Join(work, "dest")

	if err := seedComponent(dest, seedTestRef, binary, manifest, "linux/amd64", engine); err != nil {
		t.Fatalf("seedComponent: %v", err)
	}
	for _, rel := range append([]string{"collector", "manifest.yaml"}, []string{
		"engine/bin/tesseract",
		"engine/lib/libtesseract.so.5",
		"engine/tessdata/eng.traineddata",
		"engine/pin-manifest.yaml",
		"engine/lib-path",
	}...) {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Fatalf("seed dest misses %q: %v", rel, err)
		}
	}

	tampered := fixtureDocEnginePayload(t)
	// Reach the engine copy: the platform guard would otherwise reject this
	// fixture ("linux/test") before any digest verification, and the test
	// would pass without exercising fail-closed cleanup.
	tampered.platform = "linux/amd64"
	fh, err := os.OpenFile(tampered.files[0].source, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open fixture for tampering: %v", err)
	}
	if _, err := fh.WriteString("tampered"); err != nil {
		_ = fh.Close()
		t.Fatalf("tamper fixture: %v", err)
	}
	_ = fh.Close()
	badDest := filepath.Join(work, "bad-dest")
	seedErr := seedComponent(badDest, seedTestRef, binary, manifest, "linux/amd64", tampered)
	if seedErr == nil {
		t.Fatal("seedComponent with a tampered engine binary: got nil error, want a digest failure")
	}
	if !strings.Contains(seedErr.Error(), "bin/tesseract") {
		t.Fatalf("tamper error = %q, want it to name the mismatched engine file", seedErr)
	}
	if _, statErr := os.Stat(badDest); !os.IsNotExist(statErr) {
		t.Fatalf("tampered seed left dest behind, want no partial payload")
	}
}

// TestEmbeddedManifestIdentity proves the embedded manifest names this
// component, so the claim path serves exactly the binary's own manifest.
func TestEmbeddedManifestIdentity(t *testing.T) {
	t.Parallel()

	manifest, err := loadSDKStdioManifest("")
	if err != nil {
		t.Fatalf("loadSDKStdioManifest(embedded): %v", err)
	}
	if manifest.Metadata.ID != componentID {
		t.Fatalf("embedded manifest id = %q, want %q", manifest.Metadata.ID, componentID)
	}
	if len(manifest.Spec.Artifacts) == 0 {
		t.Fatal("embedded manifest declares no artifacts")
	}
}

// TestClaimUsesEmbeddedManifest proves the claim path needs no manifest file:
// with no manifestPath configured it serves the embedded manifest and acks
// unchanged on an empty artifact dir.
func TestClaimUsesEmbeddedManifest(t *testing.T) {
	t.Parallel()

	grantHome := t.TempDir()
	recordDocGrants(t, grantHome)

	var stdout bytes.Buffer
	err := serveSDKStdio(context.Background(), bytes.NewReader(stdioTestRequest(t, stdioTestClaim())), &stdout, sdkStdioDeps{
		getenv:       func(key string) string { return stdioTestEnv(t, t.TempDir())[key] },
		manifestPath: "",
		grantHome:    grantHome,
		newExtractor: func(Config) (extractor, error) { return stdioTestEngine(), nil },
	})
	if err != nil {
		t.Fatalf("serveSDKStdio with the embedded manifest: %v", err)
	}
	result := decodeStdioResult(t, &stdout)
	if result.State != sdkcollector.ResultUnchanged {
		t.Fatalf("result state = %q, want unchanged", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("result carries %d facts, want zero", len(result.Facts))
	}
}
