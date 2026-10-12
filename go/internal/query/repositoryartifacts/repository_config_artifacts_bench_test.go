// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// benchGraph answers the related-repository read with its rows and every other
// read with none.
type benchGraph struct{ rows []map[string]any }

func (g benchGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return g.rows, nil
}

func (g benchGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// benchStore serves the same file set for every repository id. It is
// read-only after construction, so concurrent chunk reads need no lock.
type benchStore struct {
	querycontract.ContentStore
	files []querycontract.FileContent
	byKey map[string]querycontract.FileContent
}

func newBenchStore(files []querycontract.FileContent) *benchStore {
	s := &benchStore{files: files, byKey: map[string]querycontract.FileContent{}}
	for _, f := range files {
		s.byKey[f.RelativePath] = f
	}
	return s
}

func (s *benchStore) ListRepoFiles(_ context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	listed := make([]querycontract.FileContent, 0, len(s.files))
	for _, f := range s.files {
		f.RepoID = repoID
		f.Content = ""
		listed = append(listed, f)
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].RelativePath < listed[j].RelativePath })
	if limit > 0 && len(listed) > limit {
		listed = listed[:limit]
	}
	return listed, nil
}

func (s *benchStore) GetFileContent(_ context.Context, repoID, relativePath string) (*querycontract.FileContent, error) {
	f, ok := s.byKey[relativePath]
	if !ok {
		return nil, nil
	}
	f.RepoID = repoID
	return &f, nil
}

func (s *benchStore) GetFileContents(_ context.Context, repoID string, paths []string) ([]querycontract.FileContent, error) {
	out := make([]querycontract.FileContent, 0, len(paths))
	for _, p := range paths {
		if f, ok := s.byKey[p]; ok {
			f.RepoID = repoID
			out = append(out, f)
		}
	}
	return out, nil
}

func (s *benchStore) NonBlankRepoFilePaths(_ context.Context, _ string, paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if f, ok := s.byKey[p]; ok && strings.TrimSpace(f.Content) != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// benchRepositoryFiles builds one repository's files: Terraform and Terragrunt
// with locals, joins and file() calls, tfvars, a kustomization with resources, a
// compose file, Ansible paths, and unrelated files, padded so the HCL scans
// have real text to read.
func benchRepositoryFiles() []querycontract.FileContent {
	pad := strings.Repeat("# padding line that carries no config path reference\n", 120)
	var files []querycontract.FileContent
	add := func(path, content string) {
		files = append(files, querycontract.FileContent{RelativePath: path, Content: content})
	}
	for i := 0; i < 14; i++ {
		add(fmt.Sprintf("infra/m%02d/main.tf", i), fmt.Sprintf(`locals {
  cfg_dir = "${path.module}/config"
  tpl     = file("${local.cfg_dir}/m%02d.json")
}
module "m%02d" {
  source = "../modules/m%02d"
  body   = templatefile("${path.module}/templates/m%02d.tpl", {})
}
`, i, i, i, i)+pad)
	}
	add("live/terragrunt.hcl", "include \"root\" {\n  path = find_in_parent_folders()\n}\ndependency \"vpc\" {\n  config_path = \"../vpc\"\n}\n"+pad)
	add("live/prod/terragrunt.hcl", "include {\n  path = find_in_parent_folders(\"root.hcl\")\n}\nlocals {\n  vars = read_terragrunt_config(\"${get_terragrunt_dir()}/env.hcl\")\n}\n"+pad)
	for i := 0; i < 4; i++ {
		add(fmt.Sprintf("env/e%d.tfvars", i), "region = \"us-east-1\"\n")
	}
	add("deploy/kustomization.yaml", "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - policy.yaml\n  - role.yaml\n")
	add("deploy/policy.yaml", "apiVersion: iam.aws.upbound.io/v1beta1\nkind: RolePolicy\nspec:\n  policyDocument:\n    Statement:\n      - Resource:\n          - arn:aws:ssm:us-east-1:123456789012:parameter/configd/app/*\n")
	add("deploy/role.yaml", "apiVersion: iam.aws.upbound.io/v1beta1\nkind: Role\nspec: {}\n")
	add("svc/docker-compose.yml", "services:\n  app:\n    env_file:\n      - ./app.env\n    configs:\n      - source: cfg\nconfigs:\n  cfg:\n    file: ./conf/app.conf\n")
	add("ops/group_vars/all.yml", "ansible_user: deploy\n")
	add("ops/inventories/prod/hosts.yml", "all:\n  hosts: {}\n")
	add("ops/roles/web/tasks/main.yml", "- name: install\n  debug: {}\n")
	add("ops/site.yml", "- hosts: all\n  roles: [web]\n")
	for i := 0; i < 6; i++ {
		add(fmt.Sprintf("app/src/file%02d.go", i), "package app\n")
		add(fmt.Sprintf("app/values%02d.yaml", i), "replicas: 2\n")
	}
	return files
}

// BenchmarkLoadDeploymentArtifactOverview runs the story's config-artifact
// stage over one anchor and 60 related repositories through the exported
// loader, with in-memory reads. It measures the parse and merge work the
// per-repository function moved with (#7126 PR 3a): the same call on the tree
// before and after the move.
func BenchmarkLoadDeploymentArtifactOverview(b *testing.B) {
	const related = 60
	rows := make([]map[string]any, 0, related)
	for i := 0; i < related; i++ {
		rows = append(rows, map[string]any{"repo_id": fmt.Sprintf("repo-%03d", i), "repo_name": fmt.Sprintf("related-%03d", i)})
	}
	store := newBenchStore(benchRepositoryFiles())
	listed, _ := store.ListRepoFiles(context.Background(), "repo-anchor", 5000)

	var configRows int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		merged, _, err := LoadDeploymentArtifactOverviewWithStats(
			context.Background(), benchGraph{rows: rows}, store, "repo-anchor", "anchor", listed, nil, nil,
		)
		if err != nil {
			b.Fatal(err)
		}
		configRows = len(querycontract.MapSliceValue(querycontract.MapValue(merged, "deployment_artifacts"), "config_paths"))
		if configRows == 0 {
			b.Fatal("the fixture produced no config_paths; the benchmark would time nothing")
		}
	}
	b.ReportMetric(float64(configRows), "config_paths")
}
