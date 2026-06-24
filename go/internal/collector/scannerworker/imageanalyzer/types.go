// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
)

// AnalyzerConfig configures the image unpacking analyzer.
type AnalyzerConfig struct {
	CollectorInstanceID string
	Targets             []TargetConfig
	Now                 func() time.Time
}

// TargetConfig maps one image scanner-worker scope to local image evidence.
type TargetConfig struct {
	ScopeID            string                                `json:"scope_id"`
	RootFSPath         string                                `json:"rootfs_path"`
	LayerPaths         []string                              `json:"layer_paths"`
	SourceURI          string                                `json:"source_uri"`
	SourceRecordID     string                                `json:"source_record_id"`
	ImageReference     string                                `json:"image_reference"`
	ImageDigest        string                                `json:"image_digest"`
	Distro             ospackagevulnerability.Distro         `json:"distro"`
	DistroVersion      string                                `json:"distro_version"`
	PackageManager     ospackagevulnerability.PackageManager `json:"package_manager"`
	Repositories       []ospackagevulnerability.Repository   `json:"repositories"`
	ThirdPartyPackages []string                              `json:"third_party_packages"`
}

// EvidenceSource identifies where installed package proof came from.
type EvidenceSource string

const (
	// EvidenceSourceRootFS identifies an already-extracted rootfs directory.
	EvidenceSourceRootFS EvidenceSource = "rootfs"
	// EvidenceSourceLayer identifies ordered local OCI layer tar streams.
	EvidenceSourceLayer EvidenceSource = "layer"
)

// Snapshot is the normalized package database evidence extracted from an image.
type Snapshot struct {
	Distro             ospackagevulnerability.Distro
	DistroVersion      string
	PackageManager     ospackagevulnerability.PackageManager
	Repositories       []ospackagevulnerability.Repository
	InstalledDB        []byte
	Status             []byte
	ThirdPartyPackages []string
	SourceURI          string
	SourceRecordID     string
	ImageReference     string
	ImageDigest        string
	EvidenceSource     EvidenceSource
	ExtractionReason   string
	ObservedAt         time.Time
}

func (t TargetConfig) validate() (TargetConfig, error) {
	t.ScopeID = strings.TrimSpace(t.ScopeID)
	t.RootFSPath = strings.TrimSpace(t.RootFSPath)
	t.SourceURI = strings.TrimSpace(t.SourceURI)
	t.SourceRecordID = strings.TrimSpace(t.SourceRecordID)
	t.ImageReference = strings.TrimSpace(t.ImageReference)
	t.ImageDigest = strings.TrimSpace(t.ImageDigest)
	t.Distro = ospackagevulnerability.Distro(strings.TrimSpace(string(t.Distro)))
	t.DistroVersion = strings.TrimSpace(t.DistroVersion)
	t.PackageManager = ospackagevulnerability.PackageManager(strings.TrimSpace(string(t.PackageManager)))
	t.LayerPaths = cleanStrings(t.LayerPaths)
	t.ThirdPartyPackages = cleanStrings(t.ThirdPartyPackages)
	for i := range t.Repositories {
		t.Repositories[i].URL = strings.TrimSpace(t.Repositories[i].URL)
		t.Repositories[i].Class = ospackagevulnerability.RepositoryClass(strings.TrimSpace(string(t.Repositories[i].Class)))
		if t.Repositories[i].Class == "" {
			t.Repositories[i].Class = ospackagevulnerability.RepositoryClassUnknown
		}
		if err := validateRepositoryClass(t.Repositories[i].Class); err != nil {
			return TargetConfig{}, fmt.Errorf("repository %d: %w", i, err)
		}
	}
	if t.ScopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if t.RootFSPath == "" && len(t.LayerPaths) == 0 {
		return TargetConfig{}, fmt.Errorf("rootfs_path or layer_paths is required")
	}
	if err := validateDistro(t.Distro); err != nil {
		return TargetConfig{}, err
	}
	if err := validatePackageManager(t.PackageManager); err != nil {
		return TargetConfig{}, err
	}
	return t, nil
}

func validateDistro(distro ospackagevulnerability.Distro) error {
	switch distro {
	case "", ospackagevulnerability.DistroAlpine, ospackagevulnerability.DistroDebian:
		return nil
	default:
		return fmt.Errorf("%w: unsupported distro", errUnsupportedTarget)
	}
}

func validatePackageManager(manager ospackagevulnerability.PackageManager) error {
	switch manager {
	case "", ospackagevulnerability.PackageManagerAPK, ospackagevulnerability.PackageManagerDPKG:
		return nil
	default:
		return fmt.Errorf("%w: unsupported package manager", errUnsupportedTarget)
	}
}

func validateRepositoryClass(class ospackagevulnerability.RepositoryClass) error {
	switch class {
	case ospackagevulnerability.RepositoryClassVendor,
		ospackagevulnerability.RepositoryClassThirdParty,
		ospackagevulnerability.RepositoryClassUnknown:
		return nil
	default:
		return fmt.Errorf("unsupported repository class %q", class)
	}
}

func cleanStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
