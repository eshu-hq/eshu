// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestRPMImageRequiresDistroIdentity(t *testing.T) {
	db, err := os.ReadFile(filepath.Join("..", "..", "ospackagevulnerability", "testdata", "rpmdb.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"layer", "rootfs"} {
		for _, release := range []string{"", "VERSION_ID=35\n", "ID=fedora\n"} {
			t.Run(mode+"/"+release, func(t *testing.T) {
				input := testImageClaimInput(t)
				target := TargetConfig{ScopeID: input.Target.ScopeID, ImageDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa"}
				if mode == "layer" {
					target.LayerPaths = []string{writeLayer(t, map[string]string{"etc/os-release": release, "usr/lib/sysimage/rpm/rpmdb.sqlite": string(db)})}
				} else {
					target.RootFSPath = t.TempDir()
					writeRootFSFile(t, target.RootFSPath, "etc/os-release", release)
					writeRootFSFile(t, target.RootFSPath, "usr/lib/sysimage/rpm/rpmdb.sqlite", string(db))
				}
				result, err := newTestAnalyzer(t, target).Analyze(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				if result.Output.ResultCount != 0 {
					t.Fatalf("ResultCount = %d, want 0", result.Output.ResultCount)
				}
				warning := firstFactByKind(t, result.Output.Facts, facts.ScannerWorkerWarningFactKind)
				if warning.Payload["reason"] != "image_analyzer_unsupported_target" {
					t.Fatalf("warning = %#v", warning.Payload)
				}
			})
		}
	}
}
