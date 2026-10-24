// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

const (
	parserOutputPasswordSeed = "pwSeedValue0123456789"
	parserOutputDefaultSeed  = "defaultSeedValue0123456789"
	parserOutputLocalSeed    = "localSeedValue0123456789"
)

const parserOutputTerraform = "variable \"db_value\" {\n  default = \"" + parserOutputDefaultSeed + "\"\n}\n\n" +
	"resource \"aws_db_instance\" \"main\" {\n  password = \"" + parserOutputPasswordSeed + "\"\n}\n\n" +
	"locals {\n  db_password = \"" + parserOutputLocalSeed + "\"\n}\n"

// TestParserOutputIsNotGovernedKnownLeak pins the arbiter's ruling A1
// (2026-10-24) and the #8813 record: the content-store policy governs the
// raw-text copies of a file (content_body, content_entities.source_cache, the
// whole-file parsed_file_data.source, and entity source snippets). Values a
// parser extracts into its own fields are parser output and are outside it.
// The hcl parser echoes .tf attributes, defaults, and locals, so a definition
// bound to metadata or redact still carries those values in facts.
//
// Parser output is outside the policy by design (8813 record). Flip the leak
// assertions below to assertNoSeedInAnyFact when parser output becomes
// governed. #8833 binding precondition: a definition may bind metadata or
// redact only after a seeded full-stream scan over every path shape it covers
// shows no credential in any fact kind.
func TestParserOutputIsNotGovernedKnownLeak(t *testing.T) {
	t.Parallel()

	cases := map[string]parser.ContentStore{
		"metadata": {Policy: parser.ContentStoreMetadata},
		"redact": {
			Policy: parser.ContentStoreRedact, Redactor: parser.RedactorMarker,
			CredentialKeys: []string{"password", "default"},
		},
	}
	seeds := []string{parserOutputPasswordSeed, parserOutputDefaultSeed, parserOutputLocalSeed}
	for name, store := range cases {
		_, envelopes := snapshotWithPolicy(t, policyRegistry(t, "hcl", store), "main.tf", parserOutputTerraform)

		// The governed raw-text copies are clean.
		body := contentFact(t, envelopes, "main.tf").Payload["content_body"]
		for _, seed := range seeds {
			if strings.Contains(asString(body), seed) {
				t.Errorf("%s: content_body holds %q", name, seed)
			}
			assertNoSeedInContentFacts(t, envelopes, seed)
		}
		if name == "metadata" && body != "" {
			t.Errorf("metadata content_body = %q, want empty", body)
		}

		// The known leak: parser-extracted values are present.
		file := parsedFileData(t, envelopes, "main.tf")
		wantAt := map[string]string{
			"terraform_variables[].default":             jsonOf(t, file["terraform_variables"]),
			"terraform_resources[].attributes.password": jsonOf(t, file["terraform_resources"]),
			"terraform_locals[].value":                  jsonOf(t, file["terraform_locals"]),
		}
		wantSeed := map[string]string{
			"terraform_variables[].default":             parserOutputDefaultSeed,
			"terraform_resources[].attributes.password": parserOutputPasswordSeed,
			"terraform_locals[].value":                  parserOutputLocalSeed,
		}
		for path, encoded := range wantAt {
			if !strings.Contains(encoded, wantSeed[path]) {
				t.Errorf("%s: parsed_file_data.%s no longer carries the seed; parser output is now governed or the parser changed: flip this pin", name, path)
			}
		}
		var entityMetadata string
		for _, envelope := range envelopes {
			if envelope.FactKind == "content_entity" {
				entityMetadata += jsonOf(t, envelope.Payload["entity_metadata"])
			}
		}
		for _, seed := range seeds {
			if !strings.Contains(entityMetadata, seed) {
				t.Errorf("%s: content_entity.entity_metadata no longer carries %q; flip this pin", name, seed)
			}
		}

		// Liveness: a full-stream scan DOES find the seed today, so flipping
		// the pins to assertNoSeedInAnyFact fails at this head.
		for _, seed := range seeds {
			if kinds := factKindsWithSeed(t, envelopes, seed); !containsAll(kinds, "file", "content_entity") {
				t.Errorf("%s: a full-stream scan for %q found %v, want file and content_entity", name, seed, kinds)
			}
		}
	}
}

func parsedFileData(t *testing.T, envelopes []facts.Envelope, relativePath string) map[string]any {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != "file" || envelope.Payload["relative_path"] != relativePath {
			continue
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(jsonOf(t, envelope.Payload["parsed_file_data"])), &out); err != nil {
			t.Fatalf("decode parsed_file_data: %v", err)
		}
		return out
	}
	t.Fatalf("no file fact for %q", relativePath)
	return nil
}

func jsonOf(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func containsAll(kinds []string, want ...string) bool {
	for _, kind := range want {
		found := false
		for _, have := range kinds {
			found = found || have == kind
		}
		if !found {
			return false
		}
	}
	return true
}
