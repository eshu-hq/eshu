// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// kitchenSinkFiles holds one file of every class the config-artifact
// extractors treat differently, with content that moves each extractor.
func kitchenSinkFiles() []querycontract.FileContent {
	return []querycontract.FileContent{
		fileRow("deploy/kustomization.yaml", kustomizationFor("policy.yaml", "../shared/", "values-resource.json", "missing.yaml")),
		fileRow("deploy/policy.yaml", policyResource("payments")),
		fileRow("deploy/values-resource.json", `{"Statement":[{"Resource":"arn:aws:ssm:us-east-1:1:parameter/configd/json/*"}]}`),
		fileRow("deploy/unreferenced.yaml", policyResource("ignored")),
		fileRow("shared/kustomization.yaml", kustomizationFor("../deploy/policy.yaml", "extra.yaml")),
		fileRow("shared/extra.yaml", policyResource("shared")),
		fileRow("infra/main.tf", "module \"x\" {\n  source = \"./modules/x\"\n}\n"),
		fileRow("infra/terragrunt.hcl", "include {\n  path = find_in_parent_folders()\n}\n"),
		fileRow("infra/prod.tfvars", "region = \"us-east-1\"\n"),
		fileRow("infra/prod.tfvars.json", `{"region":"us-east-1"}`),
		fileRow("infra/blank.tfvars", " \n\t\n"),
		fileRow("svc/docker-compose.yml", "services:\n  app:\n    env_file:\n      - ./app.env\n"),
		fileRow("ops/deploy.yml", "- hosts: all\n  tasks: []\n"),
		fileRow("ops/playbook.yml", "# nothing here\nkey: value\n"),
		fileRow("ops/playbooks/run.yml", "- name: x\n"),
		fileRow("ops/group_vars/all.yml", "ansible_user: deploy\n"),
		fileRow("ops/inventories/prod/hosts.yml", "all:\n  hosts: {}\n"),
		fileRow("ops/roles/web/tasks/main.yml", "- name: install\n"),
		fileRow("charts/values.yaml", "replicas: 2\n"),
		fileRow("web/package.json", `{"name":"x"}`),
		fileRow("web/empty.json", ""),
		fileRow("README.md", "docs\n"),
		fileRow("main.go", "package main\n"),
	}
}

func kitchenSinkStore() *hydrationStore {
	return newHydrationStore(kitchenSinkFiles()...)
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func TestNeededContentHydrationFetchesOnlyConsumedFiles(t *testing.T) {
	t.Parallel()

	oracle, got, oracleStore, gotStore := requireSameConfigOutput(t, "kitchen sink", kitchenSinkStore, productionConfigArtifactPasses)
	if oracle.rows == 0 {
		t.Fatal("fixture produced no config_paths rows; the equality check is vacuous")
	}

	wantFetched := []string{
		"deploy/kustomization.yaml", "deploy/policy.yaml", "deploy/values-resource.json",
		"infra/main.tf", "infra/terragrunt.hcl", "ops/deploy.yml", "ops/playbook.yml",
		"shared/extra.yaml", "shared/kustomization.yaml", "svc/docker-compose.yml",
	}
	if gotFetched := uniqueSorted(gotStore.getCalls); strings.Join(gotFetched, ",") != strings.Join(wantFetched, ",") {
		t.Fatalf("fetched content for\n  %v\nwant\n  %v", gotFetched, wantFetched)
	}
	if len(gotStore.getCalls) >= len(oracleStore.getCalls) {
		t.Fatalf("GetFileContent calls = %d, want fewer than the full pass's %d", len(gotStore.getCalls), len(oracleStore.getCalls))
	}
	// Each of the two sources checks blankness once, for every selected file
	// whose content no extractor reads.
	if len(gotStore.blankCalls) != 2 {
		t.Fatalf("blank checks = %d, want 1 per repository (2)", len(gotStore.blankCalls))
	}
	if got.stats.FilesSkipped == 0 || got.stats.FilesHydrated == 0 {
		t.Fatalf("stats hydrated=%d skipped=%d, want both non-zero", got.stats.FilesHydrated, got.stats.FilesSkipped)
	}
	if got.stats.BlankCheckStatements != 2 {
		t.Fatalf("stats.BlankCheckStatements = %d, want 2", got.stats.BlankCheckStatements)
	}
}

func TestNeededContentHydrationBlankClassesMatchFullHydration(t *testing.T) {
	t.Parallel()

	contents := map[string]string{
		"empty":       "",
		"space":       " ",
		"tab":         "\t",
		"newline":     "\n",
		"crlf":        "\r\n",
		"vtab-ff":     "\v\f",
		"nbsp":        "\u00a0",
		"nel":         "\u0085",
		"line-sep":    "\u2028",
		"para-sep":    "\u2029",
		"ideographic": "\u3000",
		"en-quad":     "\u2000\u200a",
		"mixed":       " \t\r\n\u00a0\u3000 ",
		"bom-only":    "\ufeff",
		"zero-width":  "\u200b",
		"bom-space":   "\ufeff ",
		"text":        "a",
		"padded":      "  a  \n",
	}
	paths := map[string]string{
		"tfvars":         "env/prod.tfvars",
		"tfvars-json":    "env/prod.tfvars.json",
		"inventory":      "ops/inventories/prod/hosts.yml",
		"group-vars":     "ops/group_vars/all.yml",
		"role-tasks":     "ops/roles/web/tasks/main.yml",
		"playbook-dir":   "ops/playbooks/run.yml",
		"playbook-name":  "ops/deploy.yml",
		"hcl":            "infra/main.tf",
		"terragrunt":     "infra/terragrunt.hcl",
		"compose":        "svc/docker-compose.yml",
		"kustomization":  "deploy/kustomization.yaml",
		"plain-yaml":     "charts/values.yaml",
		"plain-json":     "web/package.json",
		"non-candidate":  "README.md",
		"upper-kustom":   "deploy/KUSTOMIZATION.YAML",
		"compose-prefix": "svc/docker-compose.override.yml",
	}
	for contentName, content := range contents {
		for pathName, relativePath := range paths {
			name := fmt.Sprintf("%s/%s", pathName, contentName)
			content, relativePath := content, relativePath
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				newStore := func() *hydrationStore {
					// Anchor files keep non-blank siblings so a blank path-only
					// file is the only variable.
					return newHydrationStore(
						fileRow(relativePath, content),
						fileRow("deploy/policy.yaml", policyResource("neighbor")),
						fileRow("neighbor/main.tf", "# neighbor\n"),
					)
				}
				requireSameConfigOutput(t, name, newStore, productionConfigArtifactPasses)
			})
		}
	}
}

func TestNeededContentHydrationKustomizeShapesMatchFullHydration(t *testing.T) {
	t.Parallel()

	cases := map[string][]querycontract.FileContent{
		"chain depth 3": {
			fileRow("a/kustomization.yaml", kustomizationFor("../b")),
			fileRow("b/kustomization.yaml", kustomizationFor("../c")),
			fileRow("c/kustomization.yaml", kustomizationFor("leaf.yaml", "../d/kustomization.yaml")),
			fileRow("c/leaf.yaml", policyResource("leaf")),
			fileRow("d/kustomization.yaml", kustomizationFor("deep.yml")),
			fileRow("d/deep.yml", policyResource("deep")),
		},
		"cycle": {
			fileRow("a/kustomization.yaml", kustomizationFor("../b", "own.yaml")),
			fileRow("a/own.yaml", policyResource("own")),
			fileRow("b/kustomization.yaml", kustomizationFor("../a")),
		},
		"missing and remote refs": {
			fileRow("a/kustomization.yaml", kustomizationFor("gone.yaml", "https://example.com/x.yaml", "../nowhere", "present.yaml")),
			fileRow("a/present.yaml", policyResource("present")),
		},
		"json resource": {
			fileRow("a/kustomization.yaml", kustomizationFor("policy.json")),
			fileRow("a/policy.json", `{"Resource": "/api/json/runtime/*"}`),
		},
		"blank resource": {
			fileRow("a/kustomization.yaml", kustomizationFor("blank.yaml", "ws.yaml", "real.yaml")),
			fileRow("a/blank.yaml", ""),
			fileRow("a/ws.yaml", " \n\t"),
			fileRow("a/real.yaml", policyResource("real")),
		},
		"shared resource two kustomizations": {
			fileRow("a/kustomization.yaml", kustomizationFor("../shared/policy.yaml")),
			fileRow("b/kustomization.yaml", kustomizationFor("../shared/policy.yaml", "./local.yaml")),
			fileRow("b/local.yaml", policyResource("local")),
			fileRow("shared/policy.yaml", policyResource("shared")),
		},
		"non-resource extension is not read": {
			fileRow("a/kustomization.yaml", kustomizationFor("notes.txt", "script.tf")),
			fileRow("a/notes.txt", policyResource("txt")),
			fileRow("a/script.tf", policyResource("tf")),
		},
		"bases patches components": {
			fileRow("a/kustomization.yaml", "bases:\n  - ../base\ncomponents:\n  - ../comp\npatches:\n  - path: patch.yaml\n"),
			fileRow("a/patch.yaml", policyResource("patch")),
			fileRow("base/kustomization.yaml", kustomizationFor("b.yaml")),
			fileRow("base/b.yaml", policyResource("base")),
			fileRow("comp/kustomization.yml", kustomizationFor("c.yaml")),
			fileRow("comp/c.yaml", policyResource("comp")),
		},
		"bare kustomization name and path variants": {
			fileRow("a/kustomization", kustomizationFor("./r.yaml", "sub/../r2.yaml")),
			fileRow("a/r.yaml", policyResource("r")),
			fileRow("a/r2.yaml", policyResource("r2")),
		},
		"padded path": {
			fileRow(" a/kustomization.yaml ", kustomizationFor("r.yaml")),
			fileRow("a/r.yaml", policyResource("padded")),
		},
		"invalid kustomization yaml": {
			fileRow("a/kustomization.yaml", "resources: [unclosed\n  - nope"),
			fileRow("a/r.yaml", policyResource("never")),
		},
		"special characters": {
			fileRow("a b/kustomization.yaml", kustomizationFor("r {1}.yaml", "ü-é.yml", "100%_x.yaml")),
			fileRow("a b/r {1}.yaml", policyResource("space")),
			fileRow("a b/ü-é.yml", policyResource("uni")),
			fileRow("a b/100%_x.yaml", policyResource("pct")),
		},
	}
	for name, files := range cases {
		files := files
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			oracle, _, _, _ := requireSameConfigOutput(t, name, func() *hydrationStore { return newHydrationStore(files...) }, productionConfigArtifactPasses)
			if name != "invalid kustomization yaml" && name != "non-resource extension is not read" && oracle.rows == 0 {
				t.Fatalf("%s produced no rows; the case does not exercise a kustomize read", name)
			}
		})
	}
}

func TestNeededContentHydrationReferenceOutsideCapIsNotRead(t *testing.T) {
	t.Parallel()

	// 49 plain yaml files, the kustomization as the 50th candidate, then the
	// resource it references as the 51st: the cap excludes the resource, so
	// the full pass leaves it blank and drops it. The needed-content pass
	// must also not fetch it.
	files := make([]querycontract.FileContent, 0, 52)
	for i := 0; i < 49; i++ {
		files = append(files, fileRow(fmt.Sprintf("a-%02d.yaml", i), "k: v\n"))
	}
	files = append(files,
		fileRow("b/kustomization.yaml", kustomizationFor("res.yaml")),
		fileRow("b/res.yaml", policyResource("beyond")),
	)
	newStore := func() *hydrationStore { return newHydrationStore(files...) }
	oracle, _, _, gotStore := requireSameConfigOutput(t, "cap", newStore, productionConfigArtifactPasses)
	if oracle.rows != 0 {
		t.Fatalf("full pass produced %d rows; the resource sits beyond the cap and must be dropped", oracle.rows)
	}
	for _, call := range gotStore.getCalls {
		if call == "b/res.yaml" {
			t.Fatal("fetched a resource beyond the 50-file hydration cap")
		}
	}
	for _, paths := range gotStore.blankCalls {
		if len(paths) > RepositoryArtifactHydrationLimit {
			t.Fatalf("blank check named %d paths, want at most the cap %d", len(paths), RepositoryArtifactHydrationLimit)
		}
	}
}

func TestNeededContentHydrationPreloadedContentIsKept(t *testing.T) {
	t.Parallel()

	newStore := func() *hydrationStore {
		return newHydrationStore(
			fileRow("svc/docker-compose.yml", "services:\n  app:\n    env_file:\n      - ./a.env\n"),
			fileRow("charts/values.yaml", "a: b\n"),
			fileRow("infra/blank.tfvars", "   "),
			fileRow("infra/real.tfvars", "x = 1\n"),
		)
	}
	oracleStore, gotStore := newStore(), newStore()
	preload := func(store *hydrationStore) []querycontract.FileContent {
		files := store.listed(t)
		for i := range files {
			files[i].Content = store.rows[files[i].RelativePath].Content
		}
		return files
	}
	oracle := runConfigLoad(t, oracleStore, preload(oracleStore), fullConfigArtifactPasses)
	got := runConfigLoad(t, gotStore, preload(gotStore), productionConfigArtifactPasses)
	if oracle.err != nil || got.err != nil || oracle.digest != got.digest {
		t.Fatalf("preloaded anchor differs: oracle=%+v got=%+v", oracle, got)
	}
	if oracle.rows == 0 {
		t.Fatal("preloaded fixture produced no rows; the equality check is vacuous")
	}
}

func TestNeededContentHydrationReadErrorsFailTheRepository(t *testing.T) {
	t.Parallel()

	t.Run("read of a consumed file", func(t *testing.T) {
		t.Parallel()
		store := kitchenSinkStore()
		store.getAt["infra/main.tf"] = errInjectedRead
		got := runConfigLoad(t, store, store.listed(t), productionConfigArtifactPasses)
		if !errors.Is(got.err, errInjectedRead) {
			t.Fatalf("error = %v, want it to wrap the read failure", got.err)
		}
	})
	t.Run("read of a referenced resource", func(t *testing.T) {
		t.Parallel()
		store := kitchenSinkStore()
		store.getAt["deploy/policy.yaml"] = errInjectedRead
		got := runConfigLoad(t, store, store.listed(t), productionConfigArtifactPasses)
		if !errors.Is(got.err, errInjectedRead) {
			t.Fatalf("error = %v, want it to wrap the read failure", got.err)
		}
	})
	t.Run("blank check", func(t *testing.T) {
		t.Parallel()
		store := kitchenSinkStore()
		store.blankErr = errInjectedRead
		got := runConfigLoad(t, store, store.listed(t), productionConfigArtifactPasses)
		if !errors.Is(got.err, errInjectedRead) {
			t.Fatalf("error = %v, want it to wrap the blank-check failure", got.err)
		}
	})
	t.Run("read of an unread file is not issued", func(t *testing.T) {
		t.Parallel()
		// The full pass fetches every candidate, so a failure on a file no
		// extractor reads fails it. The needed-content pass issues no read for
		// that file and cannot fail on it. This is the one intended difference
		// from the full pass; the output for a successful load is identical.
		store := kitchenSinkStore()
		store.getAt["web/package.json"] = errInjectedRead
		got := runConfigLoad(t, store, store.listed(t), productionConfigArtifactPasses)
		if got.err != nil {
			t.Fatalf("error = %v, want none: package.json is never read", got.err)
		}
	})
}

func TestNeededContentHydrationRowVanishedAfterListMatchesFullHydration(t *testing.T) {
	t.Parallel()

	for _, vanished := range []string{"infra/prod.tfvars", "infra/main.tf", "deploy/policy.yaml", "charts/values.yaml"} {
		vanished := vanished
		t.Run(vanished, func(t *testing.T) {
			t.Parallel()
			newStore := func() *hydrationStore {
				store := kitchenSinkStore()
				store.gone[vanished] = true
				return store
			}
			requireSameConfigOutput(t, vanished, newStore, productionConfigArtifactPasses)
		})
	}
}

func TestNeededContentHydrationIssuesNoStatementWithoutCandidates(t *testing.T) {
	t.Parallel()

	store := newHydrationStore(fileRow("README.md", "x"), fileRow("main.go", "package main"))
	got := runConfigLoad(t, store, store.listed(t), productionConfigArtifactPasses)
	if got.err != nil || got.rows != 0 {
		t.Fatalf("outcome = %+v, want no rows and no error", got)
	}
	if len(store.getCalls) != 0 || len(store.blankCalls) != 0 {
		t.Fatalf("statements issued: get=%d blank=%d, want none", len(store.getCalls), len(store.blankCalls))
	}
}
