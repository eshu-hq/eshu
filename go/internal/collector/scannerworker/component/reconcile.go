// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// VerifySeededArtifact enforces the #8878 admission contract for a
// registry-loaded component manifest: when the manifest belongs to the
// eshu.scanner.* family, its image artifact must equal the platform
// ManifestFor pin for that component. Callers on the load-then-claim path
// (the extension worker's selectActivation and the coordinator's activation
// planner) run this after loading the installed manifest — which the chart
// reconcile init container installed from the seeded manifest.yaml — and
// refuse the activation on any mismatch, before any claim or execution.
//
// The check owns only the scanner family: any other component ID passes
// through untouched. An eshu.scanner.* identity with no analyzer mapping,
// a missing image artifact, or an artifact that differs from the pin in
// image or platform all deny fail-closed. Error text names the component
// ID (safe per the package privacy rule) and the defect, never an image
// value: neither the seeded ref nor the pin is echoed.
func VerifySeededArtifact(manifest component.Manifest) error {
	id := manifest.Metadata.ID
	if !strings.HasPrefix(id, "eshu.scanner.") {
		return nil
	}
	kind, err := AnalyzerFor(id)
	if err != nil {
		return err
	}
	pinned, err := ManifestFor(kind)
	if err != nil {
		return err
	}
	if len(manifest.Spec.Artifacts) != 1 || len(pinned.Spec.Artifacts) != 1 {
		return fmt.Errorf("scanner component %q declares no single image artifact: refusing admission without the pinned platform image", id)
	}
	got, want := manifest.Spec.Artifacts[0], pinned.Spec.Artifacts[0]
	if got.Image != want.Image || got.Platform != want.Platform {
		return fmt.Errorf("scanner component %q seeded artifact is not the pinned platform image: refusing admission", id)
	}
	return nil
}
