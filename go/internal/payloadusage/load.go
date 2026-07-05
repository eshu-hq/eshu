// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// MarshalIndent renders m as indented JSON, terminated by a trailing
// newline, in the exact shape both the CLI's generate mode and this
// package's own idempotency test use. The manifest is NOT a committed
// artifact — the gate recomputes it fresh each run (see README.md's design
// note) — but generate mode can still write it to a file for inspection.
// Keeping one encoding call in the library (rather than duplicating
// json.MarshalIndent at each call site) guarantees the CLI's -out output and
// the idempotency test's byte-comparison agree on formatting.
func MarshalIndent(m Manifest) (string, error) {
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("payloadusage: encode manifest: %w", err)
	}
	return string(encoded) + "\n", nil
}

// Paths locates every filesystem input the manifest derivation reads. Every
// field defaults relative to RepoRoot when left empty; see ResolvePaths.
type Paths struct {
	// RepoRoot is the repository root used to resolve every other empty
	// field. Defaults to "." when empty.
	RepoRoot string
	// ReducerDir is go/internal/reducer — both the source of the decode
	// seam files (DecodeFiles) and the handler files ScanDecodeUsage walks.
	ReducerDir string
	// DecodeFile, when set, restricts seam parsing to that single file. It is
	// the CLI's -decode-file override; leaving it empty is the normal path and
	// lets DecodeFiles resolve to the per-family glob below. It is retained for
	// backward compatibility with callers that pin one file.
	DecodeFile string
	// DecodeFiles is the set of reducer decode-seam files ParseDecodeSeams
	// reads. Families split their decode wrappers into per-family files
	// (factschema_decode.go, factschema_decode_incident.go, ...) as the
	// 500-line cap forces a split, so the seam source is a GLOB, not a single
	// file. When empty, ResolvePaths fills it from DecodeFile (if set) or from
	// filepath.Glob(ReducerDir/"factschema_decode*.go"). A gate that read only
	// the single factschema_decode.go would silently miss a family whose
	// wrappers live in a split file — the exact false-green this glob closes.
	DecodeFiles []string
	// SchemaDir is sdk/go/factschema/schema, the checked-in JSON Schemas
	// LoadDeclaredFieldsFromSchemas reads.
	SchemaDir string
	// AWSStructDir is sdk/go/factschema/aws/v1.
	AWSStructDir string
	// IAMStructDir is sdk/go/factschema/iam/v1.
	IAMStructDir string
	// IncidentStructDir is sdk/go/factschema/incident/v1.
	IncidentStructDir string
	// GCPStructDir is sdk/go/factschema/gcp/v1.
	GCPStructDir string
	// AzureStructDir is sdk/go/factschema/azure/v1.
	AzureStructDir string
	// KubernetesLiveStructDir is sdk/go/factschema/kuberneteslive/v1.
	KubernetesLiveStructDir string
}

// ResolvePaths fills every empty DIRECTORY/RepoRoot field of p with its default
// relative to RepoRoot (defaulting RepoRoot itself to "." when empty) and
// returns the resolved copy. p is not mutated.
//
// It deliberately does NOT resolve DecodeFile or DecodeFiles: the decode-seam
// source is a glob whose resolution can fail, and ResolvePaths returns no error.
// resolveDecodeFiles (called from Load) fills them — from an explicit
// DecodeFile/DecodeFiles override, or by globbing factschema_decode*.go under
// ReducerDir. So a caller inspecting the returned Paths sees resolved
// directories but empty DecodeFile/DecodeFiles unless it set them.
func ResolvePaths(p Paths) Paths {
	resolved := p
	resolved.RepoRoot = strings.TrimSpace(resolved.RepoRoot)
	if resolved.RepoRoot == "" {
		resolved.RepoRoot = "."
	}
	if strings.TrimSpace(resolved.ReducerDir) == "" {
		resolved.ReducerDir = filepath.Join(resolved.RepoRoot, "go", "internal", "reducer")
	}
	if strings.TrimSpace(resolved.SchemaDir) == "" {
		resolved.SchemaDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "schema")
	}
	if strings.TrimSpace(resolved.AWSStructDir) == "" {
		resolved.AWSStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "aws", "v1")
	}
	if strings.TrimSpace(resolved.IAMStructDir) == "" {
		resolved.IAMStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "iam", "v1")
	}
	if strings.TrimSpace(resolved.IncidentStructDir) == "" {
		resolved.IncidentStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "incident", "v1")
	}
	if strings.TrimSpace(resolved.GCPStructDir) == "" {
		resolved.GCPStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "gcp", "v1")
	}
	if strings.TrimSpace(resolved.AzureStructDir) == "" {
		resolved.AzureStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "azure", "v1")
	}
	if strings.TrimSpace(resolved.KubernetesLiveStructDir) == "" {
		resolved.KubernetesLiveStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "kuberneteslive", "v1")
	}
	// DecodeFile / DecodeFiles are intentionally NOT defaulted here: the glob
	// path can fail, and ResolvePaths returns no error. resolveDecodeFiles (from
	// Load) fills them — from an explicit DecodeFile/DecodeFiles override, or by
	// globbing every factschema_decode*.go under ReducerDir. Defaulting
	// DecodeFile to the single legacy file here would defeat the glob and
	// silently drop the per-family split files (the false-green this closes).
	return resolved
}

// resolveDecodeFiles returns the set of reducer decode-seam files to parse.
// When DecodeFiles is already set it is returned as-is; when only DecodeFile is
// set (the CLI -decode-file override or the ResolvePaths legacy default) that
// one file is used; otherwise it globs every factschema_decode*.go under
// ReducerDir so a family whose wrappers live in a split file is covered. An
// empty glob is an error rather than a silent zero-seam manifest.
func resolveDecodeFiles(p Paths) ([]string, error) {
	if len(p.DecodeFiles) > 0 {
		return p.DecodeFiles, nil
	}
	if strings.TrimSpace(p.DecodeFile) != "" {
		return []string{p.DecodeFile}, nil
	}
	pattern := filepath.Join(p.ReducerDir, "factschema_decode*.go")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("payloadusage: glob decode seam files %s: %w", pattern, err)
	}
	// Exclude _test.go files: the glob pattern would otherwise match a
	// factschema_decode*_test.go if one were added, and a test-only helper is
	// not a production seam.
	files := matches[:0]
	for _, m := range matches {
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		files = append(files, m)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("payloadusage: no decode seam files matched %s", pattern)
	}
	sort.Strings(files)
	return files, nil
}

// parseDecodeSeamsFiles parses every file in files and returns the merged seam
// set, sorted by FuncName. A function name appearing in more than one file is a
// programming error (each decode<Kind> wrapper is defined once), reported rather
// than silently deduplicated so a copy-paste across split files is caught.
func parseDecodeSeamsFiles(files []string) ([]DecodeSeam, error) {
	seen := map[string]string{} // FuncName -> file it was first seen in
	var merged []DecodeSeam
	for _, file := range files {
		seams, err := ParseDecodeSeams(file)
		if err != nil {
			return nil, err
		}
		for _, s := range seams {
			if prior, dup := seen[s.FuncName]; dup {
				return nil, fmt.Errorf(
					"payloadusage: decode seam %s defined in both %s and %s; each decode<Kind> wrapper must be defined once",
					s.FuncName, prior, file,
				)
			}
			seen[s.FuncName] = file
			merged = append(merged, s)
		}
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].FuncName < merged[j].FuncName })
	return merged, nil
}

// Load runs the full derivation pipeline against p (auto-resolving empty
// fields via ResolvePaths): parse the decode seams, parse the aws/v1, iam/v1,
// incident/v1, gcp/v1, azure/v1, and kuberneteslive/v1 typed struct shapes,
// scan the reducer directory's handler files for field usage, and join the
// three into a Manifest.
//
// It returns an error if any seam's fact kind has no schema-file mapping
// (UnmappedSeamFactKinds) or if any seam's struct type was not found in the
// parsed shapes (a wiring gap between DecodeSeam and the struct dirs the
// caller supplied) — both are startup-time configuration errors, not gate
// findings, so they fail loudly rather than silently shrinking the manifest.
func Load(p Paths) (Manifest, error) {
	resolved := ResolvePaths(p)

	decodeFiles, err := resolveDecodeFiles(resolved)
	if err != nil {
		return Manifest{}, err
	}
	seams, err := parseDecodeSeamsFiles(decodeFiles)
	if err != nil {
		return Manifest{}, err
	}
	if len(seams) == 0 {
		return Manifest{}, fmt.Errorf("payloadusage: no decode seams found in %s", strings.Join(decodeFiles, ", "))
	}
	if missing := UnmappedSeamFactKinds(seams); len(missing) > 0 {
		return Manifest{}, fmt.Errorf(
			"payloadusage: %d decode seam(s) have no schema-file mapping in factKindSchemaFile: %s — add the mapping before this kind can be gated",
			len(missing), JoinSorted(missing),
		)
	}

	awsShapes, err := ParseStructShapes(resolved.AWSStructDir, "awsv1")
	if err != nil {
		return Manifest{}, err
	}
	iamShapes, err := ParseStructShapes(resolved.IAMStructDir, "iamv1")
	if err != nil {
		return Manifest{}, err
	}
	incidentShapes, err := ParseStructShapes(resolved.IncidentStructDir, "incidentv1")
	if err != nil {
		return Manifest{}, err
	}
	gcpShapes, err := ParseStructShapes(resolved.GCPStructDir, "gcpv1")
	if err != nil {
		return Manifest{}, err
	}
	azureShapes, err := ParseStructShapes(resolved.AzureStructDir, "azurev1")
	if err != nil {
		return Manifest{}, err
	}
	kubernetesLiveShapes, err := ParseStructShapes(resolved.KubernetesLiveStructDir, "kuberneteslivev1")
	if err != nil {
		return Manifest{}, err
	}
	shapes := make(map[string]StructShape, len(awsShapes)+len(iamShapes)+len(incidentShapes)+len(gcpShapes)+len(azureShapes)+len(kubernetesLiveShapes))
	for k, v := range awsShapes {
		shapes[k] = v
	}
	for k, v := range iamShapes {
		shapes[k] = v
	}
	for k, v := range incidentShapes {
		shapes[k] = v
	}
	for k, v := range gcpShapes {
		shapes[k] = v
	}
	for k, v := range azureShapes {
		shapes[k] = v
	}
	for k, v := range kubernetesLiveShapes {
		shapes[k] = v
	}

	usage, err := ScanDecodeUsage(resolved.ReducerDir, seams)
	if err != nil {
		return Manifest{}, err
	}

	manifest := BuildManifest(seams, shapes, usage)
	if err := verifyEverySeamProduced(seams, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// verifyEverySeamProduced fails loudly when a discovered DecodeSeam's
// QualifiedStruct has no entry in the parsed struct shapes (e.g. a decode
// function was added but its struct lives in a directory Load was not told
// to parse). Without this check BuildManifest's silent skip would let a
// newly migrated kind vanish from the manifest without any signal.
func verifyEverySeamProduced(seams []DecodeSeam, manifest Manifest) error {
	produced := make(map[string]struct{}, len(manifest.Kinds))
	for _, k := range manifest.Kinds {
		produced[k.DecodeFunc] = struct{}{}
	}
	var missing []string
	for _, s := range seams {
		if _, ok := produced[s.FuncName]; !ok {
			missing = append(missing, fmt.Sprintf("%s (struct %s)", s.FuncName, s.QualifiedStruct()))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf(
			"payloadusage: %d decode seam(s) produced no manifest entry, likely because their struct dir was not parsed: %s",
			len(missing), strings.Join(missing, ", "),
		)
	}
	return nil
}

// Gate runs Load against p and compares every used field against the
// declared JSON Schema field set at p's (resolved) SchemaDir, returning the
// violations found. An empty result means every reducer handler reads only
// fields its fact kind's schema declares.
func Gate(p Paths) (Manifest, []Violation, error) {
	manifest, err := Load(p)
	if err != nil {
		return Manifest{}, nil, err
	}
	declared, err := LoadDeclaredFieldsFromSchemas(ResolvePaths(p).SchemaDir)
	if err != nil {
		return Manifest{}, nil, err
	}
	return manifest, CheckManifest(manifest, declared), nil
}
