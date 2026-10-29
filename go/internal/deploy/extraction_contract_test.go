// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// contractRecordRel is the #8871 deploy-contract extraction record,
// relative to the repository root. It defines what eshu-hq/eshu-deploy
// publishes on day one (Helm, Compose, GitOps examples, deployment
// defaults, and the cross-repository compatibility matrix) and the
// core-owned boundary it must not absorb (schema-bootstrap and repair
// implementation). The record is frozen extraction evidence: the
// cutover-time tree pins were removed with the in-tree deploy surface,
// and what remains pins the record's prose plus the living core-owned
// boundary against the pinned eshu-deploy checkout.
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
	"scripts/ci/promote-moving-tags.sh",
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
	"docker-publish.yml",
	"promote-moving-tags",
	"scripts/ci/promote-moving-tags.sh",
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

// TestExtractionContractCoreOwnsBootstrap pins the migration-plan
// boundary inside the deploy contract: the schema-bootstrap Job template
// moved to eshu-deploy, but what it invokes stays core. The template must
// render the core image and exec the core-owned bootstrap binary, the
// values schema must still gate the schemaBootstrap stanza, and the
// implementation behind the binary must still live in core.
func TestExtractionContractCoreOwnsBootstrap(t *testing.T) {
	text := readContractRecord(t)
	root := repoRootForCensus(t)
	checkout := deployCheckoutForContract(t)
	jobRaw, err := os.ReadFile(filepath.Join(checkout, "helm/eshu/templates/job-schema-bootstrap.yaml"))
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
	for _, f := range []string{
		"scripts/ci/promote-moving-tags.sh",
		".github/workflows/docker-publish.yml",
	} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("release-automation file %s is gone: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(checkout, "scripts/ci/push-helm-chart.sh")); err != nil {
		t.Errorf("deploy release-automation file scripts/ci/push-helm-chart.sh is gone from the pinned checkout: %v", err)
	}
	schemaRaw, err := os.ReadFile(filepath.Join(checkout, "helm/eshu/values.schema.json"))
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
