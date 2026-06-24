// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsOpenAPIYAMLContractDocumentation(t *testing.T) {
	t.Parallel()

	body := `openapi: 3.1.0
info:
  title: Payments API
  version: v1
  description: Public payment operations.
externalDocs:
  url: https://docs.example.test/payments
paths:
  /orders:
    get:
      operationId: listOrders
      summary: List orders
      description: Returns bounded order summaries.
      tags: [orders]
      responses:
        "200":
          description: Orders response
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/OrderList"
components:
  schemas:
    OrderList:
      description: A bounded order page.
      type: object
`
	envelopes := streamContractDocumentationFacts(t, "openapi.yaml", body)
	document := singleFact(t, envelopes, facts.DocumentationDocumentFactKind)
	if got, want := payloadString(document.Payload, "format"), "openapi"; got != want {
		t.Fatalf("document format = %q, want %q", got, want)
	}
	if got, want := payloadString(document.Payload, "document_type"), "api_contract"; got != want {
		t.Fatalf("document_type = %q, want %q", got, want)
	}

	operation := findDocumentationSection(t, envelopes, "GET /orders")
	for _, want := range []string{"listOrders", "Returns bounded order summaries", "Schema refs: #/components/schemas/OrderList"} {
		if content := payloadString(operation.Payload, "content"); !strings.Contains(content, want) {
			t.Fatalf("operation section content missing %q: %q", want, content)
		}
	}
	if got := payloadString(operation.Payload, "source_start_ref"); !strings.Contains(got, "/paths/~1orders/get") {
		t.Fatalf("operation source_start_ref = %q, want JSON pointer anchor", got)
	}
	if got := payloadString(operation.Payload, "section_anchor"); got != "operation-get-orders" {
		t.Fatalf("operation section_anchor = %q, want operation-get-orders", got)
	}

	schema := findDocumentationSection(t, envelopes, "Schema OrderList")
	if got := payloadString(schema.Payload, "content"); !strings.Contains(got, "A bounded order page.") {
		t.Fatalf("schema content = %q, want schema description", got)
	}
	links := factsByKind(envelopes, facts.DocumentationLinkFactKind)
	if got, want := len(links), 1; got != want {
		t.Fatalf("documentation_link count = %d, want %d", got, want)
	}
	if got, want := payloadString(links[0].Payload, "target_uri"), "https://docs.example.test/payments"; got != want {
		t.Fatalf("link target_uri = %q, want %q", got, want)
	}
}

func TestStreamFactsEmitsAPIContractDocumentationFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		relative    string
		body        string
		format      string
		wantHeading string
		wantContent string
	}{
		{
			name:     "openapi json",
			relative: "openapi.json",
			format:   "openapi",
			body: `{
  "openapi": "3.0.3",
  "info": {"title": "Health API", "version": "v1"},
  "paths": {"/healthz": {"get": {"operationId": "getHealth", "summary": "Read health"}}}
}`,
			wantHeading: "GET /healthz",
			wantContent: "getHealth",
		},
		{
			name:     "openapi structure on api filename",
			relative: "service-api.yaml",
			format:   "openapi",
			body: `openapi: 3.0.3
info:
  title: Service API
  version: v1
paths:
  /ready:
    get:
      operationId: getReady
      summary: Read readiness
`,
			wantHeading: "GET /ready",
			wantContent: "getReady",
		},
		{
			name:     "swagger yaml",
			relative: "swagger.yaml",
			format:   "swagger",
			body: `swagger: "2.0"
info:
  title: Users API
  version: v1
paths:
  /users:
    post:
      operationId: createUser
      summary: Create user
definitions:
  User:
    description: A user resource.
`,
			wantHeading: "POST /users",
			wantContent: "createUser",
		},
		{
			name:     "asyncapi yaml",
			relative: "asyncapi.yaml",
			format:   "asyncapi",
			body: `asyncapi: 2.6.0
info:
  title: User Events
  version: v1
channels:
  user/signedup:
    subscribe:
      operationId: onUserSignedUp
      summary: User signed up
components:
  schemas:
    UserSignedUp:
      description: Signup event payload.
`,
			wantHeading: "SUBSCRIBE user/signedup",
			wantContent: "onUserSignedUp",
		},
		{
			name:     "graphql sdl",
			relative: "schema.graphql",
			format:   "graphql_sdl",
			body: `"""Payments read model."""
type Query {
  "Fetch one order."
  order(id: ID!): Order
}

type Order {
  id: ID!
  total: Float
}`,
			wantHeading: "Query.order",
			wantContent: "Fetch one order.",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			envelopes := streamContractDocumentationFacts(t, tt.relative, tt.body)
			document := singleFact(t, envelopes, facts.DocumentationDocumentFactKind)
			if got := payloadString(document.Payload, "format"); got != tt.format {
				t.Fatalf("document format = %q, want %q", got, tt.format)
			}
			section := findDocumentationSection(t, envelopes, tt.wantHeading)
			if got := payloadString(section.Payload, "content"); !strings.Contains(got, tt.wantContent) {
				t.Fatalf("section content = %q, want %q", got, tt.wantContent)
			}
			assertDocumentationFactLinkedRepository(t, section, "repository:r_12345678")
		})
	}
}

func TestStreamFactsHandlesMalformedAndHugeAPIContractDocs(t *testing.T) {
	t.Parallel()

	malformed := streamContractDocumentationFacts(t, "openapi.yaml", "openapi: 3.1.0\npaths:\n  /broken: [")
	document := singleFact(t, malformed, facts.DocumentationDocumentFactKind)
	metadata := document.Payload["source_metadata"].(map[string]any)
	if got := payloadStringFromMap(metadata, "warning"); !strings.Contains(got, "malformed_api_contract") {
		t.Fatalf("malformed document warning = %q, want malformed_api_contract", got)
	}

	var builder strings.Builder
	builder.WriteString("openapi: 3.1.0\ninfo:\n  title: Huge API\n  version: v1\npaths:\n")
	for i := 0; i < 140; i++ {
		builder.WriteString("  /items/")
		builder.WriteString(strconv.Itoa(i))
		builder.WriteString(strings.Repeat("x", i%4))
		builder.WriteString("op")
		builder.WriteString(strings.Repeat("y", i%3))
		builder.WriteString(":\n    get:\n      operationId: listItems")
		builder.WriteString(strings.Repeat("Z", i%5))
		builder.WriteString("\n      summary: Huge operation\n")
	}
	huge := streamContractDocumentationFacts(t, "openapi.yaml", builder.String())
	sections := factsByKind(huge, facts.DocumentationSectionFactKind)
	if got, max := len(sections), 80; got > max {
		t.Fatalf("documentation_section count = %d, want at most %d", got, max)
	}
	hugeDocument := singleFact(t, huge, facts.DocumentationDocumentFactKind)
	hugeMetadata := hugeDocument.Payload["source_metadata"].(map[string]any)
	if got := payloadStringFromMap(hugeMetadata, "warning"); !strings.Contains(got, "section_limit_exceeded") {
		t.Fatalf("huge document warning = %q, want section_limit_exceeded", got)
	}
}

func streamContractDocumentationFacts(t *testing.T, relativePath string, body string) []facts.Envelope {
	t.Helper()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, filepath.FromSlash(relativePath)), body)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: relativePath,
			Digest:       "sha256:" + strings.ReplaceAll(relativePath, "/", "-"),
			Language:     "api_contract",
			CommitSHA:    "abc123",
		}},
	}
	collected := buildStreamingGeneration(
		repoPath,
		repo,
		"run-1",
		time.Date(2026, time.June, 9, 1, 0, 0, 0, time.UTC),
		snapshot,
		false,
	)
	return drainFactChannel(collected.Facts)
}

func singleFact(t *testing.T, envelopes []facts.Envelope, kind string) facts.Envelope {
	t.Helper()

	matches := factsByKind(envelopes, kind)
	if got, want := len(matches), 1; got != want {
		t.Fatalf("%s count = %d, want %d", kind, got, want)
	}
	return matches[0]
}

func findDocumentationSection(t *testing.T, envelopes []facts.Envelope, heading string) facts.Envelope {
	t.Helper()

	for _, section := range factsByKind(envelopes, facts.DocumentationSectionFactKind) {
		if payloadString(section.Payload, "heading_text") == heading {
			return section
		}
	}
	t.Fatalf("missing documentation section heading %q", heading)
	return facts.Envelope{}
}

func payloadStringFromMap(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}
