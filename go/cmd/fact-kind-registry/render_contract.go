// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// contractsDirName is the repo-relative directory holding the generated
// fact-family contract shards and catalog pages.
const contractsDirName = "specs/fact-kind-registry-contracts.d"

// maxGeneratedLines is the exclusive line ceiling for every generated
// output, matching the repository 500-line file cap: an output at or
// over the ceiling is rejected instead of written.
const maxGeneratedLines = 500

// outputFile is one generated file in repo-relative slash form.
type outputFile struct {
	rel  string
	data []byte
}

// yamlRegistry is the generated specs/fact-kind-registry.v1.yaml shape.
// Field order here is the emitted key order; values keep their legacy
// semantics exactly.
type yamlRegistry struct {
	Version  string                `yaml:"version"`
	Families map[string]yamlFamily `yaml:"families"`
}

// yamlFamily is one legacy registry family in generated YAML form.
type yamlFamily struct {
	LifecycleOwner         string            `yaml:"lifecycle_owner"`
	SchemaVersion          string            `yaml:"schema_version,omitempty"`
	SchemaVersionOverrides map[string]string `yaml:"schema_version_overrides,omitempty,flow"`
	AdmissionExempt        bool              `yaml:"admission_exempt,omitempty"`
	ReducerDomain          string            `yaml:"reducer_domain"`
	ReducerDir             string            `yaml:"reducer_dir,omitempty"`
	ProjectionHook         string            `yaml:"projection_hook"`
	AdmissionHook          string            `yaml:"admission_hook"`
	ReadSurface            string            `yaml:"read_surface"`
	ReadSurfaceOverrides   map[string]string `yaml:"read_surface_overrides,omitempty,flow"`
	TruthProfile           string            `yaml:"truth_profile"`
	PolicyGate             string            `yaml:"policy_gate,omitempty"`
	ProviderKeyIndependent bool              `yaml:"provider_key_independent"`
	PayloadSchema          string            `yaml:"payload_schema,omitempty"`
	PayloadSchemaOverrides map[string]string `yaml:"payload_schema_overrides,omitempty,flow"`
	DeprecatedIn           string            `yaml:"deprecated_in,omitempty"`
	DeprecatedInOverrides  map[string]string `yaml:"deprecated_in_overrides,omitempty,flow"`
	RemovedIn              string            `yaml:"removed_in,omitempty"`
	RemovedInOverrides     map[string]string `yaml:"removed_in_overrides,omitempty,flow"`
	Kinds                  []string          `yaml:"kinds,flow"`
}

const registryYAMLHeader = `# Generated fact-kind registry contract v1. DO NOT EDIT.
# Source of truth: typed fact-family declarations in Go source
# (contract.Declare beside fact definitions, contract.Bind beside
# reducer implementations). Regenerate with
# scripts/generate-fact-kind-registry.sh. Producer, disposition,
# evidence, and reducer-binding metadata lives in the generated
# shards under specs/fact-kind-registry-contracts.d/. This file keeps
# the legacy v1.1 family, kind, schema, admission, and reducer_dir
# semantics for existing consumers.
`

// renderRegistryYAML renders the compact generated v1 registry from
// the derived spec.
func renderRegistryYAML(spec specFile) ([]byte, error) {
	out := yamlRegistry{Version: spec.Version, Families: map[string]yamlFamily{}}
	for id, fam := range spec.Families {
		out.Families[id] = yamlFamily{
			LifecycleOwner:         fam.LifecycleOwner,
			SchemaVersion:          fam.SchemaVersion,
			SchemaVersionOverrides: fam.SchemaVersionOverride,
			AdmissionExempt:        fam.AdmissionExempt,
			ReducerDomain:          fam.ReducerDomain,
			ReducerDir:             fam.ReducerDir,
			ProjectionHook:         fam.ProjectionHook,
			AdmissionHook:          fam.AdmissionHook,
			ReadSurface:            fam.ReadSurface,
			ReadSurfaceOverrides:   fam.ReadSurfaceOverrides,
			TruthProfile:           fam.TruthProfile,
			PolicyGate:             fam.PolicyGate,
			ProviderKeyIndependent: fam.ProviderKeyIndependent,
			PayloadSchema:          fam.PayloadSchema,
			PayloadSchemaOverrides: fam.PayloadSchemaOverrides,
			DeprecatedIn:           fam.DeprecatedIn,
			DeprecatedInOverrides:  fam.DeprecatedInOverrides,
			RemovedIn:              fam.RemovedIn,
			RemovedInOverrides:     fam.RemovedInOverrides,
			Kinds:                  sortedUnique(fam.Kinds),
		}
	}
	data, err := yaml.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("render registry YAML: %w", err)
	}
	return append([]byte(registryYAMLHeader), data...), nil
}

// producerShard is one producer entry in a generated family shard.
type producerShard struct {
	ID                string   `yaml:"id"`
	Repository        string   `yaml:"repository"`
	Path              string   `yaml:"path"`
	PlannedRepository string   `yaml:"planned_repository,omitempty"`
	AuthorityMode     string   `yaml:"authority_mode"`
	Kinds             []string `yaml:"kinds"`
}

// familyShard is the generated families/<family-id>.yaml shape.
type familyShard struct {
	FamilyID               string          `yaml:"family_id"`
	FactDir                string          `yaml:"fact_dir"`
	OwnerRepository        string          `yaml:"owner_repository"`
	LifecycleOwner         string          `yaml:"lifecycle_owner"`
	SchemaVersion          string          `yaml:"schema_version,omitempty"`
	AdmissionExempt        bool            `yaml:"admission_exempt,omitempty"`
	ReducerDomain          string          `yaml:"reducer_domain"`
	ReducerDir             string          `yaml:"reducer_dir,omitempty"`
	ProjectionHook         string          `yaml:"projection_hook"`
	AdmissionHook          string          `yaml:"admission_hook"`
	ReadSurface            string          `yaml:"read_surface"`
	TruthProfile           string          `yaml:"truth_profile"`
	PolicyGate             string          `yaml:"policy_gate,omitempty"`
	ProviderKeyIndependent bool            `yaml:"provider_key_independent"`
	PayloadSchema          string          `yaml:"payload_schema,omitempty"`
	DeprecatedIn           string          `yaml:"deprecated_in,omitempty"`
	RemovedIn              string          `yaml:"removed_in,omitempty"`
	Declaration            string          `yaml:"declaration"`
	Kinds                  []string        `yaml:"kinds"`
	Producers              []producerShard `yaml:"producers"`
}

// kindShard is the generated kinds/<family-id>/<name>.yaml shape. The
// YAML always carries the full unchanged family_id and kind; only the
// filename applies the stutter-free prefix mapping.
type kindShard struct {
	FamilyID                string   `yaml:"family_id"`
	Kind                    string   `yaml:"kind"`
	SchemaVersion           string   `yaml:"schema_version"`
	ReadSurface             string   `yaml:"read_surface"`
	PayloadSchema           string   `yaml:"payload_schema"`
	PayloadSchemaRegistered bool     `yaml:"payload_schema_registered"`
	DeprecatedIn            string   `yaml:"deprecated_in,omitempty"`
	RemovedIn               string   `yaml:"removed_in,omitempty"`
	AdmissionExempt         bool     `yaml:"admission_exempt,omitempty"`
	Producers               []string `yaml:"producers"`
	ReducerBindings         []string `yaml:"reducer_bindings,omitempty"`
	Disposition             string   `yaml:"disposition,omitempty"`
	NoReducerReason         string   `yaml:"no_reducer_reason,omitempty"`
	NoReducerEvidence       string   `yaml:"no_reducer_evidence,omitempty"`
	Declaration             string   `yaml:"declaration"`
}

// bindingShard is the generated bindings/<directory>/<name>.yaml
// shape, preserving the full unchanged binding ID.
type bindingShard struct {
	ID          string   `yaml:"id"`
	FamilyID    string   `yaml:"family_id"`
	ReducerDir  string   `yaml:"reducer_dir"`
	Symbol      string   `yaml:"symbol"`
	Kinds       []string `yaml:"kinds"`
	Declaration string   `yaml:"declaration"`
}

const shardHeader = "# Generated by go/cmd/fact-kind-registry; DO NOT EDIT. Source: typed contract declarations in Go source.\n"

// kindShardPath maps a kind to its shard path: kinds/<family-id>/
// with an exact case-sensitive "<family-id>_" prefix stripped once
// from the filename.
func kindShardPath(familyID, kind string) string {
	name := strings.TrimPrefix(kind, familyID+"_")
	return contractsDirName + "/kinds/" + familyID + "/" + name + ".yaml"
}

// bindingShardPath maps a binding ID (directory/name) to its shard
// path, stripping an exact case-sensitive "<directory>_" prefix once
// from the filename component.
func bindingShardPath(id string) string {
	dir, name, _ := splitBindingID(id)
	name = strings.TrimPrefix(name, dir+"_")
	return contractsDirName + "/bindings/" + dir + "/" + name + ".yaml"
}

// renderShards renders every contracts.d output: family, kind, and
// binding shards plus one catalog page per family.
func renderShards(families []discoveredFamily, bindings []discoveredBinding, entries []registryEntry) ([]outputFile, error) {
	entryByKind := map[string]registryEntry{}
	for _, entry := range entries {
		entryByKind[entry.Kind] = entry
	}
	bindingsByKind := map[string][]string{}
	for _, bind := range bindings {
		for _, kind := range bind.binding.Kinds {
			bindingsByKind[kind] = append(bindingsByKind[kind], bind.binding.ID)
		}
	}
	var outputs []outputFile
	for _, fam := range families {
		f := fam.family
		shard := familyShard{
			FamilyID: f.ID, FactDir: f.FactDir, OwnerRepository: f.OwnerRepository,
			LifecycleOwner: f.LifecycleOwner, SchemaVersion: f.SchemaVersion,
			AdmissionExempt: f.AdmissionExempt, ReducerDomain: f.ReducerDomain,
			ReducerDir: f.ReducerDir, ProjectionHook: f.ProjectionHook,
			AdmissionHook: f.AdmissionHook, ReadSurface: f.ReadSurface,
			TruthProfile: f.TruthProfile, PolicyGate: f.PolicyGate,
			ProviderKeyIndependent: f.ProviderKeyIndependent,
			PayloadSchema:          f.PayloadSchema, DeprecatedIn: f.DeprecatedIn,
			RemovedIn: f.RemovedIn, Declaration: fam.pos.String(),
		}
		producersByKind := map[string][]string{}
		for _, producer := range f.Producers {
			kinds := append([]string(nil), producer.Kinds...)
			sort.Strings(kinds)
			shard.Producers = append(shard.Producers, producerShard{
				ID: producer.ID, Repository: producer.Repository, Path: producer.Path,
				PlannedRepository: producer.PlannedRepository,
				AuthorityMode:     string(producer.AuthorityMode), Kinds: kinds,
			})
			for _, kind := range kinds {
				producersByKind[kind] = append(producersByKind[kind], producer.ID)
			}
		}
		sort.Slice(shard.Producers, func(i, j int) bool { return shard.Producers[i].ID < shard.Producers[j].ID })
		kindNames := make([]string, 0, len(f.Kinds))
		for _, kind := range f.Kinds {
			kindNames = append(kindNames, kind.Kind)
		}
		sort.Strings(kindNames)
		shard.Kinds = kindNames
		data, err := marshalShard(shard)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, outputFile{rel: contractsDirName + "/families/" + f.ID + ".yaml", data: data})
		for _, kind := range f.Kinds {
			entry := entryByKind[kind.Kind]
			bindingIDs := append([]string(nil), bindingsByKind[kind.Kind]...)
			sort.Strings(bindingIDs)
			kindOut := kindShard{
				FamilyID: f.ID, Kind: kind.Kind, SchemaVersion: entry.SchemaVersion,
				ReadSurface: entry.ReadSurface, PayloadSchema: entry.PayloadSchema,
				PayloadSchemaRegistered: entry.PayloadSchema != "",
				DeprecatedIn:            entry.DeprecatedIn, RemovedIn: entry.RemovedIn,
				AdmissionExempt: entry.AdmissionExempt,
				Producers:       sortedUnique(producersByKind[kind.Kind]),
				ReducerBindings: bindingIDs, Disposition: string(kind.Disposition),
				NoReducerReason: kind.NoReducerReason, NoReducerEvidence: kind.NoReducerEvidence,
				Declaration: fam.kindPos[kind.Kind].String(),
			}
			data, err := marshalShard(kindOut)
			if err != nil {
				return nil, err
			}
			outputs = append(outputs, outputFile{rel: kindShardPath(f.ID, kind.Kind), data: data})
		}
		outputs = append(outputs, outputFile{
			rel:  contractsDirName + "/families/" + f.ID + ".md",
			data: renderCatalogPage(fam, entryByKind, bindingsByKind, producersByKind),
		})
	}
	for _, bind := range bindings {
		b := bind.binding
		kinds := append([]string(nil), b.Kinds...)
		sort.Strings(kinds)
		data, err := marshalShard(bindingShard{
			ID: b.ID, FamilyID: b.FamilyID, ReducerDir: b.ReducerDir,
			Symbol: b.Symbol, Kinds: kinds, Declaration: bind.pos.String(),
		})
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, outputFile{rel: bindingShardPath(b.ID), data: data})
	}
	sort.Slice(outputs, func(i, j int) bool { return outputs[i].rel < outputs[j].rel })
	return outputs, nil
}

func marshalShard(value any) ([]byte, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("render contract shard: %w", err)
	}
	return append([]byte(shardHeader), data...), nil
}

// validateOutputSet rejects unsafe, duplicate, case-colliding, or
// file/directory-conflicting generated paths and outputs at or over
// the line ceiling.
func validateOutputSet(outputs []outputFile) error {
	seen := map[string]string{}
	lowerSeen := map[string]string{}
	dirs := map[string]bool{}
	for _, out := range outputs {
		// Component safety constrains ID-derived shard paths. The
		// fixed legacy outputs (registry YAML, generated Go, registry
		// Markdown) keep their historical paths, including uppercase
		// FACT_KIND_REGISTRIES.md, and are exempt from the ID rules.
		if strings.HasPrefix(out.rel, contractsDirName+"/") {
			for _, component := range strings.Split(out.rel, "/") {
				if !safePathComponent(component) {
					return fmt.Errorf("generated output %q has unsafe path component %q; ACTION: fix the family, kind, or binding ID it derives from", out.rel, component)
				}
			}
		}
		if prev, dup := seen[out.rel]; dup {
			return fmt.Errorf("generated outputs %q and %q resolve to the same destination %q; ACTION: rename one source ID (no silent disambiguation)", prev, out.rel, out.rel)
		}
		seen[out.rel] = out.rel
		lower := strings.ToLower(out.rel)
		if prev, dup := lowerSeen[lower]; dup && prev != out.rel {
			return fmt.Errorf("generated outputs %q and %q collide case-insensitively; ACTION: rename one source ID", prev, out.rel)
		}
		lowerSeen[lower] = out.rel
		parts := strings.Split(out.rel, "/")
		for i := 1; i < len(parts); i++ {
			dirs[strings.Join(parts[:i], "/")] = true
		}
		if lines := bytes.Count(out.data, []byte("\n")); lines >= maxGeneratedLines {
			return fmt.Errorf("generated output %q has %d lines (limit under %d); ACTION: shard the output further instead of raising the cap", out.rel, lines, maxGeneratedLines)
		}
		if strings.HasPrefix(out.rel, contractsDirName+"/bindings/") && shardFilenameStutters(out.rel) {
			return fmt.Errorf("generated output %q repeats its directory name in the filename (filename-stutter gate); ACTION: rename the binding ID so its name component drops the directory word", out.rel)
		}
	}
	for _, out := range outputs {
		if dirs[out.rel] {
			return fmt.Errorf("generated output %q is both a file and a directory prefix of another output; ACTION: rename the conflicting source ID", out.rel)
		}
	}
	return nil
}

// shardFilenameStutters reports whether a generated path's filename
// stem contains its leaf directory name as a whole word (splitting on
// "_" and "-"), which the repository filename-stutter gate rejects
// for new paths. Symbol-derived binding names repeat their domain
// directory word naturally, so the generator fails at declaration
// time instead of emitting a shard the commit gate would refuse.
func shardFilenameStutters(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return false
	}
	dir := parts[len(parts)-2]
	stem := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
	for _, token := range strings.FieldsFunc(stem, func(r rune) bool { return r == '_' || r == '-' }) {
		if token == dir {
			return true
		}
	}
	return false
}

// applyOutputs writes or verifies the full generated output set. In
// check mode it reports stale, missing, and (under contractsDirName)
// extra files. In write mode it writes every output and removes extra
// files under contractsDirName so the committed set is exact.
func applyOutputs(repoRoot string, outputs []outputFile, check bool) error {
	generateHint := "scripts/generate-fact-kind-registry.sh"
	expected := map[string]bool{}
	for _, out := range outputs {
		expected[out.rel] = true
		abs := filepath.Join(repoRoot, filepath.FromSlash(out.rel))
		if check {
			got, err := os.ReadFile(abs) // #nosec G304 -- internally configured generated artifact.
			if err != nil {
				return fmt.Errorf("generated file %s is missing; run %s", out.rel, generateHint)
			}
			if !bytes.Equal(got, out.data) {
				return fmt.Errorf("generated file %s is stale; run %s", out.rel, generateHint)
			}
			continue
		}
		if err := writeFile(abs, out.data); err != nil {
			return err
		}
	}
	contractsAbs := filepath.Join(repoRoot, filepath.FromSlash(contractsDirName))
	var extras []string
	_ = filepath.WalkDir(contractsAbs, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil //nolint:nilerr // a missing contracts dir is handled by the missing-file check.
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !expected[rel] {
			extras = append(extras, rel)
		}
		return nil
	})
	sort.Strings(extras)
	if check {
		if len(extras) > 0 {
			return fmt.Errorf("generated output set has extra files not derived from declarations: %s; run %s", strings.Join(extras, ", "), generateHint)
		}
		return nil
	}
	for _, rel := range extras {
		if err := os.Remove(filepath.Join(repoRoot, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("remove extra generated file %s: %w", rel, err)
		}
	}
	// Prune now-empty shard directories, deepest first.
	var dirList []string
	_ = filepath.WalkDir(contractsAbs, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() && path != contractsAbs {
			dirList = append(dirList, path)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(dirList)))
	for _, dir := range dirList {
		_ = os.Remove(dir)
	}
	return nil
}
