// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// kitchenSinkFiles holds one file of every class the config-artifact
// extractors treat differently, with content that moves each extractor.
func kitchenSinkFiles() []File {
	return []File{
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

	oracle, got, oracleStore, gotStore := requireSameConfigOutput(t, "kitchen sink", kitchenSinkStore, productionLoadPasses)
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
				requireSameConfigOutput(t, name, newStore, productionLoadPasses)
			})
		}
	}
}

func TestNeededContentHydrationKustomizeShapesMatchFullHydration(t *testing.T) {
	t.Parallel()

	cases := map[string][]File{
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
		"blank duplicate of a cleaned path": {
			fileRow("a/kustomization.yaml", kustomizationFor("r.yaml")),
			fileRow("a/kustomization.yaml ", "\n"),
			fileRow("a/r.yaml", policyResource("dup")),
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
			oracle, _, _, _ := requireSameConfigOutput(t, name, func() *hydrationStore { return newHydrationStore(files...) }, productionLoadPasses)
			if name != "invalid kustomization yaml" && name != "non-resource extension is not read" && oracle.rows == 0 {
				t.Fatalf("%s produced no rows; the case does not exercise a kustomize read", name)
			}
		})
	}
}

// referenceBeyondTheFloorFiles is 49 plain YAML files, a kustomization as the
// 50th candidate, and the resource it references as the 51st.
func referenceBeyondTheFloorFiles() []File {
	files := make([]File, 0, 51)
	for i := 0; i < 49; i++ {
		files = append(files, fileRow(fmt.Sprintf("a-%02d.yaml", i), "k: v\n"))
	}
	return append(files,
		fileRow("b/kustomization.yaml", kustomizationFor("res.yaml")),
		fileRow("b/res.yaml", policyResource("beyond")),
	)
}

// TestNeededContentHydrationReferenceBeyondTheFloorIsRead is the inverse of the
// pre-#7951 cap test: with the cap lifted, a resource a kustomization names
// past the 50th candidate is read and its rows are returned, and the
// needed-content pass still equals the full pass at the same allowance.
func TestNeededContentHydrationReferenceBeyondTheFloorIsRead(t *testing.T) {
	t.Parallel()

	files := referenceBeyondTheFloorFiles()
	oracleStore := newHydrationStore(files...)
	gotStore := newHydrationStore(files...)
	oracle := runConfigLoadAllowance(t, oracleStore, oracleStore.listed(t), fullConfigArtifactPasses, AllowanceWholeWindow)
	got := runConfigLoadAllowance(t, gotStore, gotStore.listed(t), productionLoadPasses, AllowanceWholeWindow)
	if got.err != nil || oracle.err != nil {
		t.Fatalf("load errors: got %v, oracle %v", got.err, oracle.err)
	}
	if got.digest != oracle.digest {
		t.Fatalf("needed-content output differs from the full pass at the whole window: got %d rows, oracle %d", got.rows, oracle.rows)
	}
	if got.rows == 0 {
		t.Fatal("no rows: the resource beyond the 50th candidate was not read")
	}
	read := false
	for _, call := range gotStore.getCalls {
		read = read || call == "b/res.yaml"
	}
	if !read {
		t.Fatal("the resource beyond the 50th candidate was not fetched")
	}
}

// TestNeededContentHydrationReferenceOutsideTheAllowanceIsNotRead keeps the
// floor behavior: at the allowance HydrationFloor the resource is the 51st
// candidate, outside the selection, so neither pass reads it.
func TestNeededContentHydrationReferenceOutsideTheAllowanceIsNotRead(t *testing.T) {
	t.Parallel()

	files := referenceBeyondTheFloorFiles()
	newStore := func() *hydrationStore { return newHydrationStore(files...) }
	oracle, _, _, gotStore := requireSameConfigOutput(t, "floor", newStore, productionLoadPasses)
	if oracle.rows != 0 {
		t.Fatalf("full pass produced %d rows; the resource sits beyond the allowance and must be dropped", oracle.rows)
	}
	for _, call := range gotStore.getCalls {
		if call == "b/res.yaml" {
			t.Fatal("fetched a resource beyond the allowance")
		}
	}
	for _, paths := range gotStore.blankCalls {
		if len(paths) > HydrationFloor {
			t.Fatalf("blank check named %d paths, want at most the allowance %d", len(paths), HydrationFloor)
		}
	}
}

// TestNeededContentHydrationReferenceOutsideTheWindowIsNotRead proves the list
// window bounds kustomize reads too: a resource whose path sorts past the
// ListLimit window is never read, even at the whole-window allowance, and the
// load reports the cut list.
func TestNeededContentHydrationReferenceOutsideTheWindowIsNotRead(t *testing.T) {
	t.Parallel()

	tree := func(fillers int) []File {
		files := []File{fileRow("a/kustomization.yaml", kustomizationFor("../z/res.yaml"))}
		for i := 0; i < fillers; i++ {
			files = append(files, fileRow(fmt.Sprintf("m/f%05d.yaml", i), "k: v\n"))
		}
		return append(files, fileRow("z/res.yaml", policyResource("outside")))
	}

	// Control: with the resource inside the window it is read, so the
	// reference resolves and the check below is not vacuous.
	inside := newHydrationStore(tree(ListLimit - 2)...)
	if _, stats, err := LoadConfigPaths(context.Background(), inside,
		Repository{ID: hydrationTestRepoID, Name: "svc"}, nil, false, AllowanceWholeWindow); err != nil || stats.ListTruncated {
		t.Fatalf("control load: err = %v, ListTruncated = %v", err, stats.ListTruncated)
	}
	readInside := false
	for _, call := range inside.getCalls {
		readInside = readInside || call == "z/res.yaml"
	}
	if !readInside {
		t.Fatal("control: the resource inside the window was not read; the reference does not resolve")
	}

	store := newHydrationStore(tree(ListLimit)...)
	_, stats, err := LoadConfigPaths(context.Background(), store,
		Repository{ID: hydrationTestRepoID, Name: "svc"}, nil, false, AllowanceWholeWindow)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	if !stats.ListTruncated {
		t.Fatal("ListTruncated = false for a list of ListLimit+2 files")
	}
	for _, call := range store.getCalls {
		if call == "z/res.yaml" {
			t.Fatal("fetched a resource outside the 5,000-file window")
		}
	}
	for _, chunk := range store.blankCalls {
		for _, path := range chunk {
			if path == "z/res.yaml" {
				t.Fatal("blank-checked a file outside the 5,000-file window")
			}
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
	preload := func(store *hydrationStore) []File {
		files := store.listed(t)
		for i := range files {
			files[i].Content = store.rows[files[i].RelativePath].Content
		}
		return files
	}
	oracle := runConfigLoad(t, oracleStore, preload(oracleStore), fullConfigArtifactPasses)
	got := runConfigLoad(t, gotStore, preload(gotStore), productionLoadPasses)
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
		got := runConfigLoad(t, store, store.listed(t), productionLoadPasses)
		if !errors.Is(got.err, errInjectedRead) {
			t.Fatalf("error = %v, want it to wrap the read failure", got.err)
		}
	})
	t.Run("read of a referenced resource", func(t *testing.T) {
		t.Parallel()
		store := kitchenSinkStore()
		store.getAt["deploy/policy.yaml"] = errInjectedRead
		got := runConfigLoad(t, store, store.listed(t), productionLoadPasses)
		if !errors.Is(got.err, errInjectedRead) {
			t.Fatalf("error = %v, want it to wrap the read failure", got.err)
		}
	})
	t.Run("blank check", func(t *testing.T) {
		t.Parallel()
		store := kitchenSinkStore()
		store.blankErr = errInjectedRead
		got := runConfigLoad(t, store, store.listed(t), productionLoadPasses)
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
		got := runConfigLoad(t, store, store.listed(t), productionLoadPasses)
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
			requireSameConfigOutput(t, vanished, newStore, productionLoadPasses)
		})
	}
}

func TestNeededContentHydrationIssuesNoStatementWithoutCandidates(t *testing.T) {
	t.Parallel()

	store := newHydrationStore(fileRow("README.md", "x"), fileRow("main.go", "package main"))
	got := runConfigLoad(t, store, store.listed(t), productionLoadPasses)
	if got.err != nil || got.rows != 0 {
		t.Fatalf("outcome = %+v, want no rows and no error", got)
	}
	if len(store.getCalls) != 0 || len(store.blankCalls) != 0 {
		t.Fatalf("statements issued: get=%d blank=%d, want none", len(store.getCalls), len(store.blankCalls))
	}
}

func TestNeededContentHydrationRowVanishedBetweenBlankCheckAndReadIsDropped(t *testing.T) {
	t.Parallel()

	// group_vars/all.yml is referenced by the kustomization, so it is read in
	// the second pass. The blank check saw it non-blank; it then vanishes. The
	// full pass reads it once, gets no row, and drops it, so no ansible_vars
	// row exists. The needed-content pass must drop it too, not keep the empty
	// list row and emit a path-only row.
	files := []File{
		fileRow("ops/kustomization.yaml", kustomizationFor("group_vars/all.yml", "keep.yaml")),
		fileRow("ops/group_vars/all.yml", "ansible_user: deploy\n"),
		fileRow("ops/keep.yaml", policyResource("keep")),
	}
	oracleStore := newHydrationStore(files...)
	oracleStore.gone["ops/group_vars/all.yml"] = true
	gotStore := newHydrationStore(files...)
	gotStore.goneAfterCheck["ops/group_vars/all.yml"] = true

	oracle := runConfigLoad(t, oracleStore, oracleStore.listed(t), fullConfigArtifactPasses)
	got := runConfigLoad(t, gotStore, gotStore.listed(t), productionLoadPasses)
	if oracle.err != nil || got.err != nil {
		t.Fatalf("unexpected errors: oracle=%v got=%v", oracle.err, got.err)
	}
	if oracle.digest != got.digest {
		t.Fatalf("a row that vanished after the blank check changed the output: oracle rows=%d got rows=%d", oracle.rows, got.rows)
	}
	if oracle.rows == 0 {
		t.Fatal("fixture produced no rows; the equality check is vacuous")
	}
}
