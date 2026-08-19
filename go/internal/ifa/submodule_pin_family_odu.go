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

// The submodule_pin_edges family Odù (#6002, under the #5543 umbrella).
//
// submodulePinFamilyOdu in submodule_pin_family_catalog.go is the
// binary-portable compiled catalog representation. This file projects the
// committed cassette through the same strict envelope boundary for
// TestSubmodulePinFamilyIsCatalogedAndResolvable, which deeply compares the
// two representations so a one-sided edit fails the focused suite.
//
// submodule_pin_edges mirrors codeowners_ownership_edges structurally
// (single-stage, EdgeWriter-only, no IntentWriter — see
// SubmodulePinEdgeMaterializationHandler,
// go/internal/reducer/submodule_pin_materialization.go), so this loader is
// codeownersFamilyOdu's loader (codeowners_family_odu.go) with the family
// name swapped: same strict-decode rationale, same DisallowUnknownFields
// contract, same single-scope/non-empty-facts fail-closed checks.
const (
	submodulePinFamilyOduName      = "odu:ifa-submodule-pin-family"
	submodulePinFamilyCassettePath = "testdata/cassettes/submodulepin/ifa-submodule-pin-family.json"
	submodulePinExpectedEdgesPath  = "go/internal/ifa/testdata/submodulepin/ifa-submodule-pin-family-expected-edges.json"
)

// submodulePinFamilyCassetteFile declares the cassette's FULL envelope shape,
// matching codeownersFamilyCassetteFile field for field (see that type's doc
// comment for why every field — including the ones this loader never reads —
// must still be declared before DisallowUnknownFields is safe to turn on).
type submodulePinFamilyCassetteFile struct {
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

// submodulePinFamilyCassetteFullPath joins repoRoot onto the cassette path.
func submodulePinFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, submodulePinFamilyCassettePath)
}

// submodulePinFamilyExpectedEdgesPath joins repoRoot onto the expected-edge
// fixture. It lives under go/internal/ifa/testdata/ rather than
// testdata/cassettes/ for the same reason codeownersFamilyExpectedEdgesPath
// does: the offline cassette validator globs every testdata/cassettes/*/*.json
// as a replay cassette, and this file is a gate ASSERTION, not a cassette.
func submodulePinFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, submodulePinExpectedEdgesPath)
}

// loadSubmodulePinFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes.
//
// Unexported because it is the test-side lockstep loader for the committed
// cassette. Production registers the compiled submodulePinFamilyOdu in
// catalogSeed; TestSubmodulePinFamilyIsCatalogedAndResolvable compares that
// registered Odù with this strict cassette projection and exercises the
// submodule_pin_edges resolver guard.
//
// It fails closed on an empty scope or fact list, and disallows unknown
// fields so a typo in the envelope fails loudly at load time instead of
// silently decoding to a zero value (see codeownersFamilyCassetteFile's doc
// comment for the #5994 false-attestation incident this pattern exists to
// prevent). The second Decode call closes the same trailing-content gap.
func loadSubmodulePinFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read submodule-pin cassette %s: %w", cassettePath, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed submodulePinFamilyCassetteFile
	if err := decoder.Decode(&parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse submodule-pin cassette %s: %w", cassettePath, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return Odu{}, fmt.Errorf("ifa: submodule-pin cassette %s has trailing content after its JSON object", cassettePath)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: submodule-pin cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: submodule-pin cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
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
	return Odu{Name: submodulePinFamilyOduName, Facts: envelopes}, nil
}
