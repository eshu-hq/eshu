// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
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
	Libraries []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"libraries"`
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
		if len(payload.files) != 4 {
			t.Fatalf("%s: payload files = %d, want 4 (binary + model + 2 runtime libs)", arch, len(payload.files))
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
		for _, lib := range pins.Libraries {
			base := filepath.Base(lib.Path)
			if base != "libstdc++.so.6.0.33" && base != "libgcc_s.so.1" {
				continue
			}
			entry, ok := bySource[lib.Path]
			if !ok {
				t.Fatalf("%s: payload omits pinned runtime library %q (a pin unseeded)", arch, lib.Path)
			}
			if entry.sha256 != lib.SHA256 || entry.target != "lib/"+base || entry.executable {
				t.Fatalf("%s: library entry = %+v, want target lib/%s sha %q non-executable",
					arch, entry, base, lib.SHA256)
			}
		}
	}
}

// TestEnginePayloadSeedsWhisperRuntimeLibs proves the payload carries the
// two shared libraries whisper-cli needs at runtime (libstdc++.so.6 and
// libgcc_s.so.1): live QA showed the binary fails to start with the seeded
// empty lib/ dir because neither library ships in the worker image.
func TestEnginePayloadSeedsWhisperRuntimeLibs(t *testing.T) {
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
		want := map[string]string{}
		for _, lib := range pins.Libraries {
			if base := filepath.Base(lib.Path); base == "libstdc++.so.6.0.33" || base == "libgcc_s.so.1" {
				want[lib.Path] = lib.SHA256
			}
		}
		if len(want) != 2 {
			t.Fatalf("%s: pins list %d whisper runtime libs, want libstdc++.so.6.0.33 + libgcc_s.so.1", arch, len(want))
		}
		bySource := map[string]engineSeedFile{}
		for _, file := range payload.files {
			bySource[file.source] = file
		}
		for path, sha := range want {
			entry, ok := bySource[path]
			if !ok {
				t.Fatalf("%s: payload omits runtime library %q (whisper-cli cannot start without it)", arch, path)
			}
			if entry.target != "lib/"+filepath.Base(path) || entry.sha256 != sha || entry.executable {
				t.Fatalf("%s: library entry = %+v, want target lib/%s sha %q non-executable",
					arch, entry, filepath.Base(path), sha)
			}
		}
	}
}

// writeSonameFixture builds a minimal ELF64 shared object whose DT_SONAME
// is soname and returns its path plus hex SHA-256. It exercises the
// production debug/elf derivation path without vendoring a real library.
func writeSonameFixture(t *testing.T, dir, name, soname string) (string, string) {
	t.Helper()
	dynstr := append([]byte{0}, append([]byte(soname), 0)...)
	dyn := make([]byte, 32)
	binary.LittleEndian.PutUint64(dyn[0:8], 14)
	binary.LittleEndian.PutUint64(dyn[8:16], 1)
	shstr := []byte{0}
	dynstrName := ".dynstr\x00"
	dynName := ".dynamic\x00"
	shName := ".shstrtab\x00"
	offDynstrName, offDynName, offShName := len(shstr), 0, 0
	shstr = append(shstr, []byte(dynstrName)...)
	offDynName = len(shstr)
	shstr = append(shstr, []byte(dynName)...)
	offShName = len(shstr)
	shstr = append(shstr, []byte(shName)...)
	offDynstr, offDyn, offShstr := 64, 64+len(dynstr), 64+len(dynstr)+len(dyn)
	shoff := offShstr + len(shstr)
	for shoff%8 != 0 {
		shoff++
	}
	buf := make([]byte, shoff+4*64)
	copy(buf[0:4], []byte{0x7f, 'E', 'L', 'F'})
	buf[4], buf[5], buf[6] = 2, 1, 1
	binary.LittleEndian.PutUint16(buf[16:18], 3)
	binary.LittleEndian.PutUint16(buf[18:20], 62)
	binary.LittleEndian.PutUint32(buf[20:24], 1)
	binary.LittleEndian.PutUint64(buf[40:48], uint64(shoff))
	buf[52], buf[58], buf[60], buf[62] = 64, 64, 4, 3
	copy(buf[offDynstr:], dynstr)
	copy(buf[offDyn:], dyn)
	copy(buf[offShstr:], shstr)
	writeSH := func(i int, name uint32, typ uint32, flags uint64, off uint64, size uint64, link uint32, align uint64, entsize uint64) {
		b := buf[shoff+i*64:]
		binary.LittleEndian.PutUint32(b[0:4], name)
		binary.LittleEndian.PutUint32(b[4:8], typ)
		binary.LittleEndian.PutUint64(b[8:16], flags)
		binary.LittleEndian.PutUint64(b[24:32], off)
		binary.LittleEndian.PutUint64(b[32:40], size)
		binary.LittleEndian.PutUint32(b[40:44], link)
		binary.LittleEndian.PutUint64(b[48:56], align)
		binary.LittleEndian.PutUint64(b[56:64], entsize)
	}
	writeSH(1, uint32(offDynstrName), 3, 2, uint64(offDynstr), uint64(len(dynstr)), 0, 1, 0)
	writeSH(2, uint32(offDynName), 6, 3, uint64(offDyn), uint64(len(dyn)), 1, 8, 16)
	writeSH(3, uint32(offShName), 3, 0, uint64(offShstr), uint64(len(shstr)), 0, 1, 0)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write soname fixture: %v", err)
	}
	sum := sha256.Sum256(buf)
	return path, hex.EncodeToString(sum[:])
}

// TestSeedEnginePayloadPlantsSonameSymlink proves the composed seed derives
// the ELF DT_SONAME from verified lib bytes and plants a resolving relative
// soname link beside the real file.
func TestSeedEnginePayloadPlantsSonameSymlink(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	real, soname := "libsonametest.so.1.2.3", "libsonametest.so.1"
	libSrc, libSHA := writeSonameFixture(t, src, real, soname)
	binSrc, binSHA := fixtureEngineFile(t, src, "whisper-cli", []byte("fake-whisper-binary"))
	modelSrc, modelSHA := fixtureEngineFile(t, src, "ggml-base.en.bin", []byte("fake-model-bytes"))
	payload := engineSeedPayload{engine: "whisper.cpp", version: "v1.9.5", platform: "linux/test", files: []engineSeedFile{
		{source: binSrc, target: "bin/whisper-cli", sha256: binSHA, executable: true},
		{source: libSrc, target: "lib/" + real, sha256: libSHA},
		{source: modelSrc, target: "models/ggml-base.en.bin", sha256: modelSHA},
	}}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := seedEnginePayload(dest, payload); err != nil {
		t.Fatalf("seedEnginePayload: %v", err)
	}
	link := filepath.Join(dest, "engine", "lib", soname)
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("seed left no soname link %q: %v", soname, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("seeded %q mode = %v, want a symlink", soname, fi.Mode())
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink %q: %v", soname, err)
	}
	if got != real {
		t.Fatalf("soname link target = %q, want relative %q", got, real)
	}
	// #nosec G304 -- link joins the seeded engine lib dir under test.
	viaLink, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("read through soname link: %v", err)
	}
	// #nosec G304 -- source path is a test fixture created above.
	original, err := os.ReadFile(libSrc)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !bytes.Equal(viaLink, original) {
		t.Fatal("bytes through the soname link differ from the pinned source bytes")
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

// writeMultiSonameFixture builds a minimal ELF64 shared object declaring
// two DT_SONAME entries. The seeder must fail closed: exactly one SONAME
// is the contract.
func writeMultiSonameFixture(t *testing.T, dir, name, first, second string) string {
	t.Helper()
	dynstr := []byte{0}
	off1 := len(dynstr)
	dynstr = append(dynstr, append([]byte(first), 0)...)
	off2 := len(dynstr)
	dynstr = append(dynstr, append([]byte(second), 0)...)
	dyn := make([]byte, 48)
	binary.LittleEndian.PutUint64(dyn[0:8], 14)
	binary.LittleEndian.PutUint64(dyn[8:16], uint64(off1))
	binary.LittleEndian.PutUint64(dyn[16:24], 14)
	binary.LittleEndian.PutUint64(dyn[24:32], uint64(off2))
	shstr := []byte{0}
	offDynstrName := len(shstr)
	shstr = append(shstr, []byte(".dynstr\x00")...)
	offDynName := len(shstr)
	shstr = append(shstr, []byte(".dynamic\x00")...)
	offShName := len(shstr)
	shstr = append(shstr, []byte(".shstrtab\x00")...)
	offDynstr, offDyn := 64, 64+len(dynstr)
	offShstr := offDyn + len(dyn)
	shoff := offShstr + len(shstr)
	for shoff%8 != 0 {
		shoff++
	}
	buf := make([]byte, shoff+4*64)
	copy(buf[0:4], []byte{0x7f, 'E', 'L', 'F'})
	buf[4], buf[5], buf[6] = 2, 1, 1
	binary.LittleEndian.PutUint16(buf[16:18], 3)
	binary.LittleEndian.PutUint16(buf[18:20], 62)
	binary.LittleEndian.PutUint32(buf[20:24], 1)
	binary.LittleEndian.PutUint64(buf[40:48], uint64(shoff))
	buf[52], buf[58], buf[60], buf[62] = 64, 64, 4, 3
	copy(buf[offDynstr:], dynstr)
	copy(buf[offDyn:], dyn)
	copy(buf[offShstr:], shstr)
	writeSH := func(i int, name uint32, typ uint32, flags uint64, off uint64, size uint64, link uint32) {
		b := buf[shoff+i*64:]
		binary.LittleEndian.PutUint32(b[0:4], name)
		binary.LittleEndian.PutUint32(b[4:8], typ)
		binary.LittleEndian.PutUint64(b[8:16], flags)
		binary.LittleEndian.PutUint64(b[24:32], off)
		binary.LittleEndian.PutUint64(b[32:40], size)
		binary.LittleEndian.PutUint32(b[40:44], link)
		binary.LittleEndian.PutUint64(b[48:56], 1)
		if typ == 6 {
			binary.LittleEndian.PutUint64(b[48:56], 8)
			binary.LittleEndian.PutUint64(b[56:64], 16)
		}
	}
	writeSH(1, uint32(offDynstrName), 3, 2, uint64(offDynstr), uint64(len(dynstr)), 0)
	writeSH(2, uint32(offDynName), 6, 3, uint64(offDyn), uint64(len(dyn)), 1)
	writeSH(3, uint32(offShName), 3, 0, uint64(offShstr), uint64(len(shstr)), 0)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write multi-soname fixture: %v", err)
	}
	return path
}

// TestPlantSonameSymlinkGuards proves every fail-closed branch of the
// soname derivation: unsafe names, collisions, malformed ELF, and
// multi-SONAME objects. Deleting any guard must turn its row red.
func TestPlantSonameSymlinkGuards(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		soname  string
		multi   bool
		prelink string
		wantErr string
	}{
		{name: "absolute soname", soname: "/lib/evil.so", wantErr: "unsafe SONAME"},
		{name: "parent traversal", soname: "../evil.so", wantErr: "unsafe SONAME"},
		{name: "nested path", soname: "sub/evil.so", wantErr: "unsafe SONAME"},
		{name: "dot soname", soname: ".", wantErr: "unsafe SONAME"},
		{name: "dotdot soname", soname: "..", wantErr: "unsafe SONAME"},
		{name: "multi soname", soname: "libmulti.so.1", multi: true, wantErr: "exactly one"},
		{name: "colliding regular file", soname: "libcollide.so.1", prelink: "file", wantErr: "collides"},
		{name: "colliding wrong symlink", soname: "libcollide.so.1", prelink: "symlink", wantErr: "collides"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			real := "libguard.so.9.9.9"
			var lib string
			if tc.multi {
				lib = filepath.Join(dir, real)
				src := writeMultiSonameFixture(t, dir, "src.so", "libmulti.so.1", "libmulti.so.2")
				// #nosec G304 -- source is a test fixture created above.
				raw, err := os.ReadFile(src)
				if err != nil {
					t.Fatalf("read multi-soname fixture: %v", err)
				}
				if err := os.WriteFile(lib, raw, 0o600); err != nil {
					t.Fatalf("stage multi-soname fixture: %v", err)
				}
			} else {
				var src string
				src, _ = writeSonameFixture(t, dir, "src.so", tc.soname)
				// #nosec G304 -- source is a test fixture created above.
				raw, err := os.ReadFile(src)
				if err != nil {
					t.Fatalf("read soname fixture: %v", err)
				}
				lib = filepath.Join(dir, real)
				if err := os.WriteFile(lib, raw, 0o600); err != nil {
					t.Fatalf("stage soname fixture: %v", err)
				}
			}
			switch tc.prelink {
			case "file":
				if err := os.WriteFile(filepath.Join(dir, "libcollide.so.1"), []byte("x"), 0o600); err != nil {
					t.Fatalf("stage colliding file: %v", err)
				}
			case "symlink":
				if err := os.Symlink("elsewhere.so", filepath.Join(dir, "libcollide.so.1")); err != nil {
					t.Fatalf("stage colliding symlink: %v", err)
				}
			}
			err := plantSonameSymlink(lib)
			if err == nil {
				t.Fatalf("plantSonameSymlink(%q): got nil error, want %q", tc.soname, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("plantSonameSymlink(%q) error = %q, want substring %q", tc.soname, err, tc.wantErr)
			}
		})
	}
}

// TestSeedEnginePayloadSonameReseed proves re-seeding is idempotent: the
// second run succeeds and leaves the identical relative link.
func TestSeedEnginePayloadSonameReseed(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	real, soname := "libsonametest.so.1.2.3", "libsonametest.so.1"
	libSrc, libSHA := writeSonameFixture(t, src, real, soname)
	binSrc, binSHA := fixtureEngineFile(t, src, "whisper-cli", []byte("fake-whisper-binary"))
	modelSrc, modelSHA := fixtureEngineFile(t, src, "ggml-base.en.bin", []byte("fake-model-bytes"))
	payload := engineSeedPayload{engine: "whisper.cpp", version: "v1.9.5", platform: "linux/test", files: []engineSeedFile{
		{source: binSrc, target: "bin/whisper-cli", sha256: binSHA, executable: true},
		{source: libSrc, target: "lib/" + real, sha256: libSHA},
		{source: modelSrc, target: "models/ggml-base.en.bin", sha256: modelSHA},
	}}
	dest := filepath.Join(t.TempDir(), "dest")
	for i := 0; i < 2; i++ {
		if err := seedEnginePayload(dest, payload); err != nil {
			t.Fatalf("seedEnginePayload run %d: %v", i, err)
		}
	}
	link := filepath.Join(dest, "engine", "lib", soname)
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink after reseed: %v", err)
	}
	if got != real {
		t.Fatalf("soname link target after reseed = %q, want %q", got, real)
	}
	// The re-seed retry loosens the binary to 0600 mid-copy; the final
	// mode must be re-asserted or the engine loses its execute bit.
	binInfo, err := os.Stat(filepath.Join(dest, "engine", "bin", "whisper-cli"))
	if err != nil {
		t.Fatalf("stat reseeded binary: %v", err)
	}
	if perm := binInfo.Mode().Perm(); perm != 0o555 {
		t.Fatalf("reseeded binary mode = %o, want 555", perm)
	}
}

// TestSeedEnginePayloadIgnoresNonELF proves non-ELF lib payload entries
// pass through without a link and without an error.
func TestSeedEnginePayloadIgnoresNonELF(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	binSrc, binSHA := fixtureEngineFile(t, src, "whisper-cli", []byte("fake-whisper-binary"))
	modelSrc, modelSHA := fixtureEngineFile(t, src, "ggml-base.en.bin", []byte("fake-model-bytes"))
	blobSrc, blobSHA := fixtureEngineFile(t, src, "opaque.dat", []byte("not-an-elf-object"))
	payload := engineSeedPayload{engine: "whisper.cpp", version: "v1.9.5", platform: "linux/test", files: []engineSeedFile{
		{source: binSrc, target: "bin/whisper-cli", sha256: binSHA, executable: true},
		{source: blobSrc, target: "lib/opaque.dat", sha256: blobSHA},
		{source: modelSrc, target: "models/ggml-base.en.bin", sha256: modelSHA},
	}}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := seedEnginePayload(dest, payload); err != nil {
		t.Fatalf("seedEnginePayload with non-ELF lib entry: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dest, "engine", "lib"))
	if err != nil {
		t.Fatalf("read lib dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "opaque.dat" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("lib dir = %v, want only the opaque payload file", names)
	}
}

// TestStaleSonameLinksReportsOnlyUnplantedSymlinks proves the re-seed sweep
// candidate set is exactly the symlinks a fresh payload did not plant: real
// files are never candidates, even when absent from the planted set.
func TestStaleSonameLinksReportsOnlyUnplantedSymlinks(t *testing.T) {
	t.Parallel()
	libDir := t.TempDir()
	// #nosec G306 -- test fixture dir under t.TempDir.
	if err := os.WriteFile(filepath.Join(libDir, "libreal.so.6.0.0"), []byte("fake-lib"), 0o444); err != nil {
		t.Fatalf("write real fixture: %v", err)
	}
	// #nosec G306 -- test fixture dir under t.TempDir.
	if err := os.WriteFile(filepath.Join(libDir, "keep.dat"), []byte("not-a-link"), 0o444); err != nil {
		t.Fatalf("write real fixture: %v", err)
	}
	if err := os.Symlink("libreal.so.6.0.0", filepath.Join(libDir, "libreal.so.6")); err != nil {
		t.Fatalf("plant keeper link: %v", err)
	}
	if err := os.Symlink("libgone.so.5.0.5", filepath.Join(libDir, "libgone.so.5")); err != nil {
		t.Fatalf("plant stale link: %v", err)
	}
	stale := staleSonameLinks(libDir, map[string]bool{"libreal.so.6": true})
	if len(stale) != 1 || stale[0] != "libgone.so.5" {
		t.Fatalf("staleSonameLinks = %v, want [libgone.so.5]", stale)
	}
}
