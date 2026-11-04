// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
)

// manifestChange is the set of dependency keys a marker generation changed,
// in the "ecosystem\x00name" form manifestDependencyKeySet produces.
type manifestChange struct {
	keys map[string]struct{}
	// packages counts the distinct (package manager, name) pairs changed.
	packages int
	// priorBaseMissing reports the known gap: the prior generation was a
	// delta whose base could not be read, so removals of pins only that base
	// held are not seen (see TestManifestTriggerKnownLimitPriorDeltaWithMissingBase).
	priorBaseMissing bool
	// deltaPaths is the number of paths a delta generation touched; 0 for a
	// full generation.
	deltaPaths int
}

// changedManifestDependencyKeys derives the changed packages from the
// generation's own dependency rows (ruling Option B):
//
//   - no prior activated generation: every pin, live or tombstoned, is new;
//   - otherwise the symmetric difference of dependency signatures between the
//     current rows and the prior view. A delta is compared on its touched
//     paths only, because it carries rows for nothing else. The prior view of
//     a delta prior is its base overlaid with its own rows
//     (facts.OverlayDeltaBase), so a pin only the base held still counts.
//
// Tombstoned rows in the current generation always count as changed: a
// tombstone is a removal. Both sides of a change (old and new version) report
// the same package key, so a version bump reopens that package's holders once.
func changedManifestDependencyKeys(generation ManifestDependencyGeneration) manifestChange {
	scope := facts.ParseDeltaScope(generation.Current.Rows)
	touched := scope.Touched()
	inScope := func(envelope facts.Envelope) bool {
		if !scope.IsDelta {
			return true
		}
		_, ok := touched[scope.FilePath(envelope)]
		return ok
	}
	change := manifestChange{keys: map[string]struct{}{}}
	if scope.IsDelta {
		change.deltaPaths = len(touched)
	}
	packages := map[string]struct{}{}
	add := func(dependency correlation.PackageManifestDependency) {
		keys := manifestDependencyKeySet(dependency)
		if len(keys) == 0 {
			return
		}
		for key := range keys {
			change.keys[key] = struct{}{}
		}
		packages[dependency.PackageManager+"\x00"+strings.ToLower(dependency.DependencyName)] = struct{}{}
	}

	current := dependencySignatures(generation.Current.Rows, inScope)
	for _, dependency := range tombstonedDependencies(generation.Current.Rows) {
		add(dependency)
	}
	if !generation.HasPrior {
		for _, dependency := range current {
			add(dependency)
		}
		change.packages = len(packages)
		return change
	}
	priorRows := generation.Prior.Rows
	priorScope := facts.ParseDeltaScope(priorRows)
	if priorScope.IsDelta {
		if generation.Prior.BaseMissing {
			change.priorBaseMissing = true
		} else {
			priorRows = facts.OverlayDeltaBase(generation.Prior.Base, priorRows, priorScope)
		}
	}
	prior := dependencySignatures(priorRows, inScope)
	for signature, dependency := range current {
		if _, unchanged := prior[signature]; !unchanged {
			add(dependency)
		}
	}
	for signature, dependency := range prior {
		if _, unchanged := current[signature]; !unchanged {
			add(dependency)
		}
	}
	change.packages = len(packages)
	return change
}

// dependencySignatures extracts the live dependency rows that pass keep, keyed
// by dependencySignature: every field the finding can carry.
func dependencySignatures(rows []facts.Envelope, keep func(facts.Envelope) bool) map[string]correlation.PackageManifestDependency {
	kept := make([]facts.Envelope, 0, len(rows))
	for _, row := range rows {
		if keep(row) {
			kept = append(kept, row)
		}
	}
	out := map[string]correlation.PackageManifestDependency{}
	for _, dependency := range correlation.ExtractPackageManifestDependencies(kept) {
		out[dependencySignature(dependency)] = dependency
	}
	return out
}

// dependencySignature is the dependency with the two fields every re-index
// rewrites (FactID, ObservedAt) cleared, encoded whole. Encoding the struct
// rather than listing fields means a field added to PackageManifestDependency
// is part of change detection by default (#9874 F5);
// TestDependencySignatureCoversEveryFindingField enumerates the fields to
// prove it. encoding/json writes struct fields in declaration order and
// follows pointers, so equal dependencies encode equally.
func dependencySignature(dependency correlation.PackageManifestDependency) string {
	dependency.FactID = ""
	dependency.ObservedAt = time.Time{}
	encoded, err := json.Marshal(dependency)
	if err != nil {
		// Unreachable for this all-plain-field struct; fall back to a
		// signature that differs from every other so the row counts as changed.
		return "unencodable\x00" + dependency.PackageManager + "\x00" + dependency.DependencyName
	}
	return string(encoded)
}

// tombstonedDependencies extracts the dependency rows the generation
// tombstoned. The extractor skips tombstones, so each is read as live once.
func tombstonedDependencies(rows []facts.Envelope) []correlation.PackageManifestDependency {
	var revived []facts.Envelope
	for _, row := range rows {
		if row.IsTombstone {
			row.IsTombstone = false
			revived = append(revived, row)
		}
	}
	if len(revived) == 0 {
		return nil
	}
	return correlation.ExtractPackageManifestDependencies(revived)
}

// ecosystems returns the sorted distinct ecosystems the changed keys name, the
// prefilter the package-keyed holder read takes.
func (c manifestChange) ecosystems() []string {
	seen := map[string]struct{}{}
	for key := range c.keys {
		if ecosystem := keyEcosystem(key); ecosystem != "" {
			seen[ecosystem] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for ecosystem := range seen {
		out = append(out, ecosystem)
	}
	sort.Strings(out)
	return out
}

// mergeSubSignals adds the change shape to a result's sub-signals.
func (c manifestChange) mergeSubSignals(signals map[string]float64) {
	signals["manifest_trigger_changed_packages"] = float64(c.packages)
	signals["manifest_trigger_delta_paths"] = float64(c.deltaPaths)
	signals["manifest_trigger_prior_base_missing"] = boolSignal(c.priorBaseMissing)
}
