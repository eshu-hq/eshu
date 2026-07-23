// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	rawPayloadIndexAccessor  = "payload_index"
	rawPayloadDynamicKey     = "*"
	rawPayloadExemptionLimit = 54
)

// RawPayloadAccess is one direct read of a fact payload map outside an approved
// decode seam.
type RawPayloadAccess struct {
	Path     string
	Line     int
	Accessor string
	Key      string
}

// String renders a raw-payload convention finding with enough location and key
// context for a maintainer to migrate or exempt it intentionally.
func (a RawPayloadAccess) String() string {
	return fmt.Sprintf("%s:%d reads raw fact payload via %s[%q]", a.Path, a.Line, a.Accessor, a.Key)
}

// RawPayloadError is returned when the raw-payload ratchet finds unapproved
// raw fact payload reads.
type RawPayloadError []RawPayloadAccess

func (e RawPayloadError) Error() string {
	if len(e) == 0 {
		return "payloadusage: raw payload convention failed with no findings"
	}
	limit := len(e)
	if limit > 10 {
		limit = 10
	}
	var b strings.Builder
	fmt.Fprintf(&b, "payloadusage: %d unapproved raw fact payload read(s):", len(e))
	for i := 0; i < limit; i++ {
		fmt.Fprintf(&b, "\n  %s", e[i].String())
	}
	if len(e) > limit {
		fmt.Fprintf(&b, "\n  ... %d more", len(e)-limit)
	}
	return b.String()
}

// RawPayloadExemption permits one already-known raw payload read while typed
// seams are still being migrated. Key may be "*" for dynamic helper keys.
type RawPayloadExemption struct {
	Path     string
	Accessor string
	Key      string
}

// RawPayloadConfig configures the raw-payload convention scan.
type RawPayloadConfig struct {
	RepoRoot      string
	Dirs          []string
	Exemptions    []RawPayloadExemption
	MaxExemptions int
}

// DefaultRawPayloadConfig returns the production ratchet: new W2c/W2d surfaces
// are checked, with the current explicit exemption list capped so it can shrink
// without allowing unreviewed growth.
func DefaultRawPayloadConfig(p Paths) RawPayloadConfig {
	resolved := ResolvePaths(p)
	return RawPayloadConfig{
		RepoRoot: resolved.RepoRoot,
		Dirs: []string{
			resolved.LoaderDir,
			resolved.RelationshipsDir,
			resolved.ReplayDir,
		},
		Exemptions:    defaultRawPayloadExemptions(),
		MaxExemptions: rawPayloadExemptionLimit,
	}
}

// CheckRawPayloadConvention scans configured directories for raw fact-payload
// reads outside factschema_decode*.go files and returns every access not covered
// by the explicit exemption set.
func CheckRawPayloadConvention(cfg RawPayloadConfig) ([]RawPayloadAccess, error) {
	if cfg.MaxExemptions >= 0 && len(cfg.Exemptions) > cfg.MaxExemptions {
		return nil, fmt.Errorf("payloadusage: raw payload exemption list grew to %d entry(ies), max %d", len(cfg.Exemptions), cfg.MaxExemptions)
	}
	allowed := rawPayloadExemptionSet(cfg.Exemptions)
	var findings []RawPayloadAccess
	for _, dir := range cfg.Dirs {
		accesses, err := scanRawPayloadDir(cfg.RepoRoot, dir)
		if err != nil {
			return nil, err
		}
		for _, access := range accesses {
			if rawPayloadAllowed(access, allowed) {
				continue
			}
			findings = append(findings, access)
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Accessor != b.Accessor {
			return a.Accessor < b.Accessor
		}
		return a.Key < b.Key
	})
	return findings, nil
}

func scanRawPayloadDir(repoRoot string, dir string) ([]RawPayloadAccess, error) {
	fset := token.NewFileSet()
	var accesses []RawPayloadAccess
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || isDecodeSeamFile(name) {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("payloadusage: parse raw payload scan target %s: %w", path, err)
		}
		rel := rawPayloadRelPath(repoRoot, path)
		accesses = append(accesses, scanRawPayloadFile(fset, rel, file)...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("payloadusage: scan raw payload dir %s: %w", dir, err)
	}
	return accesses, nil
}

func scanRawPayloadFile(fset *token.FileSet, rel string, file *ast.File) []RawPayloadAccess {
	var accesses []RawPayloadAccess
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		accesses = append(accesses, scanRawPayloadFunc(fset, rel, fn)...)
	}
	return accesses
}

func scanRawPayloadFunc(fset *token.FileSet, rel string, fn *ast.FuncDecl) []RawPayloadAccess {
	aliases := payloadAliasSetFromParams(fn.Type.Params)
	var accesses []RawPayloadAccess
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch expr := n.(type) {
		case *ast.AssignStmt:
			recordPayloadAssignmentAliases(aliases, expr.Lhs, expr.Rhs)
		case *ast.ValueSpec:
			recordPayloadAssignmentAliases(aliases, identsAsExprs(expr.Names), expr.Values)
		case *ast.IndexExpr:
			if isPayloadSource(expr.X, aliases) {
				key, ok := rawPayloadStringKey(expr.Index)
				if !ok {
					return true
				}
				accesses = append(accesses, RawPayloadAccess{
					Path:     rel,
					Line:     fset.Position(expr.Pos()).Line,
					Accessor: rawPayloadIndexAccessor,
					Key:      key,
				})
			}
		case *ast.CallExpr:
			name, ok := rawPayloadHelperName(expr.Fun)
			if ok {
				accesses = append(accesses, rawPayloadHelperAccesses(fset, rel, name, expr)...)
				return true
			}
			name, ok = rawPayloadCallName(expr.Fun)
			if ok && len(expr.Args) > 0 && isPayloadSource(expr.Args[0], aliases) {
				accesses = append(accesses, rawPayloadLiteralHelperAccesses(fset, rel, name, expr)...)
			}
		}
		return true
	})
	return accesses
}

func isDecodeSeamFile(name string) bool {
	return strings.HasPrefix(name, "factschema_decode") && strings.HasSuffix(name, ".go")
}

func isPayloadSelector(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Payload"
}

func isPayloadSource(expr ast.Expr, aliases map[string]struct{}) bool {
	if isPayloadSelector(expr) {
		return true
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = aliases[ident.Name]
	return ok
}

func recordPayloadAssignmentAliases(aliases map[string]struct{}, lhs []ast.Expr, rhs []ast.Expr) {
	for i, right := range rhs {
		if i >= len(lhs) || !isPayloadSource(right, aliases) {
			continue
		}
		ident, ok := lhs[i].(*ast.Ident)
		if !ok {
			continue
		}
		aliases[ident.Name] = struct{}{}
	}
}

func identsAsExprs(idents []*ast.Ident) []ast.Expr {
	exprs := make([]ast.Expr, 0, len(idents))
	for _, ident := range idents {
		exprs = append(exprs, ident)
	}
	return exprs
}

func payloadAliasSetFromParams(params *ast.FieldList) map[string]struct{} {
	aliases := map[string]struct{}{}
	if params == nil {
		return aliases
	}
	for _, field := range params.List {
		if !isMapStringAny(field.Type) {
			continue
		}
		for _, name := range field.Names {
			if name.Name == "payload" {
				aliases[name.Name] = struct{}{}
			}
		}
	}
	return aliases
}

func isMapStringAny(expr ast.Expr) bool {
	mapType, ok := expr.(*ast.MapType)
	if !ok {
		return false
	}
	key, ok := mapType.Key.(*ast.Ident)
	if !ok || key.Name != "string" {
		return false
	}
	switch value := mapType.Value.(type) {
	case *ast.Ident:
		return value.Name == "any" || value.Name == "interface{}"
	case *ast.InterfaceType:
		return len(value.Methods.List) == 0
	}
	return false
}

func rawPayloadHelperName(expr ast.Expr) (string, bool) {
	name, ok := rawPayloadCallName(expr)
	if !ok {
		return "", false
	}
	if name == "payloadString" || name == "payloadStrings" {
		return name, true
	}
	return "", false
}

func rawPayloadCallName(expr ast.Expr) (string, bool) {
	var name string
	switch callee := expr.(type) {
	case *ast.Ident:
		name = callee.Name
	case *ast.SelectorExpr:
		name = callee.Sel.Name
	}
	return name, name != ""
}

func rawPayloadHelperAccesses(fset *token.FileSet, rel string, name string, call *ast.CallExpr) []RawPayloadAccess {
	var accesses []RawPayloadAccess
	for _, arg := range call.Args[1:] {
		key := rawPayloadKey(arg)
		if key == "" {
			continue
		}
		accesses = append(accesses, RawPayloadAccess{
			Path:     rel,
			Line:     fset.Position(call.Pos()).Line,
			Accessor: name,
			Key:      key,
		})
	}
	if len(accesses) == 0 {
		accesses = append(accesses, RawPayloadAccess{
			Path:     rel,
			Line:     fset.Position(call.Pos()).Line,
			Accessor: name,
			Key:      rawPayloadDynamicKey,
		})
	}
	return accesses
}

func rawPayloadLiteralHelperAccesses(fset *token.FileSet, rel string, name string, call *ast.CallExpr) []RawPayloadAccess {
	var accesses []RawPayloadAccess
	for _, arg := range call.Args[1:] {
		key, ok := rawPayloadStringKey(arg)
		if !ok {
			continue
		}
		accesses = append(accesses, RawPayloadAccess{
			Path:     rel,
			Line:     fset.Position(call.Pos()).Line,
			Accessor: name,
			Key:      key,
		})
	}
	return accesses
}

func rawPayloadKey(expr ast.Expr) string {
	key, ok := rawPayloadStringKey(expr)
	if !ok {
		return rawPayloadDynamicKey
	}
	return key
}

func rawPayloadStringKey(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func rawPayloadRelPath(repoRoot string, path string) string {
	if rel, err := filepath.Rel(repoRoot, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func rawPayloadExemptionSet(exemptions []RawPayloadExemption) map[RawPayloadExemption]struct{} {
	set := make(map[RawPayloadExemption]struct{}, len(exemptions))
	for _, exemption := range exemptions {
		set[exemption] = struct{}{}
	}
	return set
}

func rawPayloadAllowed(access RawPayloadAccess, allowed map[RawPayloadExemption]struct{}) bool {
	for _, key := range []string{access.Key, rawPayloadDynamicKey} {
		if _, ok := allowed[RawPayloadExemption{Path: access.Path, Accessor: access.Accessor, Key: key}]; ok {
			return true
		}
	}
	return false
}

func defaultRawPayloadExemptions() []RawPayloadExemption {
	return []RawPayloadExemption{
		{Path: "go/internal/relationships/argocd_generator_config.go", Accessor: rawPayloadIndexAccessor, Key: "parsed_file_data"},
		{Path: "go/internal/relationships/evidence.go", Accessor: rawPayloadIndexAccessor, Key: "artifact_type"},
		{Path: "go/internal/relationships/evidence.go", Accessor: rawPayloadIndexAccessor, Key: "parsed_file_data"},
		{Path: "go/internal/relationships/evidence.go", Accessor: rawPayloadIndexAccessor, Key: "repo_id"},
		{Path: "go/internal/relationships/evidence_content_index.go", Accessor: rawPayloadIndexAccessor, Key: "content"},
		{Path: "go/internal/relationships/evidence_content_index.go", Accessor: rawPayloadIndexAccessor, Key: "content_body"},
		{Path: "go/internal/relationships/evidence_content_index.go", Accessor: rawPayloadIndexAccessor, Key: "content_path"},
		{Path: "go/internal/relationships/evidence_content_index.go", Accessor: rawPayloadIndexAccessor, Key: "relative_path"},
		{Path: "go/internal/relationships/evidence_byte_citation.go", Accessor: rawPayloadIndexAccessor, Key: "commit_sha"},
		{Path: "go/internal/storage/postgres/incident_routing_evidence_loader.go", Accessor: "incidentRoutingPayloadMap", Key: "service"},
		// crossplaneRedriveXRDFields (issue #5476's cross-scope SATISFIED_BY
		// redrive sweep) reads a CrossplaneXRD content_entity fact's
		// entity_metadata to extract the XRD's (group, claim_kind) join key.
		// content_entity is parser output: every entity_type's extra fields
		// (Variable, Function, K8sResource, CrossplaneXRD, ...) live under
		// entity_metadata as an untyped bag, and content_entity has no typed
		// factschema struct -- sdk/go/factschema models only collector-emitted
		// families (AWS, IAM, incident, ...), never parsed code/content
		// entities, so there is no seam to migrate to. The reducer's own
		// pre-existing SATISFIED_BY correlation
		// (internal/reducer/crossplane_satisfied_by_edge_rows.go's
		// crossplaneEntityMetadataString) reads this exact field the exact
		// same raw way; this exemption keeps the sweep's read consistent with
		// that already-established pattern rather than inventing a one-off
		// typed struct ahead of a content_entity family migration this change
		// does not own.
		{Path: "go/internal/storage/postgres/crossplane_satisfied_by_redrive_sweep.go", Accessor: rawPayloadIndexAccessor, Key: "entity_metadata"},
		// The repository-catalog payload parser moved from the Postgres ingestion
		// path to relationships.RepositoryCatalogEntry so Ifá can derive the same
		// catalog offline (#4394 T2). The reads are unchanged (still pre-typed
		// repository payload keys); the exemption follows the code to its new home.
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "graph_id"},
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "name"},
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "repo_id"},
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "repo_name"},
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "repo_slug"},
		// remote_url feeds CatalogEntry.RemoteURL, the strict cross-repo URL
		// resolution primitive discoverStructuredFluxEvidence uses (issue #5483
		// C2). Same untyped repository-payload pattern as the sibling reads
		// above; the repository fact kind has no typed struct yet.
		{Path: "go/internal/relationships/catalog.go", Accessor: "catalogPayloadString", Key: "remote_url"},
		{Path: "go/internal/replay/offlinetier/delta.go", Accessor: rawPayloadIndexAccessor, Key: "path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "optionalString", Key: "needs"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireInt", Key: "depth"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "dir_path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "file_path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "language"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "name"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "parent_path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "relative_path"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "repo_id"},
		{Path: "go/internal/replay/offlinetier/materialization.go", Accessor: "requireString", Key: "uid"},
		// The relationship-reference side table records the source repo_id for
		// candidate content/file/cloud-relationship facts so the deferred
		// backfill can keep exact self-exclusion semantics while avoiding
		// per-candidate regex scans.
		{Path: "go/internal/storage/postgres/relationship_reference_keys.go", Accessor: rawPayloadIndexAccessor, Key: "repo_id"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "advisory_id"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "confidence"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "cve_id"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "ecosystem"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "epss_probability"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "package_name"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "repository_id"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadString", Key: "selected_severity_label"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: "payloadBool", Key: "known_exploited"},
		{Path: "go/internal/storage/postgres/service_vulnerability_advisory_loader.go", Accessor: rawPayloadIndexAccessor, Key: "provenance"},
		{Path: "go/internal/storage/postgres/shared_intents.go", Accessor: "sharedIntentPayloadString", Key: "acceptance_unit_id"},
		{Path: "go/internal/storage/postgres/shared_intents.go", Accessor: "sharedIntentPayloadString", Key: "scope_id"},
		{Path: "go/internal/storage/postgres/shared_intents_history.go", Accessor: "sharedIntentPayloadString", Key: "intent_type"},
		{Path: "go/internal/storage/postgres/shared_intents_history.go", Accessor: "sharedIntentPayloadString", Key: "repo_id"},
		{Path: "go/internal/storage/postgres/shared_intents_history.go", Accessor: "sharedIntentPayloadStringSlice", Key: "delta_file_paths"},
	}
}
