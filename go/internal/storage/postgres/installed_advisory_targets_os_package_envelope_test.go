// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestOSPackageAdvisoryFactEnvelopeFromTargetSkipsMalformedTarget(t *testing.T) {
	// Missing required fields (distro, arch, installed_version_raw) should
	// return ok=false so the caller increments the skip counter.
	target := workflow.OSPackageAdvisoryTarget{
		Distro:           "",
		DistroVersion:    "12",
		PackageManager:   "dpkg",
		PackageName:      "openssl",
		Arch:             "amd64",
		InstalledVersion: "1.1.1-1",
		FactID:           "fact-1",
		ScopeID:          "scope-1",
		GenerationID:     "gen-1",
	}
	_, ok := osPackageAdvisoryFactEnvelopeFromTarget(target)
	if ok {
		t.Error("target missing distro should return ok=false")
	}
}

func TestOSPackageAdvisoryFactEnvelopeFromTargetReturnsValidTarget(t *testing.T) {
	target := workflow.OSPackageAdvisoryTarget{
		Distro:           "debian",
		DistroVersion:    "12",
		PackageManager:   "dpkg",
		PackageName:      "openssl",
		Arch:             "amd64",
		InstalledVersion: "1.1.1-1",
		FactID:           "fact-1",
		ScopeID:          "scope-1",
		GenerationID:     "gen-1",
	}
	envelope, ok := osPackageAdvisoryFactEnvelopeFromTarget(target)
	if !ok {
		t.Fatal("valid target should return ok=true")
	}
	if envelope.FactKind == "" {
		t.Error("valid target envelope should have non-empty FactKind")
	}
}
