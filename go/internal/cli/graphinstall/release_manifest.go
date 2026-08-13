// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphinstall

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

//go:embed nornicdb_release_manifest.json
var defaultPinnedReleaseManifest []byte

var (
	pinnedReleaseManifestData = defaultPinnedReleaseManifest
	installHostOS             = runtime.GOOS
	installHostArch           = runtime.GOARCH
	installAppVersion         = buildinfo.AppVersion
)

// pinnedReleaseManifest is the decoded shape of the embedded
// nornicdb_release_manifest.json: the NornicDB release assets Eshu pins per
// Eshu version, OS, and architecture. Eshu currently tracks the latest
// NornicDB main branch, so this manifest ships with zero releases and
// resolvePinnedReleaseSource always falls through to its "install with
// --from" guidance -- see release_manifest_test.go.
type pinnedReleaseManifest struct {
	Version  int                 `json:"version"`
	Backend  string              `json:"backend"`
	Releases []pinnedReleaseSpec `json:"releases"`
}

type pinnedReleaseSpec struct {
	EshuVersion string               `json:"eshu_version"`
	ReleaseTag  string               `json:"release_tag"`
	Assets      []pinnedReleaseAsset `json:"assets"`
}

type pinnedReleaseAsset struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Format   string `json:"format"`
	Headless bool   `json:"headless"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
}

func resolvePinnedReleaseSource(preferHeadless bool) (string, string, error) {
	manifest, err := readPinnedReleaseManifest()
	if err != nil {
		return "", "", err
	}
	eshuVersion := strings.TrimSpace(installAppVersion())
	if eshuVersion == "" {
		eshuVersion = "dev"
	}
	hostOS := strings.TrimSpace(installHostOS)
	hostArch := strings.TrimSpace(installHostArch)
	for _, release := range manifest.Releases {
		if strings.TrimSpace(release.EshuVersion) != eshuVersion {
			continue
		}
		for _, asset := range release.Assets {
			if asset.OS == hostOS && asset.Arch == hostArch && asset.Headless == preferHeadless {
				if strings.TrimSpace(asset.URL) == "" || strings.TrimSpace(asset.SHA256) == "" {
					return "", "", fmt.Errorf("pinned NornicDB release asset for %s/%s is incomplete", hostOS, hostArch)
				}
				return asset.URL, strings.ToLower(strings.TrimSpace(asset.SHA256)), nil
			}
		}
		if preferHeadless {
			return "", "", missingPinnedReleaseAssetError("headless", eshuVersion, hostOS, hostArch)
		}
		return "", "", missingPinnedReleaseAssetError("full", eshuVersion, hostOS, hostArch)
	}
	if preferHeadless {
		return "", "", missingPinnedReleaseAssetError("headless", eshuVersion, hostOS, hostArch)
	}
	return "", "", missingPinnedReleaseAssetError("full", eshuVersion, hostOS, hostArch)
}

func missingPinnedReleaseAssetError(kind, eshuVersion, hostOS, hostArch string) error {
	sourceHint := "<path-to-nornicdb-headless>"
	if kind == "full" {
		sourceHint = "<path-to-nornicdb>"
	}
	return fmt.Errorf("no embedded %s NornicDB release asset for Eshu %s on %s/%s; Eshu currently tracks the latest NornicDB main branch, so build NornicDB from main and run eshu install nornicdb --from %s", kind, eshuVersion, hostOS, hostArch, sourceHint)
}

func readPinnedReleaseManifest() (pinnedReleaseManifest, error) {
	var manifest pinnedReleaseManifest
	if err := json.Unmarshal(pinnedReleaseManifestData, &manifest); err != nil {
		return pinnedReleaseManifest{}, fmt.Errorf("decode pinned NornicDB release manifest: %w", err)
	}
	if manifest.Version <= 0 {
		return pinnedReleaseManifest{}, fmt.Errorf("decode pinned NornicDB release manifest: missing version")
	}
	if manifest.Backend != "nornicdb" {
		return pinnedReleaseManifest{}, fmt.Errorf("decode pinned NornicDB release manifest: unexpected backend %q", manifest.Backend)
	}
	return manifest, nil
}
