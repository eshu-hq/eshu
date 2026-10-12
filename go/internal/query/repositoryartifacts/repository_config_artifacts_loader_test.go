// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestConfigArtifactListLimitMatchesTheSemanticEntityLimit pins the neutral
// package's list window to the limit the repository story uses for the anchor
// list. The window is part of the config-artifact accuracy contract (#7126), so
// the two must not drift.
func TestConfigArtifactListLimitMatchesTheSemanticEntityLimit(t *testing.T) {
	t.Parallel()

	if contentartifacts.ListLimit != querycontract.RepositorySemanticEntityLimit {
		t.Fatalf("ListLimit = %d, RepositorySemanticEntityLimit = %d", contentartifacts.ListLimit, querycontract.RepositorySemanticEntityLimit)
	}
	if contentartifacts.HydrationFloor != RepositoryArtifactHydrationLimit {
		t.Fatalf("HydrationFloor = %d, RepositoryArtifactHydrationLimit = %d", contentartifacts.HydrationFloor, RepositoryArtifactHydrationLimit)
	}
}

// loaderFiles is a file set whose Ansible row for one path carries two
// evidence kinds: a file under group_vars and inventory is both a vars file
// and an inventory. Both rows share path, source_repo and relative_path.
func loaderFiles() []querycontract.FileContent {
	return []querycontract.FileContent{
		contractFile("group_vars/inventory/all.yml", "key: value\n"),
		contractFile("infra/main.tf", "module \"x\" {\n  source = \"./modules/x\"\n}\n"),
		contractFile("infra/prod.tfvars", "region = \"us-east-1\"\n"),
	}
}

// TestConfigArtifactLoadOrdersRowsWithEveryKeyOfTheTotalOrder proves the merged
// load sorts on evidence_kind after path, source_repo and relative_path. Rows
// that share the first three fields once had no defined order; they follow the
// evidence_kind order now.
func TestConfigArtifactLoadOrdersRowsWithEveryKeyOfTheTotalOrder(t *testing.T) {
	t.Parallel()

	store := newFakeContentStore(map[string][]querycontract.FileContent{"repo-a": loaderFiles()})
	var stats ConfigArtifactReadStats
	got, err := loadRepositoryConfigArtifactsForSources(
		context.Background(), store, []repositoryArtifactSource{{RepoID: "repo-a", RepoName: "svc"}}, &stats,
	)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	rows := querycontract.MapSliceValue(got, "config_paths")
	var sharedKinds []string
	for _, row := range rows {
		if querycontract.StringVal(row, "path") == "group_vars/inventory/all.yml" {
			sharedKinds = append(sharedKinds, querycontract.StringVal(row, "evidence_kind"))
		}
	}
	if want := []string{"ansible_inventory", "ansible_vars"}; !reflect.DeepEqual(sharedKinds, want) {
		t.Fatalf("rows that share path, source_repo and relative_path = %v, want %v in evidence_kind order", sharedKinds, want)
	}
	for i := 1; i < len(rows); i++ {
		if contentartifacts.CompareConfigPaths(rows[i-1], rows[i]) >= 0 {
			t.Fatalf("rows %d and %d are not strictly ordered: %v, %v", i-1, i, rows[i-1], rows[i])
		}
	}
}

// TestConfigArtifactLoadKeepsOneDedupAcrossRepositoriesThatShareAName proves the
// merge keeps one dedup map across sources: two repositories with the same
// graph name and the same files yield each row once.
func TestConfigArtifactLoadKeepsOneDedupAcrossRepositoriesThatShareAName(t *testing.T) {
	t.Parallel()

	store := newFakeContentStore(map[string][]querycontract.FileContent{
		"repo-a": loaderFiles(),
		"repo-b": loaderFiles(),
	})
	load := func(sources ...repositoryArtifactSource) []map[string]any {
		t.Helper()
		var stats ConfigArtifactReadStats
		got, err := loadRepositoryConfigArtifactsForSources(context.Background(), store, sources, &stats)
		if err != nil {
			t.Fatalf("load error = %v", err)
		}
		return querycontract.MapSliceValue(got, "config_paths")
	}
	one := load(repositoryArtifactSource{RepoID: "repo-a", RepoName: "shared"})
	two := load(
		repositoryArtifactSource{RepoID: "repo-a", RepoName: "shared"},
		repositoryArtifactSource{RepoID: "repo-b", RepoName: "shared"},
	)
	if len(one) == 0 {
		t.Fatal("fixture produced no rows; the dedup check is vacuous")
	}
	if !reflect.DeepEqual(one, two) {
		t.Fatalf("two repositories with one graph name changed the merged rows\n  one: %v\n  two: %v", one, two)
	}

	distinct := load(
		repositoryArtifactSource{RepoID: "repo-a", RepoName: "left"},
		repositoryArtifactSource{RepoID: "repo-b", RepoName: "right"},
	)
	if len(distinct) != 2*len(one) {
		t.Fatalf("two repositories with two graph names gave %d rows, want %d: source_repo is part of the row identity", len(distinct), 2*len(one))
	}
}

// TestConfigArtifactLoadMatchesTheAnchorFullPass proves the production load,
// which runs the neutral package's needed-content pass through the adapter,
// returns the rows of the full pass: HydrateRepositoryCandidateFiles reads
// every candidate one file at a time, then the extractors run on the non-blank
// files. It keeps the retained full pass of the anchor repository, which the
// neutral package cannot import, tied to the shared per-repository function.
func TestConfigArtifactLoadMatchesTheAnchorFullPass(t *testing.T) {
	t.Parallel()

	files := append(loaderFiles(),
		contractFile("deploy/kustomization.yaml", "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - policy.yaml\n"),
		contractFile("deploy/policy.yaml", "apiVersion: iam.aws.upbound.io/v1beta1\nkind: RolePolicy\nspec:\n  policyDocument:\n    Statement:\n      - Resource:\n          - arn:aws:ssm:us-east-1:123456789012:parameter/configd/payments/*\n"),
		contractFile("svc/docker-compose.yml", "services:\n  app:\n    env_file:\n      - ./svc/app.env\n"),
		contractFile("infra/blank.tf", " \n\t\n"),
		contractFile("README.md", "docs\n"),
	)
	store := newFakeContentStore(map[string][]querycontract.FileContent{"repo-a": files})

	var stats ConfigArtifactReadStats
	got, err := loadRepositoryConfigArtifactsForSources(
		context.Background(), store, []repositoryArtifactSource{{RepoID: "repo-a", RepoName: "svc"}}, &stats,
	)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}

	listed, err := store.ListRepoFiles(context.Background(), "repo-a", querycontract.RepositorySemanticEntityLimit)
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	hydrated, err := HydrateRepositoryCandidateFiles(context.Background(), store, "repo-a", listed,
		func(querycontract.FileContent) bool { return true })
	if err != nil {
		t.Fatalf("full pass error = %v", err)
	}
	nonBlank := make([]querycontract.FileContent, 0, len(hydrated))
	for _, file := range hydrated {
		if strings.TrimSpace(file.Content) != "" {
			nonBlank = append(nonBlank, file)
		}
	}
	want := querycontract.MapSliceValue(BuildRepositoryConfigArtifacts("svc", nonBlank), "config_paths")
	rows := querycontract.MapSliceValue(got, "config_paths")
	if len(want) == 0 {
		t.Fatal("fixture produced no rows; the equality check is vacuous")
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("production load differs from the full pass\n  load: %v\n  full: %v", rows, want)
	}
}

// TestFileFromContractCopiesEveryFileField pins the adapter field by field. It
// sets every field of the contract's file to a distinct non-zero value and
// requires each field of the neutral file to carry the value of the field of
// the same name. A field added to contentartifacts.File that the adapter does
// not copy, or that the contract type lacks, fails here and not as a silent
// zero in a derived row.
func TestFileFromContractCopiesEveryFileField(t *testing.T) {
	t.Parallel()

	var source querycontract.FileContent
	sourceValue := reflect.ValueOf(&source).Elem()
	for i := 0; i < sourceValue.NumField(); i++ {
		field := sourceValue.Field(i)
		switch field.Kind() {
		case reflect.String:
			field.SetString(fmt.Sprintf("value-of-%s", sourceValue.Type().Field(i).Name))
		case reflect.Int:
			field.SetInt(int64(100 + i))
		}
	}

	got := reflect.ValueOf(fileFromContract(source))
	for i := 0; i < got.NumField(); i++ {
		name := got.Type().Field(i).Name
		want := sourceValue.FieldByName(name)
		if !want.IsValid() {
			t.Errorf("contentartifacts.File.%s has no field of that name in querycontract.FileContent", name)
			continue
		}
		if want.IsZero() {
			t.Errorf("fixture left FileContent.%s zero; the check is vacuous", name)
			continue
		}
		if !reflect.DeepEqual(got.Field(i).Interface(), want.Interface()) {
			t.Errorf("File.%s = %v, want %v from FileContent.%s", name, got.Field(i).Interface(), want.Interface(), name)
		}
	}
}
