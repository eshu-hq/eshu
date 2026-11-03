// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// osReleaseLineVectorsPath is the table the converter's osvReleaseLine test
// also reads. The two key functions are written apart (the OSV ecosystem string
// versus the installed row's distro_version), so one shared table is the only
// thing that makes them agree (#9806 PR-2b, ruling decision 8).
const osReleaseLineVectorsPath = "../../../collector/vulnerabilityintelligence/testdata/os_release_line_vectors.json"

// TestOSHostReleaseLineMatchesSharedVectors pins osHostReleaseLine to the
// shared vector table. A change to either key function alone fails the
// converter's or this test.
func TestOSHostReleaseLineMatchesSharedVectors(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.FromSlash(osReleaseLineVectorsPath))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var table struct {
		Vectors []struct {
			Name        string `json:"name"`
			HostDistro  string `json:"host_distro"`
			HostVersion string `json:"host_version"`
			WantLine    string `json:"want_line"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
	checked := 0
	for _, vector := range table.Vectors {
		if vector.HostDistro == "" {
			continue
		}
		checked++
		if got := osHostReleaseLine(vector.HostDistro, vector.HostVersion); got != vector.WantLine {
			t.Errorf("%s: osHostReleaseLine(%q, %q) = %q, want %q", vector.Name, vector.HostDistro, vector.HostVersion, got, vector.WantLine)
		}
	}
	if checked == 0 {
		t.Fatalf("no vector pins the reducer side")
	}
}
