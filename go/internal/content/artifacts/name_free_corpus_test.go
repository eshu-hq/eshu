// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Corpus and name generators for the name-free proof; see
// name_free_harness_test.go.

package artifacts

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// nameFreeNamesFor returns the names to bind a file set to: fixed names that cover
// the ruling's classes, plus names derived from the tree's own path components
// (equal, a proper prefix, a suffix, and equal plus a letter), so the name
// actually meets a path component.
func nameFreeNamesFor(files []File, rng *rand.Rand) []string {
	names := []string{
		"", "svc", "env", "modules", "accounts", "configd", "roles", "payments", "group_vars", "a.b*c[d]", "(.*)", "^svc$", "svc|api",
		"$1", "..", ".", "[", `a\b`, "ü-é", " svc ", "svc/api", "%s",
	}
	raw := nameFreeFlatten(BuildConfigPaths("", files))
	set := map[string]struct{}{}
	add := func(p string) {
		for _, part := range strings.Split(p, "/") {
			if part != "" && part != "." && part != ".." {
				set[part] = struct{}{}
			}
		}
	}
	for _, r := range raw {
		add(r.Path)
		add(r.Rel)
	}
	for _, f := range files {
		add(f.RelativePath)
	}
	components := make([]string, 0, len(set))
	for c := range set {
		components = append(components, c)
	}
	sort.Strings(components)
	rng.Shuffle(len(components), func(i, j int) { components[i], components[j] = components[j], components[i] })
	if len(components) > 6 {
		components = components[:6]
	}
	for _, c := range components {
		names = append(names, c, c+"x")
		if len(c) >= 3 {
			names = append(names, c[:len(c)-1], c[1:])
		}
	}
	return names
}

// nameFreeSafeComponent returns name when the kustomize policy-path pattern can
// carry it, so a kustomize row's path holds the name as a component.
func nameFreeSafeComponent(name string) string {
	if regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(name) {
		return name
	}
	return "svc"
}

// nameFreeNameBearingFiles builds a file set in which name appears as a path
// component in the file path and in the references the extractors resolve, and
// in which two raw references collapse to one named path in one file.
func nameFreeNameBearingFiles(name string) []File {
	p := name
	if p == "" {
		p = "svc"
	}
	return []File{
		fileRow(p+"/env/a.tfvars", "x = 1"),
		fileRow("env/b.tfvars", "x = 1"),
		fileRow(p+"s/c.tfvars", "x = 1"),
		fileRow("docker-compose.yml", "services:\n  app:\n    env_file:\n      - "+p+"/conf/.env\n      - conf/.env\n      - ./"+p+"/conf/.env\n"),
		fileRow(p+"/main.tf", "module \"a\" {\n  source = \"../"+p+"/modules/a\"\n}\nmodule \"b\" {\n  source = \"./modules/a\"\n}\n"),
		fileRow("terragrunt.hcl", "dependency \"d\" {\n  config_path = \""+p+"/dep\"\n}\ndependency \"e\" {\n  config_path = \"./dep\"\n}\n"),
		fileRow(p+"/group_vars/all.yml", "a: b"),
		fileRow(p+"/inventories/prod/hosts.yml", "all: {}"),
		fileRow(p+"/kustomization.yaml", kustomizationFor("res.yaml")),
		fileRow(p+"/res.yaml", policyResource(nameFreeSafeComponent(p))),
		fileRow(p+"/roles/"+nameFreeSafeComponent(p)+"/tasks/main.yml", "- debug: {}"),
	}
}

func nameFreeLoadCorpus(t *testing.T) []nameFreeCase {
	t.Helper()
	handle, err := os.Open(filepath.Join("testdata", "name_free_corpus.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	var cases []nameFreeCase
	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		var c nameFreeCase
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, c)
	}
	return cases
}

// nameFreeEcosystemRepos reads tests/fixtures as repositories: every fixture
// directory and each of its child directories is one file set, filtered the way
// the loader filters (config candidate, non-blank content).
func nameFreeEcosystemRepos(t *testing.T) map[string][]File {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "tests", "fixtures")
	repos := map[string][]File{}
	add := func(dir string) {
		var files []File
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(dir, p)
			if relErr != nil {
				return nil
			}
			content, readErr := os.ReadFile(p)
			if readErr != nil || len(content) > 256<<10 {
				return nil
			}
			file := File{RelativePath: filepath.ToSlash(rel), Content: string(content)}
			if isConfigArtifactCandidate(file) && strings.TrimSpace(file.Content) != "" {
				files = append(files, file)
			}
			return nil
		})
		if len(files) > 0 {
			repos[dir] = files
		}
	}
	top, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range top {
		if !entry.IsDir() {
			continue
		}
		add(filepath.Join(root, entry.Name()))
		children, _ := os.ReadDir(filepath.Join(root, entry.Name()))
		for _, child := range children {
			if child.IsDir() {
				add(filepath.Join(root, entry.Name(), child.Name()))
			}
		}
	}
	return repos
}

func nameFreeRunCorpus(t *testing.T, cfg nameFreeApply, seeds int64) *nameFreeAgg {
	t.Helper()
	agg := &nameFreeAgg{}
	rng := rand.New(rand.NewSource(7126))
	run := func(label string, files []File, extraNames ...string) {
		for _, name := range append(nameFreeNamesFor(files, rng), extraNames...) {
			agg.check(label, name, files, cfg)
		}
	}
	for i, c := range nameFreeLoadCorpus(t) {
		run(fmt.Sprintf("recorded[%d]", i), c.files(), c.Name)
	}
	run("kitchen-sink", kitchenSinkFiles())
	mutants := mutantCorpus()
	scenarios := make([]string, 0, len(mutants))
	for scenario := range mutants {
		scenarios = append(scenarios, scenario)
	}
	sort.Strings(scenarios)
	for _, scenario := range scenarios {
		run("mutant-corpus/"+scenario, mutants[scenario])
	}
	ecosystem := nameFreeEcosystemRepos(t)
	dirs := make([]string, 0, len(ecosystem))
	for dir := range ecosystem {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		run("tests/fixtures/"+filepath.ToSlash(dir), ecosystem[dir], path.Base(filepath.ToSlash(dir)))
	}
	// PR1's generated trees, plain and with every path under a name directory.
	for seed := int64(1); seed <= seeds; seed++ {
		files := randomTree(rand.New(rand.NewSource(seed)))
		run(fmt.Sprintf("seed-%d", seed), files)
		nameDir := []string{"svc", "env", "deploy", "ops"}[seed%4]
		nested := make([]File, len(files))
		for i, f := range files {
			f.RelativePath = nameDir + "/" + f.RelativePath
			nested[i] = f
		}
		run(fmt.Sprintf("seed-%d-nested-%s", seed, nameDir), nested, nameDir)
	}
	// Name-bearing trees, generated per name so the name sits inside the
	// references and two raw paths collapse.
	for _, name := range []string{"", "svc", "env", "a.b*c[d]", "(.*)", "..", "ü-é", "svc/api"} {
		files := nameFreeNameBearingFiles(name)
		agg.check("name-bearing", name, files, cfg)
		agg.check("name-bearing/other-name", "other", files, cfg)
	}
	return agg
}
