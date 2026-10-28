// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestCov7LooseSpecAccessors proves the zero-value contract for foreign
// shapes: slices, maps, and strings pass through while anything else yields
// the zero value instead of panicking.
func TestCov7LooseSpecAccessors(t *testing.T) {
	t.Parallel()

	if got := SliceValue([]any{"a"}); len(got) != 1 {
		t.Fatalf("SliceValue = %#v, want passthrough", got)
	}
	if got := SliceValue("nope"); got != nil {
		t.Fatalf("SliceValue(string) = %#v, want nil", got)
	}
	if got := MapValue(map[string]any{"a": 1}); len(got) != 1 {
		t.Fatalf("MapValue = %#v, want passthrough", got)
	}
	if got := MapValue([]any{}); got != nil {
		t.Fatalf("MapValue(slice) = %#v, want nil", got)
	}
	if got := StringValue("ok"); got != "ok" {
		t.Fatalf("StringValue = %q, want ok", got)
	}
	if got := StringValue(42); got != "" {
		t.Fatalf("StringValue(int) = %q, want empty", got)
	}
}

// TestCov7OpenAPIRefFilePath proves fragment stripping, directory-relative
// resolution, and the fragment-only empty contract.
func TestCov7OpenAPIRefFilePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		base string
		ref  string
		want string
	}{
		{"empty ref", "docs/openapi.yaml", "", ""},
		{"fragment only", "docs/openapi.yaml", "#/paths/~1x", ""},
		{"fragment stripped", "docs/openapi.yaml", "./paths/index.yaml#/paths", "docs/paths/index.yaml"},
		{"relative join", "docs/openapi.yaml", "../shared/common.yaml", "shared/common.yaml"},
		{"nested clean", "a/b/spec.yaml", "./c/../d.yaml", "a/b/d.yaml"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := OpenAPIRefFilePath(tc.base, tc.ref); got != tc.want {
				t.Fatalf("OpenAPIRefFilePath(%q, %q) = %q, want %q", tc.base, tc.ref, got, tc.want)
			}
		})
	}
}

// TestCov7LooksLikeDocsRoute proves the docs/swagger/openapi smell and the
// exact spec/specs path-segment rule.
func TestCov7LooksLikeDocsRoute(t *testing.T) {
	t.Parallel()

	for _, route := range []string{"/api/docs", "/swagger/index.html", "/openapi.json", "/api/spec/list", "/v1/specs/open"} {
		if !LooksLikeDocsRoute(route) {
			t.Fatalf("LooksLikeDocsRoute(%q) = false, want true", route)
		}
	}
	for _, route := range []string{"/api/users", "/v1/specification", "/api/special-offers", "/healthz"} {
		if LooksLikeDocsRoute(route) {
			t.Fatalf("LooksLikeDocsRoute(%q) = true, want false", route)
		}
	}
}

// TestCov7ExtractDocsRoutes proves sorted dedup of quoted docs-like routes
// and the nil contract when nothing matches.
func TestCov7ExtractDocsRoutes(t *testing.T) {
	t.Parallel()

	if got := ExtractDocsRoutes("no quoted routes here"); got != nil {
		t.Fatalf("ExtractDocsRoutes(plain) = %#v, want nil", got)
	}
	content := `see "/api/docs" and '/swagger/ui' plus "/api/docs" again and "/api/users"`
	got := ExtractDocsRoutes(content)
	if len(got) != 2 || got[0] != "/api/docs" || got[1] != "/swagger/ui" {
		t.Fatalf("ExtractDocsRoutes = %#v, want sorted deduped docs routes", got)
	}
}

// TestCov7ExtractAPISpecEvidenceFormats proves non-spec paths decline,
// malformed YAML stays tolerated-but-unparsed, and a full OpenAPI document
// summarizes endpoints, methods, operation IDs, hostnames, and docs routes.
func TestCov7ExtractAPISpecEvidenceFormats(t *testing.T) {
	t.Parallel()

	spec, ok, err := ExtractAPISpecEvidence(querycontract.FileContent{
		RelativePath: "docs/readme.md",
		Content:      "openapi: 3.0.0",
	}, nil)
	if err != nil || ok {
		t.Fatalf("non-spec path = (%#v, %v, %v), want declined", spec, ok, err)
	}

	spec, ok, err = ExtractAPISpecEvidence(querycontract.FileContent{
		RelativePath: "docs/openapi.yaml",
		Content:      "openapi: [unclosed",
	}, nil)
	if err != nil || !ok || spec.Parsed || spec.Format != "yaml" {
		t.Fatalf("malformed yaml = (%#v, %v, %v), want tolerated unparsed yaml", spec, ok, err)
	}

	doc := "openapi: \"3.0.0\"\n" +
		"info:\n  version: \"2.1.0\"\n" +
		"servers:\n" +
		"  - url: \"https://api.example.com/v1\"\n" +
		"  - url: \"https://api.example.com/v1\"\n" +
		"  - url: \"not a url at all %%\"\n" +
		"  - {}\n" +
		"paths:\n" +
		"  /api/docs:\n    get:\n      operationId: listDocs\n" +
		"    parameters:\n      - name: q\n" +
		"  /api/users:\n    POST:\n      operationId: createUser\n" +
		"    delete: {}\n"
	spec, ok, err = ExtractAPISpecEvidence(querycontract.FileContent{
		RelativePath: "docs/openapi.yaml",
		Content:      doc,
	}, nil)
	if err != nil || !ok || !spec.Parsed {
		t.Fatalf("openapi doc = (%#v, %v, %v), want parsed", spec, ok, err)
	}
	if spec.SpecVersion != "3.0.0" || spec.APIVersion != "2.1.0" {
		t.Fatalf("spec versions = %#v, want 3.0.0 / 2.1.0", spec)
	}
	if spec.EndpointCount != 2 || spec.MethodCount != 3 || spec.OperationIDCount != 2 {
		t.Fatalf("spec counts = %#v, want 2 endpoints / 3 methods / 2 operation ids", spec)
	}
	if len(spec.DocsRoutes) != 1 || spec.DocsRoutes[0] != "/api/docs" {
		t.Fatalf("docs routes = %#v, want [/api/docs]", spec.DocsRoutes)
	}
	if len(spec.Hostnames) != 1 || spec.Hostnames[0] != "api.example.com" {
		t.Fatalf("hostnames = %#v, want [api.example.com]", spec.Hostnames)
	}
	if len(spec.Endpoints) != 2 || spec.Endpoints[0].Path != "/api/docs" || spec.Endpoints[1].Path != "/api/users" {
		t.Fatalf("endpoints = %#v, want stable path order", spec.Endpoints)
	}

	// A paths-only document without a version still counts as a spec.
	pathsOnly, ok := ExtractAPISpecEvidenceWithoutRefs(querycontract.FileContent{
		RelativePath: "spec/paths.yaml",
		Content:      "paths:\n  /x:\n    get: {}\n",
	})
	if !ok || !pathsOnly.Parsed || pathsOnly.Format != "yaml" {
		t.Fatalf("paths-only = (%#v, %v), want parsed", pathsOnly, ok)
	}

	// Neither version nor paths means no spec, reported tolerated.
	empty, ok := ExtractAPISpecEvidenceWithoutRefs(querycontract.FileContent{
		RelativePath: "spec/notes.yaml",
		Content:      "info:\n  version: \"1\"\n",
	})
	if !ok || empty.Parsed {
		t.Fatalf("versionless = (%#v, %v), want tolerated unparsed", empty, ok)
	}

	// Swagger 2.0 documents read through the same seam.
	swagger, ok := ExtractAPISpecEvidenceWithoutRefs(querycontract.FileContent{
		RelativePath: "swagger.json",
		Content:      "{\"swagger\": \"2.0\", \"paths\": {\"/a\": {\"get\": {}}}}",
	})
	if !ok || !swagger.Parsed || swagger.SpecVersion != "2.0" || swagger.Format != "json" {
		t.Fatalf("swagger = (%#v, %v, %v), want parsed json 2.0", swagger, ok, err)
	}
}

// TestCov7ExtractAPISpecEvidenceRefs proves whole-paths and per-item $ref
// resolution, resolver failures, and the tolerated malformed-reference read.
func TestCov7ExtractAPISpecEvidenceRefs(t *testing.T) {
	t.Parallel()

	resolver := func(files map[string]string, err error) querycontract.SpecFileResolver {
		return func(_, ref string) (string, error) {
			if err != nil {
				return "", err
			}
			return files[ref], nil
		}
	}
	WHOLE := "/whole:\n  get:\n    operationId: wholeOp\n"
	ITEM := "get:\n  operationId: itemOp\n"

	spec, ok, err := ExtractAPISpecEvidence(querycontract.FileContent{
		RelativePath: "docs/openapi.yaml",
		Content:      "openapi: \"3.0.0\"\npaths:\n  $ref: \"./paths.yaml\"\n",
	}, resolver(map[string]string{"./paths.yaml": WHOLE}, nil))
	if err != nil || !ok || !spec.Parsed || spec.MethodCount != 1 {
		t.Fatalf("whole-paths ref = (%#v, %v, %v), want resolved", spec, ok, err)
	}

	spec, ok, err = ExtractAPISpecEvidence(querycontract.FileContent{
		RelativePath: "docs/openapi.yaml",
		Content:      "openapi: \"3.0.0\"\npaths:\n  /item:\n    $ref: \"./item.yaml\"\n",
	}, resolver(map[string]string{"./item.yaml": ITEM}, nil))
	if err != nil || !ok || !spec.Parsed || spec.OperationIDCount != 1 {
		t.Fatalf("per-item ref = (%#v, %v, %v), want resolved", spec, ok, err)
	}

	wantErr := errors.New("reader down")
	if _, _, err := ExtractAPISpecEvidence(querycontract.FileContent{
		RelativePath: "docs/openapi.yaml",
		Content:      "openapi: \"3.0.0\"\npaths:\n  $ref: \"./paths.yaml\"\n",
	}, resolver(nil, wantErr)); !errors.Is(err, wantErr) {
		t.Fatalf("resolver error = %v, want %v", err, wantErr)
	}

	// A malformed referenced file is tolerated, not fatal.
	spec, ok, err = ExtractAPISpecEvidence(querycontract.FileContent{
		RelativePath: "docs/openapi.yaml",
		Content:      "openapi: \"3.0.0\"\npaths:\n  /item:\n    $ref: \"./bad.yaml\"\n",
	}, resolver(map[string]string{"./bad.yaml": "paths: [unclosed"}, nil))
	if err != nil || !ok || !spec.Parsed {
		t.Fatalf("malformed ref = (%#v, %v, %v), want tolerated", spec, ok, err)
	}

	// An empty referenced read leaves the original paths in place.
	spec, ok, err = ExtractAPISpecEvidence(querycontract.FileContent{
		RelativePath: "docs/openapi.yaml",
		Content:      "openapi: \"3.0.0\"\npaths:\n  /kept:\n    get: {}\n  /item:\n    $ref: \"./missing.yaml\"\n",
	}, resolver(map[string]string{}, nil))
	if err != nil || !ok || !spec.Parsed || spec.EndpointCount != 2 {
		t.Fatalf("empty ref = (%#v, %v, %v), want original paths kept", spec, ok, err)
	}

	// Non-map path items and blank refs are skipped without reads.
	reads := 0
	counting := querycontract.SpecFileResolver(func(_, _ string) (string, error) {
		reads++
		return "", nil
	})
	if _, _, err := ExtractAPISpecEvidence(querycontract.FileContent{
		RelativePath: "docs/openapi.yaml",
		Content:      "openapi: \"3.0.0\"\npaths:\n  /skip: 42\n  /blank:\n    $ref: \"\"\n",
	}, counting); err != nil || reads != 0 {
		t.Fatalf("skipped items = (err %v, reads %d), want no reads", err, reads)
	}
}

// TestCov7ServiceEvidenceFormatAndHosts proves extension mapping and
// hostname lowercasing with the invalid-URL empty contract.
func TestCov7ServiceEvidenceFormatAndHosts(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a.yaml": "yaml", "a.yml": "yaml", "a.json": "json",
		"a.js": "javascript", "a.mjs": "javascript", "a.cjs": "javascript",
		"a.ts": "typescript", "a.mts": "typescript", "a.cts": "typescript",
		"a.md": "markdown", "a.txt": "text", "Makefile": "text",
	}
	for path, want := range cases {
		if got := serviceEvidenceFormat(path); got != want {
			t.Fatalf("serviceEvidenceFormat(%q) = %q, want %q", path, got, want)
		}
	}
	for _, path := range []string{"a.OpenAPI.YAML", "x/Swagger.json", "docs/spec.txt"} {
		if !isPotentialAPISpecPath(path) {
			t.Fatalf("isPotentialAPISpecPath(%q) = false, want true", path)
		}
	}
	if isPotentialAPISpecPath("docs/readme.md") {
		t.Fatalf("isPotentialAPISpecPath(readme) = true, want false")
	}
	if got := hostnameFromURL("https://API.Example.COM:8443/v1"); got != "api.example.com" {
		t.Fatalf("hostnameFromURL = %q, want lowercased host", got)
	}
	if got := hostnameFromURL("http://exa mple.com/%zz"); got != "" {
		t.Fatalf("hostnameFromURL(invalid) = %q, want empty", got)
	}
	if _, err := parseLooseYAMLDocument("a: [unclosed"); err == nil {
		t.Fatalf("parseLooseYAMLDocument(malformed) = nil error, want error")
	}
}
