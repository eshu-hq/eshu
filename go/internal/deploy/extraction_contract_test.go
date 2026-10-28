// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// contractRecordRel is the #8871 deploy-contract extraction record,
// relative to the repository root. It defines what eshu-hq/eshu-deploy
// publishes on day one (Helm, Compose, GitOps examples, deployment
// defaults, and the cross-repository compatibility matrix) and the
// core-owned boundary it must not absorb (schema-bootstrap and repair
// implementation). The census record inventories the assets; this record
// is the versioned contract the cutover compares against.
const contractRecordRel = "docs/internal/evidence/8871-eshu-deploy-contract.md"

// contractSourceFamilies are the deployment-asset families the contract
// record must name as its extraction surface. They mirror the census
// families so the contract cannot silently drop a lane the inventory
// already committed to.
var contractSourceFamilies = []string{
	"deploy/helm/eshu/values.yaml",
	"deploy/helm/eshu/values.schema.json",
	"deploy/helm/eshu/templates/job-schema-bootstrap.yaml",
	"docker-compose.yaml",
	"deploy/argocd",
	"docs/public/deploy/kubernetes/runtime-parity-matrix.md",
	"scripts/ci/push-helm-chart.sh",
}

// contractRecordMustName lists strings the contract record must contain:
// the destination repository and issue anchors, the census PR it builds
// on, the core-owned bootstrap boundary, the release-automation lane, and
// the compatibility-matrix deliverable.
var contractRecordMustName = []string{
	"eshu-deploy",
	"#8871",
	"#9356",
	"schemaBootstrap",
	"go/cmd/bootstrap-data-plane",
	"eshu-bootstrap-data-plane",
	"promote-moving-tags",
	"compatibility matrix",
	"## Compatibility matrix",
	"oldest",
	"newest",
}

func readContractRecord(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRootForCensus(t), contractRecordRel))
	if err != nil {
		t.Fatalf("read contract record %s: %v", contractRecordRel, err)
	}
	return string(body)
}

// contractParityMatrixRel is the in-tree parity baseline the contract
// record supersedes as the published cross-repository matrix. The matrix
// test reads it so the supersede claim stays pinned to a baseline that
// still exists with its expected heading.
const contractParityMatrixRel = "docs/public/deploy/kubernetes/runtime-parity-matrix.md"

func TestExtractionContractNamesSourceFamilies(t *testing.T) {
	text := readContractRecord(t)
	for _, family := range contractSourceFamilies {
		if !strings.Contains(text, family) {
			t.Errorf("contract record %s does not name source family %s", contractRecordRel, family)
		}
	}
	for _, want := range contractRecordMustName {
		if !strings.Contains(text, want) {
			t.Errorf("contract record %s does not name %q", contractRecordRel, want)
		}
	}
}

// TestExtractionContractPinsChartIdentity keeps the contract's version
// basis honest: the chart name, version, and appVersion the record
// presents as the extraction baseline must match the tree, so a silent
// bump fails here instead of shipping a stale contract to eshu-deploy.
func TestExtractionContractPinsChartIdentity(t *testing.T) {
	text := readContractRecord(t)
	root := repoRootForCensus(t)
	chartRaw, err := os.ReadFile(filepath.Join(root, "deploy/helm/eshu/Chart.yaml"))
	if err != nil {
		t.Fatalf("read Chart.yaml: %v", err)
	}
	pins := []struct{ tree, record string }{
		{"name: eshu", "name `eshu`"},
		{"version: 0.0.3-pre-release-26", "version: 0.0.3-pre-release-26"},
		{`appVersion: "v0.0.3-pre-release-18"`, "appVersion: v0.0.3-pre-release-18"},
	}
	for _, pin := range pins {
		if !strings.Contains(string(chartRaw), pin.tree) {
			t.Errorf("Chart.yaml no longer contains %q; update the chart or the contract record", pin.tree)
		}
		if !strings.Contains(text, pin.record) {
			t.Errorf("contract record %s no longer states %q; update the record", contractRecordRel, pin.record)
		}
	}
}

// TestExtractionContractPinsImageIdentities guards the immutable-artifact
// half of the deploy contract: the core image, the digest-pinned bundled
// collectors, and the NornicDB default the record presents as the tested
// pair must resolve to the same refs the chart defaults carry.
func TestExtractionContractPinsImageIdentities(t *testing.T) {
	text := readContractRecord(t)
	root := repoRootForCensus(t)
	raw, err := os.ReadFile(filepath.Join(root, "deploy/helm/eshu/values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse values.yaml: %v", err)
	}
	image, _ := doc["image"].(map[string]any)
	repo, _ := image["repository"].(string)
	tag, _ := image["tag"].(string)
	if repo != "ghcr.io/eshu-hq/eshu" || tag != "v0.0.3-pre-release-18" {
		t.Fatalf("core image defaults are %s:%s; the contract pins ghcr.io/eshu-hq/eshu:v0.0.3-pre-release-18", repo, tag)
	}
	for _, want := range []string{
		"ghcr.io/eshu-hq/eshu",
		"v0.0.3-pre-release-18",
		"eshu-collector-loki@sha256:",
		"eshu-collector-tempo@sha256:",
		"nornicdb-amd64-cpu",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("values.yaml no longer carries %q; update the defaults or the contract record", want)
		}
		if !strings.Contains(text, want) {
			t.Errorf("contract record %s no longer states %q; update the record", contractRecordRel, want)
		}
	}
}

// TestExtractionContractCoreOwnsBootstrap pins the migration-plan
// boundary inside the deploy contract: the schema-bootstrap Job template
// moves to eshu-deploy, but what it invokes stays core. The template must
// render the core image and exec the core-owned bootstrap binary, the
// values schema must still gate the schemaBootstrap stanza, and the
// implementation behind the binary must still live in core.
func TestExtractionContractCoreOwnsBootstrap(t *testing.T) {
	text := readContractRecord(t)
	root := repoRootForCensus(t)
	jobRaw, err := os.ReadFile(filepath.Join(root, "deploy/helm/eshu/templates/job-schema-bootstrap.yaml"))
	if err != nil {
		t.Fatalf("read job-schema-bootstrap.yaml: %v", err)
	}
	for _, want := range []string{
		".Values.image.repository",
		".Values.image.tag",
		"eshu-bootstrap-data-plane",
	} {
		if !strings.Contains(string(jobRaw), want) {
			t.Errorf("job-schema-bootstrap.yaml no longer references %q; the core-image boundary moved without review", want)
		}
	}
	for _, want := range []string{
		"go/cmd/bootstrap-data-plane",
		"eshu-bootstrap-data-plane",
		"core-owned",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("contract record %s no longer states the core-owned boundary %q; update the record", contractRecordRel, want)
		}
	}
	schemaRaw, err := os.ReadFile(filepath.Join(root, "deploy/helm/eshu/values.schema.json"))
	if err != nil {
		t.Fatalf("read values.schema.json: %v", err)
	}
	if !strings.Contains(string(schemaRaw), `"schemaBootstrap"`) {
		t.Error("values.schema.json no longer gates the schemaBootstrap stanza")
	}
	if _, err := os.Stat(filepath.Join(root, "go/cmd/bootstrap-data-plane")); err != nil {
		t.Errorf("core-owned bootstrap implementation go/cmd/bootstrap-data-plane is gone: %v", err)
	}
}

// TestExtractionContractMatrixStatesTestedPair pins the compatibility
// matrix half of the deploy contract: the record must state the tested
// core/component/external pair it publishes (single supported pair at
// extraction; oldest and newest converge on it until independent
// releases widen the range), and the external pins must match the tree.
func TestExtractionContractMatrixStatesTestedPair(t *testing.T) {
	text := readContractRecord(t)
	root := repoRootForCensus(t)
	for _, want := range []string{
		"v0.0.3-pre-release-18",
		"postgres:18-alpine",
		"neo4j:2026-community@sha256:",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("contract record %s matrix no longer states tested pin %q; update the record", contractRecordRel, want)
		}
	}
	composeRaw, err := os.ReadFile(filepath.Join(root, "docker-compose.neo4j.yml"))
	if err != nil {
		t.Fatalf("read docker-compose.neo4j.yml: %v", err)
	}
	if !strings.Contains(string(composeRaw), "postgres:18-alpine") {
		t.Error("docker-compose.neo4j.yml no longer pins postgres:18-alpine; update the compose lane or the contract record")
	}
	if !strings.Contains(string(composeRaw), "neo4j:2026-community@sha256:") {
		t.Error("docker-compose.neo4j.yml no longer pins the neo4j community digest; update the compose lane or the contract record")
	}
	baselineRaw, err := os.ReadFile(filepath.Join(root, contractParityMatrixRel))
	if err != nil {
		t.Fatalf("read superseded parity baseline %s: %v", contractParityMatrixRel, err)
	}
	if !strings.Contains(string(baselineRaw), "# Runtime Parity Matrix") {
		t.Errorf("parity baseline %s lost its heading; the contract supersedes a baseline that moved without review", contractParityMatrixRel)
	}
	neoDigest := neo4jDigestOfCompose(t, string(composeRaw))
	if !strings.Contains(text, neoDigest) {
		t.Errorf("contract record %s matrix no longer carries the full neo4j digest %s; update the record", contractRecordRel, neoDigest)
	}
}

// neo4jDigestOfCompose extracts the full neo4j image ref (with digest)
// from the compose default so the matrix test pins the exact ref, not a
// prefix that could hide a digest rotation.
func neo4jDigestOfCompose(t *testing.T, compose string) string {
	t.Helper()
	const marker = "neo4j:2026-community@sha256:"
	i := strings.Index(compose, marker)
	if i < 0 {
		t.Fatal("compose file has no neo4j digest ref")
	}
	// The marker itself starts at the ref ("neo4j:..."), whether the
	// compose line carries a bare image or a ${VAR:-default} fallback.
	end := strings.IndexAny(compose[i:], " \n\"'}")
	if end < 0 {
		t.Fatal("cannot terminate the neo4j digest ref")
	}
	return compose[i : i+end]
}
