// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestUnsupportedWarningKeepsImageKeysPresentWhenImageIdentityBlank locks the
// image analyzer's warning wire shape after scanner_worker.warning made the
// image-analysis fields optional. A layer-only target with no configured image
// reference or digest still passes TargetConfig.validate() (which requires only
// ScopeID plus a rootfs or layer source), so an unsupported warning for it must
// still carry image_reference and image_digest as present (empty) keys — the
// pre-contract required-string behavior — not omit them. Only the non-image
// WarningAnalyzer fallback omits those keys. The fact must also still decode
// cleanly through the typed contract seam.
func TestUnsupportedWarningKeepsImageKeysPresentWhenImageIdentityBlank(t *testing.T) {
	t.Parallel()

	layerPath := filepath.Join(t.TempDir(), "layer.tar.gz")
	if err := os.WriteFile(layerPath, []byte("not a tar archive"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", layerPath, err)
	}
	input := testImageClaimInput(t)
	// ImageReference and ImageDigest are deliberately blank: validate() permits
	// a layer-only target without image identity.
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:    input.Target.ScopeID,
		LayerPaths: []string{layerPath},
	})

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil warning output", err)
	}
	warning := firstFactByKind(t, result.Output.Facts, facts.ScannerWorkerWarningFactKind)

	for _, key := range []string{"image_reference", "image_digest"} {
		got, ok := warning.Payload[key]
		if !ok {
			t.Fatalf("warning payload missing %q key; want present (possibly empty): %#v", key, warning.Payload)
		}
		if got != "" {
			t.Fatalf("%s = %#v, want empty string for a blank image identity", key, got)
		}
	}

	if _, err := factschema.DecodeScannerWorkerWarning(factschema.Envelope{
		FactKind:      warning.FactKind,
		SchemaVersion: warning.SchemaVersion,
		Payload:       warning.Payload,
	}); err != nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = %v, want nil", err)
	}
}
