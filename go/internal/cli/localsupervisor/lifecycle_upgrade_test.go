// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import (
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/graphinstall"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestUpgradeForLayoutRequiresStoppedOwner(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalProcessAlive := graphProcessAlive
	originalInstall := graphInstallNornicDB
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphProcessAlive = originalProcessAlive
		graphInstallNornicDB = originalInstall
	})

	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return eshulocal.OwnerRecord{
			PID:           42,
			Profile:       string(query.ProfileLocalAuthoritative),
			GraphBackend:  string(query.GraphBackendNornicDB),
			GraphPID:      88,
			GraphBoltPort: 17687,
			GraphHTTPPort: 17474,
		}, nil
	}
	graphProcessAlive = func(pid int) bool {
		return pid == 42
	}
	graphInstallNornicDB = func(opts graphinstall.Options) (graphinstall.Result, error) {
		t.Fatal("graphInstallNornicDB called while owner was live")
		return graphinstall.Result{}, nil
	}

	_, err := UpgradeForLayout(eshulocal.Layout{
		WorkspaceRoot:   t.TempDir(),
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: "/workspace/owner.json",
	}, graphinstall.Options{From: "/tmp/nornicdb-headless"})
	if err == nil {
		t.Fatal("UpgradeForLayout() error = nil, want live-owner error")
	}
	if !strings.Contains(err.Error(), "eshu graph stop") {
		t.Fatalf("UpgradeForLayout() error = %q, want stop guidance", err.Error())
	}
}

func TestUpgradeForLayoutInstallsWithForceWhenStopped(t *testing.T) {
	originalReadOwnerRecord := graphReadOwnerRecord
	originalInstall := graphInstallNornicDB
	t.Cleanup(func() {
		graphReadOwnerRecord = originalReadOwnerRecord
		graphInstallNornicDB = originalInstall
	})

	graphReadOwnerRecord = func(path string) (eshulocal.OwnerRecord, error) {
		return eshulocal.OwnerRecord{}, os.ErrNotExist
	}
	var gotOptions graphinstall.Options
	graphInstallNornicDB = func(opts graphinstall.Options) (graphinstall.Result, error) {
		gotOptions = opts
		return graphinstall.Result{
			Installed:  true,
			BinaryPath: "/eshu/bin/nornicdb-headless",
			Version:    "v1.0.43",
		}, nil
	}

	result, err := UpgradeForLayout(eshulocal.Layout{
		WorkspaceRoot:   t.TempDir(),
		WorkspaceID:     "workspace-id",
		OwnerRecordPath: "/workspace/owner.json",
	}, graphinstall.Options{From: "/tmp/nornicdb-headless", SHA256: "abc123"})
	if err != nil {
		t.Fatalf("UpgradeForLayout() error = %v, want nil", err)
	}
	if !gotOptions.Force {
		t.Fatal("Force = false, want true for upgrade")
	}
	if gotOptions.From != "/tmp/nornicdb-headless" || gotOptions.SHA256 != "abc123" {
		t.Fatalf("upgrade options = %+v, want source/checksum flags", gotOptions)
	}
	if result.Version != "v1.0.43" {
		t.Fatalf("UpgradeForLayout() version = %q, want v1.0.43", result.Version)
	}
}
