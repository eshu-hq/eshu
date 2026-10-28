// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"reflect"
	"strings"
	"testing"
)

// TestBoundedCollectionMetadata proves the metadata block carries every
// bound field verbatim.
func TestBoundedCollectionMetadata(t *testing.T) {
	t.Parallel()

	got := BoundedCollectionMetadata(10, 11, 7, 42, true, []string{"rank"})
	want := map[string]any{
		"limit":                         10,
		"query_sentinel_limit":          11,
		"returned_count":                7,
		"observed_count":                42,
		"observed_count_is_lower_bound": true,
		"truncated":                     true,
		"ordering":                      []string{"rank"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BoundedCollectionMetadata = %#v, want %#v", got, want)
	}
}

// TestBoundedTraceEnrichmentLimit proves non-positive depths take the
// default, ordinary depths scale by ten, and huge depths clamp without
// overflowing.
func TestBoundedTraceEnrichmentLimit(t *testing.T) {
	t.Parallel()

	if got := BoundedTraceEnrichmentLimit(0); got != DefaultIndirectEvidenceSearchLimit {
		t.Fatalf("zero depth = %d, want default %d", got, DefaultIndirectEvidenceSearchLimit)
	}
	if got := BoundedTraceEnrichmentLimit(-3); got != DefaultIndirectEvidenceSearchLimit {
		t.Fatalf("negative depth = %d, want default", got)
	}
	if got := BoundedTraceEnrichmentLimit(5); got != 50 {
		t.Fatalf("depth 5 = %d, want 50", got)
	}
	if got := BoundedTraceEnrichmentLimit(11); got != MaxIndirectEvidenceSearchLimit {
		t.Fatalf("depth 11 = %d, want ceiling %d", got, MaxIndirectEvidenceSearchLimit)
	}
	if got := BoundedTraceEnrichmentLimit(int(^uint(0) >> 1)); got != MaxIndirectEvidenceSearchLimit {
		t.Fatalf("max-int depth = %d, want ceiling without overflow", got)
	}
}

// TestCanonicalServiceName proves the workload context name wins over the
// requested name.
func TestCanonicalServiceName(t *testing.T) {
	t.Parallel()

	if got := CanonicalServiceName("requested", map[string]any{"name": "canonical"}); got != "canonical" {
		t.Fatalf("CanonicalServiceName = %q, want canonical", got)
	}
	if got := CanonicalServiceName("  requested  ", map[string]any{}); got != "requested" {
		t.Fatalf("CanonicalServiceName fallback = %q, want trimmed requested", got)
	}
}

// TestFirstNonEmpty proves the first non-blank value returns untrimmed.
func TestFirstNonEmpty(t *testing.T) {
	t.Parallel()

	if got := FirstNonEmpty("  ", "", "  kept  ", "other"); got != "  kept  " {
		t.Fatalf("FirstNonEmpty = %q, want the untrimmed kept value", got)
	}
	if got := FirstNonEmpty("", "   "); got != "" {
		t.Fatalf("FirstNonEmpty blanks = %q, want empty", got)
	}
}

// TestIsK8sResourceKind proves kind matching is case-insensitive and
// non-K8s entities never match.
func TestIsK8sResourceKind(t *testing.T) {
	t.Parallel()

	entity := EntityContent{EntityType: "K8sResource", Metadata: map[string]any{"kind": " Deployment "}}
	if !IsK8sResourceKind(entity, "deployment") {
		t.Fatal("deployment kind should match case-insensitively")
	}
	if IsK8sResourceKind(entity, "Service") {
		t.Fatal("Service kind must not match a Deployment")
	}
	if IsK8sResourceKind(EntityContent{EntityType: "Workload"}, "Deployment") {
		t.Fatal("non-K8s entity must never match")
	}
}

// TestPlatformTargets proves projected platforms win with the synthesized
// single target as fallback.
func TestPlatformTargets(t *testing.T) {
	t.Parallel()

	projected := []any{map[string]any{"platform_name": "ecs"}}
	instance := map[string]any{"platforms": projected, "platform_name": "k8s"}
	got := PlatformTargets(instance)
	if len(got) != 1 || got[0]["platform_name"] != "ecs" {
		t.Fatalf("PlatformTargets = %#v, want the projected list", got)
	}
	synth := PlatformTargets(map[string]any{
		"platform_name": "k8s", "platform_kind": "kubernetes",
		"platform_confidence": 0.9, "platform_reason": "probe",
	})
	if len(synth) != 1 || synth[0]["platform_kind"] != "kubernetes" {
		t.Fatalf("synthesized target = %#v, want the k8s target", synth)
	}
	if got := PlatformTargets(map[string]any{}); got != nil {
		t.Fatalf("empty PlatformTargets = %#v, want nil", got)
	}
}

// TestHostnameAndPlatformLabels proves label rendering skips blank names
// and annotates kinds.
func TestHostnameAndPlatformLabels(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"hostname": "b.test"}, {"hostname": ""}, {"hostname": "a.test"}, {"other": 1}}
	if got := HostnameLabels(rows); !reflect.DeepEqual(got, []string{"a.test", "b.test"}) {
		t.Fatalf("HostnameLabels = %#v, want sorted distinct", got)
	}
	if got := HostnameLabels(nil); got != nil {
		t.Fatalf("nil HostnameLabels = %#v, want nil", got)
	}
	platforms := []map[string]any{
		{"platform_name": "ecs", "platform_kind": "aws"},
		{"platform_name": "bare"},
		{"platform_name": ""},
	}
	if got := PlatformTargetLabels(platforms); !reflect.DeepEqual(got, []string{"ecs (aws)", "bare"}) {
		t.Fatalf("PlatformTargetLabels = %#v, want kind-annotated labels", got)
	}
}

// TestBuildWorkloadStory proves the narrative covers the no-instance,
// multi-instance, and API-surface branches.
func TestBuildWorkloadStory(t *testing.T) {
	t.Parallel()

	if got := BuildWorkloadStory(nil); got != "" {
		t.Fatalf("nil story = %q, want empty", got)
	}
	bare := BuildWorkloadStory(map[string]any{"name": "api", "kind": "Deployment"})
	if !strings.Contains(bare, "Workload api (kind: Deployment)") || !strings.Contains(bare, "no linked repository") {
		t.Fatalf("bare story = %q, want identity plus no-repo", bare)
	}
	if !strings.Contains(bare, "No materialized workload instances found.") {
		t.Fatalf("bare story = %q, want the no-instance sentence", bare)
	}
	full := BuildWorkloadStory(map[string]any{
		"name":      "api",
		"repo_name": "shop",
		"instances": []map[string]any{
			{"environment": "prod", "platforms": []any{map[string]any{"platform_name": "ecs", "platform_kind": "aws"}}},
			{"environment": "stage"},
		},
		"observed_config_environments": []string{"prod"},
		"hostnames":                    []any{map[string]any{"hostname": "api.test"}},
		"api_surface": map[string]any{
			"endpoint_count": 3, "spec_count": 1, "docs_routes": []string{"/docs"},
		},
		"consumer_repositories":      []any{map[string]any{"repo": "x"}},
		"provisioning_source_chains": []any{map[string]any{"repo": "y"}},
	})
	for _, fragment := range []string{
		"is defined in repository shop", "2 instances", "prod on ecs (aws)",
		"Public entrypoints: api.test", "exposes 3 endpoint(s) across 1 spec file(s)",
		"Docs routes: /docs", "consumer repos", "Provisioning chains span 1 repo(s)",
	} {
		if !strings.Contains(full, fragment) {
			t.Fatalf("full story = %q, missing %q", full, fragment)
		}
	}
}

// TestCompactStringMap proves empty strings and string slices drop in
// place while other values survive.
func TestCompactStringMap(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"keep": "x", "drop": "", "drops": []string{}, "keeps": []string{"a"}, "num": 3,
	}
	got := CompactStringMap(value)
	want := map[string]any{"keep": "x", "keeps": []string{"a"}, "num": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CompactStringMap = %#v, want %#v", got, want)
	}
}

// TestStringSliceHelpers proves the slice extraction family over string,
// any, and missing shapes.
func TestStringSliceHelpers(t *testing.T) {
	t.Parallel()

	if got := FilterEmptyStrings([]string{" a ", "", "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("FilterEmptyStrings = %#v, want [a b]", got)
	}
	if got := FilterEmptyStrings(nil); got != nil {
		t.Fatalf("nil FilterEmptyStrings = %#v, want nil", got)
	}
	if got := StringSliceFromAny([]any{"a", 3, "  "}); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("StringSliceFromAny = %#v, want [a]", got)
	}
	if got := StringSliceFromAny(42); got != nil {
		t.Fatalf("scalar StringSliceFromAny = %#v, want nil", got)
	}
	if got := NonEmptyStrings([]string{"a", "", "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("NonEmptyStrings = %#v, want [a b]", got)
	}
	value := map[string]any{"s": []string{"a", ""}, "a": []any{"b", "", 4}, "n": 1}
	if got := StringSliceMapValue(value, "s"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("string branch = %#v, want [a]", got)
	}
	if got := StringSliceMapValue(value, "a"); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("any branch = %#v, want [b]", got)
	}
	if got := StringSliceMapValue(value, "n"); got != nil {
		t.Fatalf("scalar branch = %#v, want nil", got)
	}
	if got := StringSliceMapValue(map[string]any{}, "s"); got != nil {
		t.Fatalf("empty map branch = %#v, want nil", got)
	}
}

// TestCloneAnyMap proves copies are detached but equal, never nil.
func TestCloneAnyMap(t *testing.T) {
	t.Parallel()

	src := map[string]any{"a": 1}
	clone := CloneAnyMap(src)
	clone["b"] = 2
	if !reflect.DeepEqual(src, map[string]any{"a": 1}) {
		t.Fatalf("clone mutated the source: %#v", src)
	}
	if got := CloneAnyMap(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil clone = %#v, want empty non-nil", got)
	}
}

// TestDistinctSortedInstanceField proves platform keys expand through
// targets while plain keys read directly.
func TestDistinctSortedInstanceField(t *testing.T) {
	t.Parallel()

	instances := []map[string]any{
		{"environment": "prod", "platforms": []any{map[string]any{"platform_name": "ecs"}}},
		{"environment": "stage", "platform_name": "k8s"},
		{"environment": "prod"},
	}
	if got := DistinctSortedInstanceField(instances, "environment"); !reflect.DeepEqual(got, []string{"prod", "stage"}) {
		t.Fatalf("environments = %#v, want [prod stage]", got)
	}
	if got := DistinctSortedInstanceField(instances, "platform_name"); !reflect.DeepEqual(got, []string{"ecs", "k8s"}) {
		t.Fatalf("platform names = %#v, want [ecs k8s]", got)
	}
}

// TestQuerycontractMetadataStringSlice proves slice, any, comma-string,
// and foreign shapes.
func TestQuerycontractMetadataStringSlice(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{
		"s": []string{" a ", ""},
		"a": []any{"b", 4, "  "},
		"c": "x, y,,",
		"n": 7,
	}
	if got := MetadataStringSlice(metadata, "s"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("string branch = %#v, want [a]", got)
	}
	if got := MetadataStringSlice(metadata, "a"); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("any branch = %#v, want [b]", got)
	}
	if got := MetadataStringSlice(metadata, "c"); !reflect.DeepEqual(got, []string{"x", "y"}) {
		t.Fatalf("comma branch = %#v, want [x y]", got)
	}
	if got := MetadataStringSlice(metadata, "n"); got != nil {
		t.Fatalf("scalar branch = %#v, want nil", got)
	}
	if got := MetadataStringSlice(metadata, "missing"); got != nil {
		t.Fatalf("missing branch = %#v, want nil", got)
	}
}

// TestCicdEvidenceStorySummary proves unknown defaults fill every blank.
func TestCicdEvidenceStorySummary(t *testing.T) {
	t.Parallel()

	summary := map[string]any{
		"static_workflow_artifacts": map[string]any{"state": "present"},
		"run_artifact_evidence":     map[string]any{"state": "absent", "reason": "no_runs"},
	}
	got := CicdEvidenceStorySummary(summary)
	want := "CI/CD evidence has static_workflow=present, provider_runs=unknown, run_artifact=absent (no_runs)."
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if got := CicdEvidenceStorySummary(map[string]any{}); !strings.Contains(got, "no_reason") {
		t.Fatalf("empty summary = %q, want the no_reason fallback", got)
	}
}

// TestServiceDeploymentToolFamilies proves explicit tool families win
// over artifact families.
func TestServiceDeploymentToolFamilies(t *testing.T) {
	t.Parallel()

	evidence := map[string]any{
		"tool_families":     []string{"argo"},
		"artifact_families": []string{"helm"},
	}
	if got := ServiceDeploymentToolFamilies(evidence); !reflect.DeepEqual(got, []string{"argo"}) {
		t.Fatalf("tool families = %#v, want [argo]", got)
	}
	artifacts := map[string]any{"artifact_families": []string{"helm"}}
	if got := ServiceDeploymentToolFamilies(artifacts); !reflect.DeepEqual(got, []string{"helm"}) {
		t.Fatalf("artifact fallback = %#v, want [helm]", got)
	}
}
