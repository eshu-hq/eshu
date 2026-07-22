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
	"sort"
	"strings"
)

// This file is the #5584 verification engine for the route-serves-data
// registry. All functions take the maps as parameters (never reading the
// package vars directly) so the BITES tests can run the exact production
// checks against poisoned copies — mirroring resolveKindConsumer's shape
// from the #5474 D2 gate.

// crossCheckRouteServesDataMap is gate A: the backing map and the registry
// must agree exactly. For every route, set(backing.ServedDomains) must equal
// set(registry.Served domains) ∪ set(registry.MapOnly domains), and the two
// maps must cover the same routes. Any drift — a poisoned ServedDomains
// entry, a registry claim the map does not admit, a route missing on either
// side — is a finding.
func crossCheckRouteServesDataMap(
	backing map[string]routeServesDataBacking,
	registry map[string]routeServesDataSource,
) []string {
	var findings []string
	for _, route := range sortedKeys(backing) {
		entry, ok := registry[route]
		if !ok {
			findings = append(findings, fmt.Sprintf("route %q is in routeServesDataBackingMap but has no routeServesDataRegistry entry — derive it from the real handler wiring", route))
			continue
		}
		claimed := map[string]bool{}
		for _, d := range backing[route].ServedDomains {
			claimed[d] = true
		}
		derived := map[string]string{}
		for _, s := range entry.Served {
			derived[s.Domain] = "served"
		}
		for _, m := range entry.MapOnly {
			if derived[m.Domain] != "" {
				findings = append(findings, fmt.Sprintf("route %q lists domain %q as BOTH Served and MapOnly — a claim cannot be evidenced and unevidenced at once", route, m.Domain))
			}
			derived[m.Domain] = "map-only"
		}
		for _, d := range sortedKeys(claimed) {
			if derived[d] == "" {
				findings = append(findings, fmt.Sprintf("route %q claims ServedDomains entry %q in routeServesDataBackingMap, but the handler-derived registry has no verified evidence for it — the map claim is unsupported (poisoned or stale)", route, d))
			}
		}
		for _, d := range sortedKeys(derived) {
			if !claimed[d] {
				findings = append(findings, fmt.Sprintf("route %q has verified registry evidence for domain %q, but routeServesDataBackingMap does not list it — the map under-claims; update it or move the registry entry to a disclosure", route, d))
			}
		}
	}
	for _, route := range sortedKeys(registry) {
		if _, ok := backing[route]; !ok {
			findings = append(findings, fmt.Sprintf("route %q is in routeServesDataRegistry but not in routeServesDataBackingMap — stale registry entry", route))
		}
	}
	return findings
}

// verifyRouteServesDataRegistry is gate B: every structural claim in the
// registry must hold against the real source tree under repoRoot. It returns
// findings instead of failing fast so a broken registry reports every lie at
// once.
func verifyRouteServesDataRegistry(
	repoRoot string,
	registry map[string]routeServesDataSource,
	signatures map[string]domainDataSignature,
) []string {
	var findings []string
	appendf := func(format string, args ...any) {
		findings = append(findings, fmt.Sprintf(format, args...))
	}
	for _, route := range sortedKeys(registry) {
		entry := registry[route]
		reg, err := readRepoFile(repoRoot, entry.RegistrationFile)
		if err != nil {
			appendf("route %q: %v", route, err)
			continue
		}
		want := `mux.HandleFunc("` + route + `", h.` + entry.Method + `)`
		if !strings.Contains(reg, want) {
			appendf("route %q: %s does not contain the registration %q — the route is not wired to the claimed method", route, entry.RegistrationFile, want)
		}
		fields, err := registryStructFields(filepath.Join(repoRoot, entry.StructFile), entry.HandlerStruct)
		if err != nil {
			appendf("route %q: %v", route, err)
			continue
		}
		body, err := registryMethodBody(filepath.Join(repoRoot, entry.MethodFile), "*"+entry.HandlerStruct, entry.Method)
		if err != nil {
			appendf("route %q: %v", route, err)
			continue
		}
		for _, served := range entry.Served {
			if _, ok := signatures[served.Domain]; !ok {
				appendf("route %q: served domain %q has no domainDataSignatures entry — closed set violated", route, served.Domain)
			}
			if served.StoreField != "" {
				fieldType, ok := fields[served.StoreField]
				if !ok {
					appendf("route %q domain %q: handler struct %s has no field named %q — the store claim is fabricated", route, served.Domain, entry.HandlerStruct, served.StoreField)
				} else if !strings.Contains(fieldType, served.StoreType) {
					appendf("route %q domain %q: field %s.%s is typed %q, not %q", route, served.Domain, entry.HandlerStruct, served.StoreField, fieldType, served.StoreType)
				}
				if !strings.Contains(body, "h."+served.StoreField) {
					appendf("route %q domain %q: method %s body never references h.%s — the store is not on this route's read path", route, served.Domain, entry.Method, served.StoreField)
				}
			}
			findings = append(findings, verifyEvidence(repoRoot, route, served.Domain, "served", served.Evidence)...)
		}
		for _, disc := range entry.Disclosed {
			if _, ok := signatures[disc.Domain]; !ok {
				appendf("route %q: disclosed domain %q has no domainDataSignatures entry", route, disc.Domain)
			}
			if disc.Reason == "" {
				appendf("route %q: disclosure for domain %q has no reason", route, disc.Domain)
			}
			if len(disc.Evidence) == 0 {
				appendf("route %q: disclosure for domain %q cites no evidence — a disclosure must be falsifiable", route, disc.Domain)
			}
			findings = append(findings, verifyEvidence(repoRoot, route, disc.Domain, "disclosed", disc.Evidence)...)
		}
		for _, m := range entry.MapOnly {
			if _, ok := signatures[m.Domain]; !ok {
				appendf("route %q: map-only domain %q has no domainDataSignatures entry", route, m.Domain)
			}
			if m.Reason == "" {
				appendf("route %q: map-only claim for domain %q has no reason", route, m.Domain)
			}
		}
	}
	return findings
}

// verifyEvidence asserts every cited marker appears verbatim in its file.
func verifyEvidence(repoRoot, route, domain, kind string, evidence []routeReadEvidence) []string {
	var findings []string
	for _, ev := range evidence {
		contents, err := readRepoFile(repoRoot, ev.File)
		if err != nil {
			findings = append(findings, fmt.Sprintf("route %q %s domain %q: %v", route, kind, domain, err))
			continue
		}
		if !strings.Contains(contents, ev.Marker) {
			findings = append(findings, fmt.Sprintf("route %q %s domain %q: %s does not contain the cited marker %q — the evidence is stale or fabricated", route, kind, domain, ev.File, ev.Marker))
		}
	}
	return findings
}

// verifyRouteServesDataScan is gate C, the map-independent anti-poison scan:
// for every route and every domain NOT declared as Served or Disclosed for
// that route, none of the domain's signature markers may appear in the
// route's read-path files and none of its store types may be wired
// (struct field + method-body reference). This is the check that makes a
// (route, domain) pairing impossible to smuggle in: real handler source is
// the oracle, so a backing-map edit — or a registry Served entry moved to
// MapOnly to dodge evidence requirements — contradicts the scan and goes RED.
func verifyRouteServesDataScan(
	repoRoot string,
	registry map[string]routeServesDataSource,
	signatures map[string]domainDataSignature,
) []string {
	var findings []string
	for _, route := range sortedKeys(registry) {
		entry := registry[route]
		allowed := map[string]bool{}
		for _, s := range entry.Served {
			allowed[s.Domain] = true
		}
		for _, d := range entry.Disclosed {
			allowed[d.Domain] = true
		}
		var scans []string
		for _, f := range entry.ScanFiles {
			contents, err := readRepoFile(repoRoot, f)
			if err != nil {
				findings = append(findings, fmt.Sprintf("route %q: %v", route, err))
				continue
			}
			scans = append(scans, contents)
		}
		fields, fieldsErr := registryStructFields(filepath.Join(repoRoot, entry.StructFile), entry.HandlerStruct)
		body, bodyErr := registryMethodBody(filepath.Join(repoRoot, entry.MethodFile), "*"+entry.HandlerStruct, entry.Method)
		for _, domain := range sortedKeys(signatures) {
			if allowed[domain] {
				continue
			}
			sig := signatures[domain]
			for _, marker := range sig.Markers {
				for i, contents := range scans {
					if strings.Contains(contents, marker) {
						findings = append(findings, fmt.Sprintf("route %q read path (%s) contains domain %q signature marker %q, but the pair is neither Served nor Disclosed — either the route really serves that domain (add verified Served evidence and a backing-map entry) or the touch needs a reviewed disclosure", route, entry.ScanFiles[i], domain, marker))
					}
				}
			}
			if fieldsErr != nil || bodyErr != nil {
				continue
			}
			for _, storeType := range sig.StoreTypes {
				for fieldName, fieldType := range fields {
					if strings.Contains(fieldType, storeType) && strings.Contains(body, "h."+fieldName) {
						findings = append(findings, fmt.Sprintf("route %q: handler %s field %s (type %s) matches domain %q store type %q and is referenced by %s, but the pair is neither Served nor Disclosed", route, entry.HandlerStruct, fieldName, fieldType, domain, storeType, entry.Method))
					}
				}
			}
		}
	}
	return findings
}

// verifyDomainSignaturesClosed asserts the signature set and the domain
// population of the backing map are identical — no unsigned domain, no
// stale signature.
func verifyDomainSignaturesClosed(
	backing map[string]routeServesDataBacking,
	signatures map[string]domainDataSignature,
) []string {
	var findings []string
	used := map[string]bool{}
	for _, route := range sortedKeys(backing) {
		for _, d := range backing[route].ServedDomains {
			used[d] = true
			if _, ok := signatures[d]; !ok {
				findings = append(findings, fmt.Sprintf("domain %q (route %q) has no domainDataSignatures entry — every backing-map domain needs a discriminative signature", d, route))
			}
		}
	}
	for _, d := range sortedKeys(signatures) {
		if !used[d] {
			findings = append(findings, fmt.Sprintf("domainDataSignatures has a stale entry for %q — no backing-map route serves it", d))
		}
	}
	return findings
}

// registryStructFields parses path and returns the named struct's fields as
// a name -> source-shaped type map (e.g. "Correlations" ->
// "KubernetesCorrelationStore", "Instruments" -> "*telemetry.Instruments").
// Embedded (anonymous) fields are keyed by their rendered type.
func registryStructFields(path, structName string) (map[string]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			tspec, ok := spec.(*ast.TypeSpec)
			if !ok || tspec.Name.Name != structName {
				continue
			}
			structType, ok := tspec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			fields := map[string]string{}
			for _, field := range structType.Fields.List {
				rendered := registryExprString(field.Type)
				if len(field.Names) == 0 {
					fields[rendered] = rendered
					continue
				}
				for _, name := range field.Names {
					fields[name.Name] = rendered
				}
			}
			return fields, nil
		}
	}
	return nil, fmt.Errorf("struct %q not found in %s", structName, path)
}

// registryMethodBody returns the literal source text of the named method's
// body for the given pointer receiver type, so callers can substring-search
// what the method actually touches.
func registryMethodBody(path, receiverType, methodName string) (string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	src, err := os.ReadFile(path) // #nosec G304 -- path is a fixed, committed repo file joined under repoRoot
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != methodName || fn.Body == nil {
			continue
		}
		if len(fn.Recv.List) != 1 || registryExprString(fn.Recv.List[0].Type) != receiverType {
			continue
		}
		start := fset.Position(fn.Body.Pos()).Offset
		end := fset.Position(fn.Body.End()).Offset
		return string(src[start:end]), nil
	}
	return "", fmt.Errorf("method (%s) %s not found in %s", receiverType, methodName, path)
}

// registryExprString renders an AST type expression back to source-shaped
// text ("*telemetry.Instruments", "KubernetesCorrelationStore") without
// pulling in go/printer.
func registryExprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + registryExprString(e.X)
	case *ast.SelectorExpr:
		return registryExprString(e.X) + "." + e.Sel.Name
	default:
		return ""
	}
}

// readRepoFile reads a repo-relative file under repoRoot.
func readRepoFile(repoRoot, rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, rel)) // #nosec G304 -- rel comes from the committed registry, not user input
	if err != nil {
		return "", fmt.Errorf("read %s: %w", rel, err)
	}
	return string(data), nil
}

// sortedKeys returns map keys in deterministic order for stable findings.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
