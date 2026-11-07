// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
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
	if err := os.WriteFile(filepath.Join(root, "pin-manifest.yaml"), raw, 0o444); err != nil {
		_ = os.RemoveAll(root)
		return fmt.Errorf("write engine pin manifest: %w", err)
	}
	libDir, err := filepath.Abs(filepath.Join(root, "lib"))
	if err != nil {
		_ = os.RemoveAll(root)
		return fmt.Errorf("resolve seeded engine lib dir: %w", err)
	}
	// #nosec G304,G306 -- target joins the operator --dest dir with the fixed engine/lib-path name; the lib-path carries no secrets (a directory path only) and the worker reads it as the component user.
	if err := os.WriteFile(filepath.Join(root, "lib-path"), []byte(libDir+"\n"), 0o444); err != nil {
		_ = os.RemoveAll(root)
		return fmt.Errorf("write engine lib-path: %w", err)
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
	return nil
}
