// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deploy

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// censusRecordRel is the #8871 AC1 extraction census record, relative to the
// repository root. It must name every deployment-asset family the cutover
// moves to eshu-hq/eshu-deploy and the core-owned boundary it preserves.
const censusRecordRel = "docs/internal/evidence/8871-eshu-deploy-census.md"

// censusSourceFamilies are the deployment-asset directories the census record
// must account for. They are asserted against the tree so the record cannot
// drift silently when a family moves before extraction.
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

// censusCountChecks pins the census "## Counts" lines to the tree. Each label
// maps to a directory walked (recursive for overlays/docs) or listed from
// the repository root; the expected file count lives only in the census
// record, parsed as "- <label>: <N>".
var censusCountChecks = []struct {
	label     string
	dir       string
	recursive bool
}{
	{"chart templates", "deploy/helm/eshu/templates", false},
	{"argocd files", "deploy/argocd", true},
	{"minimal manifests", "deploy/manifests", true},
	{"observability files", "deploy/observability", false},
	{"grafana dashboards", "deploy/grafana/dashboards", false},
	{"deploy docs", "docs/public/deploy", true},
	{"run-locally files", "docs/public/run-locally", false},
}

// censusKeyFiles are individual files the census must name and the tree must
// contain: chart entry points, release automation, and the core-owned
// implementation behind the schema-bootstrap boundary.
var censusKeyFiles = []string{
	"deploy/helm/eshu/Chart.yaml",
	"deploy/helm/eshu/values.yaml",
	"deploy/helm/eshu/values.schema.json",
	"deploy/helm/eshu/README.md",
	"deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml",
	"deploy/helm/eshu/templates/job-schema-bootstrap.yaml",
	"deploy/argocd/base/application.yaml",
	"deploy/manifests/minimal/kustomization.yaml",
	"Dockerfile",
	".github/workflows/docker-publish.yml",
	"scripts/ci/push-helm-chart.sh",
	"scripts/ci/select-docker-publish-lanes.sh",
	"go/cmd/bootstrap-data-plane",
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

func TestExtractionCensusFamiliesExist(t *testing.T) {
	root := repoRootForCensus(t)
	for _, family := range censusSourceFamilies {
		info, err := os.Stat(filepath.Join(root, family))
		if err != nil {
			t.Errorf("census family %s missing from tree: %v", family, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("census family %s is not a directory", family)
		}
	}
}

func TestExtractionCensusKeyFilesExist(t *testing.T) {
	root := repoRootForCensus(t)
	for _, f := range censusKeyFiles {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("census key file %s missing from tree: %v", f, err)
		}
	}
}

// censusCountRE parses one census "## Counts" line: "- <label>: <N>".
var censusCountRE = regexp.MustCompile(`(?m)^- (.+): (\d+)$`)

func TestExtractionCensusCountsMatchTree(t *testing.T) {
	text := readCensusRecord(t)
	counts := map[string]int{}
	for _, m := range censusCountRE.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("census count for %q is not a number: %v", m[1], err)
		}
		counts[strings.TrimSpace(m[1])] = n
	}
	root := repoRootForCensus(t)
	for _, check := range censusCountChecks {
		want, ok := counts[check.label]
		if !ok {
			t.Errorf("census record %s has no count line for %q", censusRecordRel, check.label)
			continue
		}
		got, err := countTreeFiles(filepath.Join(root, check.dir), check.recursive)
		if err != nil {
			t.Errorf("count files under %s: %v", check.dir, err)
			continue
		}
		if got != want {
			t.Errorf("census claims %d %s but tree holds %d under %s; update the record or account for the move",
				want, check.label, got, check.dir)
		}
	}
}

func countTreeFiles(dir string, recursive bool) (int, error) {
	if !recursive {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return 0, err
		}
		n := 0
		for _, e := range entries {
			if !e.IsDir() {
				n++
			}
		}
		return n, nil
	}
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n, err
}

func TestExtractionCensusNamesEveryRootComposeFile(t *testing.T) {
	text := readCensusRecord(t)
	matches, err := filepath.Glob(filepath.Join(repoRootForCensus(t), "docker-compose.*"))
	if err != nil {
		t.Fatalf("glob root compose files: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no docker-compose.* files at repository root")
	}
	for _, m := range matches {
		base := filepath.Base(m)
		if !strings.Contains(text, base) {
			t.Errorf("census record %s does not name root compose file %s", censusRecordRel, base)
		}
	}
}

// TestExtractionImageRefsCarryExplicitTags guards the immutable-artifact half
// of the deployment-defaults contract: every image repository named in the
// chart defaults must resolve to an explicit tag or digest. A bare `latest`
// or empty tag would make the extracted repository unreleasable as an
// independent artifact stream.
func TestExtractionImageRefsCarryExplicitTags(t *testing.T) {
	root := repoRootForCensus(t)
	raw, err := os.ReadFile(filepath.Join(root, "deploy/helm/eshu/values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse values.yaml: %v", err)
	}
	images := collectImageStanzas(doc)
	if len(images) == 0 {
		t.Fatal("values.yaml has no image stanzas; the chart defaults moved without review")
	}
	for _, img := range images {
		tag, _ := img["tag"].(string)
		if tag == "" || tag == "latest" {
			t.Errorf("image stanza %v carries no explicit tag; immutable artifacts require one", img)
		}
	}
}

func collectImageStanzas(node any) []map[string]any {
	var out []map[string]any
	switch v := node.(type) {
	case map[string]any:
		if repo, ok := v["repository"].(string); ok && repo != "" {
			if _, hasTag := v["tag"]; hasTag {
				out = append(out, v)
			}
		}
		for _, child := range v {
			out = append(out, collectImageStanzas(child)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, collectImageStanzas(child)...)
		}
	}
	return out
}

// TestExtractionValuesSchemaIsValidJSON keeps the chart's value gate honest:
// values.schema.json must parse, since helm and the runtime contract tests
// reject values it cannot read.
func TestExtractionValuesSchemaIsValidJSON(t *testing.T) {
	root := repoRootForCensus(t)
	raw, err := os.ReadFile(filepath.Join(root, "deploy/helm/eshu/values.schema.json"))
	if err != nil {
		t.Fatalf("read values.schema.json: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("values.schema.json does not parse: %v", err)
	}
	if _, ok := doc["$schema"]; !ok {
		t.Error("values.schema.json has no $schema key; not a JSON schema")
	}
}
