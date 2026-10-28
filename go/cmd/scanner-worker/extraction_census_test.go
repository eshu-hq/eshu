// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// censusRecordRel is the #8878 extraction prep census record, relative to
// the repository root. It must name every source family the cutover moves
// and the claim/resource boundary the extraction must preserve.
const censusRecordRel = "go/cmd/scanner-worker/EXTRACTION_PREP.md"

// censusSourceFamilies are the package directories the census record must
// account for. They are asserted against the tree so the record cannot drift
// silently when a family moves before extraction.
var censusSourceFamilies = []string{
	"go/cmd/scanner-worker",
	"go/internal/collector/scannerworker",
	"go/internal/collector/scannerworker/imageanalyzer",
	"go/internal/collector/scannerworker/sbomgenerator",
	"go/internal/collector/ospackagevulnerability",
	"go/internal/collector/ospackagevulnerability/osruntime",
}

// censusRecordMustName lists strings the census record must contain: the
// scope identity, the analyzer vocabulary, the emitted fact kinds, and the
// claim/resource boundary the extraction must preserve.
var censusRecordMustName = []string{
	"scanner_worker",
	"sbom_generation",
	"image_unpacking",
	"os_package_extraction",
	"vulnerability.os_package",
	"scanner_worker.analysis",
	"sbom.document",
	"ESHU_SCANNER_WORKER_MAX_FACTS",
	"FencingToken",
	"analyzer_not_configured",
}

func repoRootForCensus(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is go/cmd/scanner-worker/extraction_census_test.go.
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return filepath.Dir(root)
	}
	return root
}

func TestExtractionCensusRecordNamesSourceFamilies(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRootForCensus(t), censusRecordRel))
	if err != nil {
		t.Fatalf("read census record %s: %v", censusRecordRel, err)
	}
	text := string(body)
	for _, family := range censusSourceFamilies {
		if !strings.Contains(text, family) {
			t.Errorf("census record %s does not name source family %s", censusRecordRel, family)
		}
	}
	for _, want := range censusRecordMustName {
		if !strings.Contains(text, want) {
			t.Errorf("census record %s does not name %q", censusRecordRel, want)
		}
	}
}

func TestExtractionCensusPackagesExist(t *testing.T) {
	root := repoRootForCensus(t)
	for _, family := range censusSourceFamilies {
		info, err := os.Stat(filepath.Join(root, family))
		if err != nil {
			t.Errorf("census family %s missing from tree: %v", family, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("census family %s is not a directory", family)
		}
	}
}

func TestExtractionScopeIdentityAgrees(t *testing.T) {
	if string(scope.CollectorScannerWorker) != "scanner_worker" {
		t.Errorf("scope identity %q is not the scanner-worker collector kind", string(scope.CollectorScannerWorker))
	}
	if id := newClaimID(); !strings.HasPrefix(id, "scanner_worker-claim-") {
		t.Errorf("claim ID %q does not carry the scanner-worker scope prefix", id)
	}
}

// TestExtractionBoundaryConfigCarriesNoSecret guards the cutover contract:
// the config that crosses from the command into the analyzer packages (and
// later into the extracted repository) must carry non-secret identity and
// local-path configuration only. Scanner inputs hold no registry
// credentials by design; a future secret-bearing field must fail loudly
// here before it can cross the boundary. The walk is recursive: nested
// target structs carried inside runtimeConfig (osruntime.TargetConfig,
// imageanalyzer.TargetConfig and their element types) are checked too, so a
// secret-named field cannot hide one level down.
func TestExtractionBoundaryConfigCarriesNoSecret(t *testing.T) {
	banned := []string{"token", "secret", "password", "credential"}
	seen := map[reflect.Type]bool{}
	var walk func(path string, typ reflect.Type)
	walk = func(path string, typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			// Both the Go field name and the JSON wire name cross the
			// command-to-analyzer boundary (instance configuration decodes
			// via encoding/json), so a secret-bearing name must trip the
			// guard in either spelling.
			names := []string{strings.ToLower(field.Name)}
			if tag, _, _ := strings.Cut(field.Tag.Get("json"), ","); tag != "" && tag != "-" {
				names = append(names, strings.ToLower(tag))
			}
			for _, name := range names {
				for _, word := range banned {
					if strings.Contains(name, word) {
						t.Errorf("cross-boundary %s.%s carries %q material; scanner inputs hold no credentials",
							path, field.Name, word)
					}
				}
			}
			walk(path+"."+field.Name, field.Type)
		}
	}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(runtimeConfig{}),
		reflect.TypeOf(sbomgenerator.RepositoryTarget{}),
		reflect.TypeOf(resourceLimitsJSON{}),
	} {
		walk(typ.Name(), typ)
	}
}
