// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"math/rand"
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// extractHCLConfigAssetRowsShipped is a frozen copy of
// extractHCLConfigAssetRows as it shipped before the required-literal
// prefilter (#7126): every scan runs on every file. It is the oracle of the
// equality proof, so it must never call the prefilter and must keep calling
// the same helpers the production function calls.
func extractHCLConfigAssetRowsShipped(repoName string, files []querycontract.FileContent) []map[string]any {
	rows := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, file := range files {
		lowerBase := strings.ToLower(path.Base(file.RelativePath))
		if strings.HasSuffix(lowerBase, ".tfvars") || strings.HasSuffix(lowerBase, ".tfvars.json") {
			rows = appendConfigArtifactRow(rows, seen, CleanRepositoryRelativePath(file.RelativePath), repoName, file.RelativePath, "terraform_var_file")
			continue
		}
		if lowerBase != "terragrunt.hcl" && !strings.HasSuffix(lowerBase, ".tf") && !strings.HasSuffix(lowerBase, ".hcl") {
			continue
		}
		localConfigAssignments := extractLocalConfigAssignments(file.RelativePath, file.Content)
		for _, match := range terragruntDependencyConfigPathPattern.FindAllStringSubmatch(file.Content, -1) {
			if len(match) < 2 {
				continue
			}
			configPath := NormalizeConfigArtifactExpression(match[1], localConfigAssignments)
			if configPath == "" {
				continue
			}
			rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terragrunt_dependency_config_path")
		}
		for _, match := range terragruntReadConfigPattern.FindAllStringSubmatch(file.Content, -1) {
			configPath := normalizeTerragruntParentConfigPath(match)
			if configPath == "" {
				continue
			}
			rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terragrunt_read_config")
		}
		for _, match := range terragruntIncludePathPattern.FindAllStringSubmatch(file.Content, -1) {
			configPath := normalizeTerragruntParentConfigPath(match)
			if configPath == "" {
				continue
			}
			rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terragrunt_include_path")
		}
		for _, match := range terragruntFindInParentFoldersPattern.FindAllStringSubmatch(file.Content, -1) {
			configPath := normalizeTerragruntParentConfigPath(match)
			if configPath == "" {
				continue
			}
			rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terragrunt_find_in_parent_folders")
		}
		for _, configPath := range extractConfigAssetFunctionPaths(file.Content, localConfigAssignments) {
			if configPath == "" {
				continue
			}
			rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "local_config_asset")
		}
		for _, match := range localTerraformModuleSourcePattern.FindAllStringSubmatch(file.Content, -1) {
			if len(match) < 2 {
				continue
			}
			configPath := NormalizeConfigArtifactExpression(match[1], localConfigAssignments)
			if configPath == "" {
				continue
			}
			rows = appendConfigArtifactRow(rows, seen, configPath, repoName, file.RelativePath, "terraform_module_source_path")
		}
	}
	return rows
}

// hclFragments are building blocks for generated HCL files: for each scan
// the shapes that match and the near misses that must not, plus the content
// shapes that dominate the real hydrated set (provider lock files, resource
// files, required_providers blocks).
var hclFragments = []string{
	// terragruntDependencyConfigPathPattern
	"dependency \"vpc\" {\n  config_path = \"../vpc\"\n}\n",
	"dependency \"db\" {\n  CONFIG_PATH   =   \"../../db\"   # trailing\n}\n",
	"dependency \"x\" {\n  config_path = local.vpc_path\n}\n",
	"dependency \"y\" {\n  config_path = \"${get_repo_root()}/envs/y\"\n}\n",
	"dependency \"z\" {\n  config_path = \"${local.base}/z\"\n}\n",
	"dependency \"w\" {\n  config_path=\"../w\"\n}\n",
	"config_path = \"../start-of-text\"",
	"my_config_path = \"../not-a-dependency\"\n",
	"# config_path = \"../commented\"\n",
	"config_path_extra = \"../near-miss\"\n",
	// terragruntReadConfigPattern
	"locals {\n  env = read_terragrunt_config(find_in_parent_folders(\"env.hcl\"))\n}\n",
	"locals {\n  env = read_terragrunt_config(\"${get_terragrunt_dir()}/env.hcl\")\n}\n",
	"x = READ_TERRAGRUNT_CONFIG( FIND_IN_PARENT_FOLDERS( \"Env.Hcl\" ) )\n",
	"x = read_terragrunt_config(find_in_parent_folders())\n",
	"x = read_terragrunt_config (find_in_parent_folders(\"env.hcl\"))\n",
	"x = READ_TERRAGRUNT_CONFIG(FIND_IN_PARENT_FOLDERS())\n",
	"x = read_terragrunt_config(find_in_parent_folder\u017f())\n",
	// terragruntIncludePathPattern / terragruntFindInParentFoldersPattern
	"include \"root\" {\n  path = find_in_parent_folders()\n}\n",
	"include \"root\" {\n  path   =   find_in_parent_folders(\"root.hcl\")\n  expose = true\n}\n",
	"include {\n  PATH = FIND_IN_PARENT_FOLDERS(\"Root.HCL\")\n}\n",
	"path = find_in_parent_folders()\n",
	"include {\n  subpath = find_in_parent_folders(\"not-include-path.hcl\")\n}\n",
	"x = find_in_parent_folders(\"a.hcl\")\n",
	"x = find_in_parent_folder\u017f(\"long-s.hcl\")\n",
	"x = find_in_parent_folders ( \"space.hcl\" )\n",
	"x = find_in_parent_folders(\n  \"multi.hcl\"\n)\n",
	"x = find_in_parent_folders(var.name)\n",
	// localConfigFunctionStartPattern
	"x = yamldecode(file(\"${path.module}/config/app.yaml\"))\n",
	"x = templatefile(\"${path.module}/templates/boot.tpl\", { id = 1 })\n",
	"x = FILE(\"./data/Settings.JSON\")\n",
	"x = file(local.cfg_file)\n",
	"x = jsonencode(templatefile(\n  \"${path.module}/tpl/x.tmpl\",\n  { a = 1 }\n))\n",
	"x = file (\"spaced.yaml\")\n",
	"x = profile(\"not-file.yaml\")\n",
	"x = filename(\"also-not.yaml\")\n",
	"x = file(\"${path.module}/unterminated.yaml\"\n",
	"x = file(\"a/b.yaml\") + file(\"a/b.yaml\") + templatefile(\"c/d.tpl\", {})\n",
	// localTerraformModuleSourcePattern
	"module \"a\" {\n  source = \"./modules/a\"\n  name   = \"a\"\n}\n",
	"module \"b\" {\n  source = \"git::https://example.com/b.git\"\n}\n",
	"module \"c\" {\n  source  = \"${path.module}/../modules/c\"\n}\n",
	"module \"d\" {\n  SOURCE = \"../d\"\n}\n",
	"module \"e\" {\n  \u017fource = \"./e\"\n}\n",
	"module \"e2\" {\n  x\u017fource = \"./e2\"\n}\n",
	"xmodule \"x1\" {\n  source = \"./x1\"\n}\n",
	"xterraform {\n  source = \"./x2\"\n}\n",
	"module \"f\" {\n  resource = \"./not-source\"\n}\n",
	"module \"g\" {\n  name = \"g\"\n}\n",
	"terraform {\n  source = \"${get_repo_root()}/modules//svc\"\n}\n",
	"terraform{\n  source=\"../mods//x\"\n}\n",
	"terraform {\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \">= 5.0\"\n    }\n  }\n}\n",
	"terraform {\n  required_version = \">= 1.5\"\n}\n",
	"resource \"aws_s3_bucket\" \"b\" {\n  source = \"./not-a-module\"\n}\n",
	"terraform\n{\n  source = \"./newline-brace\"\n}\n",
	// locals
	"locals {\n  config_dir = \"${path.module}/config\"\n  env_file   = \"${local.config_dir}/env.yaml\"\n}\n",
	"locals {\n  parts = split(\"/\", path_relative_to_include())\n  env   = local.parts[0]\n}\n",
	"locals {\n  joined = join(\"/\", [\n    path.module,\n    \"shared\",\n  ])\n}\n",
	"LOCALS {\n  config_dir = \"./case-sensitive\"\n}\n",
	"  locals {\n    indented = \"./indented\"\n  }\n",
	"locals = {\n  assigned = \"./assigned\"\n}\n",
	"# locals { commented = \"./c\" }\nvariable \"v\" {}\n",
	// content shapes that dominate the real set
	"# This file is maintained automatically by \"terraform init\".\n# Manual edits may be lost in future updates.\n\nprovider \"registry.terraform.io/hashicorp/aws\" {\n  version     = \"5.31.0\"\n  constraints = \">= 5.0.0\"\n  hashes = [\n    \"h1:abcdef=\",\n    \"zh:0123456789\",\n  ]\n}\n",
	"resource \"aws_s3_bucket\" \"b\" {\n  bucket = \"acme\"\n  acl    = \"private\"\n  tags = {\n    Owner = \"team\"\n  }\n}\n",
	"variable \"region\" {\n  type    = string\n  default = \"us-east-1\"\n}\n",
	"output \"id\" {\n  value = aws_s3_bucket.b.id\n}\n",
	"data \"aws_caller_identity\" \"current\" {}\n",
	"// comment only\n",
	"",
}

// hclPaths cover each file class the extractor branches on, nested and flat.
var hclPaths = []string{
	"main.tf",
	"stacks/prod/terragrunt.hcl",
	"modules/app/main.tf",
	".terraform.lock.hcl",
	"live/env/.terraform.lock.hcl",
	"envs/prod/root.hcl",
	"envs/prod/vars.tfvars",
	"envs/prod/vars.tfvars.json",
	"MAIN.TF",
	"Terragrunt.HCL",
	"docs/readme.md",
	"data/config.json",
	"repo/envs/x/terragrunt.hcl",
}

var hclLineEndings = []string{"\n", "\r\n", "\r"}

// hclCorpus returns generated files for the equality proof: every fragment
// alone, every ordered pair, and seeded random mixes with case swaps, special
// folds, CRLF endings, NUL, invalid UTF-8 and truncation.
func hclCorpus(randomFiles int, seed int64) []querycontract.FileContent {
	rng := rand.New(rand.NewSource(seed))
	var out []querycontract.FileContent
	for _, a := range hclFragments {
		for _, p := range hclPaths {
			out = append(out, querycontract.FileContent{RelativePath: p, Content: a})
		}
		for _, b := range hclFragments {
			for _, p := range []string{"main.tf", "envs/root.hcl"} {
				out = append(out, querycontract.FileContent{RelativePath: p, Content: a + b})
			}
		}
	}
	for i := 0; i < randomFiles; i++ {
		var b strings.Builder
		for n := 1 + rng.Intn(8); n > 0; n-- {
			b.WriteString(hclFragments[rng.Intn(len(hclFragments))])
			if rng.Intn(4) == 0 {
				b.WriteString(noiseFragment(rng))
			}
		}
		content := b.String()
		for n := rng.Intn(4); n > 0; n-- {
			content = perturbHCL(content, rng)
		}
		out = append(out, querycontract.FileContent{
			RelativePath: hclPaths[rng.Intn(len(hclPaths))],
			Content:      content,
		})
	}
	return out
}

func noiseFragment(rng *rand.Rand) string {
	pool := []string{"\x00", "\xff\xfe", "\xc5", "\xe2\x82", "\u212a", "\u017f", "  ", "\t", "\n\n", "#x\n", "=", "{", "}", "(", ")", "\"", "\u00e9"}
	return pool[rng.Intn(len(pool))]
}

// perturbHCL applies one random corruption that probes a gate boundary.
func perturbHCL(content string, rng *rand.Rand) string {
	switch rng.Intn(9) {
	case 0:
		return strings.ToUpper(content)
	case 1:
		return strings.NewReplacer("k", "\u212a", "s", "\u017f").Replace(content)
	case 2:
		return strings.ReplaceAll(content, "\n", hclLineEndings[rng.Intn(len(hclLineEndings))])
	case 3:
		if len(content) > 0 {
			return content[:rng.Intn(len(content)+1)]
		}
	case 4:
		if len(content) > 0 {
			at := rng.Intn(len(content))
			return content[:at] + noiseFragment(rng) + content[at:]
		}
	case 5:
		if len(content) > 0 {
			at := rng.Intn(len(content))
			return content[:at] + content[at+1:]
		}
	case 6:
		return "\ufeff" + content
	case 7:
		return strings.ReplaceAll(content, " ", "")
	case 8:
		return strings.Map(func(r rune) rune {
			if rng.Intn(5) == 0 && r >= 'a' && r <= 'z' {
				return r - 32
			}
			return r
		}, content)
	}
	return content
}
