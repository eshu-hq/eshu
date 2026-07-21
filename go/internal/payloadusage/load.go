// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"encoding/json"
	"fmt"
	"maps"
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
	return resolveOptionalDecodeFiles("projector", p.ProjectorDir, p.ProjectorDecodeFiles)
}

// resolveQueryDecodeFiles returns the set of query-layer decode-seam files to
// parse. When QueryDecodeFiles is already set it is returned as-is; otherwise it
// globs every factschema_decode*.go under QueryDir so a query family whose
// wrappers live in a split file is covered, mirroring resolveProjectorDecodeFiles.
// Like the projector glob (and unlike the reducer's fail-closed glob), an EMPTY
// match is NOT an error: the query read model has exactly one typed family today
// (work_item), so a repo checkout with no query decode seam is a valid
// intermediate state rather than a fail-closed misconfiguration.
func resolveQueryDecodeFiles(p Paths) ([]string, error) {
	return resolveOptionalDecodeFiles("query", p.QueryDir, p.QueryDecodeFiles)
}

// resolveOptionalDecodeFiles globs factschema_decode*.go under dir, excluding
// test files, for pipeline surfaces whose typed coverage is allowed to be empty
// during incremental migration. explicit, when non-empty, is returned as-is so
// tests and callers can pin fixture files.
func resolveOptionalDecodeFiles(label string, dir string, explicit []string) ([]string, error) {
	if len(explicit) > 0 {
		return explicit, nil
	}
	pattern := filepath.Join(dir, "factschema_decode*.go")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("payloadusage: glob %s decode seam files %s: %w", label, pattern, err)
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

func resolveLoaderDecodeFiles(p Paths) ([]string, error) {
	return resolveOptionalDecodeFiles("loader", p.LoaderDir, p.LoaderDecodeFiles)
}

func resolveRelationshipsDecodeFiles(p Paths) ([]string, error) {
	return resolveOptionalDecodeFiles("relationships", p.RelationshipsDir, p.RelationshipsDecodeFiles)
}

func resolveReplayDecodeFiles(p Paths) ([]string, error) {
	return resolveOptionalDecodeFiles("replay", p.ReplayDir, p.ReplayDecodeFiles)
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

func mergeUsageSets(sets ...map[string][]FieldUsage) map[string][]FieldUsage {
	merged := map[string][]FieldUsage{}
	for _, set := range sets {
		merged = mergeUsage(merged, set)
	}
	return merged
}

// parseAllStructShapes parses every typed-struct directory a migrated family
// declares (aws/v1, iam/v1, ... workitem/v1) and returns their union keyed by
// qualified struct name, the shape lookup BuildManifest joins each decode seam
// against. A data-driven family list (rather than one repeated 3-line
// parse-and-merge block per family) keeps this proportional to the number of
// typed families as new ones are added. Any family's parse error is returned
// as a startup-time configuration error, not silently skipped.
func parseAllStructShapes(resolved Paths) (map[string]StructShape, error) {
	families := []struct {
		dir   string
		alias string
	}{
		{resolved.AWSStructDir, "awsv1"},
		{resolved.IAMStructDir, "iamv1"},
		{resolved.IncidentStructDir, "incidentv1"},
		{resolved.GCPStructDir, "gcpv1"},
		{resolved.AzureStructDir, "azurev1"},
		{resolved.KubernetesLiveStructDir, "kuberneteslivev1"},
		{resolved.OCIRegistryStructDir, "ociregistryv1"},
		{resolved.TerraformStateStructDir, "tfstatev1"},
		{resolved.PackageRegistryStructDir, "packageregistryv1"},
		{resolved.SBOMStructDir, "sbomv1"},
		{resolved.VulnerabilityStructDir, "vulnerabilityv1"},
		{resolved.CICDRunStructDir, "cicdrunv1"},
		{resolved.SecretsIAMStructDir, "secretsiamv1"},
		{resolved.WorkItemStructDir, "workitemv1"},
		{resolved.SecurityAlertStructDir, "securityalertv1"},
		{resolved.ObservabilityStructDir, "observabilityv1"},
		{resolved.DocumentationStructDir, "documentationv1"},
		{resolved.CodegraphStructDir, "codegraphv1"},
		{resolved.CodedataflowStructDir, "codedataflowv1"},
		{resolved.ServiceCatalogStructDir, "servicecatalogv1"},
		{resolved.ReducerDerivedStructDir, "reducerderivedv1"},
		{resolved.CodeownersStructDir, "codeownersv1"},
	}
	shapes := make(map[string]StructShape)
	for _, family := range families {
		parsed, err := ParseStructShapes(family.dir, family.alias)
		if err != nil {
			return nil, err
		}
		maps.Copy(shapes, parsed)
	}
	return shapes, nil
}

// Load runs the full derivation pipeline against p (auto-resolving empty
// fields via ResolvePaths): parse the reducer and projector decode seams, parse
// the aws/v1, iam/v1, incident/v1, gcp/v1, azure/v1, kuberneteslive/v1,
// ociregistry/v1, terraformstate/v1, packageregistry/v1, sbom/v1,
// vulnerability/v1, cicdrun/v1, secretsiam/v1, workitem/v1,
// documentation/v1, codegraph/v1, codedataflow/v1, and servicecatalog/v1 typed
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
	for _, group := range []struct {
		name  string
		files func(Paths) ([]string, error)
	}{
		{"query", resolveQueryDecodeFiles},
		{"loader", resolveLoaderDecodeFiles},
		{"relationships", resolveRelationshipsDecodeFiles},
		{"replay", resolveReplayDecodeFiles},
	} {
		files, resolveErr := group.files(resolved)
		if resolveErr != nil {
			return Manifest{}, resolveErr
		}
		groupSeams, parseErr := parseDecodeSeamsFiles(files)
		if parseErr != nil {
			return Manifest{}, fmt.Errorf("payloadusage: parse %s decode seams: %w", group.name, parseErr)
		}
		seams = mergeSeams(seams, groupSeams)
	}

	if missing := UnmappedSeamFactKinds(seams); len(missing) > 0 {
		return Manifest{}, fmt.Errorf(
			"payloadusage: %d decode seam(s) have no schema-file mapping in factKindSchemaFile: %s — add the mapping before this kind can be gated",
			len(missing), JoinSorted(missing),
		)
	}

	shapes, err := parseAllStructShapes(resolved)
	if err != nil {
		return Manifest{}, err
	}

	// Scan every typed-decode surface for field usage against the merged seam
	// set, so a field a projector canonical extractor, query read-model builder,
	// loader, relationship extractor, or replay materializer reads off a decoded
	// struct is gated the same as a reducer handler read.
	reducerUsage, err := ScanDecodeUsage(resolved.ReducerDir, seams)
	if err != nil {
		return Manifest{}, err
	}
	projectorUsage, err := ScanDecodeUsage(resolved.ProjectorDir, seams)
	if err != nil {
		return Manifest{}, err
	}
	queryUsage, err := ScanDecodeUsage(resolved.QueryDir, seams)
	if err != nil {
		return Manifest{}, err
	}
	loaderUsage, err := ScanDecodeUsage(resolved.LoaderDir, seams)
	if err != nil {
		return Manifest{}, err
	}
	relationshipsUsage, err := ScanDecodeUsage(resolved.RelationshipsDir, seams)
	if err != nil {
		return Manifest{}, err
	}
	replayUsage, err := ScanDecodeUsage(resolved.ReplayDir, seams)
	if err != nil {
		return Manifest{}, err
	}
	usage := mergeUsageSets(reducerUsage, projectorUsage, queryUsage, loaderUsage, relationshipsUsage, replayUsage)

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
	rawAccesses, err := CheckRawPayloadConvention(DefaultRawPayloadConfig(ResolvePaths(p)))
	if err != nil {
		return Manifest{}, nil, err
	}
	if len(rawAccesses) > 0 {
		return Manifest{}, nil, RawPayloadError(rawAccesses)
	}
	declared, err := LoadDeclaredFieldsFromSchemas(ResolvePaths(p).SchemaDir)
	if err != nil {
		return Manifest{}, nil, err
	}
	return manifest, CheckManifest(manifest, declared), nil
}
