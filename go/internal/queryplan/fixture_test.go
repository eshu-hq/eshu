// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestLegacyHotCypherManifestRegistrationMetadata(t *testing.T) {
	manifest, err := LoadManifestFile("testdata/hot-cypher.yaml")
	if err != nil {
		t.Fatalf("LoadManifestFile() error = %v", err)
	}
	entries := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entries[entry.ID] = struct{}{}
		if entry.QueryKind != queryKindCypher {
			continue
		}
		if strings.TrimSpace(entry.Cypher) != "" {
			t.Errorf("legacy hot path %s must bind Cypher from its production execution path", entry.ID)
		}
		if digest, err := hex.DecodeString(entry.CypherSHA256); err != nil || len(digest) != 32 {
			t.Errorf("legacy hot path %s has invalid cypher_sha256 %q", entry.ID, entry.CypherSHA256)
		}
		if digest, err := hex.DecodeString(entry.Source.SourceSHA256); err != nil || len(digest) != 32 {
			t.Errorf("legacy hot path %s has invalid source.source_sha256 %q", entry.ID, entry.Source.SourceSHA256)
		}
	}
	for _, requiredID := range manifest.RequiredIDs {
		if _, ok := entries[requiredID]; !ok {
			t.Errorf("legacy hot path manifest is missing required entry %s", requiredID)
		}
	}
}

func TestHandlerHotCypherManifestRegistrationMetadata(t *testing.T) {
	manifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatalf("LoadManifestFile() error = %v", err)
	}
	entries := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entries[entry.ID] = struct{}{}
		if entry.QueryKind != queryKindCypher {
			continue
		}
		if strings.TrimSpace(entry.Cypher) != "" {
			t.Errorf("handler hot path %s must bind Cypher from its production builder", entry.ID)
		}
		if digest, err := hex.DecodeString(entry.CypherSHA256); err != nil || len(digest) != 32 {
			t.Errorf("handler hot path %s has invalid cypher_sha256 %q", entry.ID, entry.CypherSHA256)
		}
		if digest, err := hex.DecodeString(entry.Source.SourceSHA256); err != nil || len(digest) != 32 {
			t.Errorf("handler hot path %s has invalid source.source_sha256 %q", entry.ID, entry.Source.SourceSHA256)
		}
		if strings.TrimSpace(entry.QueryFragment) == "" {
			t.Errorf("handler hot path %s is missing query_fragment", entry.ID)
		}
	}
	for _, requiredID := range manifest.RequiredIDs {
		if _, ok := entries[requiredID]; !ok {
			t.Errorf("handler hot path manifest is missing required entry %s", requiredID)
		}
	}
	if err := ValidateManifestSources(manifest, "../../.."); err != nil {
		t.Fatalf("ValidateManifestSources() error = %v", err)
	}
}
