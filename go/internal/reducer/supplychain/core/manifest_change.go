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
	// priorBaseMissing reports the known gap: a delta in the diffed chain had
	// a base that could not be read, so removals of pins only that base held
	// are not seen (TestManifestTriggerKnownLimitPriorDeltaWithMissingBase,
	// tracked in #10059).
	priorBaseMissing bool
	// degraded and chainPruned mirror the generation read: the diff fell back
	// to every current pin, or skipped a pruned intermediate.
	degraded    bool
	chainPruned bool
	// deltaPaths is the number of paths the current delta touched; 0 for a
	// full generation.
	deltaPaths int
	// views is the number of generation views compared.
	views int
}

// changedManifestDependencyKeys derives the changed packages from the full
// dependency views of the activated chain [Baseline .. Current] (arbiter
// ruling D3). A package key is changed when its set of dependency signatures
// is not identical in every generation of the chain, which is the union of
// the pairwise symmetric differences: a pin added in an intermediate that
// never completed its own marker pass, and removed since, still counts. Each
// view is base-overlaid (facts.OverlayDeltaBase) on every side, current
// included, so a delta that reverts a path to its base content is compared
// against the base, not skipped.
//
//   - Degraded (chain over the depth cap, or an unreadable baseline): every
//     current pin counts.
//   - No baseline: every pin in any view of the chain counts.
//
// Tombstoned rows in the current generation always count as changed: a
// tombstone is a removal. Both sides of a change report the same package key,
// so a version bump reopens that package's holders once.
func changedManifestDependencyKeys(generation ManifestDependencyGeneration) manifestChange {
	change := manifestChange{
		keys:        map[string]struct{}{},
		degraded:    generation.Degraded,
		chainPruned: generation.ChainPruned,
	}
	if scope := facts.ParseDeltaScope(generation.Current.Rows); scope.IsDelta {
		change.deltaPaths = len(scope.Touched())
	}
	packages := map[string]struct{}{}
	labels := map[string]string{}
	mark := func(key string) {
		change.keys[key] = struct{}{}
		packages[labels[key]] = struct{}{}
	}
	keyed := func(dependency correlation.PackageManifestDependency) []string {
		label := dependency.PackageManager + "\x00" + strings.ToLower(dependency.DependencyName)
		var keys []string
		for key := range manifestDependencyKeySet(dependency) {
			labels[key] = label
			keys = append(keys, key)
		}
		return keys
	}
	for _, dependency := range tombstonedDependencies(generation.Current.Rows) {
		for _, key := range keyed(dependency) {
			mark(key)
		}
	}

	views := []ManifestDependencyView{}
	if !generation.Degraded {
		if generation.HasBaseline {
			views = append(views, generation.Baseline)
		}
		views = append(views, generation.Chain...)
	}
	views = append(views, generation.Current)
	change.views = len(views)

	// perView[key][i] is the sorted signature list of key in view i.
	perView := map[string][]string{}
	for i, view := range views {
		dependencies, baseMissing := viewDependencies(view)
		change.priorBaseMissing = change.priorBaseMissing || baseMissing
		signatures := map[string][]string{}
		for _, dependency := range dependencies {
			signature := dependencySignature(dependency)
			for _, key := range keyed(dependency) {
				signatures[key] = append(signatures[key], signature)
			}
		}
		for key, list := range signatures {
			sort.Strings(list)
			if perView[key] == nil {
				perView[key] = make([]string, len(views))
			}
			perView[key][i] = strings.Join(list, "\x01")
		}
	}
	for key, sets := range perView {
		if !generation.HasBaseline || generation.Degraded {
			mark(key)
			continue
		}
		for _, set := range sets[1:] {
			if set != sets[0] {
				mark(key)
				break
			}
		}
	}
	change.packages = len(packages)
	return change
}

// viewDependencies extracts one view's live dependencies: a delta view is its
// base overlaid with its own rows, or its own rows alone when the base could
// not be read (reported as baseMissing).
func viewDependencies(view ManifestDependencyView) ([]correlation.PackageManifestDependency, bool) {
	rows := view.Rows
	scope := facts.ParseDeltaScope(rows)
	if scope.IsDelta {
		if view.BaseMissing {
			return correlation.ExtractPackageManifestDependencies(rows), true
		}
		rows = facts.OverlayDeltaBase(view.Base, rows, scope)
	}
	return correlation.ExtractPackageManifestDependencies(rows), false
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
	signals["manifest_trigger_baseline_degraded"] = boolSignal(c.degraded)
	signals["manifest_trigger_chain_pruned"] = boolSignal(c.chainPruned)
	signals["manifest_trigger_views"] = float64(c.views)
}
