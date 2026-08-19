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

// The code_calls family Odù (#5991, under the #5543 umbrella).
//
// codeCallFamilyOdu in code_call_family_catalog.go is the binary-portable
// compiled catalog representation. This file projects the committed cassette
// through the same strict envelope boundary for
// TestCodeCallFamilyIsCatalogedAndResolvable, which deeply compares the two
// representations so a one-sided edit fails the focused suite.
//
// Both live gate scripts drive this cassette. The offline extractor guard and
// the determinism/fault-injection matrices therefore assert the same committed
// bytes rather than maintaining parallel fixtures that can drift.
const (
	codeCallFamilyOduName      = "odu:ifa-code-call-family"
	codeCallFamilyCassettePath = "testdata/cassettes/codecalls/ifa-code-call-family.json"
	codeCallExpectedEdgesPath  = "go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"
)

// codeCallFamilyCassetteFile declares the cassette's COMPLETE envelope
// schema -- every field the real committed cassette
// (testdata/cassettes/codecalls/ifa-code-call-family.json) carries at the
// file, scope, and fact level, verified against that file directly -- not
// just the load-bearing subset LoadCodeCallFamilyOdu actually consumes.
//
// Declaring the full schema, rather than only the fields this loader reads,
// is what makes DisallowUnknownFields (below) safe to enable: a decoder that
// only declared the load-bearing subset would reject the real cassette's
// Collector, top-level SchemaVersion, and every scope-level field but
// ScopeID/GenerationID/Facts as "unknown". Fields declared here purely to
// satisfy that constraint (Collector, the scopes' SourceSystem/ScopeKind/
// PartitionKey/Metadata/ObservedAt/TriggerKind) are read but never used
// downstream; they exist only so a genuine typo elsewhere in the envelope
// still fails loudly instead of being masked by an intentionally-narrow
// struct.
//
// The per-fact fields ARE load-bearing: schema_version was originally
// dropped here, and an empty version reads as "latest", so a cassette
// carrying an unsupported major would sail through this projection and
// satisfy the offline guard while live replay preserved the version and
// quarantined the fact -- the extractor guard would then certify input the
// live gate rejects. stable_fact_key, collector_kind and source_confidence
// ride along for the same reason: this projection must never be more
// permissive than production.
type codeCallFamilyCassetteFile struct {
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

// CodeCallFamilyCassetteFullPath joins repoRoot onto the cassette path.
// Exported (#6163) so materializededges' moved code-call-family tests can
// locate the same committed cassette LoadCodeCallFamilyOdu reads.
func CodeCallFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeCallFamilyCassettePath)
}

// LoadCodeCallFamilyOdu reads the committed cassette and projects it onto the
// fact envelopes the reducer's extractor consumes.
//
// It is the test-side lockstep loader for the committed cassette. Production
// registers the compiled codeCallFamilyOdu in catalogSeed;
// TestCodeCallFamilyIsCatalogedAndResolvable in materializededges (#6163,
// moved with the rest of the code_calls guard) compares that registered Odù
// with this strict cassette projection and exercises the code_calls resolver
// guard. Exported so that moved test can reach it across the package
// boundary.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts would
// make every downstream assertion vacuous, which is the failure mode the whole
// #5543 exhaustiveness effort exists to remove.
//
// The decoder disallows unknown fields, so a typo anywhere in the envelope
// (e.g. "fact_knd" instead of "fact_kind") fails loudly at load time instead
// of silently decoding to a zero value. Without this, an unnoticed typo would
// silently project the WRONG fact set from the cassette, and the resulting
// Odù would still drive an exact-set gate whose green result would then
// attest to something the cassette did not actually say -- the same
// false-attestation shape that forced a coverage-row withdrawal on #5994.
// json.Decoder.Decode reads exactly one JSON value and stops, unlike the
// json.Unmarshal this replaces, so a second Decode requiring io.EOF closes
// the trailing-content gap switching decoders would otherwise reopen.
func LoadCodeCallFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read code-call cassette %s: %w", cassettePath, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed codeCallFamilyCassetteFile
	if err := decoder.Decode(&parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse code-call cassette %s: %w", cassettePath, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return Odu{}, fmt.Errorf("ifa: code-call cassette %s has trailing content after its JSON object", cassettePath)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: code-call cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: code-call cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
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
	return Odu{Name: codeCallFamilyOduName, Facts: envelopes}, nil
}
