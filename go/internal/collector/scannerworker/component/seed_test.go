// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	eshucomponent "github.com/eshu-hq/eshu/go/internal/component"
)

// pinnedSeedImage returns the digest-pinned artifact image ManifestFor
// declares for one analyzer: the only --image-ref Seed accepts.
func pinnedSeedImage(t *testing.T, kind scannerworker.AnalyzerKind) string {
	t.Helper()

	manifest, err := ManifestFor(kind)
	if err != nil {
		t.Fatalf("ManifestFor(%q) error = %v, want nil", kind, err)
	}
	if len(manifest.Spec.Artifacts) != 1 {
		t.Fatalf("ManifestFor(%q) declares %d artifacts, want exactly 1", kind, len(manifest.Spec.Artifacts))
	}
	return manifest.Spec.Artifacts[0].Image
}

// TestSeedRejectsBadArgs proves Seed fails closed on every malformed
// invocation: missing flags, empty values, unknown flags, and positional
// arguments. Error text names the flag, never the value.
func TestSeedRejectsBadArgs(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()
	ref := pinnedSeedImage(t, scannerworker.AnalyzerImageUnpacking)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no args", []string{}, "--dest"},
		{"dest only", []string{"--dest", dest}, "--image-ref"},
		{"image ref only", []string{"--image-ref", ref}, "--dest"},
		{"empty dest", []string{"--dest=", "--image-ref", ref}, "--dest"},
		{"empty image ref", []string{"--dest", dest, "--image-ref="}, "--image-ref"},
		{"unknown flag", []string{"--dest", dest, "--image-ref", ref, "--force"}, "unknown"},
		{"single dash", []string{"--dest", dest, "--image-ref", ref, "-x"}, "unknown"},
		{"positional", []string{"--dest", dest, "--image-ref", ref, "extra"}, "unknown"},
		{"version passthrough", []string{"--dest", dest, "--image-ref", ref, "--version"}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Seed takes the arguments after the subcommand word, exactly
			// what the mains pass as os.Args[2:].
			if err := Seed(IDImage, tc.args); err == nil {
				t.Fatalf("Seed(%q) error = nil, want an error mentioning %q", tc.args, tc.want)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Seed(%q) error = %v, want it to mention %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestSeedRejectsUnknownComponent proves an unknown binary identity never
// reaches the filesystem.
func TestSeedRejectsUnknownComponent(t *testing.T) {
	t.Parallel()

	if err := Seed("eshu.scanner.unknown", []string{"--dest", t.TempDir(), "--image-ref", "ref"}); err == nil {
		t.Fatal("Seed(unknown) error = nil, want unknown component error")
	}
}

// TestSeedRejectsImageRefMismatch proves a --image-ref outside the pinned
// manifest artifact fails closed before anything is written: the
// destination stays empty so a stale or foreign image never seeds.
func TestSeedRejectsImageRefMismatch(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "runtime")
	if err := Seed(IDImage, []string{"--dest", dest, "--image-ref", "ghcr.io/eshu-hq/scanner-image:latest"}); err == nil {
		t.Fatal("Seed(mismatched ref) error = nil, want pinned-artifact mismatch error")
	} else if !strings.Contains(err.Error(), "image-ref") {
		t.Fatalf("Seed(mismatched ref) error = %v, want it to name --image-ref", err)
	}
	if entries, readErr := os.ReadDir(dest); readErr == nil && len(entries) > 0 {
		t.Fatalf("mismatched seed wrote %d entries to %s, want none", len(entries), dest)
	}
}

// TestSeedWritesCollectorAndManifest proves the happy path: the running
// executable lands at <dest>/collector with owner-execute bits and the
// pinned manifest lands at <dest>/manifest.yaml, decoding back to the exact
// ManifestFor document and passing manifest validation with its own
// RequiredGrants.
func TestSeedWritesCollectorAndManifest(t *testing.T) {
	t.Parallel()

	for _, kind := range []scannerworker.AnalyzerKind{
		scannerworker.AnalyzerImageUnpacking,
		scannerworker.AnalyzerSBOMGeneration,
		scannerworker.AnalyzerOSPackageExtraction,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			id, err := componentID(kind)
			if err != nil {
				t.Fatalf("componentID(%q) error = %v", kind, err)
			}
			dest := filepath.Join(t.TempDir(), "runtime")
			if err := Seed(id, []string{"--dest", dest, "--image-ref", pinnedSeedImage(t, kind)}); err != nil {
				t.Fatalf("Seed(%q) error = %v, want nil", kind, err)
			}
			self, err := os.Executable()
			if err != nil {
				t.Fatalf("os.Executable() error = %v", err)
			}
			wantBin, err := os.ReadFile(self)
			if err != nil {
				t.Fatalf("read test executable error = %v", err)
			}
			gotBin, err := os.ReadFile(filepath.Join(dest, "collector"))
			if err != nil {
				t.Fatalf("read seeded collector error = %v", err)
			}
			if !reflect.DeepEqual(gotBin, wantBin) {
				t.Fatalf("seeded collector is %d bytes, want the %d-byte running executable", len(gotBin), len(wantBin))
			}
			info, err := os.Stat(filepath.Join(dest, "collector"))
			if err != nil {
				t.Fatalf("stat seeded collector error = %v", err)
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Fatalf("seeded collector mode = %o, want owner-execute bits", info.Mode().Perm())
			}
			raw, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
			if err != nil {
				t.Fatalf("read seeded manifest error = %v", err)
			}
			var decoded eshucomponent.Manifest
			if err := yaml.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("decode seeded manifest error = %v", err)
			}
			wantManifest, err := ManifestFor(kind)
			if err != nil {
				t.Fatalf("ManifestFor(%q) error = %v", kind, err)
			}
			if !reflect.DeepEqual(decoded, wantManifest) {
				t.Fatalf("seeded manifest differs from ManifestFor(%q)", kind)
			}
			if err := decoded.ValidateWithGrants(RequiredGrants(wantManifest)); err != nil {
				t.Fatalf("seeded manifest ValidateWithGrants() error = %v, want nil", err)
			}
		})
	}
}

// TestSeedIsIdempotent proves re-seeding the same destination succeeds and
// leaves byte-identical files: init-container retries never corrupt state.
func TestSeedIsIdempotent(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "runtime")
	ref := pinnedSeedImage(t, scannerworker.AnalyzerImageUnpacking)
	args := []string{"--dest", dest, "--image-ref", ref}
	if err := Seed(IDImage, args); err != nil {
		t.Fatalf("first Seed() error = %v, want nil", err)
	}
	firstCollector, err := os.ReadFile(filepath.Join(dest, "collector"))
	if err != nil {
		t.Fatalf("read first collector error = %v", err)
	}
	firstManifest, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read first manifest error = %v", err)
	}
	if err := Seed(IDImage, args); err != nil {
		t.Fatalf("second Seed() error = %v, want nil", err)
	}
	secondCollector, err := os.ReadFile(filepath.Join(dest, "collector"))
	if err != nil {
		t.Fatalf("read second collector error = %v", err)
	}
	secondManifest, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read second manifest error = %v", err)
	}
	if !reflect.DeepEqual(firstCollector, secondCollector) {
		t.Fatal("re-seed changed collector bytes, want identical")
	}
	if !reflect.DeepEqual(firstManifest, secondManifest) {
		t.Fatal("re-seed changed manifest bytes, want identical")
	}
}

// TestSeedDestFileFailsClosed proves a --dest that exists as a file errors
// instead of writing beside it.
func TestSeedDestFileFailsClosed(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(dest, []byte("x"), 0o600); err != nil {
		t.Fatalf("stage dest file error = %v", err)
	}
	ref := pinnedSeedImage(t, scannerworker.AnalyzerImageUnpacking)
	if err := Seed(IDImage, []string{"--dest", dest, "--image-ref", ref}); err == nil {
		t.Fatal("Seed(file dest) error = nil, want destination error")
	}
}

// seedSmokeBinaries builds each scanner component binary once for the
// dispatch smoke tests below.
func seedSmokeBinaries(t *testing.T) map[scannerworker.AnalyzerKind]string {
	t.Helper()

	return map[scannerworker.AnalyzerKind]string{
		scannerworker.AnalyzerImageUnpacking:      buildComponentBinary(t, "github.com/eshu-hq/eshu/go/cmd/scanner-component-image", "scanner-component-image"),
		scannerworker.AnalyzerSBOMGeneration:      buildComponentBinary(t, "github.com/eshu-hq/eshu/go/cmd/scanner-component-sbom", "scanner-component-sbom"),
		scannerworker.AnalyzerOSPackageExtraction: buildComponentBinary(t, "github.com/eshu-hq/eshu/go/cmd/scanner-component-osruntime", "scanner-component-osruntime"),
	}
}

// TestSeedBuiltBinarySmoke proves the production path per analyzer: the
// real binary behind `seed --dest --image-ref` exits 0 and leaves an
// executable collector plus a manifest decoding to the pinned component
// ID, while a mismatched ref exits nonzero with nothing seeded.
func TestSeedBuiltBinarySmoke(t *testing.T) {
	bins := seedSmokeBinaries(t)

	for kind, bin := range bins {
		t.Run(string(kind), func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "runtime")
			cmd := exec.Command(bin, "seed", "--dest", dest, "--image-ref", pinnedSeedImage(t, kind))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("seed smoke error = %v\n%s", err, out)
			}
			info, err := os.Stat(filepath.Join(dest, "collector"))
			if err != nil {
				t.Fatalf("stat smoked collector error = %v", err)
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Fatalf("smoked collector mode = %o, want execute bits", info.Mode().Perm())
			}
			raw, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
			if err != nil {
				t.Fatalf("read smoked manifest error = %v", err)
			}
			var decoded eshucomponent.Manifest
			if err := yaml.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("decode smoked manifest error = %v", err)
			}
			id, err := componentID(kind)
			if err != nil {
				t.Fatalf("componentID(%q) error = %v", kind, err)
			}
			if decoded.Metadata.ID != id {
				t.Fatalf("smoked manifest id = %q, want %q", decoded.Metadata.ID, id)
			}

			badDest := filepath.Join(t.TempDir(), "bad")
			bad := exec.Command(bin, "seed", "--dest", badDest, "--image-ref", "ghcr.io/eshu-hq/scanner-x:latest")
			if out, err := bad.CombinedOutput(); err == nil {
				t.Fatalf("mismatched smoke error = nil, want nonzero exit\n%s", out)
			}
			if entries, readErr := os.ReadDir(badDest); readErr == nil && len(entries) > 0 {
				t.Fatalf("mismatched smoke wrote %d entries, want none", len(entries))
			}
		})
	}
}

// TestSeedDispatchLeavesVersionAndStdioAlone proves the new subcommand does
// not disturb the existing surfaces: --version still prints and a bare
// invocation still takes the stdio path (empty stdin decodes to an error,
// not a seed write).
func TestSeedDispatchLeavesVersionAndStdioAlone(t *testing.T) {
	bins := seedSmokeBinaries(t)

	for kind, bin := range bins {
		t.Run(string(kind), func(t *testing.T) {
			out, err := exec.Command(bin, "--version").Output()
			if err != nil {
				t.Fatalf("--version error = %v", err)
			}
			if !strings.Contains(string(out), "eshu-scanner-component-") {
				t.Fatalf("--version = %q, want the component binary name", out)
			}
			bare := exec.Command(bin)
			bare.Stdin = strings.NewReader("")
			if out, err := bare.CombinedOutput(); err == nil {
				t.Fatalf("bare invocation error = nil, want the stdio decode error\n%s", out)
			}
		})
	}
}
