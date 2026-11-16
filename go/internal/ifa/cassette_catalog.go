// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// cassetteOduSpec declares one cataloged Odù built from a committed recording
// instead of a compiled copy of it. A satellite collector records its own
// output into a cassette; a compiled copy would drift from that output, which
// is what recording it is meant to rule out. The spec names the cassette, the
// recorded scopes the Odù selects and the facts it plants beside them.
type cassetteOduSpec struct {
	name   string
	detail string
	// cassette is the slash-separated path of the recording under the
	// repository root.
	cassette string
	// scopes are the recorded scope ids the Odù selects.
	scopes []string
	// planted are facts added beside the recorded ones. The recording cannot
	// hold them: an SBOM component is another collector's output.
	planted []facts.Envelope
}

// CatalogAt returns every cataloged Odù in name order: the compiled seed that
// Catalog returns plus the cassette-backed entries, read from the recordings
// under repoRoot. It fails closed. A recording that is missing, holds no
// scopes, lacks a selected scope or holds no facts for one returns an error
// instead of an Odù built from nothing, because every assertion on an empty Odù
// passes vacuously.
func CatalogAt(repoRoot string) ([]familyodu.CatalogOdu, error) {
	backed, err := resolveCassetteSpecs(repoRoot, cassetteBackedSpecs())
	if err != nil {
		return nil, err
	}
	out := append(Catalog(), backed...)
	sort.Slice(out, func(i, j int) bool { return out[i].Odu.Name < out[j].Odu.Name })
	return out, nil
}

// CatalogByNameAt indexes CatalogAt by Odù name. A coverage-manifest row that
// names a cassette-backed Odù resolves only through it; CatalogByName holds the
// compiled entries alone.
func CatalogByNameAt(repoRoot string) (map[string]familyodu.Odu, error) {
	entries, err := CatalogAt(repoRoot)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]familyodu.Odu, len(entries))
	for _, entry := range entries {
		byName[entry.Odu.Name] = entry.Odu
	}
	return byName, nil
}

// resolveCassetteSpecs builds each spec's Odù from its recording. Each
// recording is read once however many specs select from it.
func resolveCassetteSpecs(repoRoot string, specs []cassetteOduSpec) ([]familyodu.CatalogOdu, error) {
	recordings := map[string]map[string][]facts.Envelope{}
	out := make([]familyodu.CatalogOdu, 0, len(specs))
	for _, spec := range specs {
		if len(spec.scopes) == 0 {
			return nil, fmt.Errorf("ifa: %s selects no recorded scope", spec.name)
		}
		byScope, ok := recordings[spec.cassette]
		if !ok {
			var err error
			if byScope, err = readRecordedScopes(repoRoot, spec); err != nil {
				return nil, err
			}
			recordings[spec.cassette] = byScope
		}
		odu, err := spec.build(byScope)
		if err != nil {
			return nil, err
		}
		out = append(out, familyodu.CatalogOdu{Odu: odu, Detail: spec.detail})
	}
	return out, nil
}

// readRecordedScopes replays the recording through the cassette source, so the
// envelopes are the ones a replay hands the reducer, and groups them by scope.
func readRecordedScopes(repoRoot string, spec cassetteOduSpec) (map[string][]facts.Envelope, error) {
	source, err := cassette.NewSource(filepath.Join(repoRoot, filepath.FromSlash(spec.cassette)))
	if err != nil {
		return nil, fmt.Errorf("ifa: load cassette %s for %s: %w", spec.cassette, spec.name, err)
	}
	byScope := make(map[string][]facts.Envelope, len(source.File.Scopes))
	for {
		generation, ok, err := source.Next(context.Background())
		if err != nil {
			return nil, fmt.Errorf("ifa: replay cassette %s for %s: %w", spec.cassette, spec.name, err)
		}
		if !ok {
			return byScope, nil
		}
		envelopes := byScope[generation.Scope.ScopeID]
		for envelope := range generation.Facts {
			envelopes = append(envelopes, envelope)
		}
		byScope[generation.Scope.ScopeID] = envelopes
	}
}

// build assembles the Odù from the recorded scopes it selects and its planted
// facts. The Odù owns its facts, so no two Odùs share a payload map.
func (s cassetteOduSpec) build(byScope map[string][]facts.Envelope) (familyodu.Odu, error) {
	var recorded []facts.Envelope
	for _, scopeID := range s.scopes {
		envelopes, ok := byScope[scopeID]
		if !ok {
			held := make([]string, 0, len(byScope))
			for id := range byScope {
				held = append(held, id)
			}
			slices.Sort(held)
			return familyodu.Odu{}, fmt.Errorf("ifa: %s selects scope %q, which cassette %s does not hold (it holds %s)",
				s.name, scopeID, s.cassette, strings.Join(held, ", "))
		}
		if len(envelopes) == 0 {
			return familyodu.Odu{}, fmt.Errorf("ifa: %s selects scope %q, which cassette %s holds no facts for; an empty scope makes every assertion on it vacuous",
				s.name, scopeID, s.cassette)
		}
		recorded = append(recorded, envelopes...)
	}
	return familyodu.Odu{Name: s.name, Facts: cloneFacts(append(recorded, s.planted...))}, nil
}
