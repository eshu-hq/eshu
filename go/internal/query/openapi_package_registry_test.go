// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPISpecNamesHexPackageRegistryEcosystemScope(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/package-registry/packages")
	get := mustMapField(t, path, "get")
	parameters := get["parameters"].([]any)
	for _, parameter := range parameters {
		parameterMap := parameter.(map[string]any)
		if parameterMap["name"] != "ecosystem" {
			continue
		}
		description, _ := parameterMap["description"].(string)
		if !strings.Contains(description, "hex") {
			t.Fatalf("ecosystem description = %q, want Hex named among package-registry scopes", description)
		}
		return
	}
	t.Fatalf("package-registry packages parameters missing ecosystem: %#v", parameters)
}

func TestOpenAPISpecIncludesPackageRegistryCorrelations(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/package-registry/correlations")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listPackageRegistryCorrelations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := mustMapField(t, get, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	schema := mustMapField(t, content, "schema")
	properties := mustMapField(t, schema, "properties")
	correlations := mustMapField(t, properties, "correlations")
	items := mustMapField(t, correlations, "items")
	itemProperties := mustMapField(t, items, "properties")
	if got, want := mustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
}

func TestOpenAPISpecIncludesPackageRegistryDependencyChains(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/package-registry/dependency-chains")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listPackageRegistryDependencyChains"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := mustMapField(t, get, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	schema := mustMapField(t, content, "schema")
	properties := mustMapField(t, schema, "properties")
	chains := mustMapField(t, properties, "chains")
	items := mustMapField(t, chains, "items")
	itemProperties := mustMapField(t, items, "properties")
	publishers := mustMapField(t, itemProperties, "publishers")
	publisherItems := mustMapField(t, publishers, "items")
	publisherProperties := mustMapField(t, publisherItems, "properties")
	if got, want := mustMapField(t, publisherProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("publisher provenance_only type = %#v, want %#v", got, want)
	}
}

func TestOpenAPISpecIncludesPackageRegistryIdentityIssues(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/package-registry/packages")
	get := mustMapField(t, path, "get")
	responses := mustMapField(t, get, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	schema := mustMapField(t, content, "schema")
	properties := mustMapField(t, schema, "properties")
	required := schema["required"].([]any)
	if !openAPISliceContains(required, "identity_issues") {
		t.Fatalf("response required = %#v, want identity_issues", required)
	}
	identityIssues := mustMapField(t, properties, "identity_issues")
	items := mustMapField(t, identityIssues, "items")
	itemProperties := mustMapField(t, items, "properties")
	itemRequired := items["required"].([]any)
	if !openAPISliceContains(itemRequired, "missing_evidence") {
		t.Fatalf("identity_issues required = %#v, want missing_evidence", itemRequired)
	}
	for _, field := range []string{
		"reason",
		"missing_evidence",
		"ecosystem",
		"registry",
		"normalized_name",
		"source_specific_id",
		"version_count",
	} {
		if _, ok := itemProperties[field]; !ok {
			t.Fatalf("identity_issues schema missing %q: %#v", field, itemProperties)
		}
	}
}

func openAPISliceContains(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
