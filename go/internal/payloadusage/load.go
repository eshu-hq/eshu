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
	// seam (DecodeFile) and the handler files ScanDecodeUsage walks.
	ReducerDir string
	// DecodeFile is go/internal/reducer/factschema_decode.go, the single
	// file ParseDecodeSeams reads.
	DecodeFile string
	// SchemaDir is sdk/go/factschema/schema, the checked-in JSON Schemas
	// LoadDeclaredFieldsFromSchemas reads.
	SchemaDir string
	// AWSStructDir is sdk/go/factschema/aws/v1.
	AWSStructDir string
	// IAMStructDir is sdk/go/factschema/iam/v1.
	IAMStructDir string
}

// ResolvePaths fills every empty field of p with its default relative to
// RepoRoot (defaulting RepoRoot itself to "." when empty) and returns the
// resolved copy. p is not mutated.
func ResolvePaths(p Paths) Paths {
	resolved := p
	resolved.RepoRoot = strings.TrimSpace(resolved.RepoRoot)
	if resolved.RepoRoot == "" {
		resolved.RepoRoot = "."
	}
	if strings.TrimSpace(resolved.ReducerDir) == "" {
		resolved.ReducerDir = filepath.Join(resolved.RepoRoot, "go", "internal", "reducer")
	}
	if strings.TrimSpace(resolved.DecodeFile) == "" {
		resolved.DecodeFile = filepath.Join(resolved.ReducerDir, "factschema_decode.go")
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
	return resolved
}

// Load runs the full derivation pipeline against p (auto-resolving empty
// fields via ResolvePaths): parse the decode seams, parse the aws/v1 and
// iam/v1 typed struct shapes, scan the reducer directory's handler files for
// field usage, and join the three into a Manifest.
//
// It returns an error if any seam's fact kind has no schema-file mapping
// (UnmappedSeamFactKinds) or if any seam's struct type was not found in the
// parsed shapes (a wiring gap between DecodeSeam and the struct dirs the
// caller supplied) — both are startup-time configuration errors, not gate
// findings, so they fail loudly rather than silently shrinking the manifest.
func Load(p Paths) (Manifest, error) {
	resolved := ResolvePaths(p)

	seams, err := ParseDecodeSeams(resolved.DecodeFile)
	if err != nil {
		return Manifest{}, err
	}
	if len(seams) == 0 {
		return Manifest{}, fmt.Errorf("payloadusage: no decode seams found in %s", resolved.DecodeFile)
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
	shapes := make(map[string]StructShape, len(awsShapes)+len(iamShapes))
	for k, v := range awsShapes {
		shapes[k] = v
	}
	for k, v := range iamShapes {
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
