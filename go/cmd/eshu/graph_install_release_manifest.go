package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

//go:embed nornicdb_release_manifest.json
var defaultPinnedNornicDBReleaseManifest []byte

var (
	graphPinnedNornicDBReleaseManifest = defaultPinnedNornicDBReleaseManifest
	graphInstallHostOS                 = runtime.GOOS
	graphInstallHostArch               = runtime.GOARCH
	graphInstallAppVersion             = buildinfo.AppVersion
)

type pinnedNornicDBReleaseManifest struct {
	Version  int                         `json:"version"`
	Backend  string                      `json:"backend"`
	Releases []pinnedNornicDBReleaseSpec `json:"releases"`
}

type pinnedNornicDBReleaseSpec struct {
	EshuVersion string                       `json:"eshu_version"`
	ReleaseTag  string                       `json:"release_tag"`
	Assets      []pinnedNornicDBReleaseAsset `json:"assets"`
}

type pinnedNornicDBReleaseAsset struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Format   string `json:"format"`
	Headless bool   `json:"headless"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
}

func resolvePinnedNornicDBReleaseSource(preferHeadless bool) (string, string, error) {
	manifest, err := readPinnedNornicDBReleaseManifest()
	if err != nil {
		return "", "", err
	}
	eshuVersion := strings.TrimSpace(graphInstallAppVersion())
	if eshuVersion == "" {
		eshuVersion = "dev"
	}
	hostOS := strings.TrimSpace(graphInstallHostOS)
	hostArch := strings.TrimSpace(graphInstallHostArch)
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
			return "", "", missingPinnedNornicDBReleaseAssetError("headless", eshuVersion, hostOS, hostArch)
		}
		return "", "", missingPinnedNornicDBReleaseAssetError("full", eshuVersion, hostOS, hostArch)
	}
	if preferHeadless {
		return "", "", missingPinnedNornicDBReleaseAssetError("headless", eshuVersion, hostOS, hostArch)
	}
	return "", "", missingPinnedNornicDBReleaseAssetError("full", eshuVersion, hostOS, hostArch)
}

func missingPinnedNornicDBReleaseAssetError(kind, eshuVersion, hostOS, hostArch string) error {
	sourceHint := "<path-to-nornicdb-headless>"
	if kind == "full" {
		sourceHint = "<path-to-nornicdb>"
	}
	return fmt.Errorf("no embedded %s NornicDB release asset for Eshu %s on %s/%s; Eshu currently tracks the latest NornicDB main branch, so build NornicDB from main and run eshu install nornicdb --from %s", kind, eshuVersion, hostOS, hostArch, sourceHint)
}

func readPinnedNornicDBReleaseManifest() (pinnedNornicDBReleaseManifest, error) {
	var manifest pinnedNornicDBReleaseManifest
	if err := json.Unmarshal(graphPinnedNornicDBReleaseManifest, &manifest); err != nil {
		return pinnedNornicDBReleaseManifest{}, fmt.Errorf("decode pinned NornicDB release manifest: %w", err)
	}
	if manifest.Version <= 0 {
		return pinnedNornicDBReleaseManifest{}, fmt.Errorf("decode pinned NornicDB release manifest: missing version")
	}
	if manifest.Backend != "nornicdb" {
		return pinnedNornicDBReleaseManifest{}, fmt.Errorf("decode pinned NornicDB release manifest: unexpected backend %q", manifest.Backend)
	}
	return manifest, nil
}
