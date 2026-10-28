// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

func TestAnalyzerExtractsRPMFromLayer(t *testing.T) {
	db, err := os.ReadFile(filepath.Join("..", "..", "ospackagevulnerability", "testdata", "rpmdb.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	layer := writeLayer(t, map[string]string{
		"etc/os-release":                    "ID=fedora\nVERSION_ID=35\n",
		"usr/lib/sysimage/rpm/rpmdb.sqlite": string(db),
	})
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:           input.Target.ScopeID,
		Repositories:      []ospackagevulnerability.Repository{{ID: "fedora", Class: ospackagevulnerability.RepositoryClassVendor, VendorAdvisorySource: "fedora"}},
		RPMOriginBindings: []ospackagevulnerability.RPMOriginBinding{{Name: "publicsuffix-list-dafsa", Version: "20210518", Release: "2.fc35", Arch: "noarch", RepositoryID: "fedora"}},
		ImageDigest:       "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		LayerPaths:        []string{layer},
	})
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.ResultCount != 1 {
		t.Fatalf("ResultCount = %d, want 1", result.Output.ResultCount)
	}
	fact := firstFactByKind(t, result.Output.Facts, chain.VulnerabilityOSPackageFactKind)
	if got := fact.Payload["vendor_advisory_source"]; got != "fedora" {
		t.Fatalf("vendor_advisory_source = %#v, want fedora", got)
	}
}

func TestAnalyzerExtractsRPMFromRootFS(t *testing.T) {
	db, err := os.ReadFile(filepath.Join("..", "..", "ospackagevulnerability", "testdata", "rpmdb.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeRootFSFile(t, root, "etc/os-release", "ID=fedora\nVERSION_ID=35\n")
	writeRootFSFile(t, root, "usr/lib/sysimage/rpm/rpmdb.sqlite", string(db))
	input := testImageClaimInput(t)
	analyzer := newTestAnalyzer(t, TargetConfig{
		ScopeID:           input.Target.ScopeID,
		Repositories:      []ospackagevulnerability.Repository{{ID: "fedora", Class: ospackagevulnerability.RepositoryClassVendor, VendorAdvisorySource: "fedora"}},
		RPMOriginBindings: []ospackagevulnerability.RPMOriginBinding{{Name: "publicsuffix-list-dafsa", Version: "20210518", Release: "2.fc35", Arch: "noarch", RepositoryID: "fedora"}},
		ImageDigest:       "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		RootFSPath:        root,
	})
	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.ResultCount != 1 {
		t.Fatalf("ResultCount = %d, want 1", result.Output.ResultCount)
	}
	fact := firstFactByKind(t, result.Output.Facts, chain.VulnerabilityOSPackageFactKind)
	if got := fact.Payload["vendor_advisory_source"]; got != "fedora" {
		t.Fatalf("vendor_advisory_source = %#v, want fedora", got)
	}
}

func TestAnalyzerRPMUnboundOriginWarns(t *testing.T) {
	db, err := os.ReadFile(filepath.Join("..", "..", "ospackagevulnerability", "testdata", "rpmdb.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"layer", "rootfs"} {
		t.Run(mode, func(t *testing.T) {
			input := testImageClaimInput(t)
			target := TargetConfig{ScopeID: input.Target.ScopeID, ImageDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa", Repositories: []ospackagevulnerability.Repository{{ID: "fedora", Class: ospackagevulnerability.RepositoryClassVendor, VendorAdvisorySource: "fedora"}}}
			if mode == "layer" {
				target.LayerPaths = []string{writeLayer(t, map[string]string{"etc/os-release": "ID=fedora\nVERSION_ID=35\n", "usr/lib/sysimage/rpm/rpmdb.sqlite": string(db)})}
			} else {
				target.RootFSPath = t.TempDir()
				writeRootFSFile(t, target.RootFSPath, "etc/os-release", "ID=fedora\nVERSION_ID=35\n")
				writeRootFSFile(t, target.RootFSPath, "usr/lib/sysimage/rpm/rpmdb.sqlite", string(db))
			}
			result, err := newTestAnalyzer(t, target).Analyze(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			fact := firstFactByKind(t, result.Output.Facts, chain.VulnerabilityOSPackageFactKind)
			if got := fact.Payload["vendor_advisory_source"]; got != "" {
				t.Fatalf("vendor source = %#v, want empty", got)
			}
			firstFactByKind(t, result.Output.Facts, chain.VulnerabilityWarningFactKind)
		})
	}
}
