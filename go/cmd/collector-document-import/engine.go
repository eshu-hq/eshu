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
// layout (bin/ + lib/ flat + tessdata/). SHA256 is the pinned hex digest
// the copy is verified against before the seed completes.
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
// engine. The IaC lane reads it to point the ESHU_DOC_IMPORT_TESSERACT_*
// pins at the runtime dir; every seeded file appears exactly once.
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
// digest specs/satellite-pins/ocr.<arch>.json records for it.
type enginePinnedSource struct {
	path   string
	sha256 string
}

// ocrEngineName and ocrEngineVersion are the reviewed engine identity. The
// drift test TestEnginePayloadPinsMatchSatellitePins proves they equal the
// satellite pins on every arch.
const (
	ocrEngineName    = "tesseract"
	ocrEngineVersion = "5.5.0"
)

// ocrBinaryPins is the pinned tesseract executable per platform.
var ocrBinaryPins = map[string]enginePinnedSource{
	"linux/amd64": {path: "/usr/bin/tesseract", sha256: "38b441bd41257037a03d6a15d9f849d8bd83da4c24dab1df428a7a4ad265332f"},
	"linux/arm64": {path: "/usr/bin/tesseract", sha256: "62a760cd1c23d95211e7c42dddc761c2c9cb9488628941e9be6cd9b6748e9c77"},
}

// ocrLibraryPins is the pinned runtime shared-object set per platform: the
// ESHU_DOC_IMPORT_TESSERACT_LIBRARIES pin set from
// specs/satellite-pins/ocr.<arch>.json. Basenames are unique per arch, so
// the seed lays them flat under engine/lib/.
var ocrLibraryPins = map[string][]enginePinnedSource{
	"linux/amd64": {
		{path: "/lib/ld-musl-x86_64.so.1", sha256: "19313c7fcdd4816403c95fa43a9d63639b17f09cbb69fbb8cae414efef4f57cb"},
		{path: "/usr/lib/engines-3/afalg.so", sha256: "365a61d3987d63eba971e9536d0c4fb112c638915a79f0a39ece89ba0d83e284"},
		{path: "/usr/lib/engines-3/capi.so", sha256: "b52c985eceaaed9c2d7abb2c990697a16692d17c5d853eaab1d7a98e0c7440aa"},
		{path: "/usr/lib/engines-3/loader_attic.so", sha256: "f6f027d76b0e71e736fff2047ce9cf046c0b9202e41235035bebd88a3b7602e8"},
		{path: "/usr/lib/engines-3/padlock.so", sha256: "a69874047ff981656deb0bae60b39dde03f7e1a5cd0f589e418b7f6e7f12e6e7"},
		{path: "/usr/lib/libcrypto.so.3", sha256: "8fcd92643f8ab0f9b697854ddb499fa386775fc878677c6bde73c53292affd5d"},
		{path: "/usr/lib/libgcc_s.so.1", sha256: "2298274ad3316109d6e895083398e220f16fbdd12957c4afcc9af8849e734151"},
		{path: "/usr/lib/libgif.so.7.2.0", sha256: "05077bd352c8afc10e388f3dc063ca9463915fb422e403a15f4bfffa256f672e"},
		{path: "/usr/lib/libgomp.so.1.0.0", sha256: "33954e2132b37621b5267cf32df4a9e869507353c31d871aea2ec71f05fb8889"},
		{path: "/usr/lib/libjpeg.so.8.3.2", sha256: "7ed0b87115fb0aa66eb76cbe6dda176cc7e7df6b669b264a7cc1daa4d4027903"},
		{path: "/usr/lib/libleptonica.so.6.0.0", sha256: "212d52829e9ca37024e3f66241983692b3fa5c1949cc869c8cdfcd2cced3c467"},
		{path: "/usr/lib/libpng16.so.16.59.0", sha256: "339a8cf22d162455b1fd43a160ff5b2c0f9b28843bb318b061a13efa4ca29607"},
		{path: "/usr/lib/libsharpyuv.so.0.1.0", sha256: "0c5660bf7052d6a8f17b23050bf990470c44bbe161dc1be5ca0e29537e83e977"},
		{path: "/usr/lib/libssl.so.3", sha256: "06f404e940a5baaf86df4ecec140947fc727bb08cafe0de9f9a503eb5aa9b3c3"},
		{path: "/usr/lib/libstdc++.so.6.0.33", sha256: "08d5c7eb7c1e210d0fb56a33af0b97ab86339f2d321ceb31fcd53c563f8803c9"},
		{path: "/usr/lib/libtesseract.so.5.0.5", sha256: "728499f0df9986738a3a9d7c3c3d310571659c2918b3fc9fb88ee05b0aea06b3"},
		{path: "/usr/lib/libtiff.so.6.3.0", sha256: "244a3fd9d8a35e42e88da9bebf12f3cd44a10c1225af43861c3deb1b05068db4"},
		{path: "/usr/lib/libwebp.so.7.1.9", sha256: "fb23f6cac892050db9e50831db72da235356b92f87d9c6041ca0578145a7b7dc"},
		{path: "/usr/lib/libwebpmux.so.3.1.0", sha256: "025feff243cfe6659d6c596a097b5cd17e46284a10aba6f105cc6324323df526"},
		{path: "/usr/lib/libz.so.1.3.2", sha256: "b4c94dba7f00904f686c8f4f9d095a9eb278d2a2c1b8c7017960dbc5cf684ac0"},
		{path: "/usr/lib/libzstd.so.1.5.6", sha256: "619b3b7dc3190538fff13efbc7c39820475c7b508c28cab9e1d138d86a4e36b7"},
		{path: "/usr/lib/ossl-modules/legacy.so", sha256: "499d2581ca59723523a9f1d499612268dd83b4436031743aaada16c380dfed7a"},
	},
	"linux/arm64": {
		{path: "/lib/ld-musl-aarch64.so.1", sha256: "a965363d0cb99ab9b28f4c51ee6bb64162b4e204f0d20efda72541a2e4147a5e"},
		{path: "/usr/lib/engines-3/afalg.so", sha256: "f3fdfc5206e49c5144aad0552874150f9f8f0a9019fc87f9d5ae657afc0c893e"},
		{path: "/usr/lib/engines-3/capi.so", sha256: "2a58755ecf3d8aee87aafa76b439351a3b128e41b01e2813149dfd97c2f121de"},
		{path: "/usr/lib/engines-3/loader_attic.so", sha256: "5f705a3fdeeffeb7f69868806e8162829bf4f418cfa523f34a816a01f12f0185"},
		{path: "/usr/lib/engines-3/padlock.so", sha256: "ba7cd1c697c6f5bfba951465db55ffb5006fc0d59e212e30ed505fd50de72cc4"},
		{path: "/usr/lib/libcrypto.so.3", sha256: "c7ca672c906187d617c8585f82c024f6a26db7188ba0dc4036732923d2203974"},
		{path: "/usr/lib/libgcc_s.so.1", sha256: "69c45f16064b7b61b9a3d64c37395d13368a8fe719745d49e9508ec650fea971"},
		{path: "/usr/lib/libgif.so.7.2.0", sha256: "52f3e972aaa5925cad90f9c3451830e39a51f8ecde703f6d4bc7548c000043c7"},
		{path: "/usr/lib/libgomp.so.1.0.0", sha256: "b626072d5ed86805137f5a69b6f1115c2648a9170c5290547fdd1c9f83014fb7"},
		{path: "/usr/lib/libjpeg.so.8.3.2", sha256: "a7f716bd69a781e53bdfa50e6b523973844a45488af089d53b9238cec8e340f5"},
		{path: "/usr/lib/libleptonica.so.6.0.0", sha256: "8b1d63154c125ceccc7968e3cf47bc91327a9fb832b3ccd7c4afc7dc3e4a7a90"},
		{path: "/usr/lib/libpng16.so.16.59.0", sha256: "0c3cbe441b75dd09707f9b1bd92e25dfca84c1dec3f417062b297811ae44cdcb"},
		{path: "/usr/lib/libsharpyuv.so.0.1.0", sha256: "158e894612f601cbaf3aaf7d28bb16a63b6b76bf7fb849cac73e389ce7a6aa22"},
		{path: "/usr/lib/libssl.so.3", sha256: "2ef41af26ced4419f0855d941783aa06f10f90f1ae6be840b9a38da48b793353"},
		{path: "/usr/lib/libstdc++.so.6.0.33", sha256: "1f1a71be4319632f743507ef7c6eaebaccad8d6f277055cf2c6b16b544f9f418"},
		{path: "/usr/lib/libtesseract.so.5.0.5", sha256: "11ffa688fd036534a6873c8b6ebd36b061151dd060068ba951731e901ab44189"},
		{path: "/usr/lib/libtiff.so.6.3.0", sha256: "18f4ca8ee26888c88426c098cf549301932d4cbddac1d6475a2bbd18bf8e0e69"},
		{path: "/usr/lib/libwebp.so.7.1.9", sha256: "91f261e152028a1667acd3c65e1830395c29cc52db53a194161fc072f68ee3d9"},
		{path: "/usr/lib/libwebpmux.so.3.1.0", sha256: "7363240cdaf3615c834d723398b200f97fd5e393f9146fd01221452a41f08902"},
		{path: "/usr/lib/libz.so.1.3.2", sha256: "84d151b84b5ba54c278ca45d620a128d46e641ff598cbcceb3e930ca337cbd04"},
		{path: "/usr/lib/libzstd.so.1.5.6", sha256: "5bb12332ec61c069eb01fd1a71c882732e8ae6402121516e2f4fa7e29c636df8"},
		{path: "/usr/lib/ossl-modules/legacy.so", sha256: "c4e9b6e541797ab7830cdcb068f1f34aa5e88cac7985ae4dfc3011ee8d2a98cf"},
	},
}

// ocrModelPins is the pinned tessdata model per platform.
var ocrModelPins = map[string]enginePinnedSource{
	"linux/amd64": {path: "/usr/share/tessdata/eng.traineddata", sha256: "daa0c97d651c19fba3b25e81317cd697e9908c8208090c94c3905381c23fc047"},
	"linux/arm64": {path: "/usr/share/tessdata/eng.traineddata", sha256: "daa0c97d651c19fba3b25e81317cd697e9908c8208090c94c3905381c23fc047"},
}

// enginePayloadForPlatform builds the pinned engine set for platform: the
// binary under bin/, every library in the TESSERACT_LIBRARIES pin set flat
// under lib/, and the model under tessdata/. An unpinned platform fails
// before anything is written.
func enginePayloadForPlatform(platform string) (engineSeedPayload, error) {
	binary, ok := ocrBinaryPins[platform]
	if !ok {
		return engineSeedPayload{}, fmt.Errorf("engine payload declares no pinned tesseract binary for platform %q", platform)
	}
	libraries, ok := ocrLibraryPins[platform]
	if !ok {
		return engineSeedPayload{}, fmt.Errorf("engine payload declares no pinned libraries for platform %q", platform)
	}
	model, ok := ocrModelPins[platform]
	if !ok {
		return engineSeedPayload{}, fmt.Errorf("engine payload declares no pinned model for platform %q", platform)
	}
	payload := engineSeedPayload{engine: ocrEngineName, version: ocrEngineVersion, platform: platform}
	payload.files = append(payload.files, engineSeedFile{
		source: binary.path, target: "bin/" + filepath.Base(binary.path),
		sha256: binary.sha256, executable: true,
	})
	for _, lib := range libraries {
		payload.files = append(payload.files, engineSeedFile{
			source: lib.path, target: "lib/" + filepath.Base(lib.path), sha256: lib.sha256,
		})
	}
	payload.files = append(payload.files, engineSeedFile{
		source: model.path, target: "tessdata/" + filepath.Base(model.path), sha256: model.sha256,
	})
	return payload, nil
}

// seedEnginePayload copies the pinned engine set into <dest>/engine/,
// verifying every file's SHA-256 against its pin during a streaming copy.
// It then writes the pin manifest and the musl loader path file. Any failure
// removes the engine dir, so no partial payload survives.
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
	for _, dir := range []string{"bin", "lib", "tessdata"} {
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
