// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deploy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// censusRecordRel is the #8871 extraction census record, relative to the
// repository root. It names every deployment-asset family the cutover moved
// to eshu-hq/eshu-deploy and the core-owned boundary it preserves. The
// record is frozen extraction evidence: the tree assertions that pinned it
// to the in-tree deploy surface were removed with that surface (#8871
// in-tree removal), and what remains pins the record's prose.
const censusRecordRel = "docs/internal/evidence/8871-eshu-deploy-census.md"

// censusSourceFamilies are the deployment-asset directories the census record
// must account for. They are asserted as named in the record so the
// inventory cannot drift silently after extraction.
var censusSourceFamilies = []string{
	"deploy/helm/eshu",
	"deploy/helm/eshu/templates",
	"deploy/argocd",
	"deploy/manifests",
	"deploy/observability",
	"deploy/grafana",
	"docs/public/deploy",
	"docs/public/run-locally",
}

// censusRecordMustName lists strings the census record must contain: the
// destination repository and issue anchors, the deployment-defaults contract
// (schemaBootstrap stays a template over a core-owned binary), the image
// policy, and the compatibility-matrix deliverable.
var censusRecordMustName = []string{
	"eshu-deploy",
	"#8871",
	"schemaBootstrap",
	"bootstrap-data-plane",
	"promote-moving-tags",
	"compatibility matrix",
	"docker-compose.yaml",
	"deploy/argocd",
	"deploy/helm/eshu/values.yaml",
	"values.schema.json",
	"runtime-parity-matrix",
}

func repoRootForCensus(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is go/internal/deploy/extraction_census_test.go.
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return filepath.Dir(root)
	}
	return root
}

func readCensusRecord(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRootForCensus(t), censusRecordRel))
	if err != nil {
		t.Fatalf("read census record %s: %v", censusRecordRel, err)
	}
	return string(body)
}

func TestExtractionCensusRecordNamesSourceFamilies(t *testing.T) {
	text := readCensusRecord(t)
	for _, family := range censusSourceFamilies {
		if !strings.Contains(text, family) {
			t.Errorf("census record %s does not name source family %s", censusRecordRel, family)
		}
	}
	for _, want := range censusRecordMustName {
		if !strings.Contains(text, want) {
			t.Errorf("census record %s does not name %q", censusRecordRel, want)
		}
	}
}
