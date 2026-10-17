// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

// validationContext carries the cross-reference state the contract
// validators share.
type validationContext struct {
	repoRoot string
	families []discoveredFamily
	bindings []discoveredBinding
	symbols  kindSymbols
	// familyByID indexes the first declaration of each family ID.
	familyByID map[string]*discoveredFamily
	// kindOwner indexes each kind's owning family ID.
	kindOwner map[string]string
	// kindFamily indexes each kind's declaration for state checks.
	kindFamily map[string]*discoveredFamily
	// evidenceByDir caches per-directory reducer evidence.
	evidenceByDir map[string]map[string]sourcePos
	knownKinds    map[string]bool
}

// validateContracts runs every fact-family contract guard over the
// discovered declarations and accumulates diagnostics. Each diagnostic
// names the declaration file and line, the conflicting source location
// when one exists, and the corrective action.
func validateContracts(repoRoot string, families []discoveredFamily, bindings []discoveredBinding) diagnostics {
	ctx := &validationContext{
		repoRoot:      repoRoot,
		families:      families,
		bindings:      bindings,
		familyByID:    map[string]*discoveredFamily{},
		kindOwner:     map[string]string{},
		kindFamily:    map[string]*discoveredFamily{},
		evidenceByDir: map[string]map[string]sourcePos{},
		knownKinds:    map[string]bool{},
	}
	var diags diagnostics
	ctx.indexFamilies(&diags)
	ctx.symbols = loadKindSymbols(repoRoot, ctx.knownKinds)
	ctx.validateFamilies(&diags)
	bound := ctx.validateBindings(&diags)
	ctx.validateKindStates(bound, &diags)
	return diags
}

// indexFamilies builds ownership indexes and rejects duplicate family
// IDs and duplicate kind ownership.
func (c *validationContext) indexFamilies(diags *diagnostics) {
	for i := range c.families {
		fam := &c.families[i]
		id := strings.TrimSpace(fam.family.ID)
		if id == "" {
			diags.addf(fam.pos, "family declaration has a blank ID; ACTION: set ID to the registry family identifier")
			continue
		}
		if prev, dup := c.familyByID[id]; dup {
			diags.addf(fam.pos, "duplicate family %q (first declared at %s); ACTION: keep one contract.Declare for the family", id, prev.pos)
			continue
		}
		c.familyByID[id] = fam
		for _, kind := range fam.family.Kinds {
			name := strings.TrimSpace(kind.Kind)
			kindPos := fam.kindPos[name]
			if name == "" {
				diags.addf(fam.pos, "family %q declares a kind with a blank name; ACTION: set Kind to the wire fact kind", id)
				continue
			}
			if owner, dup := c.kindOwner[name]; dup {
				diags.addf(kindPos, "kind %q is owned by both family %q (declared at %s) and family %q; ACTION: keep the kind in exactly one family", name, owner, c.familyByID[owner].kindPos[name], id)
				continue
			}
			c.kindOwner[name] = id
			c.kindFamily[name] = fam
			c.knownKinds[name] = true
		}
	}
}

// validateFamilies checks per-family metadata, directory colocation,
// and producer subsets.
func (c *validationContext) validateFamilies(diags *diagnostics) {
	for i := range c.families {
		fam := &c.families[i]
		f := fam.family
		c.validateFamilyMetadata(fam, diags)
		// Directory colocation: the declaration file must sit in FactDir.
		declDir := slashDir(fam.pos.file)
		if strings.TrimSpace(f.FactDir) != "" && declDir != strings.Trim(f.FactDir, "/") {
			diags.addf(fam.pos, "family %q declares FactDir %q but is declared in %q; ACTION: move the declaration into %s or fix FactDir", f.ID, f.FactDir, declDir, f.FactDir)
		}
		if strings.TrimSpace(f.FactDir) != "" {
			if info, err := os.Stat(filepath.Join(c.repoRoot, filepath.FromSlash(f.FactDir))); err != nil || !info.IsDir() {
				diags.addf(fam.pos, "family %q FactDir %q is not an existing directory; ACTION: point FactDir at the fact-definition directory", f.ID, f.FactDir)
			}
		}
		c.validateProducers(fam, diags)
		c.validateKindOverrides(fam, diags)
	}
}

// validateFamilyMetadata rejects blank required metadata. Legacy
// truth-profile and admission rules are re-checked by the registry
// pipeline over the derived spec; this pass covers the contract-only
// fields that pipeline cannot see.
func (c *validationContext) validateFamilyMetadata(fam *discoveredFamily, diags *diagnostics) {
	f := fam.family
	required := map[string]string{
		"FactDir":         f.FactDir,
		"OwnerRepository": f.OwnerRepository,
		"LifecycleOwner":  f.LifecycleOwner,
		"ReducerDomain":   f.ReducerDomain,
		"ProjectionHook":  f.ProjectionHook,
		"AdmissionHook":   f.AdmissionHook,
		"ReadSurface":     f.ReadSurface,
		"TruthProfile":    f.TruthProfile,
	}
	if !f.AdmissionExempt {
		required["SchemaVersion"] = f.SchemaVersion
	} else if strings.TrimSpace(f.SchemaVersion) != "" {
		diags.addf(fam.pos, "admission-exempt family %q must not set SchemaVersion; ACTION: clear SchemaVersion (exempt kinds stay outside version admission)", f.ID)
	}
	for _, field := range sortedKeys(required) {
		if strings.TrimSpace(required[field]) == "" {
			diags.addf(fam.pos, "family %q is missing required metadata %s; ACTION: set %s in the declaration", f.ID, field, field)
		}
	}
	if !safePathComponent(f.ID) {
		diags.addf(fam.pos, "family ID %q is not a safe single path component; ACTION: use lowercase letters, digits, underscores, dots, or hyphens with no separators", f.ID)
	}
	if len(f.Kinds) == 0 {
		diags.addf(fam.pos, "family %q declares no kinds; ACTION: list every owned kind in Kinds", f.ID)
	}
}

// validateProducers checks producer identity, authority mode, subset
// validity, and full kind coverage.
func (c *validationContext) validateProducers(fam *discoveredFamily, diags *diagnostics) {
	f := fam.family
	owned := map[string]bool{}
	for _, kind := range f.Kinds {
		owned[kind.Kind] = true
	}
	covered := map[string]bool{}
	seenProducer := map[string]sourcePos{}
	for i, producer := range f.Producers {
		pos := fam.pos
		if i < len(fam.prodPos) {
			pos = fam.prodPos[i]
		}
		for field, value := range map[string]string{"ID": producer.ID, "Repository": producer.Repository, "Path": producer.Path} {
			if strings.TrimSpace(value) == "" {
				diags.addf(pos, "family %q producer is missing %s; ACTION: set the producer's %s", f.ID, field, field)
			}
		}
		if prev, dup := seenProducer[producer.ID]; dup && producer.ID != "" {
			diags.addf(pos, "family %q declares producer %q twice (first at %s); ACTION: merge the entries into one producer with the full emitted-kind subset", f.ID, producer.ID, prev)
		}
		seenProducer[producer.ID] = pos
		switch producer.AuthorityMode {
		case contract.AuthorityCoreOwned, contract.AuthorityCoreIssuedGrant:
		default:
			diags.addf(pos, "family %q producer %q has unsupported AuthorityMode %q; ACTION: use core_owned or core_issued_grant", f.ID, producer.ID, producer.AuthorityMode)
		}
		if producer.PlannedRepository != "" && producer.PlannedRepository == producer.Repository {
			diags.addf(pos, "family %q producer %q plans to move to its current repository %q; ACTION: clear PlannedRepository when the producer stays", f.ID, producer.ID, producer.Repository)
		}
		if len(producer.Kinds) == 0 {
			diags.addf(pos, "family %q producer %q emits no kinds; ACTION: list the exact emitted-kind subset or remove the producer", f.ID, producer.ID)
		}
		seenKind := map[string]bool{}
		for _, kind := range producer.Kinds {
			if seenKind[kind] {
				diags.addf(pos, "family %q producer %q lists kind %q twice; ACTION: deduplicate the subset", f.ID, producer.ID, kind)
			}
			seenKind[kind] = true
			if !owned[kind] {
				diags.addf(pos, "family %q producer %q emits kind %q which the family does not own; ACTION: move the kind to its owning family's producer subset", f.ID, producer.ID, kind)
				continue
			}
			covered[kind] = true
		}
	}
	for _, kind := range f.Kinds {
		if !covered[kind.Kind] {
			diags.addf(fam.kindPos[kind.Kind], "family %q kind %q has no declared producer; ACTION: add the emitting producer and kind subset", f.ID, kind.Kind)
		}
	}
}

// validateKindOverrides checks per-kind schema versions on exempt
// families (other override checks run in the derived registry
// pipeline, which owns the legacy error wording).
func (c *validationContext) validateKindOverrides(fam *discoveredFamily, diags *diagnostics) {
	if !fam.family.AdmissionExempt {
		return
	}
	for _, kind := range fam.family.Kinds {
		if strings.TrimSpace(kind.SchemaVersion) != "" {
			diags.addf(fam.kindPos[kind.Kind], "admission-exempt family %q kind %q must not set SchemaVersion; ACTION: clear the per-kind SchemaVersion", fam.family.ID, kind.Kind)
		}
	}
}

// validateBindings checks binding identity, colocation, symbol
// existence, subset validity, duplicate edges, and per-kind reducer
// evidence. It returns the set of kinds covered by valid bindings.
func (c *validationContext) validateBindings(diags *diagnostics) map[string][]discoveredBinding {
	bound := map[string][]discoveredBinding{}
	seenID := map[string]sourcePos{}
	for _, bind := range c.bindings {
		b := bind.binding
		valid := true
		dirPart, namePart, wellFormed := splitBindingID(b.ID)
		if !wellFormed {
			diags.addf(bind.pos, "binding ID %q is not in directory/name form; ACTION: use \"<reducer-dir-base>/<name>\"", b.ID)
			valid = false
		} else {
			if !safePathComponent(dirPart) || !safePathComponent(namePart) {
				diags.addf(bind.pos, "binding ID %q has an unsafe path component; ACTION: use lowercase letters, digits, underscores, dots, or hyphens in both components", b.ID)
				valid = false
			}
			if base := pathBase(b.ReducerDir); dirPart != base {
				diags.addf(bind.pos, "binding %q directory component %q does not match ReducerDir base %q; ACTION: rename the binding ID or fix ReducerDir", b.ID, dirPart, base)
				valid = false
			}
		}
		if prev, dup := seenID[b.ID]; dup {
			diags.addf(bind.pos, "duplicate binding ID %q (first declared at %s); ACTION: give each binding a unique ID", b.ID, prev)
			valid = false
		}
		seenID[b.ID] = bind.pos
		declDir := slashDir(bind.pos.file)
		if declDir != strings.Trim(b.ReducerDir, "/") {
			diags.addf(bind.pos, "binding %q declares ReducerDir %q but is declared in %q; ACTION: move the binding beside its implementation in %s or fix ReducerDir", b.ID, b.ReducerDir, declDir, b.ReducerDir)
			valid = false
		}
		fam, known := c.familyByID[b.FamilyID]
		if !known {
			diags.addf(bind.pos, "binding %q references unknown family %q; ACTION: declare the family or fix FamilyID", b.ID, b.FamilyID)
			continue
		}
		dirAbs := filepath.Join(c.repoRoot, filepath.FromSlash(b.ReducerDir))
		if strings.TrimSpace(b.Symbol) == "" {
			diags.addf(bind.pos, "binding %q has a blank Symbol; ACTION: name the implementing top-level symbol in %s", b.ID, b.ReducerDir)
			valid = false
		} else if !packageSymbols(dirAbs)[b.Symbol] {
			diags.addf(bind.pos, "binding %q symbol %q does not exist in %s; ACTION: name a real top-level declaration in that package", b.ID, b.Symbol, b.ReducerDir)
			valid = false
		}
		owned := map[string]bool{}
		for _, kind := range fam.family.Kinds {
			owned[kind.Kind] = true
		}
		if len(b.Kinds) == 0 {
			diags.addf(bind.pos, "binding %q consumes no kinds; ACTION: list the exact consumed-kind subset or remove the binding", b.ID)
			valid = false
		}
		evidence := c.evidenceFor(b.ReducerDir)
		seenKind := map[string]bool{}
		for _, kind := range b.Kinds {
			if seenKind[kind] {
				diags.addf(bind.pos, "binding %q lists kind %q twice (duplicate binding edge); ACTION: deduplicate Kinds", b.ID, kind)
				valid = false
			}
			seenKind[kind] = true
			if !owned[kind] {
				diags.addf(bind.pos, "binding %q consumes kind %q which family %q does not own (family declared at %s); ACTION: bind kinds in their owning family", b.ID, kind, b.FamilyID, fam.pos)
				valid = false
				continue
			}
			if _, ok := evidence[kind]; !ok {
				diags.addf(bind.pos, "binding %q claims kind %q but %s has no reducer-consumption evidence for it (no comparison, switch case, loader call, or decode seam); ACTION: remove the kind from the binding or point the binding at the consuming package", b.ID, kind, b.ReducerDir)
				valid = false
			}
		}
		if valid {
			for _, kind := range b.Kinds {
				bound[kind] = append(bound[kind], bind)
			}
		}
	}
	return bound
}

// validateKindStates enforces exactly one reducer-consumption state
// per kind and detects stale no-reducer disclosures.
func (c *validationContext) validateKindStates(bound map[string][]discoveredBinding, diags *diagnostics) {
	reducerRoot := filepath.Join(c.repoRoot, "go", "internal", "reducer")
	var treeEvidence map[string]sourcePos
	for name, fam := range c.kindFamily {
		var declared contract.Kind
		for _, kind := range fam.family.Kinds {
			if kind.Kind == name {
				declared = kind
			}
		}
		pos := fam.kindPos[name]
		bindings := bound[name]
		hasDisposition := declared.Disposition != "" || declared.NoReducerReason != "" || declared.NoReducerEvidence != ""
		if len(bindings) > 0 {
			if hasDisposition {
				diags.addf(pos, "kind %q has both reducer bindings (for example %q at %s) and a no-reducer disposition; ACTION: keep exactly one state — clear Disposition/NoReducerReason/NoReducerEvidence for a bound kind", name, bindings[0].binding.ID, bindings[0].pos)
			}
			continue
		}
		if !hasDisposition {
			diags.addf(pos, "kind %q has no reducer binding and no no-reducer disposition; ACTION: add a contract.Bind beside the consuming reducer implementation, or set Disposition with NoReducerReason and NoReducerEvidence", name)
			continue
		}
		switch declared.Disposition {
		case contract.DispositionReadOnly, contract.DispositionProjectorOnly, contract.DispositionCollectorOnly, contract.DispositionUnconsumed:
		default:
			diags.addf(pos, "kind %q has unsupported Disposition %q; ACTION: use read_only, projector_only, collector_only, or unconsumed", name, declared.Disposition)
		}
		if strings.TrimSpace(declared.NoReducerReason) == "" {
			diags.addf(pos, "kind %q disposition %q has a blank NoReducerReason; ACTION: explain why no reducer consumes the kind", name, declared.Disposition)
		}
		if strings.TrimSpace(declared.NoReducerEvidence) == "" {
			diags.addf(pos, "kind %q disposition %q has a blank NoReducerEvidence; ACTION: cite the code anchor, ledger entry, or search that proves no reducer consumes it", name, declared.Disposition)
		}
		if treeEvidence == nil {
			treeEvidence = c.treeEvidence(reducerRoot)
		}
		if evidencePos, stale := treeEvidence[name]; stale {
			diags.addf(pos, "kind %q is disclosed as having no reducer consumer but reducer evidence exists at %s (stale disclosure); ACTION: declare a contract.Bind for the consuming package and clear the disposition", name, evidencePos)
		}
	}
}

// evidenceFor returns cached reducer evidence for one repo-relative
// directory.
func (c *validationContext) evidenceFor(relDir string) map[string]sourcePos {
	if cached, ok := c.evidenceByDir[relDir]; ok {
		return cached
	}
	evidence := evidenceInDir(c.repoRoot, filepath.Join(c.repoRoot, filepath.FromSlash(relDir)), c.symbols, c.knownKinds)
	c.evidenceByDir[relDir] = evidence
	return evidence
}

// treeEvidence merges evidence across every package directory under a
// tree root.
func (c *validationContext) treeEvidence(rootAbs string) map[string]sourcePos {
	merged := map[string]sourcePos{}
	_ = filepath.WalkDir(rootAbs, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil //nolint:nilerr // unreadable dirs simply contribute no evidence.
		}
		if entry.Name() == "testdata" {
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(c.repoRoot, path)
		if relErr != nil {
			return nil
		}
		for kind, pos := range c.evidenceFor(filepath.ToSlash(rel)) {
			if _, seen := merged[kind]; !seen {
				merged[kind] = pos
			}
		}
		return nil
	})
	return merged
}

// deriveSpec converts declarations into the legacy registry spec so
// the existing registry pipeline (live-kind/version cross-checks,
// metadata rules, payload-schema containment, Go/Markdown rendering)
// keeps its exact current semantics.
func deriveSpec(families []discoveredFamily) specFile {
	spec := specFile{Version: supportedSpecVersion, Families: map[string]familySpec{}}
	for _, fam := range families {
		f := fam.family
		derived := familySpec{
			LifecycleOwner:         f.LifecycleOwner,
			SchemaVersion:          f.SchemaVersion,
			ReducerDomain:          f.ReducerDomain,
			ReducerDir:             f.ReducerDir,
			ProjectionHook:         f.ProjectionHook,
			AdmissionHook:          f.AdmissionHook,
			ReadSurface:            f.ReadSurface,
			TruthProfile:           f.TruthProfile,
			PolicyGate:             f.PolicyGate,
			ProviderKeyIndependent: f.ProviderKeyIndependent,
			PayloadSchema:          f.PayloadSchema,
			DeprecatedIn:           f.DeprecatedIn,
			RemovedIn:              f.RemovedIn,
			AdmissionExempt:        f.AdmissionExempt,
		}
		for _, kind := range f.Kinds {
			derived.Kinds = append(derived.Kinds, kind.Kind)
			setOverride := func(m *map[string]string, value string) {
				if value == "" {
					return
				}
				if *m == nil {
					*m = map[string]string{}
				}
				(*m)[kind.Kind] = value
			}
			setOverride(&derived.SchemaVersionOverride, kind.SchemaVersion)
			setOverride(&derived.ReadSurfaceOverrides, kind.ReadSurface)
			setOverride(&derived.PayloadSchemaOverrides, kind.PayloadSchema)
			setOverride(&derived.DeprecatedInOverrides, kind.DeprecatedIn)
			setOverride(&derived.RemovedInOverrides, kind.RemovedIn)
		}
		sort.Strings(derived.Kinds)
		spec.Families[f.ID] = derived
	}
	return spec
}

func slashDir(file string) string {
	if idx := strings.LastIndex(file, "/"); idx >= 0 {
		return file[:idx]
	}
	return "."
}

func pathBase(p string) string {
	p = strings.Trim(p, "/")
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

func splitBindingID(id string) (string, string, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// safePathComponent reports whether s is a single safe generated-path
// component: non-empty, no separators or traversal, restricted
// character set, not a dot component.
func safePathComponent(s string) bool {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\") {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return !strings.HasPrefix(s, ".")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
