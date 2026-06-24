// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package guardset

import (
	"errors"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// importPathSuffix is the trailing module path under which every AWS service
// scanner registers itself. A blank import that ends with
// services/<service>/runtimebind contributes <service> to the bindings set.
const importPathSuffix = "/internal/collector/awscloud/services/"

// runtimebindLeaf is the final path element every service registration import
// must carry.
const runtimebindLeaf = "runtimebind"

// ServiceFromImportPath extracts the service token from a runtimebind blank
// import path. It returns ("", false) for any import that is not exactly a
// services/<service>/runtimebind package, so unrelated imports and deeper
// nested packages are ignored rather than misattributed.
func ServiceFromImportPath(path string) (string, bool) {
	idx := strings.Index(path, importPathSuffix)
	if idx < 0 {
		return "", false
	}
	tail := path[idx+len(importPathSuffix):]
	parts := strings.Split(tail, "/")
	// Exactly <service>/runtimebind. Reject <service>,
	// <service>/runtimebind/extra, and empty service tokens.
	if len(parts) != 2 {
		return "", false
	}
	service, leaf := parts[0], parts[1]
	if service == "" || leaf != runtimebindLeaf {
		return "", false
	}
	return service, true
}

// RuntimebindServiceDirs returns the sorted set of service tokens that have a
// services/<service>/runtimebind/ directory under servicesDir. This is the set
// of scanners the repository layout says SHOULD be registered.
func RuntimebindServiceDirs(servicesDir string) ([]string, error) {
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, fmt.Errorf("read services dir %q: %w", servicesDir, err)
	}
	var services []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		bindDir := filepath.Join(servicesDir, entry.Name(), runtimebindLeaf)
		info, statErr := os.Stat(bindDir)
		if errors.Is(statErr, os.ErrNotExist) {
			// Service directory without a runtimebind package; skip it.
			continue
		}
		if statErr != nil {
			return nil, fmt.Errorf("stat runtimebind dir %q: %w", bindDir, statErr)
		}
		if !info.IsDir() {
			continue
		}
		services = append(services, entry.Name())
	}
	sort.Strings(services)
	return services, nil
}

// BindingsImportServices parses bindingsFile and returns the sorted set of
// service tokens from its services/<service>/runtimebind blank imports. This is
// the set of scanners that ARE wired into the runtime aggregator.
func BindingsImportServices(bindingsFile string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, bindingsFile, nil, goparser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse bindings file %q: %w", bindingsFile, err)
	}
	seen := map[string]struct{}{}
	for _, spec := range file.Imports {
		// Only blank imports (_ "...") trigger the init side effects that
		// register scanners; named or default imports do not contribute to
		// the bindings set, so the helper matches its documented contract.
		if spec.Name == nil || spec.Name.Name != "_" {
			continue
		}
		path, unquoteErr := importPath(spec)
		if unquoteErr != nil {
			return nil, unquoteErr
		}
		service, ok := ServiceFromImportPath(path)
		if !ok {
			continue
		}
		seen[service] = struct{}{}
	}
	services := make([]string, 0, len(seen))
	for service := range seen {
		services = append(services, service)
	}
	sort.Strings(services)
	return services, nil
}

// importPath unquotes the literal import path of a parsed import spec.
func importPath(spec *ast.ImportSpec) (string, error) {
	path, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return "", fmt.Errorf("unquote import path %q: %w", spec.Path.Value, err)
	}
	return path, nil
}

// Diff compares the directory set against the imports set and returns the
// service tokens present in dirs but absent from imports (missing) and present
// in imports but absent from dirs (extra). Both inputs are de-duplicated first,
// so repeated entries do not distort the result. A non-empty missing slice
// means a runtimebind directory exists without a matching bindings.go import,
// which is the unwired-scanner failure the guard exists to catch.
func Diff(dirs, imports []string) (missing, extra []string) {
	dirSet := toSet(dirs)
	importSet := toSet(imports)
	for service := range dirSet {
		if _, ok := importSet[service]; !ok {
			missing = append(missing, service)
		}
	}
	for service := range importSet {
		if _, ok := dirSet[service]; !ok {
			extra = append(extra, service)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

// toSet collapses a slice into a set, dropping duplicates.
func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}
