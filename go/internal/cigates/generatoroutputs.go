// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"path"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/collector/entrypoints"
)

const (
	// collectorEntrypointsGateID is the registry gate that regenerates the
	// collector command entrypoints and diffs them against the committed files.
	collectorEntrypointsGateID = "collector-entrypoints-generated"

	// collectorEntrypointsManifest is the manifest the generator reads, relative
	// to the repo root. Together with the generator package it decides the set of
	// files the gate guards.
	collectorEntrypointsManifest = "go/internal/collector/entrypoints/collector_entrypoints.yaml"
)

// checkGeneratorOutputCoverage enforces the #10139 contract: every file the
// collector entrypoints generator writes must be selected by one of the gate's
// own triggers. Otherwise a hand edit to only a generated file selects no gate
// locally and first fails in CI verify-contracts as "generated file ... is
// stale".
//
// The output set is derived, never typed: the committed manifest is loaded
// with entrypoints.LoadManifestFile and each collector is expanded with
// entrypoints.Generate, the same two steps scripts/generate-collector-
// entrypoints.sh runs, so a collector added to the manifest, or a file added to
// the generator, is checked with no edit here. Adding a collector therefore
// fails this check until a trigger covers its command directory, and the
// manifest and generator are triggers of the ci-gate-registry gate so a PR that
// changes either runs this check locally.
//
// A registry that does not register the gate (a hermetic fixture) is skipped.
// A registered gate whose manifest cannot be read or generated is an error, not
// a skip: an unverifiable output set must not read as covered.
func checkGeneratorOutputCoverage(repoRoot string, reg *Registry) []error {
	var gate *Gate
	for i := range reg.Gates {
		if reg.Gates[i].ID == collectorEntrypointsGateID {
			gate = &reg.Gates[i]
			break
		}
	}
	if gate == nil {
		return nil
	}
	manifests, err := entrypoints.LoadManifestFile(filepath.Join(repoRoot, filepath.FromSlash(collectorEntrypointsManifest)))
	if err != nil {
		return []error{fmt.Errorf("drift: gate %q: cannot derive the generated collector output set from %s: %w", gate.ID, collectorEntrypointsManifest, err)}
	}
	var errs []error
	for _, manifest := range manifests {
		files, err := entrypoints.Generate(manifest)
		if err != nil {
			errs = append(errs, fmt.Errorf("drift: gate %q: cannot generate collector %q to derive its output set: %w", gate.ID, manifest.RuntimeName, err))
			continue
		}
		for _, file := range files {
			output := path.Join(manifest.CommandDir, file.Name)
			if !anyTriggerMatches(gate.Triggers, output) {
				errs = append(errs, fmt.Errorf(
					"drift: gate %q is not selected by generated collector output %q (collector %q, triggers: %v); "+
						"a hand edit to only that file would not select the gate locally and would first fail in CI as a stale generated file — "+
						"add a trigger for %s/**",
					gate.ID, output, manifest.RuntimeName, gate.Triggers, manifest.CommandDir,
				))
			}
		}
	}
	return errs
}
