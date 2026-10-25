// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package image

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/discovery"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestCurrentWorkflowImageFileMetasHonorsTheReadCap (#8986 review P2-2): a
// workflow whose discovered size is over the read cap is left out without a
// read failure, matching a full snapshot where it is never parsed. A workflow
// discovered under the cap that grew past it before the digest read counts as
// a read failure and is not read whole. Not parallel: it lowers the
// process-wide read cap.
func TestCurrentWorkflowImageFileMetasHonorsTheReadCap(t *testing.T) {
	defer shared.SetSourceReadCapForTest(64)()

	repoRoot := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(repoRoot, ".github", "workflows", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	small := write("small.yml", "name: ci\n")
	big := write("big.yml", "name: big\n"+strings.Repeat("# pad\n", 20))
	grew := write("grew.yml", "name: grew\n"+strings.Repeat("# pad\n", 20))

	full := []discovery.FileWithSize{
		{Path: big, Size: 130},
		{Path: grew, Size: 10}, // discovered small, larger on disk now
		{Path: small, Size: 9},
	}
	metas, readFailures := CurrentWorkflowImageFileMetasWithReadFailures(repoRoot, nil, full)
	if len(metas) != 1 || metas[0].RelativePath != ".github/workflows/small.yml" {
		t.Fatalf("metas = %#v, want only small.yml", metas)
	}
	if readFailures != 1 {
		t.Fatalf("readFailures = %d, want 1 (grew.yml only; big.yml is a size skip)", readFailures)
	}
}
