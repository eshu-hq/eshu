// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// hclBenchSink keeps the benchmarked rows live so the call is not elided.
var hclBenchSink []map[string]any

// Block templates for the benchmark files. {n} is the block counter and {m}
// a bounded octet. They mirror the content shapes of the real hydrated set:
// resource files with none of the required literals, provider lock files,
// required_providers blocks, and rich Terraform and Terragrunt files.
const (
	hclBenchPlainBlock = "resource \"aws_s3_bucket\" \"b_{n}\" {\n  bucket        = \"acme-bucket-{n}\"\n  acl           = \"private\"\n  force_destroy = false\n  versioning {\n    enabled = true\n  }\n  tags = {\n    Environment = \"prod\"\n    Owner       = \"team-{n}\"\n  }\n}\n\n"

	hclBenchLockBlock = "provider \"registry.terraform.io/hashicorp/p{n}\" {\n  version     = \"5.{n}.0\"\n  constraints = \"~> 5.0\"\n  hashes = [\n    \"h1:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGH{n}=\",\n    \"zh:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd{n}\",\n    \"zh:fedcba9876543210fedcba9876543210fedcba9876543210fedcba98765432{n}\",\n  ]\n}\n\n"

	hclBenchProvidersHead = "terraform {\n  required_version = \">= 1.5.0\"\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \">= 5.0\"\n    }\n  }\n}\n\n"
	hclBenchLockHead      = "# This file is maintained automatically by \"terraform init\".\n# Manual edits may be lost in future updates.\n\n"
	hclBenchPlainHead     = "terraform {\n  required_version = \">= 1.5.0\"\n}\n\n"
)

var hclBenchRichTF = []string{
	"locals {\n  config_dir  = \"${path.module}/config\"\n  env_file    = \"${local.config_dir}/env-{n}.yaml\"\n  settings    = yamldecode(file(\"${path.module}/settings/svc-{n}.yaml\"))\n  user_data   = templatefile(\"${path.module}/templates/boot-{n}.tpl\", { env = var.env })\n  name_prefix = \"${var.env}-svc-{n}\"\n}\n\n",
	"module \"svc_{n}\" {\n  source = \"../../modules/svc-{n}\"\n  name   = \"${var.env}-svc-{n}\"\n  config = file(\"${path.module}/files/svc-{n}.json\")\n}\n\n",
	"resource \"aws_iam_policy\" \"p_{n}\" {\n  name   = \"${var.env}-p-{n}\"\n  policy = yamldecode(file(\"${path.module}/policies/p-{n}.yaml\"))\n}\n\n",
	"resource \"aws_instance\" \"i_{n}\" {\n  ami           = var.ami\n  instance_type = \"t3.micro\"\n  user_data     = templatefile(\"${path.module}/templates/i-{n}.tpl\", { id = {n} })\n}\n\n",
	"module \"net_{n}\" {\n  source = \"./modules/net-{n}\"\n  cidr   = \"10.{m}.0.0/16\"\n}\n\n",
}

var hclBenchRichHCL = []string{
	"locals {\n  env_cfg     = read_terragrunt_config(find_in_parent_folders(\"env-{n}.hcl\"))\n  config_file = \"${get_terragrunt_dir()}/config/svc-{n}.json\"\n  common      = yamldecode(file(\"${get_parent_terragrunt_dir()}/common-{n}.yaml\"))\n}\n\n",
	"dependency \"vpc_{n}\" {\n  config_path = \"../vpc-{n}\"\n  mock_outputs = {\n    vpc_id = \"vpc-{n}\"\n  }\n}\n\n",
	"include \"root_{n}\" {\n  path   = find_in_parent_folders(\"root-{n}.hcl\")\n  expose = true\n}\n\n",
	"terraform {\n  source = \"${get_repo_root()}/modules//svc-{n}\"\n}\n\n",
	"inputs_{n} = {\n  user_data = templatefile(\"${get_terragrunt_dir()}/tpl/{n}.tpl\", { id = {n} })\n  region    = \"us-east-1\"\n}\n\n",
}

// hclBenchBuild repeats blocks after head until the content reaches size
// bytes.
func hclBenchBuild(head string, blocks []string, size int) string {
	var b strings.Builder
	b.WriteString(head)
	for n := 0; b.Len() < size; n++ {
		block := blocks[n%len(blocks)]
		block = strings.ReplaceAll(block, "{n}", strconv.Itoa(n))
		block = strings.ReplaceAll(block, "{m}", strconv.Itoa(n%250))
		b.WriteString(block)
	}
	return b.String()
}

// hclBenchCell is one benchmark input and the scans it is built to need.
type hclBenchCell struct {
	name    string
	path    string
	content string
	// wantScans is how many of the six regex scans the prefilter lets run.
	wantScans int
}

// hclBenchCells returns the cells of the shipped-versus-gated benchmark: six
// content shapes at 2, 12 and 86 KB (the p50, p90 and max of real .tf files).
func hclBenchCells() []hclBenchCell {
	sizes := []struct {
		label string
		bytes int
	}{{"2KB", 2048}, {"12KB", 12 * 1024}, {"86KB", 86017}}
	var cells []hclBenchCell
	for _, sz := range sizes {
		plain := hclBenchBuild(hclBenchPlainHead, []string{hclBenchPlainBlock}, sz.bytes)
		cells = append(cells,
			hclBenchCell{"tf-plain/" + sz.label, "stacks/prod/main.tf", plain, 0},
			hclBenchCell{"hcl-plain/" + sz.label, "stacks/prod/terragrunt.hcl", plain, 0},
			hclBenchCell{
				"lock-file/" + sz.label, "stacks/prod/.terraform.lock.hcl",
				hclBenchBuild(hclBenchLockHead, []string{hclBenchLockBlock}, sz.bytes), 0,
			},
			hclBenchCell{
				"required-providers-only/" + sz.label, "stacks/prod/versions.tf",
				hclBenchBuild(hclBenchProvidersHead, []string{hclBenchPlainBlock}, sz.bytes), 1,
			},
			hclBenchCell{
				"tf-rich/" + sz.label, "stacks/prod/main.tf",
				hclBenchBuild("", hclBenchRichTF, sz.bytes), 2,
			},
			hclBenchCell{
				"hcl-rich/" + sz.label, "stacks/prod/terragrunt.hcl",
				hclBenchBuild("", hclBenchRichHCL, sz.bytes), 6,
			},
		)
	}
	return cells
}

// TestHCLBenchCellsAreWhatTheyClaim keeps the benchmark honest: the shipped
// arm and the gated arm return identical rows on every cell, and each cell
// runs the number of scans its name promises, so a template edit cannot turn a
// no-match cell into a match cell without failing here.
func TestHCLBenchCellsAreWhatTheyClaim(t *testing.T) {
	t.Parallel()
	for _, c := range hclBenchCells() {
		files := []querycontract.FileContent{{RelativePath: c.path, Content: c.content}}
		shipped := extractHCLConfigAssetRowsShipped("repo", files)
		gated := extractHCLConfigAssetRows("repo", files)
		if !reflect.DeepEqual(shipped, gated) {
			t.Errorf("%s: shipped and gated rows differ", c.name)
		}
		if got := countRunning(planHCLScans(hclScanPrefilters, c.content)); got != c.wantScans {
			t.Errorf("%s: %d scans run, want %d", c.name, got, c.wantScans)
		}
	}
}

// BenchmarkHCLScanShippedVsGated times the frozen pre-prefilter extractor and
// the production extractor on the same cells. Run it with
//
//	go test ./internal/query/repositoryartifacts -run '^$' \
//	  -bench BenchmarkHCLScanShippedVsGated -count 10 -benchmem
//
// and compare each cell's shipped and gated arms. The numbers are per call on
// a single file; they say nothing about wall time on a pod.
func BenchmarkHCLScanShippedVsGated(b *testing.B) {
	for _, c := range hclBenchCells() {
		files := []querycontract.FileContent{{RelativePath: c.path, Content: c.content}}
		for _, arm := range []struct {
			name string
			run  func(string, []querycontract.FileContent) []map[string]any
		}{
			{"shipped", extractHCLConfigAssetRowsShipped},
			{"gated", extractHCLConfigAssetRows},
		} {
			b.Run(fmt.Sprintf("%s/%s", c.name, arm.name), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(c.content)))
				for i := 0; i < b.N; i++ {
					hclBenchSink = arm.run("repo", files)
				}
			})
		}
	}
}
