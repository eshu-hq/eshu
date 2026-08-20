// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// documentationFamilyCassetteFile declares the cassette's COMPLETE envelope
// schema, mirroring codeCallFamilyCassetteFile's shape and reasoning
// (code_call_family_odu.go) exactly: every field the real committed cassette
// (testdata/cassettes/documentation/ifa-documentation-family.json) carries,
// verified against that file directly, not just the load-bearing subset
// LoadDocumentationFamilyOdu consumes. Declaring the full schema is what
// makes DisallowUnknownFields (below) safe: a narrower struct would reject
// the real cassette's own envelope fields as "unknown". The per-fact fields
// are load-bearing: schema_version is (an empty version reads as "latest"
// and would silently sail an unsupported-major fact through this projection
// while live replay quarantines it); stable_fact_key, collector_kind and
// source_confidence ride along for the same reason.
type documentationFamilyCassetteFile struct {
	Collector     string `json:"collector"`
	SchemaVersion string `json:"schema_version"`
	Scopes        []struct {
		ScopeID       string         `json:"scope_id"`
		SourceSystem  string         `json:"source_system"`
		ScopeKind     string         `json:"scope_kind"`
		CollectorKind string         `json:"collector_kind"`
		PartitionKey  string         `json:"partition_key"`
		Metadata      map[string]any `json:"metadata"`
		GenerationID  string         `json:"generation_id"`
		ObservedAt    string         `json:"observed_at"`
		TriggerKind   string         `json:"trigger_kind"`
		Facts         []struct {
			FactKind         string         `json:"fact_kind"`
			SchemaVersion    string         `json:"schema_version"`
			StableFactKey    string         `json:"stable_fact_key"`
			CollectorKind    string         `json:"collector_kind"`
			SourceConfidence string         `json:"source_confidence"`
			IsTombstone      bool           `json:"is_tombstone"`
			Payload          map[string]any `json:"payload"`
		} `json:"facts"`
	} `json:"scopes"`
}

// DocumentationFamilyCassetteFullPath joins repoRoot onto the cassette path.
// Exported (#6053) so materializededges' moved documentation-family tests can
// locate the same committed cassette LoadDocumentationFamilyOdu reads.
func DocumentationFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, documentationFamilyCassettePath)
}

// LoadDocumentationFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes, mirroring
// LoadCodeCallFamilyOdu exactly.
//
// It is the test-side lockstep loader for the committed cassette. Production
// registers the compiled documentationFamilyOdu in catalogSeed;
// TestDocumentationFamilyIsCatalogedAndResolvable in materializededges (#6053,
// moved with the rest of the documentation_edges guard) compares that
// registered Odù with this strict cassette projection and exercises the
// documentation_edges resolver guard. Exported so that moved test can reach it
// across the package boundary.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts
// would make every downstream assertion vacuous, which is the failure mode
// the whole #5543 exhaustiveness effort exists to remove.
//
// The decoder disallows unknown fields, so a typo anywhere in the envelope
// fails loudly at load time instead of silently decoding to a zero value --
// mirroring LoadCodeCallFamilyOdu's reasoning: an unnoticed typo would
// silently project the WRONG fact set, and the resulting Odù would still
// drive an exact-set gate whose green result would then attest to something
// the cassette did not actually say, the same false-attestation shape that
// forced a coverage-row withdrawal on #5994. The second Decode/io.EOF check
// closes the trailing-content gap switching from json.Unmarshal to
// json.Decoder would otherwise reopen.
func LoadDocumentationFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read documentation cassette %s: %w", cassettePath, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed documentationFamilyCassetteFile
	if err := decoder.Decode(&parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse documentation cassette %s: %w", cassettePath, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return Odu{}, fmt.Errorf("ifa: documentation cassette %s has trailing content after its JSON object", cassettePath)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: documentation cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: documentation cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
	}

	envelopes := make([]facts.Envelope, 0, len(scope.Facts))
	for _, fact := range scope.Facts {
		envelopes = append(envelopes, facts.Envelope{
			ScopeID:          scope.ScopeID,
			GenerationID:     scope.GenerationID,
			FactKind:         fact.FactKind,
			SchemaVersion:    fact.SchemaVersion,
			StableFactKey:    fact.StableFactKey,
			CollectorKind:    fact.CollectorKind,
			SourceConfidence: fact.SourceConfidence,
			IsTombstone:      fact.IsTombstone,
			Payload:          fact.Payload,
		})
	}
	return Odu{Name: documentationFamilyOduName, Facts: envelopes}, nil
}
