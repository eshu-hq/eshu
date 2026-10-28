// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

func TestAnalyzerOversizedRPMDatabaseWarns(t *testing.T) {
	for _, mode := range []string{"layer", "rootfs"} {
		t.Run(mode, func(t *testing.T) {
			input := testImageClaimInput(t)
			target := TargetConfig{ScopeID: input.Target.ScopeID, ImageDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa"}
			if mode == "layer" {
				path := filepath.Join(t.TempDir(), "layer.tar.gz")
				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				gz := gzip.NewWriter(file)
				tw := tar.NewWriter(gz)
				for name, body := range map[string]string{"etc/os-release": "ID=fedora\nVERSION_ID=35\n"} {
					if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
						t.Fatal(err)
					}
					if _, err := tw.Write([]byte(body)); err != nil {
						t.Fatal(err)
					}
				}
				if err := tw.WriteHeader(&tar.Header{Name: "usr/lib/sysimage/rpm/rpmdb.sqlite", Mode: 0o644, Size: (64 << 20) + 1}); err != nil {
					t.Fatal(err)
				}
				if _, err := io.CopyN(tw, zeroReader{}, (64<<20)+1); err != nil {
					t.Fatal(err)
				}
				if err := tw.Close(); err != nil {
					t.Fatal(err)
				}
				if err := gz.Close(); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				target.LayerPaths = []string{path}
			} else {
				target.RootFSPath = t.TempDir()
				writeRootFSFile(t, target.RootFSPath, "etc/os-release", "ID=fedora\nVERSION_ID=35\n")
				path := filepath.Join(target.RootFSPath, "usr/lib/sysimage/rpm/rpmdb.sqlite")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Truncate(path, (64<<20)+1); err != nil {
					file, createErr := os.Create(path)
					if createErr != nil {
						t.Fatal(createErr)
					}
					if err := file.Truncate((64 << 20) + 1); err != nil {
						t.Fatal(err)
					}
					if err := file.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
			input.Limits.MaxInputBytes = 256 << 20
			result, err := newTestAnalyzer(t, target).Analyze(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Output.ResultCount != 0 {
				t.Fatalf("ResultCount = %d", result.Output.ResultCount)
			}
			fact := firstFactByKind(t, result.Output.Facts, chain.VulnerabilityWarningFactKind)
			if got := fact.Payload["reason"]; got != string(ospackagevulnerability.WarningReasonResourceLimit) {
				t.Fatalf("warning reason = %#v", got)
			}
			if result.Usage.PeakMemoryBytes > 64<<20 {
				t.Fatalf("peak bytes = %d, want bounded", result.Usage.PeakMemoryBytes)
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

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
	for key, want := range map[string]string{"name": "publicsuffix-list-dafsa", "epoch": "", "upstream_version": "20210518", "release": "2.fc35", "arch": "noarch", "installed_version_raw": "20210518-2.fc35"} {
		if got := fact.Payload[key]; got != want {
			t.Fatalf("%s = %#v, want %q", key, got, want)
		}
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
	for key, want := range map[string]string{"name": "publicsuffix-list-dafsa", "epoch": "", "upstream_version": "20210518", "release": "2.fc35", "arch": "noarch", "installed_version_raw": "20210518-2.fc35"} {
		if got := fact.Payload[key]; got != want {
			t.Fatalf("%s = %#v, want %q", key, got, want)
		}
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

func TestAnalyzerNormalizesRPMOSReleaseIDs(t *testing.T) {
	db, err := os.ReadFile(filepath.Join("..", "..", "ospackagevulnerability", "testdata", "rpmdb.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id     string
		distro ospackagevulnerability.Distro
		source string
	}{
		{"rhel", ospackagevulnerability.DistroRedHat, "redhat"},
		{"amzn", ospackagevulnerability.DistroAmazonLinux, "amazonlinux"},
		{"sles", ospackagevulnerability.DistroSLES, ""},
	} {
		for _, mode := range []string{"layer", "rootfs"} {
			t.Run(tc.id+"/"+mode, func(t *testing.T) {
				input := testImageClaimInput(t)
				target := TargetConfig{
					ScopeID: input.Target.ScopeID, ImageDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
					Repositories:      []ospackagevulnerability.Repository{{ID: "vendor", Class: ospackagevulnerability.RepositoryClassVendor, VendorAdvisorySource: tc.source}},
					RPMOriginBindings: []ospackagevulnerability.RPMOriginBinding{{Name: "publicsuffix-list-dafsa", Version: "20210518", Release: "2.fc35", Arch: "noarch", RepositoryID: "vendor"}},
				}
				osRelease := "ID=" + tc.id + "\nVERSION_ID=35\n"
				if mode == "layer" {
					target.LayerPaths = []string{writeLayer(t, map[string]string{"etc/os-release": osRelease, "usr/lib/sysimage/rpm/rpmdb.sqlite": string(db)})}
				} else {
					target.RootFSPath = t.TempDir()
					writeRootFSFile(t, target.RootFSPath, "etc/os-release", osRelease)
					writeRootFSFile(t, target.RootFSPath, "usr/lib/sysimage/rpm/rpmdb.sqlite", string(db))
				}
				result, err := newTestAnalyzer(t, target).Analyze(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				if result.Output.ResultCount != 1 {
					t.Fatalf("ResultCount = %d", result.Output.ResultCount)
				}
				fact := firstFactByKind(t, result.Output.Facts, chain.VulnerabilityOSPackageFactKind)
				if fact.Payload["distro"] != string(tc.distro) || fact.Payload["vendor_advisory_source"] != tc.source {
					t.Fatalf("RPM fact = %#v", fact.Payload)
				}
				if tc.id == "sles" {
					firstFactByKind(t, result.Output.Facts, chain.VulnerabilityWarningFactKind)
				}
			})
		}
	}
}
