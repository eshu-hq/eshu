// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// seedCollectorName is the runtime file the reconcile loop executes:
// the chart seed init container runs `<binary> seed --dest <runtimeDir>
// --image-ref <seed.image>` and reconcile requires <runtimeDir>/collector
// plus <runtimeDir>/manifest.yaml before the component can be claimed.
const seedCollectorName = "collector"

// seedManifestName is the manifest file Seed writes next to the collector
// binary, carrying the ManifestFor document with the seeded image stamped
// into its artifact.
const seedManifestName = "manifest.yaml"

// seedImageRepoPrefix is the only registry and repository Seed accepts: the
// digest-pinned scanner images the platform publishes.
const seedImageRepoPrefix = "ghcr.io/eshu-hq/scanner-"

// Seed runs the chart seed-init contract for one scanner component binary:
// it copies the running executable to <dest>/collector and marshals the
// manifest to <dest>/manifest.yaml with the --image-ref value stamped into
// Artifacts[0].Image. args carries the flags after the `seed` subcommand
// word (`--dest` and `--image-ref`, each as `--flag value` or
// `--flag=value`).
//
// Seed validates the --image-ref SHAPE only, never equality with the
// ManifestFor pin compiled into this binary: the pin changes the image
// digest, so a binary cannot satisfy equality against its own image
// (fixed-point impossibility, #8878). Equality is enforced later, at
// admission, by VerifySeededArtifact before any claim or execution.
//
// Seed fails closed: unknown flags, a missing or empty --dest or
// --image-ref, and an --image-ref outside the
// ghcr.io/eshu-hq/scanner-<name>@sha256:<64 lowercase hex> shape for this
// binary's own component name all error before anything is written, so a
// stale or foreign image never seeds a partial runtime directory.
// Re-running with the same inputs overwrites byte-identical files. Error
// text names the flag or defect, never the value: image references and
// paths stay out of errors, logs, and statuses per the package privacy
// rule.
func Seed(componentID string, args []string) error {
	kind, err := AnalyzerFor(componentID)
	if err != nil {
		return err
	}
	dest, imageRef, err := parseSeedArgs(args)
	if err != nil {
		return err
	}
	if err := validateSeedImageRef(componentID, imageRef); err != nil {
		return err
	}
	manifest, err := ManifestFor(kind)
	if err != nil {
		return err
	}
	if len(manifest.Spec.Artifacts) != 1 {
		return errors.New("seed: the pinned scanner manifest declares no single image artifact")
	}
	manifest.Spec.Artifacts[0].Image = imageRef
	executable, err := os.Executable()
	if err != nil {
		return errors.New("seed: resolve the running executable")
	}
	return writeSeed(dest, manifest, executable)
}

// validateSeedImageRef enforces the fail-closed --image-ref shape for one
// scanner component binary: ghcr.io/eshu-hq/scanner-<name>@sha256:<64
// lowercase hex> where <name> is the binary's own component name. The digest
// is deliberately not compared to the compiled ManifestFor pin (see Seed).
// A rejection names --image-ref, never the rejected value.
func validateSeedImageRef(componentID, imageRef string) error {
	name := strings.TrimPrefix(componentID, "eshu.scanner.")
	digest, ok := strings.CutPrefix(imageRef, seedImageRepoPrefix+name+"@sha256:")
	if !ok {
		return errors.New("seed: --image-ref is not a digest-pinned image for this scanner component")
	}
	if len(digest) != 64 {
		return errors.New("seed: --image-ref does not carry a 64-character image digest")
	}
	for i := 0; i < len(digest); i++ {
		c := digest[i]
		if c < '0' || (c > '9' && c < 'a') || c > 'f' {
			return errors.New("seed: --image-ref does not carry a lowercase hex image digest")
		}
	}
	return nil
}

// parseSeedArgs extracts --dest and --image-ref from the arguments after
// the `seed` subcommand word. Anything else fails closed.
func parseSeedArgs(args []string) (string, string, error) {
	var dest, imageRef string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if value, ok := strings.CutPrefix(arg, "--dest="); ok {
			dest = value
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--image-ref="); ok {
			imageRef = value
			continue
		}
		switch arg {
		case "--dest":
			i++
			if i >= len(args) {
				return "", "", errors.New("seed: --dest needs a value")
			}
			dest = args[i]
		case "--image-ref":
			i++
			if i >= len(args) {
				return "", "", errors.New("seed: --image-ref needs a value")
			}
			imageRef = args[i]
		default:
			return "", "", errors.New("seed: unknown argument: seed takes only --dest and --image-ref")
		}
	}
	if strings.TrimSpace(dest) == "" {
		return "", "", errors.New("seed: --dest is required")
	}
	if strings.TrimSpace(imageRef) == "" {
		return "", "", errors.New("seed: --image-ref is required")
	}
	return dest, imageRef, nil
}

// writeSeed copies the running executable to <dest>/collector (0755) and
// marshals the manifest to <dest>/manifest.yaml (0644), creating <dest>
// when it is absent. Modes are enforced after every write so a re-seed
// repairs drift instead of preserving it.
func writeSeed(dest string, manifest component.Manifest, executable string) error {
	// #nosec G301 -- dest is the operator-chosen seed directory; 0755 grants no group/other write and lets reconcile traverse it.
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return errors.New("seed: --dest is not a writable directory")
	}
	// #nosec G304 -- this path originates from os.Executable, never from --dest or another user-controlled flag.
	raw, err := os.ReadFile(executable)
	if err != nil {
		return errors.New("seed: read the running executable")
	}
	collector := filepath.Join(dest, seedCollectorName)
	// #nosec G306, G703 -- the collector must stay executable (0755) for the reconcile step; dest is the explicit operator-chosen target joined only with constant file names.
	if err := os.WriteFile(collector, raw, 0o755); err != nil {
		return errors.New("seed: cannot write the collector file")
	}
	// #nosec G302 -- re-seed repairs mode drift back to the required 0755.
	if err := os.Chmod(collector, 0o755); err != nil {
		return errors.New("seed: cannot write the collector file")
	}
	encoded, err := yaml.Marshal(manifest)
	if err != nil {
		return errors.New("seed: cannot encode the component manifest")
	}
	manifestPath := filepath.Join(dest, seedManifestName)
	// #nosec G306 -- the manifest must stay readable (0644) for the reconcile step.
	if err := os.WriteFile(manifestPath, encoded, 0o644); err != nil {
		return errors.New("seed: cannot write the manifest file")
	}
	// #nosec G302 -- re-seed repairs mode drift back to the required 0644.
	if err := os.Chmod(manifestPath, 0o644); err != nil {
		return errors.New("seed: cannot write the manifest file")
	}
	return nil
}
