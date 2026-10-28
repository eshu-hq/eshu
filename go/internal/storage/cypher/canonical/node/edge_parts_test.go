// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package node

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

func gitlabMaterialization() canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		GenerationID: "gen-3",
		Entities: []canonical.EntityRow{
			{EntityID: "pipe-1", Label: "GitlabPipeline", FilePath: ".gitlab-ci.yml"},
			{EntityID: "job-build", Label: "GitlabJob", EntityName: "build", FilePath: ".gitlab-ci.yml"},
			{
				EntityID: "job-test", Label: "GitlabJob", EntityName: "test",
				FilePath: ".gitlab-ci.yml",
				Metadata: map[string]any{"needs": "build, test, ghost"},
			},
			{EntityID: "other-1", Label: "Function", EntityName: "other", FilePath: "main.go"},
		},
	}
}

func countPartsByFlag(parts []Part, retract bool) int {
	count := 0
	for _, part := range parts {
		if part.IsRetract == retract {
			count++
		}
	}
	return count
}

func mergeRows(t *testing.T, parts []Part, cypherFragment string) []map[string]any {
	t.Helper()
	for _, part := range parts {
		if part.IsRetract || !strings.Contains(part.Cypher, cypherFragment) {
			continue
		}
		rows, ok := part.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("part %q rows = %T, want []map[string]any", cypherFragment, part.Parameters["rows"])
		}
		return rows
	}
	t.Fatalf("no merge part containing %q in %d parts", cypherFragment, len(parts))
	return nil
}

func TestGitlabEdgePartsBuildsRetractsAndMerges(t *testing.T) {
	t.Parallel()

	parts := GitlabEdgeParts(gitlabMaterialization())
	// Two Drain-marked retracts (DEFINES_JOB scoped to the pipeline,
	// NEEDS scoped to every job) plus one MERGE per edge family.
	if len(parts) != 4 {
		t.Fatalf("len(parts) = %d, want 4 (2 retracts + 2 merges)", len(parts))
	}
	if got := countPartsByFlag(parts, true); got != 2 {
		t.Fatalf("retract parts = %d, want 2", got)
	}
	for _, part := range parts {
		if part.IsRetract && !part.Drain {
			t.Errorf("retract part %q is not Drain-marked", part.Cypher[:40])
		}
		if part.IsRetract {
			if got := part.Parameters["generation_id"]; got != "gen-3" {
				t.Errorf("generation_id = %v, want gen-3", got)
			}
		}
	}

	defines := mergeRows(t, parts, "DEFINES_JOB")
	if len(defines) != 2 {
		t.Fatalf("DEFINES_JOB rows = %d, want 2 (one per job)", len(defines))
	}
	if got := defines[0]["generation_id"]; got != "gen-3" {
		t.Fatalf("DEFINES_JOB row generation_id = %v, want gen-3", got)
	}
	needs := mergeRows(t, parts, ":NEEDS]")
	if len(needs) != 1 {
		t.Fatalf("NEEDS rows = %d, want 1 (self and unknown needs dropped)", len(needs))
	}
	if got := needs[0]; got["source_uid"] != "job-test" || got["target_uid"] != "job-build" {
		t.Fatalf("NEEDS row = %v, want test -> build", got)
	}
}

func TestGitlabEdgePartsEmptyWithoutEntities(t *testing.T) {
	t.Parallel()

	if parts := GitlabEdgeParts(canonical.CanonicalMaterialization{}); parts != nil {
		t.Fatalf("parts = %v, want nil for a materialization without GitLab entities", parts)
	}
	mat := gitlabMaterialization()
	mat.Entities = mat.Entities[3:]
	if parts := GitlabEdgeParts(mat); parts != nil {
		t.Fatalf("parts = %v, want nil when only non-GitLab entities remain", parts)
	}
}

func TestGitlabEdgePartsJobsWithoutPipeline(t *testing.T) {
	t.Parallel()

	mat := gitlabMaterialization()
	var kept []canonical.EntityRow
	for _, entity := range mat.Entities {
		if entity.Label != "GitlabPipeline" {
			kept = append(kept, entity)
		}
	}
	mat.Entities = kept
	parts := GitlabEdgeParts(mat)
	// No pipeline means no DEFINES_JOB rows and no DEFINES_JOB retract,
	// but the NEEDS family still resolves between sibling jobs.
	if got := countPartsByFlag(parts, true); got != 1 {
		t.Fatalf("retract parts = %d, want 1 (NEEDS only)", got)
	}
	needs := mergeRows(t, parts, ":NEEDS]")
	if len(needs) != 1 {
		t.Fatalf("NEEDS rows = %d, want 1", len(needs))
	}
	for _, part := range parts {
		if !part.IsRetract && strings.Contains(part.Cypher, "DEFINES_JOB") {
			t.Fatalf("unexpected DEFINES_JOB merge without a pipeline: %v", part.Parameters)
		}
	}
}

func TestSplitGitlabList(t *testing.T) {
	t.Parallel()

	if got := splitGitlabList(""); got != nil {
		t.Fatalf("splitGitlabList(\"\") = %v, want nil", got)
	}
	if got := splitGitlabList("  , ,"); len(got) != 0 {
		t.Fatalf("splitGitlabList(blank) = %v, want empty", got)
	}
	want := []string{"build", "test"}
	if got := splitGitlabList(" build ,, test "); !slices.Equal(got, want) {
		t.Fatalf("splitGitlabList() = %v, want %v", got, want)
	}
}

func atlantisMaterialization(firstGeneration bool) canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		GenerationID:    "gen-5",
		RepoPath:        "/repo",
		FirstGeneration: firstGeneration,
		Entities: []canonical.EntityRow{
			{EntityID: "proj-base", Label: "AtlantisProject", EntityName: "base", FilePath: "atlantis.yaml", Metadata: map[string]any{"dir": "."}},
			{
				EntityID: "proj-app", Label: "AtlantisProject", EntityName: "app",
				FilePath: "atlantis.yaml",
				Metadata: map[string]any{"dir": "services/app", "depends_on": "base, app, ghost", "workflow": "standard"},
			},
			{EntityID: "wf-standard", Label: "AtlantisWorkflow", EntityName: "standard", FilePath: "atlantis.yaml"},
			{EntityID: "other-1", Label: "Function", EntityName: "other", FilePath: "main.go"},
		},
	}
}

func TestAtlantisEdgePartsBuildsAllFamilies(t *testing.T) {
	t.Parallel()

	parts := AtlantisEdgeParts(atlantisMaterialization(false))
	// Three Drain-marked retracts plus one MERGE per populated family
	// (MANAGES for the rooted dir, DEPENDS_ON for base, USES_WORKFLOW).
	if len(parts) != 6 {
		t.Fatalf("len(parts) = %d, want 6 (3 retracts + 3 merges)", len(parts))
	}
	if got := countPartsByFlag(parts, true); got != 3 {
		t.Fatalf("retract parts = %d, want 3", got)
	}
	manages := mergeRows(t, parts, "MANAGES")
	if len(manages) != 1 {
		t.Fatalf("MANAGES rows = %d, want 1 (the repo-root project manages nothing)", len(manages))
	}
	if got := manages[0]; got["source_uid"] != "proj-app" || got["target_path"] != "/repo/services/app" {
		t.Fatalf("MANAGES row = %v, want app -> /repo/services/app", got)
	}
	depends := mergeRows(t, parts, "DEPENDS_ON")
	if len(depends) != 1 {
		t.Fatalf("DEPENDS_ON rows = %d, want 1 (self and unknown deps dropped)", len(depends))
	}
	if got := depends[0]; got["source_uid"] != "proj-app" || got["target_uid"] != "proj-base" {
		t.Fatalf("DEPENDS_ON row = %v, want app -> base", got)
	}
	workflows := mergeRows(t, parts, "USES_WORKFLOW")
	if len(workflows) != 1 {
		t.Fatalf("USES_WORKFLOW rows = %d, want 1", len(workflows))
	}
	if got := workflows[0]; got["source_uid"] != "proj-app" || got["target_uid"] != "wf-standard" {
		t.Fatalf("USES_WORKFLOW row = %v, want app -> standard", got)
	}
}

func TestAtlantisEdgePartsSkipsRetractsOnFirstGeneration(t *testing.T) {
	t.Parallel()

	parts := AtlantisEdgeParts(atlantisMaterialization(true))
	if got := countPartsByFlag(parts, true); got != 0 {
		t.Fatalf("retract parts = %d, want 0 on the first generation", got)
	}
	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3 merges without retracts", len(parts))
	}
}

func TestAtlantisEdgePartsEmptyWithoutProjects(t *testing.T) {
	t.Parallel()

	if parts := AtlantisEdgeParts(canonical.CanonicalMaterialization{}); parts != nil {
		t.Fatalf("parts = %v, want nil without Atlantis projects", parts)
	}
}

func TestNormalizeAtlantisDir(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":              "",
		".":             "",
		"/":             "",
		"  ":            "",
		"services/app":  "services/app",
		"./services/x":  "services/x",
		"services/app/": "services/app",
	}
	for dir, want := range cases {
		if got := normalizeAtlantisDir(dir); got != want {
			t.Errorf("normalizeAtlantisDir(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestSplitAtlantisList(t *testing.T) {
	t.Parallel()

	if got := splitAtlantisList(""); got != nil {
		t.Fatalf("splitAtlantisList(\"\") = %v, want nil", got)
	}
	want := []string{"a", "b"}
	if got := splitAtlantisList(" a ,, b "); !slices.Equal(got, want) {
		t.Fatalf("splitAtlantisList() = %v, want %v", got, want)
	}
}

func helmMaterialization(firstGeneration bool) canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		GenerationID:    "gen-7",
		FirstGeneration: firstGeneration,
		Entities: []canonical.EntityRow{
			{EntityID: "use-1", Label: "HelmTemplateValueUsage", EntityName: "image.tag", FilePath: "charts/app/templates/deployment.yaml"},
			{EntityID: "use-orphan", Label: "HelmTemplateValueUsage", EntityName: "missing.key", FilePath: "charts/app/templates/service.yaml"},
			{EntityID: "def-1", Label: "HelmValueDefinition", EntityName: "image.tag", FilePath: "charts/app/values.yaml"},
			{EntityID: "other-1", Label: "Function", EntityName: "other", FilePath: "main.go"},
		},
	}
}

func TestHelmTemplateValueEdgePartsBuildsRetractsAndMerge(t *testing.T) {
	t.Parallel()

	parts := HelmTemplateValueEdgeParts(helmMaterialization(false))
	// Legacy REFERENCES retract, dedicated-type retract, then the MERGE.
	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3 (2 retracts + 1 merge)", len(parts))
	}
	if got := countPartsByFlag(parts, true); got != 2 {
		t.Fatalf("retract parts = %d, want 2", got)
	}
	rows := mergeRows(t, parts, "HELM_VALUE_REFERENCE")
	if len(rows) != 1 {
		t.Fatalf("merge rows = %d, want 1 (the orphan usage resolves nowhere)", len(rows))
	}
	row := rows[0]
	if row["source_uid"] != "use-1" || row["target_uid"] != "def-1" {
		t.Fatalf("row endpoints = %v/%v, want use-1 -> def-1", row["source_uid"], row["target_uid"])
	}
	if row["source_tool"] != "helm" {
		t.Fatalf("source_tool = %v, want helm", row["source_tool"])
	}
	kinds, ok := row["evidence_kinds"].([]string)
	if !ok || len(kinds) != 1 {
		t.Fatalf("evidence_kinds = %v (%T), want one kind", row["evidence_kinds"], row["evidence_kinds"])
	}
}

func TestHelmTemplateValueEdgePartsSkipsRetractsOnFirstGeneration(t *testing.T) {
	t.Parallel()

	parts := HelmTemplateValueEdgeParts(helmMaterialization(true))
	if got := countPartsByFlag(parts, true); got != 0 {
		t.Fatalf("retract parts = %d, want 0 on the first generation", got)
	}
	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want only the merge", len(parts))
	}
}

func TestHelmTemplateValueEdgePartsEmpty(t *testing.T) {
	t.Parallel()

	if parts := HelmTemplateValueEdgeParts(canonical.CanonicalMaterialization{}); parts != nil {
		t.Fatalf("parts = %v, want nil without Helm entities", parts)
	}
	mat := helmMaterialization(false)
	mat.Entities = mat.Entities[:2]
	if parts := HelmTemplateValueEdgeParts(mat); parts != nil {
		t.Fatalf("parts = %v, want nil without a definition to resolve to", parts)
	}
}

func TestHelmChartDirs(t *testing.T) {
	t.Parallel()

	if got := helmUsageChartDir("charts/app/templates/deployment.yaml"); got != "charts/app" {
		t.Fatalf("helmUsageChartDir(manifest) = %q, want charts/app", got)
	}
	if got := helmUsageChartDir("charts/app/templates/config/cm.yaml"); got != "charts/app" {
		t.Fatalf("helmUsageChartDir(nested) = %q, want the chart root, not an intermediate dir", got)
	}
	if got := helmUsageChartDir("charts/app/values.yaml"); got != "charts/app" {
		t.Fatalf("helmUsageChartDir(no templates segment) = %q, want the file directory fallback", got)
	}
	if got := helmDefinitionChartDir("charts/app/values.yaml"); got != "charts/app" {
		t.Fatalf("helmDefinitionChartDir() = %q, want charts/app", got)
	}
}
