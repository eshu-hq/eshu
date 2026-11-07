// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// engineSeedFile is one pinned engine file the seed copies from the seed
// image into the activation destination. Source is the absolute path in the
// seed image; target is the path under <dest>/engine/ using the satellite
// layout (bin/ + models/). SHA256 is the pinned hex digest the copy is
// verified against before the seed completes. The model is streamed, never
// held whole in memory.
type engineSeedFile struct {
	source     string
	target     string
	sha256     string
	executable bool
}

// engineSeedPayload is the complete pinned engine set for one platform: the
// files the seed must copy, no more and no fewer.
type engineSeedPayload struct {
	engine   string
	version  string
	platform string
	files    []engineSeedFile
}

// enginePinManifest is the pin record the seed writes beside the copied
// engine. The satellite activation's IaC consumer (component-activation
// chart helper) reads Files to repoint the WHISPER_* pins at the seeded
// engine dir; every seeded file appears exactly once. Keys are
// schema/engine/version/platform and files[{path,source,sha256}]; lib-path
// (same dir) is consumed through an extraVolumes ConfigMap subPath mount
// at the musl loader path.
type enginePinManifest struct {
	Schema   string           `yaml:"schema"`
	Engine   string           `yaml:"engine"`
	Version  string           `yaml:"version"`
	Platform string           `yaml:"platform"`
	Files    []enginePinEntry `yaml:"files"`
}

// enginePinEntry records one seeded file: its engine-relative path, the
// seed-image source it was copied from, and the verified digest.
type enginePinEntry struct {
	Path   string `yaml:"path"`
	Source string `yaml:"source"`
	SHA256 string `yaml:"sha256"`
}

// enginePinnedSource is one reviewed pin: the seed-image path plus the
// digest specs/satellite-pins/transcription.<arch>.json records for it.
type enginePinnedSource struct {
	path   string
	sha256 string
}

// whisperEngineName and whisperEngineVersion are the reviewed engine
// identity. The drift test TestEnginePayloadPinsMatchSatellitePins proves
// they equal the satellite pins on every arch.
const (
	whisperEngineName    = "whisper.cpp"
	whisperEngineVersion = "v1.9.5"
)

// whisperBinaryPins is the pinned whisper-cli executable per platform.
var whisperBinaryPins = map[string]enginePinnedSource{
	"linux/amd64": {path: "/opt/whisper/bin/whisper-cli", sha256: "6ed322b01a81a775784826863ccb8a66022ac4c39eba97c187a3d4ae2342ade6"},
	"linux/arm64": {path: "/opt/whisper/bin/whisper-cli", sha256: "85e9b7b0771b3c402d89d2969d6f39ccfee463cd22eff3fb4ab919249cd0a099"},
}

// whisperModelPins is the pinned ggml model per platform. The model is
// architecture-independent, so both entries pin the same digest.
var whisperModelPins = map[string]enginePinnedSource{
	"linux/amd64": {path: "/opt/whisper/models/ggml-base.en.bin", sha256: "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002"},
	"linux/arm64": {path: "/opt/whisper/models/ggml-base.en.bin", sha256: "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002"},
}

// enginePayloadForPlatform builds the pinned engine set for platform: the
// binary under bin/ and the model under models/. An unpinned platform fails
// before anything is written.
func enginePayloadForPlatform(platform string) (engineSeedPayload, error) {
	binary, ok := whisperBinaryPins[platform]
	if !ok {
		return engineSeedPayload{}, fmt.Errorf("engine payload declares no pinned whisper binary for platform %q", platform)
	}
	model, ok := whisperModelPins[platform]
	if !ok {
		return engineSeedPayload{}, fmt.Errorf("engine payload declares no pinned model for platform %q", platform)
	}
	return engineSeedPayload{
		engine: whisperEngineName, version: whisperEngineVersion, platform: platform,
		files: []engineSeedFile{
			{
				source: binary.path, target: "bin/" + filepath.Base(binary.path),
				sha256: binary.sha256, executable: true,
			},
			{
				source: model.path, target: "models/" + filepath.Base(model.path), sha256: model.sha256,
			},
		},
	}, nil
}

// seedEnginePayload copies the pinned engine set into <dest>/engine/,
// verifying every file's SHA-256 against its pin during a streaming copy
// (the model is never held whole in memory). It then writes the pin manifest
// and the musl loader path file. Any failure removes the engine dir, so no
// partial payload survives.
func seedEnginePayload(dest string, payload engineSeedPayload) error {
	if len(payload.files) == 0 {
		return fmt.Errorf("engine payload for platform %q names no files", payload.platform)
	}
	seen := map[string]bool{}
	for _, file := range payload.files {
		if file.target == "" || filepath.IsAbs(file.target) || file.target != filepath.Clean(file.target) ||
			file.target == ".." || strings.HasPrefix(file.target, "../") {
			return fmt.Errorf("engine payload target %q escapes the engine dir", file.target)
		}
		if seen[file.target] {
			return fmt.Errorf("engine payload target %q is pinned twice", file.target)
		}
		seen[file.target] = true
	}
	root := filepath.Join(dest, "engine")
	for _, dir := range []string{"bin", "lib", "models"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			_ = os.RemoveAll(root)
			return fmt.Errorf("create seeded engine dir %q: %w", dir, err)
		}
	}
	for _, file := range payload.files {
		mode := os.FileMode(0o444)
		if file.executable {
			mode = 0o555
		}
		if err := copyFileVerified(file.source, filepath.Join(root, file.target), file.sha256, mode); err != nil {
			_ = os.RemoveAll(root)
			return err
		}
		if strings.HasPrefix(file.target, "lib/") {
			if err := plantSonameSymlink(filepath.Join(root, file.target)); err != nil {
				_ = os.RemoveAll(root)
				return err
			}
		}
	}
	planted := make(map[string]bool)
	for _, file := range payload.files {
		if !strings.HasPrefix(file.target, "lib/") {
			continue
		}
		copied := filepath.Join(root, file.target)
		soname, err := sonameOf(copied)
		if err != nil {
			_ = os.RemoveAll(root)
			return err
		}
		if soname != "" && soname != filepath.Base(file.target) {
			planted[soname] = true
		}
	}
	for _, stale := range staleSonameLinks(filepath.Join(root, "lib"), planted) {
		if err := os.Remove(filepath.Join(root, "lib", stale)); err != nil && !os.IsNotExist(err) {
			_ = os.RemoveAll(root)
			return fmt.Errorf("remove stale soname link %q: %w", stale, err)
		}
	}
	manifest := enginePinManifest{
		Schema: "engine-payload/v1", Engine: payload.engine,
		Version: payload.version, Platform: payload.platform,
	}
	for _, file := range payload.files {
		manifest.Files = append(manifest.Files, enginePinEntry{
			Path: file.target, Source: file.source, SHA256: file.sha256,
		})
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		_ = os.RemoveAll(root)
		return fmt.Errorf("encode engine pin manifest: %w", err)
	}
	// #nosec G304,G306 -- target joins the operator --dest dir with the fixed engine/pin-manifest.yaml name; the manifest carries digests only.
	if err := writeFileOverwrite(filepath.Join(root, "pin-manifest.yaml"), raw, 0o444); err != nil {
		_ = os.RemoveAll(root)
		return fmt.Errorf("write engine pin manifest: %w", err)
	}
	libDir, err := filepath.Abs(filepath.Join(root, "lib"))
	if err != nil {
		_ = os.RemoveAll(root)
		return fmt.Errorf("resolve seeded engine lib dir: %w", err)
	}
	// #nosec G304,G306 -- target joins the operator --dest dir with the fixed engine/lib-path name; the lib-path carries no secrets (a directory path only) and the worker reads it as the component user.
	if err := writeFileOverwrite(filepath.Join(root, "lib-path"), []byte(libDir+"\n"), 0o444); err != nil {
		_ = os.RemoveAll(root)
		return fmt.Errorf("write engine lib-path: %w", err)
	}
	return nil
}

// writeFileOverwrite writes data with mode, retrying once with owner-write
// when a previous seed left the file read-only, then re-asserts the mode
// (WriteFile applies it only at creation). Keeps re-seed idempotent.
func writeFileOverwrite(path string, data []byte, mode os.FileMode) error {
	// #nosec G304 -- path joins the seeded engine dir with fixed file names.
	if err := os.WriteFile(path, data, mode); err != nil {
		if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
			return fmt.Errorf("write engine payload %q: %w", path, err)
		}
		// #nosec G304 -- same fixed engine-dir path retried after chmod.
		if err := os.WriteFile(path, data, mode); err != nil {
			return fmt.Errorf("write engine payload %q: %w", path, err)
		}
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("write engine payload %q: %w", path, err)
	}
	return nil
}

// copyFileVerified streams source to target while hashing, then compares the
// digest with the pin. The source must be a regular file; symlinks,
// directories, and devices are refused. A digest mismatch removes the target
// so no unverified bytes survive.
func copyFileVerified(source, target, wantSHA256 string, mode os.FileMode) error {
	want := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(wantSHA256), "sha256:"))
	if len(want) != sha256.Size*2 {
		return fmt.Errorf("engine payload %q pins %q, want 64 hex characters", target, wantSHA256)
	}
	if _, err := hex.DecodeString(want); err != nil {
		return fmt.Errorf("engine payload %q pins %q, want hex: %v", target, wantSHA256, err)
	}
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("read engine payload %q: %w", target, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("engine payload %q is not a regular file", target)
	}
	// #nosec G304 -- source is a pinned seed-image path or a test fixture; target joins --dest with fixed engine/ names.
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("read engine payload %q: %w", target, err)
	}
	defer func() { _ = in.Close() }()
	// #nosec G304 -- target joins the operator --dest dir with fixed engine/ names.
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		// A previous seed run left read-only payload files behind (binaries
		// seed 0555). Adding owner-write and retrying once keeps re-seed
		// idempotent; the explicit Chmod below re-asserts the final mode,
		// so no extra permission survives the copy.
		if chmodErr := os.Chmod(target, 0o600); chmodErr == nil {
			// #nosec G304 -- same fixed engine-dir target retried after chmod.
			out, err = os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		}
	}
	if err != nil {
		return fmt.Errorf("write engine payload %q: %w", target, err)
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, hasher), in)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy engine payload %q: %w", target, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("write engine payload %q: %w", target, closeErr)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != want {
		_ = os.Remove(target)
		return fmt.Errorf("engine payload %q has sha256:%s, want sha256:%s", target, got, want)
	}
	// Re-assert the mode: OpenFile applies it only at creation, so a
	// re-seed that reused an existing file would otherwise keep the
	// transient 0600 from the retry above.
	if err := os.Chmod(target, mode); err != nil {
		_ = os.Remove(target)
		return fmt.Errorf("write engine payload %q: %w", target, err)
	}
	return nil
}

// sonameOf returns the ELF DT_SONAME declared by the copied file at path,
// or "" when the file is not an ELF object or declares none. Non-ELF bytes
// (models, fixtures) are not an error: they flow through the same seeder.
// A malformed ELF or dynamic section fails closed.
func sonameOf(path string) (string, error) {
	// #nosec G304 -- path is the just-verified seed copy under --dest or a test fixture.
	fh, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read engine soname %q: %w", path, err)
	}
	magic := make([]byte, 4)
	n, _ := io.ReadFull(fh, magic)
	_ = fh.Close()
	if n < len(magic) || magic[0] != 0x7f || magic[1] != 'E' || magic[2] != 'L' || magic[3] != 'F' {
		return "", nil
	}
	// #nosec G304 -- same verified copy; debug/elf reads only headers and the dynamic section.
	ef, err := elf.Open(path)
	if err != nil {
		return "", fmt.Errorf("read engine soname %q: %w", path, err)
	}
	defer func() { _ = ef.Close() }()
	sonames, err := ef.DynString(elf.DT_SONAME)
	if err != nil {
		return "", fmt.Errorf("read engine soname %q: %w", path, err)
	}
	if len(sonames) == 0 {
		return "", nil
	}
	if len(sonames) != 1 {
		return "", fmt.Errorf("engine payload %q declares %d SONAMEs, want exactly one", path, len(sonames))
	}
	return sonames[0], nil
}

// plantSonameSymlink derives the DT_SONAME from the verified copy at
// copiedPath and plants a relative soname -> filename symlink beside it.
// Files without a SONAME, or whose SONAME equals the file name, need no
// link. An unsafe SONAME or a colliding link fails closed.
func plantSonameSymlink(copiedPath string) error {
	soname, err := sonameOf(copiedPath)
	if err != nil {
		return err
	}
	if soname == "" {
		return nil
	}
	real := filepath.Base(copiedPath)
	if soname == real {
		return nil
	}
	if soname != filepath.Base(soname) || soname == "." || soname == ".." || strings.Contains(soname, "/") || filepath.IsAbs(soname) {
		return fmt.Errorf("engine payload %q declares unsafe SONAME %q", copiedPath, soname)
	}
	link := filepath.Join(filepath.Dir(copiedPath), soname)
	if fi, statErr := os.Lstat(link); statErr == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, readErr := os.Readlink(link); readErr == nil && target == real {
				return nil
			}
		}
		return fmt.Errorf("engine payload soname link %q collides with an existing file", link)
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect engine soname link %q: %w", link, statErr)
	}
	if err := os.Symlink(real, link); err != nil {
		return fmt.Errorf("plant engine soname link %q: %w", link, err)
	}
	return nil
}

// staleSonameLinks lists lib-dir entries that are symlinks outside the
// planted set: leftovers from an earlier seed that a re-seed into the same
// dest would otherwise leave behind to shadow the fresh payload. Only
// symlinks are reported; real files are never sweep candidates.
func staleSonameLinks(libDir string, planted map[string]bool) []string {
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return nil
	}
	var stale []string
	for _, e := range entries {
		if planted[e.Name()] {
			continue
		}
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		stale = append(stale, e.Name())
	}
	return stale
}
