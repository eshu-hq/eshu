// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPISpecIncludesSBOMAttestationAttachments(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/sbom-attestations/attachments")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listSBOMAttestationAttachments"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	var repositoryParam map[string]any
	for _, parameter := range parameters {
		parameterMap, ok := parameter.(map[string]any)
		if !ok {
			t.Fatalf("parameter = %T, want map[string]any", parameter)
		}
		if parameterMap["name"] == "repository_id" {
			repositoryParam = parameterMap
			break
		}
	}
	if repositoryParam == nil {
		t.Fatal("parameters missing repository_id")
	}
	description, _ := repositoryParam["description"].(string)
	for _, want := range []string{"human repository selector", "Unknown or ambiguous selectors"} {
		if !strings.Contains(description, want) {
			t.Fatalf("repository_id description = %q, want %q", description, want)
		}
	}
	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	attachments := mustMapField(t, properties, "attachments")
	items := mustMapField(t, attachments, "items")
	itemProperties := mustMapField(t, items, "properties")
	for _, want := range []string{
		"attachment_scope",
		"missing_evidence",
		"canonical_writes",
		"component_evidence",
		"component_evidence_truncated",
		"warning_summaries",
		"warning_summary_count",
		"warning_summaries_truncated",
	} {
		if _, ok := itemProperties[want]; !ok {
			t.Fatalf("attachment schema missing %q", want)
		}
	}
}

func TestOpenAPISpecIncludesAdvisoryEvidenceRepositoryScope(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/advisories/evidence")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listAdvisoryEvidence"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	parameterDescriptions := map[string]string{}
	for _, parameter := range parameters {
		parameterMap, ok := parameter.(map[string]any)
		if !ok {
			t.Fatalf("parameter = %T, want map[string]any", parameter)
		}
		name, _ := parameterMap["name"].(string)
		description, _ := parameterMap["description"].(string)
		parameterDescriptions[name] = description
	}
	for _, want := range []string{"repository_id", "service_id", "workload_id"} {
		if _, ok := parameterDescriptions[want]; !ok {
			t.Fatalf("parameters missing %q", want)
		}
	}
	if got := parameterDescriptions["repository_id"]; !strings.Contains(got, "selector") {
		t.Fatalf("repository_id description = %q, want selector semantics", got)
	}

	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	if _, ok := properties["scope"]; !ok {
		t.Fatal("advisory evidence response schema missing scope")
	}
	required := mustStringSliceField(t, schema, "required")
	if !containsOpenAPIEnumString(required, "scope") {
		t.Fatalf("required = %#v, want scope", required)
	}
}

func TestOpenAPISpecIncludesContainerImageSourceRepositoryBridge(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/container-images/identities")
	get := mustMapField(t, path, "get")
	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	parameterNames := map[string]map[string]any{}
	for _, parameter := range parameters {
		parameterMap, ok := parameter.(map[string]any)
		if !ok {
			t.Fatalf("parameter = %T, want map[string]any", parameter)
		}
		name, _ := parameterMap["name"].(string)
		parameterNames[name] = parameterMap
	}
	if _, ok := parameterNames["source_repository_id"]; !ok {
		t.Fatal("parameters missing source_repository_id")
	}
	repositoryDescription, _ := parameterNames["repository_id"]["description"].(string)
	if !strings.Contains(repositoryDescription, "not a source repository selector") {
		t.Fatalf("repository_id description = %q, want OCI-only warning", repositoryDescription)
	}

	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	if _, ok := properties["source_bridge"]; !ok {
		t.Fatal("container identity response missing source_bridge")
	}
	identities := mustMapField(t, properties, "identities")
	items := mustMapField(t, identities, "items")
	itemProperties := mustMapField(t, items, "properties")
	if _, ok := itemProperties["source_repository_ids"]; !ok {
		t.Fatal("container identity item missing source_repository_ids")
	}
	if _, ok := itemProperties["source_revision"]; !ok {
		t.Fatal("container identity item missing source_revision")
	}
}

func TestOpenAPISpecIncludesSupplyChainImpactFindings(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listSupplyChainImpactFindings"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	parameterNames := map[string]bool{}
	for _, parameter := range parameters {
		parameterMap, ok := parameter.(map[string]any)
		if !ok {
			t.Fatalf("parameter = %T, want map[string]any", parameter)
		}
		name, _ := parameterMap["name"].(string)
		parameterNames[name] = true
	}
	for _, want := range []string{"priority_bucket", "min_priority_score", "sort"} {
		if !parameterNames[want] {
			t.Fatalf("parameters missing %q", want)
		}
	}
	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	findings := mustMapField(t, properties, "findings")
	items := mustMapField(t, findings, "items")
	itemProperties := mustMapField(t, items, "properties")
	for _, want := range []string{"priority_score", "priority_bucket", "priority_reason_codes", "priority_contributions", "vulnerable_range"} {
		if _, ok := itemProperties[want]; !ok {
			t.Fatalf("finding schema missing %q", want)
		}
	}
	readiness, ok := properties["readiness"].(map[string]any)
	if !ok {
		t.Fatalf("properties[readiness] = %T, want map describing readiness envelope", properties["readiness"])
	}
	readinessProps := mustMapField(t, readiness, "properties")
	for _, key := range []string{
		"readiness_state",
		"target_scope",
		"evidence_sources",
		"source_snapshots",
		"source_states",
		"unsupported_targets",
		"missing_evidence",
		"incomplete_reasons",
		"freshness",
		"counts",
	} {
		if _, ok := readinessProps[key]; !ok {
			t.Fatalf("readiness.properties missing %q field", key)
		}
	}
	readinessState := mustMapField(t, readinessProps, "readiness_state")
	stateEnum := mustStringSliceField(t, readinessState, "enum")
	for _, want := range []string{"ambiguous_scope", "unsupported"} {
		if !containsOpenAPIEnumString(stateEnum, want) {
			t.Fatalf("readiness_state enum = %#v, want %q surfaced", stateEnum, want)
		}
	}
	unsupportedTargets := mustMapField(t, readinessProps, "unsupported_targets")
	unsupportedTargetsItems := mustMapField(t, unsupportedTargets, "items")
	unsupportedTargetsItemProps := mustMapField(t, unsupportedTargetsItems, "properties")
	for _, key := range []string{"target_kind", "reason", "count"} {
		if _, ok := unsupportedTargetsItemProps[key]; !ok {
			t.Fatalf("unsupported_targets items.properties missing %q", key)
		}
	}
	unsupportedTargetsRequired := mustStringSliceField(t, unsupportedTargetsItems, "required")
	for _, key := range []string{"target_kind", "reason", "count"} {
		if !containsOpenAPIEnumString(unsupportedTargetsRequired, key) {
			t.Fatalf("unsupported_targets items.required = %#v, want %q (envelope normalization drops blank-reason rows)", unsupportedTargetsRequired, key)
		}
	}
	targetKindSchema := mustMapField(t, unsupportedTargetsItemProps, "target_kind")
	targetKindEnum := mustStringSliceField(t, targetKindSchema, "enum")
	for _, want := range []string{"ecosystem", "package_manager_file", "dependency_source", "sbom_target", "package_registry_metadata", "image_target"} {
		if !containsOpenAPIEnumString(targetKindEnum, want) {
			t.Fatalf("unsupported_targets.target_kind enum = %#v, want %q", targetKindEnum, want)
		}
	}
	missingEvidence := mustMapField(t, readinessProps, "missing_evidence")
	missingEvidenceItems := mustMapField(t, missingEvidence, "items")
	missingEvidenceEnum := mustStringSliceField(t, missingEvidenceItems, "enum")
	for _, want := range []string{"ambiguous_scope", "unsupported_targets"} {
		if !containsOpenAPIEnumString(missingEvidenceEnum, want) {
			t.Fatalf("missing_evidence enum = %#v, want %q stable identifier", missingEvidenceEnum, want)
		}
	}
	freshness := mustMapField(t, readinessProps, "freshness")
	enum := mustStringSliceField(t, freshness, "enum")
	for _, want := range []string{"fresh", "stale", "unknown", "pending", "rate_limited", "failed", "partial"} {
		if !containsOpenAPIEnumString(enum, want) {
			t.Fatalf("readiness.freshness enum = %#v, want %q", enum, want)
		}
	}
}

func TestOpenAPISpecIncludesSupplyChainImpactAggregateProfileFilters(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	for _, pathName := range []string{
		"/api/v0/supply-chain/impact/findings/count",
		"/api/v0/supply-chain/impact/inventory",
	} {
		path := mustMapField(t, paths, pathName)
		get := mustMapField(t, path, "get")
		parameters, ok := get["parameters"].([]any)
		if !ok {
			t.Fatalf("%s parameters = %T, want []any", pathName, get["parameters"])
		}
		parameterNames := map[string]bool{}
		for _, parameter := range parameters {
			parameterMap, ok := parameter.(map[string]any)
			if !ok {
				t.Fatalf("%s parameter = %T, want map[string]any", pathName, parameter)
			}
			name, _ := parameterMap["name"].(string)
			parameterNames[name] = true
		}
		for _, want := range []string{
			"profile",
			"priority_bucket",
			"min_priority_score",
			"suppression_state",
			"include_suppressed",
		} {
			if !parameterNames[want] {
				t.Fatalf("%s parameters missing %q", pathName, want)
			}
		}

		responses := mustMapField(t, get, "responses")
		twoHundred := mustMapField(t, responses, "200")
		content := mustMapField(t, twoHundred, "content")
		appJSON := mustMapField(t, content, "application/json")
		schema := mustMapField(t, appJSON, "schema")
		properties := mustMapField(t, schema, "properties")
		if _, ok := properties["detection_profile"]; !ok {
			t.Fatalf("%s 200 schema missing detection_profile", pathName)
		}
	}
}

func TestOpenAPISpecIncludesSupplyChainImpactRemediation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	findingsPath := mustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	findingsGet := mustMapField(t, findingsPath, "get")
	findingsResponses := mustMapField(t, findingsGet, "responses")
	findingsTwoHundred := mustMapField(t, findingsResponses, "200")
	findingsContent := mustMapField(t, findingsTwoHundred, "content")
	findingsAppJSON := mustMapField(t, findingsContent, "application/json")
	findingsSchema := mustMapField(t, findingsAppJSON, "schema")
	findingsProps := mustMapField(t, findingsSchema, "properties")
	findings := mustMapField(t, findingsProps, "findings")
	findingsItems := mustMapField(t, findings, "items")
	findingsItemProps := mustMapField(t, findingsItems, "properties")
	remediation := mustMapField(t, findingsItemProps, "remediation")
	remediationProps := mustMapField(t, remediation, "properties")
	for _, key := range []string{
		"ecosystem",
		"current_version",
		"vulnerable_range",
		"fixed_version_source",
		"match_reason",
		"first_patched_version",
		"patched_version_branches",
		"manifest_range",
		"manifest_allows_fix",
		"direct",
		"parent_package",
		"confidence",
		"reason",
		"missing_evidence",
	} {
		if _, ok := remediationProps[key]; !ok {
			t.Fatalf("findings remediation.properties missing %q", key)
		}
	}
	reasonEnum := mustStringSliceField(t, mustMapField(t, remediationProps, "reason"), "enum")
	for _, want := range []string{
		"direct_upgrade_allowed",
		"direct_range_blocked",
		"transitive_parent_upgrade_required",
		"already_fixed",
		"no_patched_version",
		"multiple_patched_branches",
		"package_manager_unsupported",
	} {
		if !containsOpenAPIEnumString(reasonEnum, want) {
			t.Fatalf("findings remediation.reason enum = %#v, want %q", reasonEnum, want)
		}
	}

	explainPath := mustMapField(t, paths, "/api/v0/supply-chain/impact/explain")
	explainGet := mustMapField(t, explainPath, "get")
	explainResponses := mustMapField(t, explainGet, "responses")
	explainTwoHundred := mustMapField(t, explainResponses, "200")
	explainContent := mustMapField(t, explainTwoHundred, "content")
	explainAppJSON := mustMapField(t, explainContent, "application/json")
	explainSchema := mustMapField(t, explainAppJSON, "schema")
	explainProps := mustMapField(t, explainSchema, "properties")
	explainRemediation := mustMapField(t, explainProps, "remediation")
	explainRemediationProps := mustMapField(t, explainRemediation, "properties")
	for _, key := range []string{
		"confidence",
		"reason",
		"manifest_allows_fix",
		"first_patched_version",
		"fixed_version_source",
		"match_reason",
	} {
		if _, ok := explainRemediationProps[key]; !ok {
			t.Fatalf("explain remediation.properties missing %q", key)
		}
	}
}

func TestOpenAPISpecIncludesSupplyChainImpactExplain(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/impact/explain")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "explainSupplyChainImpact"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	var sawFindingID bool
	for _, parameter := range parameters {
		param, ok := parameter.(map[string]any)
		if !ok {
			t.Fatalf("parameter = %T, want map[string]any", parameter)
		}
		if param["name"] == "finding_id" {
			sawFindingID = true
		}
	}
	if !sawFindingID {
		t.Fatal("parameters missing finding_id")
	}
}

func mustStringSliceField(t *testing.T, m map[string]any, key string) []string {
	t.Helper()
	values, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s = %T, want []any", key, m[key])
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("%s element = %T, want string", key, value)
		}
		out = append(out, text)
	}
	return out
}

func containsOpenAPIEnumString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
