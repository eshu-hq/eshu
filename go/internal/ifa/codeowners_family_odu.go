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

// The codeowners_ownership_edges family Odù (#5992, under the #5543 umbrella).
//
// CodeownersFamilyOdu in codeowners_family_catalog.go is the binary-portable
// compiled catalog representation. This file projects the committed cassette
// through the same strict envelope boundary for
// TestCodeownersFamilyIsCatalogedAndResolvable, which deeply compares the two
// representations so a one-sided edit fails the focused suite.
//
// Both live gate scripts drive this cassette. The offline extractor guard and
// the determinism/fault-injection matrices therefore assert the same committed
// bytes rather than maintaining parallel fixtures that can drift.
const (
	// CodeownersFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:codeowners_ownership_edges coverage-manifest row
	// names to resolve through it. Exported (#6053) so
	// materializededges' moved codeowners-family tests, which build and
	// resolve against this exact Odù, can name and look it up by that ref
	// without duplicating the literal.
	CodeownersFamilyOduName      = "odu:ifa-codeowners-family"
	codeownersFamilyCassettePath = "testdata/cassettes/codeowners/ifa-codeowners-family.json"
)

// codeownersFamilyCassetteFile declares the cassette's FULL envelope shape,
// matching codeCallFamilyCassetteFile and documentationFamilyCassetteFile
// field for field.
//
// schema_version is load-bearing and was originally dropped in the code_calls
// sibling's first draft (#5991). An empty version reads as "latest", so a
// cassette carrying an unsupported major would sail through this projection
// and satisfy the offline guard while live replay preserved the version and
// quarantined the fact — the extractor guard would then certify input the
// live gate rejects. stable_fact_key, collector_kind and source_confidence
// ride along for the same reason: this projection must never be more
// permissive than production.
//
// The remaining fields — collector, the top-level schema_version, and the
// scope's source_system/scope_kind/collector_kind/partition_key/metadata/
// observed_at/trigger_kind — are declared but never read. They exist so
// DisallowUnknownFields can be turned on without the loader rejecting the
// real committed cassette, which carries all of them. A narrow struct plus a
// strict decoder is not a stricter loader, it is a loader that fails on its
// own fixture; a narrow struct plus a permissive decoder silently accepts
// every typo outside it. Only the full declaration gives the loud failure.
type codeownersFamilyCassetteFile struct {
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

// CodeownersFamilyCassetteFullPath joins repoRoot onto the cassette path.
// Exported (#6053) so materializededges' moved codeowners-family tests can
// locate the same committed cassette LoadCodeownersFamilyOdu reads.
func CodeownersFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeownersFamilyCassettePath)
}

// codeownersFamilyExpectedEdgesPath moved to
// materializededges/materialized_edges_codeowners.go with the rest of the
// codeowners_ownership_edges vacuity guard (#6053): it was called only from
// MaterializedEdgeOduResolver.Resolve's dispatch, which moved there too.

// LoadCodeownersFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes.
//
// It is the test-side lockstep loader for the committed cassette. Production
// registers the compiled CodeownersFamilyOdu in catalogSeed;
// TestCodeownersFamilyIsCatalogedAndResolvable (still in this package) compares
// that registered Odù with this strict cassette projection, and
// materializededges' moved codeowners-family tests (#6053) call it directly to
// exercise the codeowners_ownership_edges resolver guard against the same
// cassette. Exported so both sides of that package boundary can reach it
// without a second copy of the cassette decoder.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts
// would make every downstream assertion vacuous, which is the failure mode
// the whole #5543 exhaustiveness effort exists to remove.
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
func LoadCodeownersFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read codeowners cassette %s: %w", cassettePath, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed codeownersFamilyCassetteFile
	if err := decoder.Decode(&parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse codeowners cassette %s: %w", cassettePath, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return Odu{}, fmt.Errorf("ifa: codeowners cassette %s has trailing content after its JSON object", cassettePath)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: codeowners cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: codeowners cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
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
	return Odu{Name: CodeownersFamilyOduName, Facts: envelopes}, nil
}
