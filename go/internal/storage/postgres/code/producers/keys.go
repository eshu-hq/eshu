// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import "strings"

const (
	// PackageKeyPrefix starts the import-binding key the reducer derives from a
	// definition's package_id and export_name (package:<package_id>#<export_name>).
	PackageKeyPrefix = "package:"

	// GoKeyPrefix starts the key the Go parser stamps on a function or method
	// definition: scip-go gomod <import path> <symbol>
	// (go/internal/parser/golang/scip_symbols.go).
	GoKeyPrefix = "scip-go gomod "
)

// Split separates symbol keys by how their producers are found: package keys,
// Go keys, and every other key. Each group keeps the input order. A scip-go key
// without a symbol after the import path is not a definition key and counts as
// an other key.
func Split(symbolKeys []string) (packageKeys, goKeys, otherKeys []string) {
	for _, key := range symbolKeys {
		switch {
		case strings.HasPrefix(key, PackageKeyPrefix):
			packageKeys = append(packageKeys, key)
		case GoImportPath(key) != "":
			goKeys = append(goKeys, key)
		default:
			otherKeys = append(otherKeys, key)
		}
	}
	return packageKeys, goKeys, otherKeys
}

// GoImportPath returns the import path of a scip-go gomod <import path>
// <symbol> key, or "" for any other key. The parser always emits a symbol after
// the import path, so a key with fewer than two fields after the prefix is not
// one of its definition keys.
func GoImportPath(key string) string {
	rest, ok := strings.CutPrefix(key, GoKeyPrefix)
	if !ok {
		return ""
	}
	importPath, symbol, ok := strings.Cut(rest, " ")
	if !ok || importPath == "" || strings.TrimSpace(symbol) == "" {
		return ""
	}
	return importPath
}

// GoModuleCandidates returns the import path and each of its '/'-prefixes,
// longest first. The declaring module's path is one of them. The list is a
// superset on purpose: a shorter module that does not own the package only adds
// a scope whose files the exact key match then rejects.
func GoModuleCandidates(importPath string) []string {
	var candidates []string
	for importPath != "" {
		candidates = append(candidates, importPath)
		cut := strings.LastIndex(importPath, "/")
		if cut < 0 {
			break
		}
		importPath = importPath[:cut]
	}
	return candidates
}

// PackageName returns the package_id of a package:<package_id>#<export_name>
// key, trimmed to match the trimmed manifest names, or "" when either part is
// empty. npm package names cannot contain '#', so the first '#' ends the name.
// Every key with the package: prefix takes the anchored path, so a key in any
// other shape (no '#', an empty part) resolves no producer and stays
// unresolved. No emitter produces such keys today; a new package: key shape must
// change this function too.
func PackageName(key string) string {
	packageName, exportName, ok := strings.Cut(strings.TrimPrefix(key, PackageKeyPrefix), "#")
	if !ok || strings.TrimSpace(packageName) == "" || strings.TrimSpace(exportName) == "" {
		return ""
	}
	return strings.TrimSpace(packageName)
}
