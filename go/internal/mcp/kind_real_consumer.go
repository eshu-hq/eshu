// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/payloadusage"
)

// realConsumerDecodeSeamDirs are the directories payloadusage's own gate
// (Contract System v1 §6 enforcement gate 2) scans for typed decode<Kind>
// seam functions. The #5474 D2 per-kind consumer existence gate reuses the
// exact same directory set as its primary real-consumption signal: a fact
// kind is consumed when a decode<Kind> function exists for it in one of
// these directories — not merely when the generated registry's
// PayloadSchema field is non-empty. PayloadSchema is a checked-in JSON
// Schema file PATH; it is populated for every kind that has a typed struct
// defined, whether or not any code actually decodes it. terraform_state_candidate
// is the concrete false-green: it has a PayloadSchema path but no decode
// call site (go/internal/projector/tfstate_canonical.go:113-116 documents
// the kind as intentionally unhandled).
//
// go/internal/ifa is deliberately EXCLUDED from this list. roundtrip.go
// (#4804) calls real factschema.Decode<Kind>(...) functions — including
// DecodeGCPCollectionWarning, DecodeGCPDNSRecord, and
// DecodeGCPIAMPolicyObservation, covering three kinds this gate discloses as
// unconsumed (gcp_collection_warning, gcp_dns_record,
// gcp_iam_policy_observation) — but only to decode a fixture payload and
// immediately re-encode it, proving schema round-trip fidelity for the Ifá
// coverage harness. It never serves a read surface. Adding go/internal/ifa
// here would silently flip those three kinds' disclosure to green for the
// wrong reason, the same false-green class the projector
// (go/internal/projector/runtime_phase.go's readiness dispatch) and
// go/internal/storage/postgres (tfstate_backend_queries.go's SQL-string-only
// touch) exclusions above guard against.
var realConsumerDecodeSeamDirs = []string{
	"go/internal/reducer",
	"go/internal/projector",
	"go/internal/query",
	"go/internal/storage/postgres",
	"go/internal/relationships",
	"go/internal/replay/offlinetier",
}

// realConsumerRawSQLDir is the one directory scanned for raw fact_kind SQL
// literal reads that count as a real consumer without a typed decode seam.
// Scope is deliberately narrow: go/internal/query is the read-surface
// serving layer (the layer TestFactKindRegistryReadSurfacesResolveToLiveRoutes
// and the API router actually return data through). go/internal/storage/postgres
// also contains many `fact_kind = '...'` predicates, but most of them are
// join-key/filter-only lookups feeding ingestion control-flow or status
// reporting, not read-surface data (e.g. package_registry.vulnerability_hint's
// own disclosure reason: "join-key-only ... no decode, no query read-model
// consumer" — a storage/postgres WHERE-clause touch is explicitly NOT
// consumption by that precedent). Restricting the raw-SQL signal to the
// query layer keeps it from re-introducing the same false-green class this
// gate exists to close: a storage/postgres filter touching
// terraform_state_candidate for backend-path resolution
// (tfstate_backend_queries.go) must not make the kind look consumed for
// read-surface purposes.
const realConsumerRawSQLDir = "go/internal/query"

// realConsumerNamedStoreDir is scanned for a narrower, targeted pattern than
// realConsumerRawSQLDir's literal-only scope allows: a storage/postgres file
// that declares its OWN top-level exported string constant naming a fact
// kind (e.g. multi_cloud_runtime_drift_findings.go's
// `const MultiCloudRuntimeDriftFindingFactKind = "reducer_multi_cloud_runtime_drift_finding"`)
// AND separately references `fact_kind` somewhere in that same file.
// Reducer-derived read-model kinds (ReducerDomain "reducer_derived_findings")
// query their own dedicated Store type through a `fact_kind = $1`
// PARAMETERIZED predicate bound to this local constant rather than a
// literal, which realConsumerRawSQLDir's regexes intentionally do not match
// (a parameterized predicate is usually evidence of nothing in particular,
// since the bound kind could be anything at runtime). A file that names its
// OWN constant after one specific kind and touches fact_kind is different:
// it is a single-purpose Store implementation for exactly that kind. Scoped
// to go/internal/storage/postgres only, and verified against every
// currently-disclosed kind in grandfatheredUnconsumedKinds (none declares a
// matching local constant there), so it cannot resurrect the
// terraform_state_candidate false-green: tfstate_backend_queries.go touches
// `candidate.fact_kind = 'terraform_state_candidate'` for backend-path
// resolution, but no file anywhere under storage/postgres declares a named
// constant equal to that literal.
//
// go/internal/replay/schedulereplay carries the same pattern for a kind
// classified purely at the replay layer: scanner_worker.analysis
// (go/internal/replay/schedulereplay/workitem_projection.go's
// factKindScannerWorkerAnalysis) is a real, named per-kind constant used
// alongside FactKind-matching logic in that file, just never as a literal
// `fact_kind = '...'` SQL predicate or a facts.<Kind> selector. go/internal/query
// is deliberately excluded here even though it is realConsumerRawSQLDir: a
// spot check (supply_chain_impact_readiness_decode.go's
// sourceSnapshotFamilyMarker = "vulnerability.source_snapshot") found a
// same-shaped local constant that names a fact-kind-looking string but is
// never compared against envelope.FactKind or a fact_kind column anywhere in
// that file — it is an internal classification marker compared against a
// decoded JSON "family" field, not fact-kind evidence — so admitting the
// whole query directory into this looser (any const, not query-string-shaped)
// scan trades a small amount of missed coverage for avoiding a real
// false-positive class.
var realConsumerNamedStoreDirs = []string{
	"go/internal/storage/postgres",
	"go/internal/replay/schedulereplay",
}

// factKindConstFileGlob is the sdk/go/factschema glob covering every
// top-level file in the package. FactKind* wire-string constants mostly
// live in the fact_kinds*.go files, but a few families (e.g.
// FactKindReducerPackageConsumptionCorrelation in decode_reducerderived.go)
// declare their constant inline next to the family's own decode function.
// Globbing the whole package directory (non-recursive, so typed struct
// subpackages like aws/v1 are excluded) rather than guessing a filename
// pattern keeps this derivation from going stale as new families land their
// constants in whichever file they choose.
const factKindConstFileGlob = "sdk/go/factschema/*.go"

// rawSQLFactKindPattern matches a `fact_kind = '<kind>'` (optionally
// alias-qualified, e.g. `fact.fact_kind` or `candidate.fact_kind`) SQL
// equality literal. It intentionally does not match parameterized
// (`fact_kind = $1`) or Go-identifier-concatenated predicates
// (`"fact_kind = '" + facts.XFactKind + "'"`), so it under-approximates
// rather than over-approximates: a kind this misses stays classified via
// the decode-seam signal or requires a disclosure, but a kind this matches
// is a real, literal, greppable SQL fact_kind predicate in the query layer.
var rawSQLFactKindPattern = regexp.MustCompile(`fact_kind\s*=\s*'([A-Za-z0-9_.]+)'`)

// rawSQLFactKindINPattern matches a `fact_kind IN ('a', 'b', ...)` literal
// list, capturing the whole parenthesized list for a second pass extracting
// each quoted kind.
var rawSQLFactKindINPattern = regexp.MustCompile(`fact_kind\s+IN\s*\(([^)]*)\)`)

// quotedLiteralPattern extracts one single-quoted literal from an IN(...) list.
var quotedLiteralPattern = regexp.MustCompile(`'([A-Za-z0-9_.]+)'`)

// factsPackageIdentRefPattern matches a reference to one of go/internal/facts'
// own FactKind constants (suffix style, e.g. facts.DocumentationLinkFactKind,
// unlike sdk/go/factschema's prefix style FactKindAWSResource). The query
// layer builds several SQL predicates by string-concatenating this
// identifier rather than writing the literal kind
// (documentation_read_model.go's `"'" + facts.DocumentationLinkFactKind + "'"`
// is the concrete case), so a plain `fact_kind = '<literal>'` regex alone
// misses it; this pattern catches any reference to the identifier anywhere
// in the query-layer source, which is sufficient evidence since Go would
// fail to compile a reference to an unused import.
var factsPackageIdentRefPattern = regexp.MustCompile(`facts\.(\w*FactKind\w*)\b`)

// factsPackageConstFileGlob is the go/internal/facts glob covering every
// top-level file declaring the package's own FactKind-suffixed wire-string
// constants (e.g. AWSResourceFactKind = "aws_resource"). Non-recursive:
// go/internal/facts has no subpackages carrying these constants.
const factsPackageConstFileGlob = "go/internal/facts/*.go"

// realConsumerEvidence is the computed set of fact kinds with a detectable
// real consumer, derived from source rather than from registry metadata.
type realConsumerEvidence struct {
	decodeSeamKinds map[string]bool
	rawSQLKinds     map[string]bool
	dispatchKinds   map[string]bool
}

// hasRealConsumer reports whether kind has a detectable real consumer: a
// decode<Kind> seam call site (local wrapper or a direct
// factschema.Decode<Kind> call), a literal fact_kind SQL predicate or
// FactKind-identifier reference in the query (read-surface) layer, or a
// `case facts.<Kind>:` / equality dispatch on the raw envelope kind.
func (e realConsumerEvidence) hasRealConsumer(kind string) bool {
	return e.decodeSeamKinds[kind] || e.rawSQLKinds[kind] || e.dispatchKinds[kind]
}

// loadRealConsumerEvidence computes the real-consumer evidence set from
// repoRoot. It is the #5474 D2 gate's replacement for the toothless
// PayloadSchema-non-empty / pipelineConsumer signals: both of those are
// registry METADATA (a schema file path, a set of non-empty pipeline
// fields) that are populated identically for consumed and unconsumed kinds
// alike — terraform_state_candidate has a PayloadSchema path and a full
// ReducerDomain/ProjectionHook/AdmissionHook triple despite having no
// decode call site anywhere in the codebase. This function instead derives
// consumption from actual source: does a decode<Kind> function exist for
// the kind (payloadusage.ParseDecodeSeamsGlob, the same derivation Contract
// System v1 §6 gate 2 uses), or does the query (read-surface) layer read
// the kind's payload via a literal SQL predicate.
func loadRealConsumerEvidence(repoRoot string) (realConsumerEvidence, error) {
	constValues, err := factKindConstantValues(filepath.Join(repoRoot, factKindConstFileGlob))
	if err != nil {
		return realConsumerEvidence{}, err
	}

	seamKinds := map[string]bool{}
	for _, dir := range realConsumerDecodeSeamDirs {
		glob := filepath.Join(repoRoot, dir, "factschema_decode*.go")
		seams, err := payloadusage.ParseDecodeSeamsGlob(glob)
		if err != nil {
			return realConsumerEvidence{}, fmt.Errorf("kind_real_consumer: parse decode seams %s: %w", glob, err)
		}
		for _, s := range seams {
			wire, ok := constValues[s.FactKindConst]
			if !ok {
				return realConsumerEvidence{}, fmt.Errorf(
					"kind_real_consumer: decode seam %s (%s) references factschema.%s, which has no wire-string constant under %s — the const derivation is stale",
					s.FuncName, dir, s.FactKindConst, factKindConstFileGlob,
				)
			}
			seamKinds[wire] = true
		}
	}

	directDecodeFuncKinds, err := factschemaExportedDecodeFuncKinds(repoRoot, constValues)
	if err != nil {
		return realConsumerEvidence{}, err
	}
	directCallDirs := make([]string, len(realConsumerDecodeSeamDirs))
	for i, dir := range realConsumerDecodeSeamDirs {
		directCallDirs[i] = filepath.Join(repoRoot, dir)
	}
	directDecodeKinds, err := directFactschemaDecodeCalls(directCallDirs, directDecodeFuncKinds)
	if err != nil {
		return realConsumerEvidence{}, err
	}
	for kind := range directDecodeKinds {
		seamKinds[kind] = true
	}

	rawSQLKinds, err := rawSQLFactKindReaders(filepath.Join(repoRoot, realConsumerRawSQLDir))
	if err != nil {
		return realConsumerEvidence{}, err
	}

	factsConstValues, err := factKindConstantValues(filepath.Join(repoRoot, factsPackageConstFileGlob))
	if err != nil {
		return realConsumerEvidence{}, err
	}
	identRefKinds, err := factsPackageIdentRefKinds(filepath.Join(repoRoot, realConsumerRawSQLDir), factsConstValues)
	if err != nil {
		return realConsumerEvidence{}, err
	}
	for kind := range identRefKinds {
		rawSQLKinds[kind] = true
	}

	for _, dir := range realConsumerNamedStoreDirs {
		namedStoreKinds, err := namedConstStoreKinds(filepath.Join(repoRoot, dir))
		if err != nil {
			return realConsumerEvidence{}, err
		}
		for kind := range namedStoreKinds {
			rawSQLKinds[kind] = true
		}
	}

	dispatchKinds, err := factsDispatchedKinds([]string{filepath.Join(repoRoot, "go/internal/reducer")}, factsConstValues)
	if err != nil {
		return realConsumerEvidence{}, err
	}

	postgresReaderKinds, err := postgresPayloadReaderKinds(filepath.Join(repoRoot, "go/internal/storage/postgres"), factsConstValues)
	if err != nil {
		return realConsumerEvidence{}, err
	}
	for kind := range postgresReaderKinds {
		dispatchKinds[kind] = true
	}

	pqArrayKinds, err := pqArraySliceFactKinds(filepath.Join(repoRoot, realConsumerRawSQLDir))
	if err != nil {
		return realConsumerEvidence{}, err
	}
	for kind := range pqArrayKinds {
		rawSQLKinds[kind] = true
	}

	return realConsumerEvidence{decodeSeamKinds: seamKinds, rawSQLKinds: rawSQLKinds, dispatchKinds: dispatchKinds}, nil
}

// factKindConstantValues parses every file matching glob and returns a map
// from each top-level string-const identifier to its literal value (e.g.
// "FactKindAWSResource" -> "aws_resource"). This is the derivation that
// resolves a payloadusage.DecodeSeam's FactKindConst identifier (the bare
// Go const name ParseDecodeSeams reads from the decode function body) to
// the wire fact-kind string the generated registry's Kind field carries —
// the two are never the same string, so a seam's identifier must be
// resolved through this table before it can be compared against
// facts.FactKindRegistry() entries.
func factKindConstantValues(glob string) (map[string]string, error) {
	matches, err := filepath.Glob(glob)
	if err != nil {
		return nil, fmt.Errorf("kind_real_consumer: glob fact-kind const files %s: %w", glob, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("kind_real_consumer: no fact-kind const files matched %s", glob)
	}
	sort.Strings(matches)

	values := map[string]string{}
	fset := token.NewFileSet()
	for _, path := range matches {
		if isGoTestFile(path) {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vspec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vspec.Names {
					if i >= len(vspec.Values) {
						continue
					}
					lit, ok := vspec.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					unquoted, err := unquoteGoString(lit.Value)
					if err != nil {
						return nil, fmt.Errorf("kind_real_consumer: %s: const %s: %w", path, name.Name, err)
					}
					values[name.Name] = unquoted
				}
			}
		}
	}
	return values, nil
}

// unquoteGoString strips the double quotes a Go string-literal AST node
// carries. factschema's FactKind* constants are always plain double-quoted
// literals (never backtick raw strings), so trimming one leading and
// trailing quote byte is sufficient and avoids importing strconv just for
// Unquote's broader escape handling.
func unquoteGoString(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", fmt.Errorf("const value %q is not a plain double-quoted string literal", raw)
	}
	return raw[1 : len(raw)-1], nil
}

// rawSQLFactKindReaders scans every non-test .go file directly under dir
// for literal `fact_kind = '<kind>'` equality predicates and
// `fact_kind IN ('a', 'b', ...)` list predicates, returning the set of
// fact-kind strings found. This is the raw-SQL sibling of the decode-seam
// signal: a query-layer handler that reads a kind's rows via a literal SQL
// predicate is a real consumer even when no typed decode<Kind> wrapper
// exists for it (e.g. a kind read only through `payload->>'field'` JSONB
// projection rather than a decoded Go struct).
func rawSQLFactKindReaders(dir string) (map[string]bool, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("kind_real_consumer: glob raw SQL dir %s: %w", dir, err)
	}

	kinds := map[string]bool{}
	for _, path := range matches {
		if isGoTestFile(path) {
			continue
		}
		contents, err := readFileForGate(path)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: read %s: %w", path, err)
		}
		for _, m := range rawSQLFactKindPattern.FindAllStringSubmatch(contents, -1) {
			kinds[m[1]] = true
		}
		for _, m := range rawSQLFactKindINPattern.FindAllStringSubmatch(contents, -1) {
			for _, lit := range quotedLiteralPattern.FindAllStringSubmatch(m[1], -1) {
				kinds[lit[1]] = true
			}
		}
	}
	return kinds, nil
}

// factsPackageIdentRefKinds scans every non-test .go file directly under dir
// for references to go/internal/facts' own FactKind-suffixed constants
// (facts.<Ident>FactKind) and returns the set of wire fact-kind strings
// referenced, resolved through factsConstValues (from factKindConstantValues
// run against factsPackageConstFileGlob). This is the identifier-reference
// sibling of rawSQLFactKindReaders: some query-layer SQL is built by
// string-concatenating the Go constant rather than writing the fact-kind
// literal inline, which the literal-only regexes in rawSQLFactKindReaders
// cannot see.
func factsPackageIdentRefKinds(dir string, factsConstValues map[string]string) (map[string]bool, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("kind_real_consumer: glob %s: %w", dir, err)
	}

	kinds := map[string]bool{}
	for _, path := range matches {
		if isGoTestFile(path) {
			continue
		}
		contents, err := readFileForGate(path)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: read %s: %w", path, err)
		}
		for _, m := range factsPackageIdentRefPattern.FindAllStringSubmatch(contents, -1) {
			if wire, ok := factsConstValues[m[1]]; ok {
				kinds[wire] = true
			}
		}
	}
	return kinds, nil
}

// namedConstStoreKinds scans every non-test .go file directly under dir for
// a top-level exported string const declaration whose literal value looks
// like a registry fact-kind string, and returns the set of such values found
// in files that also reference "fact_kind" somewhere (evidence the constant
// actually backs a fact_kind-scoped query, not an unrelated string). See
// realConsumerNamedStoreDir's doc comment for the motivating case.
func namedConstStoreKinds(dir string) (map[string]bool, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("kind_real_consumer: glob %s: %w", dir, err)
	}

	kinds := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range matches {
		if isGoTestFile(path) {
			continue
		}
		contents, err := readFileForGate(path)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: read %s: %w", path, err)
		}
		if !strings.Contains(contents, "fact_kind") && !strings.Contains(contents, "FactKind") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vspec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i := range vspec.Names {
					if i >= len(vspec.Values) {
						continue
					}
					lit, ok := vspec.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					if unquoted, err := unquoteGoString(lit.Value); err == nil {
						kinds[unquoted] = true
					}
				}
			}
		}
	}
	return kinds, nil
}

// isGoTestFile reports whether path is a Go test file (ends in _test.go).
func isGoTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}

// readFileForGate reads path's contents as a string. Extracted as its own
// function so the gate's file reads share one error-wrapping shape.
func readFileForGate(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path comes from a glob over a fixed repo-relative directory, not user input
	if err != nil {
		return "", err
	}
	return string(data), nil
}
