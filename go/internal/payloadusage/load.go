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
	// OCIRegistryStructDir is sdk/go/factschema/ociregistry/v1.
	OCIRegistryStructDir string
	// TerraformStateStructDir is sdk/go/factschema/terraformstate/v1.
	TerraformStateStructDir string
	// PackageRegistryStructDir is sdk/go/factschema/packageregistry/v1.
	PackageRegistryStructDir string
	// SBOMStructDir is sdk/go/factschema/sbom/v1.
	SBOMStructDir string
	// ProjectorDir is go/internal/projector — the source of the projector's
	// decode-seam files (ProjectorDecodeFiles) and the canonical-extractor files
	// ScanDecodeUsage walks for the projector-side decode sites. The projector is
	// the primary graph-identity producer for the oci_registry family (its
	// canonical extractor decodes through the same sdk/go/factschema seam the
	// reducer uses), so the manifest gate must scan it alongside ReducerDir.
	ProjectorDir string
	// ProjectorDecodeFiles is the set of projector decode-seam files to parse.
	// When empty, ResolvePaths does NOT default it (same rationale as
	// DecodeFiles); resolveProjectorDecodeFiles fills it by globbing
	// factschema_decode*.go under ProjectorDir.
	ProjectorDecodeFiles []string
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
	if strings.TrimSpace(resolved.OCIRegistryStructDir) == "" {
		resolved.OCIRegistryStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "ociregistry", "v1")
	}
	if strings.TrimSpace(resolved.TerraformStateStructDir) == "" {
		resolved.TerraformStateStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "terraformstate", "v1")
	}
	if strings.TrimSpace(resolved.PackageRegistryStructDir) == "" {
		resolved.PackageRegistryStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "packageregistry", "v1")
	}
	if strings.TrimSpace(resolved.SBOMStructDir) == "" {
		resolved.SBOMStructDir = filepath.Join(resolved.RepoRoot, "sdk", "go", "factschema", "sbom", "v1")
	}
	if strings.TrimSpace(resolved.ProjectorDir) == "" {
		resolved.ProjectorDir = filepath.Join(resolved.RepoRoot, "go", "internal", "projector")
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

// resolveProjectorDecodeFiles returns the set of projector decode-seam files to
// parse. When ProjectorDecodeFiles is already set it is returned as-is;
// otherwise it globs every factschema_decode*.go under ProjectorDir so a
// projector family whose wrappers live in a split file is covered, mirroring
// resolveDecodeFiles for the reducer. Unlike the reducer glob, an EMPTY match is
// NOT an error: the projector has exactly one typed family today (oci_registry),
// so a repo checkout with no projector decode seam is a valid intermediate
// state, not a fail-closed misconfiguration. The reducer glob remains
// fail-closed because the reducer always has at least the AWS decode seam.
func resolveProjectorDecodeFiles(p Paths) ([]string, error) {
	if len(p.ProjectorDecodeFiles) > 0 {
		return p.ProjectorDecodeFiles, nil
	}
	pattern := filepath.Join(p.ProjectorDir, "factschema_decode*.go")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("payloadusage: glob projector decode seam files %s: %w", pattern, err)
	}
	files := matches[:0]
	for _, m := range matches {
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		files = append(files, m)
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

// mergeSeams returns the union of two decode-seam sets, sorted by FuncName. A
// decode function is defined in exactly one directory (the reducer's registry
// index uses a distinct *ForIndex name that is not a seam, so it never collides
// with a projector seam), so a same-FuncName appearing in both is a real
// duplication bug the caller should never produce; when it happens the later
// (projector) entry wins deterministically after the sort, and the collision is
// harmless because both would map the same fact kind to the same struct. The
// merged set is what the manifest is derived from across both pipeline stages.
func mergeSeams(a, b []DecodeSeam) []DecodeSeam {
	byName := make(map[string]DecodeSeam, len(a)+len(b))
	for _, s := range a {
		byName[s.FuncName] = s
	}
	for _, s := range b {
		byName[s.FuncName] = s
	}
	merged := make([]DecodeSeam, 0, len(byName))
	for _, s := range byName {
		merged = append(merged, s)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].FuncName < merged[j].FuncName })
	return merged
}

// mergeUsage returns the union of two decode-func -> field-usage maps (reducer
// and projector), concatenating the usage slices for a decode func that both
// stages read (a fact kind decoded in both pipeline stages, e.g. an oci kind the
// projector projects and the reducer correlates), then re-sorting each merged
// slice by (File, GoFieldName) so the manifest stays deterministic regardless of
// scan order.
func mergeUsage(a, b map[string][]FieldUsage) map[string][]FieldUsage {
	merged := make(map[string][]FieldUsage, len(a)+len(b))
	for fn, uses := range a {
		merged[fn] = append(merged[fn], uses...)
	}
	for fn, uses := range b {
		merged[fn] = append(merged[fn], uses...)
	}
	for fn := range merged {
		sort.Slice(merged[fn], func(i, j int) bool {
			x, y := merged[fn][i], merged[fn][j]
			if x.File != y.File {
				return x.File < y.File
			}
			return x.GoFieldName < y.GoFieldName
		})
	}
	return merged
}

// Load runs the full derivation pipeline against p (auto-resolving empty
// fields via ResolvePaths): parse the reducer and projector decode seams, parse
// the aws/v1, iam/v1, incident/v1, gcp/v1, azure/v1, kuberneteslive/v1,
// ociregistry/v1, terraformstate/v1, packageregistry/v1, and sbom/v1 typed
// shapes, scan the reducer and projector directories' files for field usage,
// and join the three into a Manifest.
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
	reducerSeams, err := parseDecodeSeamsFiles(decodeFiles)
	if err != nil {
		return Manifest{}, err
	}
	if len(reducerSeams) == 0 {
		return Manifest{}, fmt.Errorf("payloadusage: no decode seams found in %s", strings.Join(decodeFiles, ", "))
	}

	// The projector is the primary graph-identity producer for the oci_registry
	// family: its canonical extractor decodes through the same sdk/go/factschema
	// seam the reducer uses. Parse its decode-seam files and merge them so the
	// manifest gate covers projector decode sites too. An empty projector seam
	// set is valid (only oci_registry is typed in the projector today).
	projectorDecodeFiles, err := resolveProjectorDecodeFiles(resolved)
	if err != nil {
		return Manifest{}, err
	}
	projectorSeams, err := parseDecodeSeamsFiles(projectorDecodeFiles)
	if err != nil {
		return Manifest{}, err
	}
	seams := mergeSeams(reducerSeams, projectorSeams)

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
	ociShapes, err := ParseStructShapes(resolved.OCIRegistryStructDir, "ociregistryv1")
	if err != nil {
		return Manifest{}, err
	}
	terraformStateShapes, err := ParseStructShapes(resolved.TerraformStateStructDir, "tfstatev1")
	if err != nil {
		return Manifest{}, err
	}
	packageRegistryShapes, err := ParseStructShapes(resolved.PackageRegistryStructDir, "packageregistryv1")
	if err != nil {
		return Manifest{}, err
	}
	sbomShapes, err := ParseStructShapes(resolved.SBOMStructDir, "sbomv1")
	if err != nil {
		return Manifest{}, err
	}
	shapes := make(map[string]StructShape, len(awsShapes)+len(iamShapes)+len(incidentShapes)+len(gcpShapes)+len(azureShapes)+len(kubernetesLiveShapes)+len(ociShapes)+len(terraformStateShapes)+len(packageRegistryShapes)+len(sbomShapes))
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
	for k, v := range ociShapes {
		shapes[k] = v
	}
	for k, v := range terraformStateShapes {
		shapes[k] = v
	}
	for k, v := range packageRegistryShapes {
		shapes[k] = v
	}
	for k, v := range sbomShapes {
		shapes[k] = v
	}

	// Scan BOTH the reducer and the projector directories for field usage
	// against the merged seam set, so a field a projector canonical extractor
	// reads off a decoded struct is gated the same as a reducer handler read.
	reducerUsage, err := ScanDecodeUsage(resolved.ReducerDir, seams)
	if err != nil {
		return Manifest{}, err
	}
	projectorUsage, err := ScanDecodeUsage(resolved.ProjectorDir, seams)
	if err != nil {
		return Manifest{}, err
	}
	usage := mergeUsage(reducerUsage, projectorUsage)

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
