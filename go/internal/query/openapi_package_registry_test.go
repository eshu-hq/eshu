// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecNamesHexPackageRegistryEcosystemScope(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/package-registry/packages")
	get := querytestutil.MustMapField(t, path, "get")
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

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/package-registry/correlations")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listPackageRegistryCorrelations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := querytestutil.MustMapField(t, get, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := querytestutil.MustMapField(t, content, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	correlations := querytestutil.MustMapField(t, properties, "correlations")
	items := querytestutil.MustMapField(t, correlations, "items")
	itemProperties := querytestutil.MustMapField(t, items, "properties")
	if got, want := querytestutil.MustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
}

func TestOpenAPISpecIncludesPackageRegistryDependencyChains(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/package-registry/dependency-chains")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listPackageRegistryDependencyChains"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := querytestutil.MustMapField(t, get, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := querytestutil.MustMapField(t, content, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	chains := querytestutil.MustMapField(t, properties, "chains")
	items := querytestutil.MustMapField(t, chains, "items")
	itemProperties := querytestutil.MustMapField(t, items, "properties")
	publishers := querytestutil.MustMapField(t, itemProperties, "publishers")
	publisherItems := querytestutil.MustMapField(t, publishers, "items")
	publisherProperties := querytestutil.MustMapField(t, publisherItems, "properties")
	if got, want := querytestutil.MustMapField(t, publisherProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("publisher provenance_only type = %#v, want %#v", got, want)
	}
	if got, want := querytestutil.MustMapField(t, properties, "publishers_truncated")["type"], "boolean"; got != want {
		t.Fatalf("publishers_truncated type = %#v, want %#v", got, want)
	}
	required := schema["required"].([]any)
	if !openAPISliceContains(required, "publishers_truncated") {
		t.Fatalf("response required = %#v, want publishers_truncated", required)
	}
}

func TestOpenAPISpecIncludesPackageRegistryIdentityIssues(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/package-registry/packages")
	get := querytestutil.MustMapField(t, path, "get")
	responses := querytestutil.MustMapField(t, get, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := querytestutil.MustMapField(t, content, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	required := schema["required"].([]any)
	if !openAPISliceContains(required, "identity_issues") {
		t.Fatalf("response required = %#v, want identity_issues", required)
	}
	identityIssues := querytestutil.MustMapField(t, properties, "identity_issues")
	items := querytestutil.MustMapField(t, identityIssues, "items")
	itemProperties := querytestutil.MustMapField(t, items, "properties")
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
