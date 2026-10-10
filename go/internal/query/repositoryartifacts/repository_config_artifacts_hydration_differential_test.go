// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"fmt"
	"math/rand"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// poisonContents holds content that moves a different extractor each. Any
// file may receive any of them, so a file class whose content an extractor
// would read in a new way meets content that makes the read matter.
var poisonContents = []string{
	"config_path = \"../shared/terragrunt.hcl\"\nmodule \"m\" {\n  source = \"./modules/m\"\n}\n",
	"include {\n  path = find_in_parent_folders()\n}\n",
	"services:\n  app:\n    env_file:\n      - ./app.env\n    configs:\n      - cfg\nconfigs:\n  cfg:\n    file: ./cfg.json\n",
	"- hosts: all\n  roles:\n    - web\n  tasks: []\n",
	"resources:\n  - a.yaml\n  - ../b\n  - c.json\n",
	"Statement:\n  - Resource: arn:aws:ssm:us-east-1:1:parameter/configd/poison/*\n",
	`{"Resource": "/api/poison/*"}`,
	"key: value\n",
	"not: [valid",
	"",
	" ",
	"\n\t\n",
	"\u00a0",
	"\u3000\u2028",
	"\ufeff",
}

var pathStems = []string{
	"kustomization.yaml", "kustomization.yml", "kustomization", "KUSTOMIZATION.YAML",
	"docker-compose.yml", "docker-compose.override.yaml", "compose.yaml",
	"main.tf", "vars.tf", "terragrunt.hcl", "root.hcl", "env.tfvars", "env.tfvars.json",
	"deploy.yml", "site.yml", "playbook.yaml", "ci-playbook.yml", "main.yml", "hosts.yml",
	"values.yaml", "package.json", "policy.json", "res.yaml", "res.yml", "data.json",
	"notes.txt", "Dockerfile", "README.md", "ü-é.yaml", "a {b}.yaml", "100%_x.json",
}

var pathDirs = []string{
	"", "deploy/", "deploy/base/", "deploy/overlays/prod/", "infra/", "ops/", "ops/playbooks/",
	"ops/roles/web/tasks/", "ops/roles/web/vars/", "ops/group_vars/", "ops/inventories/prod/",
	"charts/", "a b/", "./deploy/",
}

// randomTree builds a repository from a seeded generator: random paths from
// the class corpus, kustomizations that reference other files in the tree (by
// file, by directory, missing, remote), and random content from the poison
// pool.
func randomTree(rng *rand.Rand) []querycontract.FileContent {
	count := 4 + rng.Intn(70)
	byPath := map[string]querycontract.FileContent{}
	order := make([]string, 0, count)
	for i := 0; i < count; i++ {
		relativePath := pathDirs[rng.Intn(len(pathDirs))] + pathStems[rng.Intn(len(pathStems))]
		if rng.Intn(25) == 0 {
			relativePath = " " + relativePath + " "
		}
		if _, ok := byPath[relativePath]; ok {
			continue
		}
		byPath[relativePath] = fileRow(relativePath, poisonContents[rng.Intn(len(poisonContents))])
		order = append(order, relativePath)
	}
	for _, relativePath := range order {
		file := byPath[relativePath]
		cleaned := strings.TrimSpace(relativePath)
		if !isKustomizationFile(cleaned) || rng.Intn(5) == 0 {
			continue
		}
		dir := path.Dir(CleanRepositoryRelativePath(cleaned))
		refs := make([]string, 0, 4)
		for n := rng.Intn(5); n > 0; n-- {
			switch rng.Intn(6) {
			case 0:
				refs = append(refs, "https://example.com/remote.yaml")
			case 1:
				refs = append(refs, "does-not-exist.yaml")
			default:
				target := order[rng.Intn(len(order))]
				rel, err := filepath.Rel(dir, CleanRepositoryRelativePath(target))
				if err != nil {
					continue
				}
				if rng.Intn(4) == 0 {
					rel = path.Dir(rel)
				}
				refs = append(refs, rel)
			}
		}
		file.Content = kustomizationFor(refs...)
		byPath[relativePath] = file
	}
	files := make([]querycontract.FileContent, 0, len(order))
	for _, relativePath := range order {
		files = append(files, byPath[relativePath])
	}
	return files
}

func TestNeededContentHydrationMatchesFullHydrationOnGeneratedTrees(t *testing.T) {
	t.Parallel()

	nonEmpty := 0
	for seed := int64(1); seed <= 400; seed++ {
		files := randomTree(rand.New(rand.NewSource(seed)))
		name := fmt.Sprintf("seed %d", seed)
		oracle, got, oracleStore, gotStore := requireSameConfigOutput(t, name,
			func() *hydrationStore { return newHydrationStore(files...) }, productionConfigArtifactPasses)
		if oracle.rows > 0 {
			nonEmpty++
		}
		if len(gotStore.getCalls) > len(oracleStore.getCalls) {
			t.Fatalf("%s: needed-content pass issued %d reads, full pass %d; it must never read more", name, len(gotStore.getCalls), len(oracleStore.getCalls))
		}
		_ = got
	}
	if nonEmpty < 100 {
		t.Fatalf("only %d of 400 generated trees produced rows; the generator does not exercise the extractors", nonEmpty)
	}
}

// configContentMutants are deliberate defects in the needed-content pass.
// Each one must change the output of at least one scenario in the corpus
// below; a mutant that survives means no test depends on that decision.
func configContentMutants() map[string]struct {
	policy configHydrationPolicy
	store  func(*hydrationStore)
} {
	type mutant = struct {
		policy configHydrationPolicy
		store  func(*hydrationStore)
	}
	without := func(drop func(querycontract.FileContent) bool) configHydrationPolicy {
		return configHydrationPolicy{
			readsContent: func(file querycontract.FileContent) bool {
				if drop(file) {
					return false
				}
				return configArtifactReadsContent(file)
			},
			planResources: planKustomizeResourceReads,
		}
	}
	production := productionConfigHydration
	return map[string]mutant{
		"drop kustomization branch": {policy: without(func(f querycontract.FileContent) bool {
			return isKustomizationIndexPath(CleanRepositoryRelativePath(f.RelativePath))
		})},
		"drop compose branch": {policy: without(IsDockerComposeArtifact)},
		"drop HCL source branch": {policy: without(func(f querycontract.FileContent) bool {
			return classifyHCLConfigFile(f.RelativePath) == hclConfigSourceFile
		})},
		"drop ansible playbook content branch": {policy: without(func(f querycontract.FileContent) bool {
			lowerPath, lowerBase, ok := ansiblePathKeys(f)
			return ok && ansiblePlaybookReadsContent(lowerPath, lowerBase)
		})},
		"drop phase 2 resource reads": {policy: configHydrationPolicy{
			readsContent:  production.readsContent,
			planResources: func([]querycontract.FileContent) map[string]struct{} { return nil },
		}},
		"phase 2 reads only the first kustomization": {policy: configHydrationPolicy{
			readsContent: production.readsContent,
			planResources: func(files []querycontract.FileContent) map[string]struct{} {
				for i, file := range files {
					if isKustomizationIndexPath(CleanRepositoryRelativePath(file.RelativePath)) {
						return planKustomizeResourceReads(files[i : i+1])
					}
				}
				return nil
			},
		}},
		"blank means empty string only": {policy: production, store: func(s *hydrationStore) {
			s.blankMode = func(content string) bool { return content != "" }
		}},
		"blank is never": {policy: production, store: func(s *hydrationStore) {
			s.blankMode = func(string) bool { return false }
		}},
		"blank is always": {policy: production, store: func(s *hydrationStore) {
			s.blankMode = func(string) bool { return true }
		}},
		"blank trims only ASCII space": {policy: production, store: func(s *hydrationStore) {
			s.blankMode = func(content string) bool { return strings.Trim(content, " ") != "" }
		}},
	}
}

func mutantCorpus() map[string][]querycontract.FileContent {
	corpus := map[string][]querycontract.FileContent{"kitchen sink": kitchenSinkFiles()}
	corpus["whitespace path-only files"] = []querycontract.FileContent{
		fileRow("env/a.tfvars", "\u00a0\u3000"),
		fileRow("env/b.tfvars", "\n"),
		fileRow("env/c.tfvars", "\u0085"),
		fileRow("env/d.tfvars", "x = 1"),
		fileRow("ops/group_vars/e.yml", "\t "),
		fileRow("ops/group_vars/f.yml", "a: b"),
	}
	corpus["bom only path-only file"] = []querycontract.FileContent{
		fileRow("env/bom.tfvars", "\ufeff"),
		fileRow("ops/group_vars/bom.yml", "\ufeff"),
	}
	corpus["second kustomization resource"] = []querycontract.FileContent{
		fileRow("a/kustomization.yaml", kustomizationFor("one.yaml")),
		fileRow("a/one.yaml", policyResource("one")),
		fileRow("b/kustomization.yaml", kustomizationFor("two.yaml")),
		fileRow("b/two.yaml", policyResource("two")),
	}
	return corpus
}

func TestNeededContentHydrationMutantsAreKilled(t *testing.T) {
	t.Parallel()

	corpus := mutantCorpus()
	for mutantName, mutant := range configContentMutants() {
		mutantName, mutant := mutantName, mutant
		t.Run(mutantName, func(t *testing.T) {
			t.Parallel()
			passes := configArtifactPasses{hydrate: mutant.policy.hydrate, build: buildRepositoryConfigArtifacts}
			killedBy := ""
			for scenario, files := range corpus {
				newStore := func() *hydrationStore {
					store := newHydrationStore(files...)
					if mutant.store != nil {
						mutant.store(store)
					}
					return store
				}
				oracleStore := newHydrationStore(files...)
				oracle := runConfigLoad(t, oracleStore, oracleStore.listed(t), fullConfigArtifactPasses)
				gotStore := newStore()
				got := runConfigLoad(t, gotStore, gotStore.listed(t), passes)
				if oracle.digest != got.digest {
					killedBy = scenario
					break
				}
			}
			if killedBy == "" {
				t.Fatalf("mutant %q survived: no scenario in the corpus changed the output", mutantName)
			}
		})
	}
}

// TestNeededContentHydrationSupersetReadIsOutputNeutral shows the safety
// direction: reading more than the plan asks for cannot change the output. A
// predicate that reads every file is the full pass.
func TestNeededContentHydrationSupersetReadIsOutputNeutral(t *testing.T) {
	t.Parallel()

	readEverything := configHydrationPolicy{
		readsContent:  func(querycontract.FileContent) bool { return true },
		planResources: planKustomizeResourceReads,
	}
	passes := configArtifactPasses{hydrate: readEverything.hydrate, build: buildRepositoryConfigArtifacts}
	for scenario, files := range mutantCorpus() {
		files := files
		requireSameConfigOutput(t, scenario, func() *hydrationStore { return newHydrationStore(files...) }, passes)
	}
}

// seededContentReadingBuild is a stand-in for a future extractor branch that
// reads the content of a file class the planner does not fetch: any .json
// file that mentions the marker emits a row. It is the planted violation for
// the guard below.
func seededContentReadingBuild(repoName string, files []querycontract.FileContent) map[string]any {
	result := buildRepositoryConfigArtifacts(repoName, files)
	rows := querycontract.MapSliceValue(result, "config_paths")
	for _, file := range files {
		if strings.HasSuffix(strings.ToLower(file.RelativePath), ".json") && strings.Contains(file.Content, "SEEDED_BRANCH") {
			rows = append(rows, map[string]any{
				"path": "/seeded", "source_repo": repoName,
				"relative_path": file.RelativePath, "evidence_kind": "seeded",
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return map[string]any{"config_paths": rows}
}

// TestNeededContentHydrationGuardCatchesUnplannedContentRead is the
// seeded-violation RED/GREEN pair for the equality harness. GREEN: the real
// extractors and the real plan agree with full hydration on content that
// would move any branch. RED: the same harness, given an extractor that reads
// content of a class the plan skips, reports a difference. A new extractor
// branch that reads such content fails the generated-tree test the same way.
func TestNeededContentHydrationGuardCatchesUnplannedContentRead(t *testing.T) {
	t.Parallel()

	files := []querycontract.FileContent{
		fileRow("web/data.json", `{"marker": "SEEDED_BRANCH"}`),
		fileRow("infra/main.tf", "# nothing\n"),
	}
	newStore := func() *hydrationStore { return newHydrationStore(files...) }

	t.Run("green: real extractors", func(t *testing.T) {
		t.Parallel()
		_, _, _, _, equal := compareHydrationPasses(t, newStore, true, productionConfigArtifactPasses)
		if !equal {
			t.Fatal("real extractors differ from full hydration on the guard fixture")
		}
	})
	t.Run("red: seeded extractor reads an unplanned class", func(t *testing.T) {
		t.Parallel()
		seededFull := configArtifactPasses{hydrate: fullConfigHydration, build: seededContentReadingBuild}
		seededNeeded := configArtifactPasses{hydrate: hydrateConfigArtifactFiles, build: seededContentReadingBuild}
		oracleStore, gotStore := newStore(), newStore()
		oracle := runConfigLoad(t, oracleStore, oracleStore.listed(t), seededFull)
		got := runConfigLoad(t, gotStore, gotStore.listed(t), seededNeeded)
		if oracle.rows == 0 {
			t.Fatal("seeded extractor emitted no row under full hydration; the planted violation is inert")
		}
		if oracle.digest == got.digest {
			t.Fatal("the harness did not flag an extractor that reads content the plan does not fetch")
		}
	})
}

// TestConfigArtifactExtractorsIgnoreContentOfUnreadClasses is the unit-level
// guard: for every path in the class corpus that configArtifactReadsContent
// rejects, the extractors must produce the same rows for any non-blank
// content. A branch that starts reading content for such a path fails here,
// before the loader would silently stop fetching it.
func TestConfigArtifactExtractorsIgnoreContentOfUnreadClasses(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, dir := range pathDirs {
		for _, stem := range pathStems {
			relativePath := dir + stem
			file := fileRow(relativePath, "x")
			if !isConfigArtifactCandidate(file) || configArtifactReadsContent(file) {
				continue
			}
			baseline, haveBaseline := "", false
			for _, content := range poisonContents {
				if strings.TrimSpace(content) == "" {
					continue
				}
				got := fmt.Sprint(buildRepositoryConfigArtifacts("svc", []querycontract.FileContent{fileRow(relativePath, content)}))
				if !haveBaseline {
					baseline, haveBaseline = got, true
					continue
				}
				if got != baseline {
					t.Fatalf("extractors read the content of %q, which configArtifactReadsContent rejects: %s vs %s", relativePath, baseline, got)
				}
			}
			checked++
		}
	}
	if checked < 100 {
		t.Fatalf("checked only %d unread-class paths; the corpus is too small to guard the plan", checked)
	}
}
