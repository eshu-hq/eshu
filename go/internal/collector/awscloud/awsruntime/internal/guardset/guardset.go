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
// service/<service>/runtimebind contributes <service> to the bindings set,
// and one that ends with service/<group>/<service>/runtimebind contributes
// the relative <group>/<service> token.
const importPathSuffix = "/internal/collector/awscloud/service/"

// runtimebindLeaf is the final path element every service registration import
// must carry.
const runtimebindLeaf = "runtimebind"

// ServiceFromImportPath extracts the service token from a runtimebind blank
// import path. It returns ("", false) for any import that is not exactly a
// service/<service>/runtimebind or service/<group>/<service>/runtimebind
// package, so unrelated imports and deeper nested packages are ignored rather
// than misattributed. Nested services report the relative <group>/<service>
// token so the directory walk and the import set stay comparable.
func ServiceFromImportPath(path string) (string, bool) {
	idx := strings.Index(path, importPathSuffix)
	if idx < 0 {
		return "", false
	}
	tail := path[idx+len(importPathSuffix):]
	parts := strings.Split(tail, "/")
	// Exactly <service>/runtimebind or <group>/<service>/runtimebind. Reject
	// <service>, <service>/runtimebind/extra, and empty tokens.
	if len(parts) != 2 && len(parts) != 3 {
		return "", false
	}
	if parts[len(parts)-1] != runtimebindLeaf {
		return "", false
	}
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			return "", false
		}
	}
	return strings.Join(parts[:len(parts)-1], "/"), true
}

// RuntimebindServiceDirs returns the sorted set of service tokens that have a
// service/<service>/runtimebind/ directory under servicesDir. Nested service
// groups (service/<group>/<service>/) contribute their relative
// <group>/<service> token, matching ServiceFromImportPath. This is the set of
// scanners the repository layout says SHOULD be registered.
func RuntimebindServiceDirs(servicesDir string) ([]string, error) {
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, fmt.Errorf("read service dir %q: %w", servicesDir, err)
	}
	var services []string
	collect := func(dir, token string) error {
		bindDir := filepath.Join(dir, token, runtimebindLeaf)
		info, statErr := os.Stat(bindDir)
		if errors.Is(statErr, os.ErrNotExist) {
			// Service directory without a runtimebind package; skip it.
			return nil
		}
		if statErr != nil {
			return fmt.Errorf("stat runtimebind dir %q: %w", bindDir, statErr)
		}
		if !info.IsDir() {
			return nil
		}
		services = append(services, token)
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := collect(servicesDir, entry.Name()); err != nil {
			return nil, err
		}
		// One nested level: AWS proper-noun services group under service/
		// to satisfy the filename-stutter gate (naming rule 3).
		nested, nestErr := os.ReadDir(filepath.Join(servicesDir, entry.Name()))
		if nestErr != nil {
			return nil, fmt.Errorf("read service group dir %q: %w", entry.Name(), nestErr)
		}
		for _, child := range nested {
			if !child.IsDir() {
				continue
			}
			if err := collect(servicesDir, filepath.Join(entry.Name(), child.Name())); err != nil {
				return nil, err
			}
		}
	}
	sort.Strings(services)
	return services, nil
}

// BindingsImportServices parses bindingsFile and returns the sorted set of
// service tokens from its service/<service>/runtimebind blank imports
// (relative <group>/<service> tokens for nested services). This is
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
