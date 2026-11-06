// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// osReleaseLineVectorsPath is a frozen copy of the table the converter's
// osvReleaseLine test reads from the satellite collector
// (eshu-collector-vulnerability-intelligence testdata/os_release_line_vectors.json).
// The two key functions are written apart (the OSV ecosystem string versus the
// installed row's distro_version), so one shared table was the only thing that
// made them agree (#9806 PR-2b, ruling decision 8). The collector delete (#9999)
// splits the table: the satellite owns the converter side, this frozen copy pins
// the reducer side. A change to either key function alone fails the converter's
// or this test; a cross-repo drift needs a satellite-side bump to resync.
const osReleaseLineVectorsPath = "testdata/os_release_line_vectors.json"

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
