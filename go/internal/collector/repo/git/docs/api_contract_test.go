// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"testing"
)

func TestAPIContractFormatForPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path     string
		format   string
		language string
		wantOK   bool
	}{
		{"openapi.yaml", "openapi", "yaml", true},
		{"openapi.json", "openapi", "json", true},
		{"oas-spec.yml", "openapi", "yaml", true},
		{"swagger.json", "swagger", "json", true},
		{"asyncapi.yaml", "asyncapi", "yaml", true},
		{"api.yaml", "", "", false},
		{"config.json", "", "", false},
		{"openapi.txt", "", "", false},
	} {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			format, ok := apiContractFormatForPath(tc.path)
			if ok != tc.wantOK {
				t.Fatalf("apiContractFormatForPath(%q) ok = %v, want %v", tc.path, ok, tc.wantOK)
			}
			if ok && (format.Format != tc.format || format.Language != tc.language) {
				t.Fatalf("apiContractFormatForPath(%q) = %+v, want %s/%s", tc.path, format, tc.format, tc.language)
			}
		})
	}
}

func TestDetectStructuredAPIContractFormat(t *testing.T) {
	t.Parallel()

	t.Run("name match wins without parsing", func(t *testing.T) {
		t.Parallel()

		format, ok := detectStructuredAPIContractFormat("openapi.yaml", []byte("not json"))
		if !ok || format.Format != "openapi" {
			t.Fatalf("detectStructuredAPIContractFormat() = (%+v, %v)", format, ok)
		}
	})

	t.Run("body sniffing finds version keys", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			body   string
			format string
		}{
			{`{"openapi": "3.0.0"}`, "openapi"},
			{`{"swagger": "2.0"}`, "swagger"},
			{"asyncapi: 2.0.0\n", "asyncapi"},
		} {
			format, ok := detectStructuredAPIContractFormat("api.yaml", []byte(tc.body))
			if !ok || format.Format != tc.format || format.Language != "yaml" {
				t.Fatalf("detectStructuredAPIContractFormat(%q) = (%+v, %v)", tc.body, format, ok)
			}
		}
	})

	t.Run("non contracts are rejected", func(t *testing.T) {
		t.Parallel()

		for _, body := range []string{"{oops", `{"name": "plain"}`, ""} {
			if _, ok := detectStructuredAPIContractFormat("api.yaml", []byte(body)); ok {
				t.Fatalf("detectStructuredAPIContractFormat(%q) = true, want false", body)
			}
		}
		if isPotentialStructuredAPIContractPath("notes.md") {
			t.Fatal("isPotentialStructuredAPIContractPath(md) = true, want false")
		}
		if !isPotentialStructuredAPIContractPath("schema.json") {
			t.Fatal("isPotentialStructuredAPIContractPath(schema) = false, want true")
		}
	})
}

func TestExtractAPIContractDocumentation(t *testing.T) {
	t.Parallel()

	t.Run("minimal openapi yields path sections", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"openapi": "3.0.0", "info": {"title": "T", "version": "1"}, "paths": {"/pets": {"get": {"summary": "List"}}}}`)
		document, sections, _ := extractAPIContractDocumentation(testDocRepo(), "openapi.json", "d", "c", body, "openapi")
		if document.Format != "openapi" {
			t.Fatalf("format = %q", document.Format)
		}
		if len(sections) == 0 {
			t.Fatal("no sections extracted from minimal openapi")
		}
		if document.SourceMetadata["warning"] != "" {
			t.Fatalf("metadata = %v, want no warnings", document.SourceMetadata)
		}
	})

	t.Run("malformed body warns without sections", func(t *testing.T) {
		t.Parallel()

		document, sections, links := extractAPIContractDocumentation(testDocRepo(), "openapi.json", "d", "c", []byte("{oops"), "openapi")
		if document.SourceMetadata["warning"] != "malformed_api_contract" {
			t.Fatalf("metadata = %v, want exactly malformed_api_contract", document.SourceMetadata)
		}
		if sections != nil || links != nil {
			t.Fatalf("sections/links = (%v, %v), want nil", sections, links)
		}
	})
}
