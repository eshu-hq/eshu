// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deploy

import (
	"bytes"
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
	// Pin the count as well as the names: the loop above only sees files
	// present in the tree, so a silent deletion would otherwise pass.
	if len(matches) != 20 {
		t.Errorf("repository root has %d docker-compose.* files, census pins 20; update the record or account for the move", len(matches))
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
	images := collectImageRefs(doc, "$")
	if len(images) == 0 {
		t.Fatal("values.yaml has no image refs; the chart defaults moved without review")
	}
	for _, img := range images {
		// Normalize case: a mixed-case moving tag ("Latest") violates the
		// immutable-artifact intent while passing an exact-match guard.
		if tag := strings.ToLower(img.tag); tag == "" || tag == "latest" {
			t.Errorf("image ref %q at %s carries no explicit tag; immutable artifacts require one", img.ref, img.where)
		}
		if img.single && !img.pinned {
			t.Errorf("single-string image ref %q at %s is neither digest-pinned nor explicitly tagged", img.ref, img.where)
		}
	}
}

// imageRef is one image reference found in chart defaults: either a
// repository/tag stanza map or a single-string `image:` value such as a
// satellite `seed.image` digest ref.
type imageRef struct {
	where  string // YAML path of the reference, for failure messages
	ref    string // repository, or the full single-string reference
	tag    string // tag value, digest, or "" when the stanza names none
	single bool   // true for single-string `image:` values
	pinned bool   // true when a single-string ref carries a digest
}

func collectImageRefs(node any, path string) []imageRef {
	var out []imageRef
	switch v := node.(type) {
	case map[string]any:
		if repo, ok := v["repository"].(string); ok && repo != "" {
			tag, _ := v["tag"].(string)
			out = append(out, imageRef{where: path, ref: repo, tag: tag})
		}
		for key, child := range v {
			if s, ok := child.(string); ok && key == "image" && s != "" {
				out = append(out, imageRef{
					where:  path + "." + key,
					ref:    s,
					tag:    imageTagOfSingleRef(s),
					single: true,
					pinned: strings.Contains(s, "@sha256:"),
				})
				continue
			}
			out = append(out, collectImageRefs(child, path+"."+key)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, collectImageRefs(child, path)...)
		}
	}
	return out
}

// imageTagOfSingleRef extracts the tag half of a single-string image
// reference: the digest when pinned, the `:tag` suffix otherwise, or "" when
// the reference names neither.
func imageTagOfSingleRef(s string) string {
	if i := strings.Index(s, "@"); i >= 0 {
		return s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[i+1:]
	}
	return ""
}

// TestCollectImageRefsSeesAllForms is the regression test for the
// single-string/tagless blind spot: the collector must surface every image
// reference form so the tag guard below can judge it.
func TestCollectImageRefsSeesAllForms(t *testing.T) {
	doc := map[string]any{
		"stanza": map[string]any{
			"repository": "ghcr.io/eshu-hq/eshu",
			"tag":        "v0.0.1",
		},
		"tagless": map[string]any{
			"repository": "ghcr.io/eshu-hq/orphan",
		},
		"satellite": map[string]any{
			"image": "ghcr.io/eshu-hq/eshu-collector-loki@sha256:12eac89e",
		},
		"rogue": map[string]any{
			"image": "ghcr.io/eshu-hq/eshu:latest",
		},
	}
	refs := collectImageRefs(doc, "$")
	if len(refs) != 4 {
		t.Fatalf("collector saw %d image refs, want 4 (stanza, tagless, satellite, rogue)", len(refs))
	}
}

// TestExtractionCensusPinsHelmContractFigures keeps the census prose honest:
// the chart identity, default-file sizes, and template breakdown the record
// presents as the #8871 baseline must match the tree, so a silent rename or
// version bump fails here instead of drifting into the cutover comparison.
func TestExtractionCensusPinsHelmContractFigures(t *testing.T) {
	text := readCensusRecord(t)
	root := repoRootForCensus(t)
	chartDir := filepath.Join(root, "deploy/helm/eshu")

	chartRaw, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	if err != nil {
		t.Fatalf("read Chart.yaml: %v", err)
	}
	chartPins := []struct{ tree, record string }{
		{"name: eshu", "name `eshu`"},
		{"version: 0.0.3-pre-release-26", "version: 0.0.3-pre-release-26"},
		{`appVersion: "v0.0.3-pre-release-18"`, "appVersion: v0.0.3-pre-release-18"},
	}
	for _, pin := range chartPins {
		if !strings.Contains(string(chartRaw), pin.tree) {
			t.Errorf("Chart.yaml no longer contains %q; update the chart or the census record", pin.tree)
		}
		if !strings.Contains(text, pin.record) {
			t.Errorf("census record %s no longer states %q; update the record", censusRecordRel, pin.record)
		}
	}

	lineCounts := map[string]struct {
		want   int
		needle string
	}{
		"values.yaml":        {1571, "1571 lines of deployment defaults"},
		"values.schema.json": {1739, "1739 lines, value gate"},
	}
	for file, pin := range lineCounts {
		raw, err := os.ReadFile(filepath.Join(chartDir, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if got := bytes.Count(raw, []byte{'\n'}); got != pin.want {
			t.Errorf("%s has %d lines, record states %d; update the record", file, got, pin.want)
		}
		// Anchor to the full phrase, not the bare number: a bare
		// Contains("31") also matches "1731 lines" and proves nothing.
		if !strings.Contains(text, pin.needle) {
			t.Errorf("census record %s no longer states %q for %s", censusRecordRel, pin.needle, file)
		}
	}

	breakdown := map[string]int{
		"deployment-*.yaml": 27,
		"service-*.yaml":    31,
		"servicemonitor*":   4,
		"_*.tpl":            17,
	}
	for pattern, want := range breakdown {
		matches, err := filepath.Glob(filepath.Join(chartDir, "templates", pattern))
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		if len(matches) != want {
			t.Errorf("templates/%s matches %d files, record states %d; update the record or account for the move",
				pattern, len(matches), want)
		}
		// Anchor to the "N `pattern`" phrase: bare numbers collide
		// ("1731" contains "31" and "17"), so Contains(number) is vacuous.
		if needle := strconv.Itoa(want) + " `" + pattern + "`"; !strings.Contains(text, needle) {
			t.Errorf("census record %s no longer states %q", censusRecordRel, needle)
		}
	}
	if _, err := os.Stat(filepath.Join(chartDir, "templates", "service.yaml")); err != nil {
		t.Errorf("templates/service.yaml (the +1 beside the 31 service-*.yaml) is gone: %v", err)
	}
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
