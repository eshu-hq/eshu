// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesServiceCatalogCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/service-catalog/correlations")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listServiceCatalogCorrelations"; got != want {
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
	candidates := querytestutil.MustMapField(t, itemProperties, "candidate_repository_ids")
	if got, want := candidates["type"], "array"; got != want {
		t.Fatalf("candidate_repository_ids type = %#v, want %#v", got, want)
	}
	requiredAnchors := querytestutil.MustMapField(t, itemProperties, "required_anchor_keys")
	if got, want := requiredAnchors["type"], "array"; got != want {
		t.Fatalf("required_anchor_keys type = %#v, want %#v", got, want)
	}
	missing := querytestutil.MustMapField(t, properties, "missing_evidence")
	if got, want := missing["type"], "array"; got != want {
		t.Fatalf("missing_evidence type = %#v, want %#v", got, want)
	}
	evidenceSummary := querytestutil.MustMapField(t, properties, "evidence_summary")
	evidenceProperties := querytestutil.MustMapField(t, evidenceSummary, "properties")
	localDescriptors := querytestutil.MustMapField(t, evidenceProperties, "local_descriptors")
	localProperties := querytestutil.MustMapField(t, localDescriptors, "properties")
	if got, want := querytestutil.MustMapField(t, localProperties, "source_uris")["type"], "array"; got != want {
		t.Fatalf("local_descriptors.source_uris type = %#v, want %#v", got, want)
	}
	external := querytestutil.MustMapField(t, evidenceProperties, "external_catalog_confirmation")
	externalProperties := querytestutil.MustMapField(t, external, "properties")
	if got, want := querytestutil.MustMapField(t, externalProperties, "reason")["type"], "string"; got != want {
		t.Fatalf("external_catalog_confirmation.reason type = %#v, want %#v", got, want)
	}
}
