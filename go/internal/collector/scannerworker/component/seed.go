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
// binary, carrying the exact ManifestFor document for the analyzer.
const seedManifestName = "manifest.yaml"

// Seed runs the chart seed-init contract for one scanner component binary:
// it copies the running executable to <dest>/collector and marshals the
// pinned manifest to <dest>/manifest.yaml. args carries the flags after the
// `seed` subcommand word (`--dest` and `--image-ref`, each as `--flag
// value` or `--flag=value`).
//
// Seed fails closed: unknown flags, a missing or empty --dest or
// --image-ref, and an --image-ref outside the ManifestFor pinned artifact
// all error before anything is written, so a stale or foreign image never
// seeds a partial runtime directory. Re-running with the same inputs
// overwrites byte-identical files. Error text names the flag or defect,
// never the value: image references and paths stay out of errors, logs,
// and statuses per the package privacy rule.
func Seed(componentID string, args []string) error {
	kind, err := AnalyzerFor(componentID)
	if err != nil {
		return err
	}
	dest, imageRef, err := parseSeedArgs(args)
	if err != nil {
		return err
	}
	manifest, err := ManifestFor(kind)
	if err != nil {
		return err
	}
	if len(manifest.Spec.Artifacts) != 1 || manifest.Spec.Artifacts[0].Image != imageRef {
		return errors.New("seed: --image-ref does not match the pinned scanner image artifact")
	}
	executable, err := os.Executable()
	if err != nil {
		return errors.New("seed: resolve the running executable")
	}
	return writeSeed(dest, manifest, executable)
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
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return errors.New("seed: --dest is not a writable directory")
	}
	raw, err := os.ReadFile(executable)
	if err != nil {
		return errors.New("seed: read the running executable")
	}
	collector := filepath.Join(dest, seedCollectorName)
	if err := os.WriteFile(collector, raw, 0o755); err != nil {
		return errors.New("seed: cannot write the collector file")
	}
	if err := os.Chmod(collector, 0o755); err != nil {
		return errors.New("seed: cannot write the collector file")
	}
	encoded, err := yaml.Marshal(manifest)
	if err != nil {
		return errors.New("seed: cannot encode the component manifest")
	}
	manifestPath := filepath.Join(dest, seedManifestName)
	if err := os.WriteFile(manifestPath, encoded, 0o644); err != nil {
		return errors.New("seed: cannot write the manifest file")
	}
	if err := os.Chmod(manifestPath, 0o644); err != nil {
		return errors.New("seed: cannot write the manifest file")
	}
	return nil
}
