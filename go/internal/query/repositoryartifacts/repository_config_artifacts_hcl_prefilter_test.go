// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func hclRandomFileCount() int {
	if testing.Short() {
		return 1500
	}
	return 12000
}

// TestHCLPrefilterOutputEqualsShippedExtractor is the equality proof: over a
// generated corpus the prefiltered extractor returns byte-identical rows, in
// the same order, to the frozen pre-prefilter extractor.
func TestHCLPrefilterOutputEqualsShippedExtractor(t *testing.T) {
	t.Parallel()
	corpus := hclCorpus(hclRandomFileCount(), 7126)
	var withRows, skippedAny, skippedAll int
	for i, file := range corpus {
		files := []querycontract.FileContent{file}
		want := extractHCLConfigAssetRowsShipped("acme-repo", files)
		got := extractHCLConfigAssetRows("acme-repo", files)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("file %d (%s) rows differ\n content: %q\n shipped: %v\n gated:   %v", i, file.RelativePath, file.Content, want, got)
		}
		if len(want) > 0 {
			withRows++
		}
		plan := planHCLScans(hclScanPrefilters, file.Content)
		ran := countRunning(plan)
		if ran < 6 {
			skippedAny++
		}
		if ran == 0 {
			skippedAll++
		}
	}
	t.Logf("corpus files=%d with_rows=%d skipped_any_scan=%d skipped_all_scans=%d", len(corpus), withRows, skippedAny, skippedAll)
	// Vacuity guards: the proof only means something if rows were produced
	// and if the gates really skipped scans.
	if withRows < len(corpus)/10 {
		t.Fatalf("only %d of %d corpus files emit rows; the corpus is too weak", withRows, len(corpus))
	}
	if skippedAny < len(corpus)/4 || skippedAll < len(corpus)/20 {
		t.Fatalf("gates skipped scans on %d files (all scans on %d) of %d; the prefilter is not exercised", skippedAny, skippedAll, len(corpus))
	}
}

func countRunning(p hclScanPlan) int {
	n := 0
	for _, b := range []bool{p.dependencyConfigPath, p.readConfig, p.includePath, p.findInParentFolders, p.configFunction, p.moduleSource} {
		if b {
			n++
		}
	}
	return n
}

// TestHCLPrefilterOutputEqualsShippedExtractorAcrossFileLists proves the
// equality also holds when several files share the dedupe state.
func TestHCLPrefilterOutputEqualsShippedExtractorAcrossFileLists(t *testing.T) {
	t.Parallel()
	corpus := hclCorpus(0, 1)
	for start := 0; start+40 <= len(corpus); start += 997 {
		files := corpus[start : start+40]
		want := extractHCLConfigAssetRowsShipped("acme-repo", files)
		got := extractHCLConfigAssetRows("acme-repo", files)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("file list at %d differs\n shipped: %v\n gated:   %v", start, want, got)
		}
	}
}

// TestHCLPrefilterEdgeInputsMatchShipped covers the inputs a generated mix
// may not reach: empty, whitespace, huge, binary and boundary splits.
func TestHCLPrefilterEdgeInputsMatchShipped(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("resource \"aws_s3_bucket\" \"b\" {\n  bucket = \"x\"\n}\n", 4000)
	cases := map[string]string{
		"empty":               "",
		"whitespace":          " \n\t\r\n ",
		"nul only":            "\x00\x00\x00",
		"invalid utf8":        "\xff\xfe\xfd module \"a\" {\n source = \"./a\"\n}\n\xff",
		"crlf module":         "module \"a\" {\r\n  source = \"./a\"\r\n}\r\n",
		"cr only lines":       "dependency \"a\" {\rconfig_path = \"../a\"\r}\r",
		"huge no match":       big,
		"huge with match end": big + "module \"a\" {\n  source = \"./a\"\n}\n",
		"huge with match top": "module \"a\" {\n  source = \"./a\"\n}\n" + big,
		"source split":        "module \"a\" {\n  sou" + "rce = \"./a\"\n}\n",
		"long s source":       "module \"a\" {\n  \u017fource = \"./a\"\n}\n",
		"kelvin only":         "\u212a\u212a\u212a",
		"locals boundary":     "locals{\n  a = \"./a\"\n}\nx = file(local.a)\n",
	}
	for name, content := range cases {
		for _, p := range []string{"main.tf", "terragrunt.hcl", "x.hcl"} {
			files := []querycontract.FileContent{{RelativePath: p, Content: content}}
			want := extractHCLConfigAssetRowsShipped("r", files)
			got := extractHCLConfigAssetRows("r", files)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s (%s): shipped %v, gated %v", name, p, want, got)
			}
		}
	}
}

// TestHCLPrefilterDerivationIsPinned pins what the derivation reads from
// each production pattern. Review the diff of this table when a pattern
// changes: it is the human-readable statement of what each gate requires.
func TestHCLPrefilterDerivationIsPinned(t *testing.T) {
	t.Parallel()
	want := []string{
		`("config_path"~ AND "=")`,
		`("read_terragrunt_config("~ AND (("find_in_parent_folders("~ AND ")") OR "\""))`,
		`("find_in_parent_folders("~ AND \b"path"~ AND "=" AND ")")`,
		`("find_in_parent_folders("~ AND ")")`,
		`(\b"file("~ OR \b"templatefile("~)`,
		`((("module"~ AND "\"" AND "{") OR (\b"terraform"~ AND "{")) AND \b"source"~\b AND "=")`,
	}
	if hclScanPrefilters.Len() != len(want) {
		t.Fatalf("prefilter set has %d scans, want %d", hclScanPrefilters.Len(), len(want))
	}
	for i, w := range want {
		if !hclScanPrefilters.Gated(i) {
			t.Errorf("scan %d is not gated", i)
		}
		if got := hclScanPrefilters.Describe(i); got != w {
			t.Errorf("scan %d derived %s, want %s", i, got, w)
		}
	}
}

// TestHCLPrefilterPlansTheShapesTheRealSetIsMadeOf pins the selectivity that
// motivates the change: lock files and plain resource files run no regex
// scan, a required_providers file runs only the module-source scan, and a
// terragrunt include runs only the parent-folder scans.
func TestHCLPrefilterPlansTheShapesTheRealSetIsMadeOf(t *testing.T) {
	t.Parallel()
	lock := "# This file is maintained automatically by \"terraform init\".\nprovider \"registry.terraform.io/hashicorp/aws\" {\n  version = \"5.0.0\"\n  hashes = [\n    \"h1:x=\",\n  ]\n}\n"
	plain := "resource \"aws_s3_bucket\" \"b\" {\n  bucket = \"x\"\n  acl = \"private\"\n}\n"
	providers := "terraform {\n  required_providers {\n    aws = {\n      source = \"hashicorp/aws\"\n    }\n  }\n}\n"
	include := "include \"root\" {\n  path = find_in_parent_folders()\n}\n"
	cases := []struct {
		name    string
		content string
		want    hclScanPlan
	}{
		{"provider lock file", lock, hclScanPlan{}},
		{"plain resource file", plain, hclScanPlan{}},
		{"required_providers only", providers, hclScanPlan{moduleSource: true}},
		{"terragrunt include", include, hclScanPlan{includePath: true, findInParentFolders: true}},
		{"function call", "x = file(\"a.json\")\n", hclScanPlan{configFunction: true}},
		{"locals keyword alone", "locals {\n  a = 1\n}\n", hclScanPlan{locals: true}},
	}
	for _, tc := range cases {
		if got := planHCLScans(hclScanPrefilters, tc.content); got != tc.want {
			t.Errorf("%s: plan %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestHCLLocalsGateIsCaseSensitiveAndExact documents what the locals walk
// requires, and proves the plan flag is exactly that: the walk finds no
// assignment in text without the case-sensitive keyword.
func TestHCLLocalsGateIsCaseSensitiveAndExact(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		content string
		locals  bool
	}{
		{"locals {\n  a = \"./a\"\n}\n", true},
		{"LOCALS {\n  a = \"./a\"\n}\n", false},
		{"Locals {\n  a = \"./a\"\n}\n", false},
		{"\u017focals {\n  a = \"./a\"\n}\n", false},
		{"", false},
	} {
		if got := planHCLScans(hclScanPrefilters, tc.content).locals; got != tc.locals {
			t.Errorf("locals flag for %q = %v, want %v", tc.content, got, tc.locals)
		}
		if !tc.locals {
			if got := extractLocalConfigAssignments("main.tf", tc.content); len(got) != 0 {
				t.Errorf("walk found assignments %v in text the gate rejects: %q", got, tc.content)
			}
		}
	}
}

// TestHCLPrefilterSkipsLocalWalkOnlyWhenNoScanReadsIt guards the one place
// the plan skips non-regex work: the locals map is needed only by scans that
// resolve expressions through it.
func TestHCLPrefilterSkipsLocalWalkOnlyWhenNoScanReadsIt(t *testing.T) {
	t.Parallel()
	for _, p := range []hclScanPlan{
		{locals: true},
		{locals: true, readConfig: true, includePath: true, findInParentFolders: true},
	} {
		if p.needsLocalAssignments() {
			t.Errorf("plan %+v must not need the locals map", p)
		}
	}
	for _, p := range []hclScanPlan{
		{locals: true, dependencyConfigPath: true},
		{locals: true, configFunction: true},
		{locals: true, moduleSource: true},
	} {
		if !p.needsLocalAssignments() {
			t.Errorf("plan %+v must need the locals map", p)
		}
	}
	if (hclScanPlan{moduleSource: true}).needsLocalAssignments() {
		t.Error("no locals keyword means there is no map to resolve against")
	}
}

// TestHCLPrefilterIsSafeForConcurrentUse documents that the shared package
// level prefilter set holds no per-call state: the story builder may call the
// extractor from several requests at once.
func TestHCLPrefilterIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	corpus := hclCorpus(0, 1)
	want := make([][]map[string]any, 64)
	for i := range want {
		want[i] = extractHCLConfigAssetRowsShipped("r", corpus[i*7:i*7+1])
	}
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 20; round++ {
				for i := range want {
					if got := extractHCLConfigAssetRows("r", corpus[i*7:i*7+1]); !reflect.DeepEqual(got, want[i]) {
						errs <- fmt.Sprintf("file %d differs under concurrency", i)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
