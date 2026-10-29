// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"path/filepath"
	"strings"
	"testing"
)

// Issue #9201: a deploy can roll new pods before Postgres migrations run.
// The schema bootstrap Job doubles as an ArgoCD PreSync hook behind the
// schemaBootstrap.argocdPreSync switch (default off), so a sync cannot skip
// migrations while direct helm installs keep their pre-install/pre-upgrade
// hooks.

func helmSchemaBootstrapAnnotations(t *testing.T, manifests []helmManifest) map[string]any {
	t.Helper()

	job := requireHelmManifest(t, manifests, "Job", "eshu-schema-bootstrap")
	annotations := helmMap(helmMap(job["metadata"])["annotations"])
	if len(annotations) == 0 {
		t.Fatal("schema bootstrap Job has no annotations")
	}
	return annotations
}

func TestHelmSchemaBootstrapDefaultKeepsHelmHooks(t *testing.T) {
	t.Parallel()

	annotations := helmSchemaBootstrapAnnotations(t, renderHelmChart(t))
	if got := helmString(annotations["helm.sh/hook"]); got != "pre-install,pre-upgrade" {
		t.Fatalf(`helm.sh/hook = %q, want "pre-install,pre-upgrade"`, got)
	}
	if _, ok := annotations["argocd.argoproj.io/hook"]; ok {
		t.Fatal("default render carries ArgoCD hook annotations, want Helm hooks only")
	}
}

func TestHelmSchemaBootstrapArgoCDPreSyncHook(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t,
		"--set", "schemaBootstrap.useHelmHooks=false",
		"--set", "schemaBootstrap.argocdPreSync.enabled=true",
	)
	annotations := helmSchemaBootstrapAnnotations(t, manifests)
	if got := helmString(annotations["argocd.argoproj.io/hook"]); got != "PreSync" {
		t.Fatalf(`argocd.argoproj.io/hook = %q, want "PreSync"`, got)
	}
	if got := helmString(annotations["argocd.argoproj.io/sync-wave"]); got != "-10" {
		t.Fatalf(`argocd.argoproj.io/sync-wave = %q, want "-10"`, got)
	}
	if got := helmString(annotations["argocd.argoproj.io/hook-delete-policy"]); got != "BeforeHookCreation" {
		t.Fatalf(`argocd.argoproj.io/hook-delete-policy = %q, want "BeforeHookCreation"`, got)
	}
	if _, ok := annotations["helm.sh/hook"]; ok {
		t.Fatal("ArgoCD render carries helm.sh/hook annotations, want ArgoCD hooks only")
	}
}

func TestHelmSchemaBootstrapRejectsBothHookFamilies(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(t,
		"--set", "schemaBootstrap.argocdPreSync.enabled=true",
	)
	if !strings.Contains(output, "mutually exclusive") {
		t.Fatalf("helm template failure does not name the hook conflict:\n%s", output)
	}
}

func TestHelmSchemaBootstrapKeepsFailedJobEvidence(t *testing.T) {
	t.Parallel()

	job := requireHelmManifest(t, renderHelmChart(t), "Job", "eshu-schema-bootstrap")
	spec := helmMap(job["spec"])
	if got, want := schemaBootstrapInt(spec["ttlSecondsAfterFinished"]), 86400; got != want {
		t.Fatalf("ttlSecondsAfterFinished = %d, want %d", got, want)
	}

	container := requireHelmContainer(t, job, "schema-bootstrap")
	if got, want := helmString(container["terminationMessagePolicy"]), "FallbackToLogsOnError"; got != want {
		t.Fatalf("terminationMessagePolicy = %q, want %q", got, want)
	}
}

func schemaBootstrapInt(raw any) int {
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func TestHelmSchemaBootstrapQAOverlayPinsHook(t *testing.T) {
	t.Parallel()

	chartValues := func(name string) string {
		t.Helper()
		return deployGitOpsPath(t, filepath.Join("argocd", name))
	}
	manifests := renderHelmChart(t,
		"-f", chartValues(filepath.Join("base", "values.yaml")),
		"-f", chartValues(filepath.Join("overlays", "qa", "values.yaml")),
	)
	job := requireHelmManifest(t, manifests, "Job", "eshu-schema-bootstrap")
	annotations := helmMap(helmMap(job["metadata"])["annotations"])
	if got := helmString(annotations["argocd.argoproj.io/hook"]); got != "PreSync" {
		t.Fatalf(`qa overlay argocd.argoproj.io/hook = %q, want "PreSync"`, got)
	}
	if got := helmString(annotations["argocd.argoproj.io/sync-wave"]); got != "-10" {
		t.Fatalf(`qa overlay argocd.argoproj.io/sync-wave = %q, want "-10"`, got)
	}
	if got := helmString(annotations["argocd.argoproj.io/hook-delete-policy"]); got != "BeforeHookCreation" {
		t.Fatalf(`qa overlay argocd.argoproj.io/hook-delete-policy = %q, want "BeforeHookCreation"`, got)
	}
	if got, want := schemaBootstrapInt(helmMap(job["spec"])["activeDeadlineSeconds"]), 3600; got != want {
		t.Fatalf("qa overlay activeDeadlineSeconds = %d, want %d", got, want)
	}
}

func TestHelmSchemaBootstrapRejectsDisabledJobWithPreSync(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(t,
		"--set", "schemaBootstrap.enabled=false",
		"--set", "schemaBootstrap.useHelmHooks=false",
		"--set", "schemaBootstrap.argocdPreSync.enabled=true",
	)
	if !strings.Contains(output, "requires schemaBootstrap.enabled=true") {
		t.Fatalf("helm template failure does not name the disabled-Job conflict:\n%s", output)
	}
}

func TestHelmSchemaBootstrapRejectsPreSyncWithBundledBackend(t *testing.T) {
	t.Parallel()

	output := renderHelmChartFailure(t,
		"--set", "schemaBootstrap.useHelmHooks=false",
		"--set", "schemaBootstrap.argocdPreSync.enabled=true",
		"--set", "nornicdb.enabled=true",
		"--set", "nornicdb.capabilities.relationshipMergePropertyIdentity=true",
		"--set", "env.ESHU_GRAPH_BACKEND=nornicdb",
	)
	if !strings.Contains(output, "before the ArgoCD PreSync hook") {
		t.Fatalf("helm template failure does not name the PreSync backend ordering:\n%s", output)
	}
}
