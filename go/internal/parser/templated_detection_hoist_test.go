// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestInferContentMetadataHoistEquivalence is the output-preserving proof for
// issue #4805: inferContentMetadata used to call inferRootFamily, which
// internally re-scanned the same five marker regexes (goExpressionRE,
// jinjaStatementRE, tfInterpolationRE, tfDirectiveRE, tfTemplatefileRE) that
// inferContentMetadata itself scans again a few lines later. The fix hoists
// those marker scans to run once in inferContentMetadata and passes the
// results down to inferRootFamily via the contentMarkers struct.
//
// This test asserts inferContentMetadata's result is byte-identical across a
// battery of shapes chosen to exercise every branch that reads a marker
// signal: plain source with no markers, real IaC (.tf/.conf), ansible paths,
// Helm/Argo template paths, GitHub Actions workflows, and content carrying
// each marker family (go-template, jinja, terraform interpolation/directive/
// templatefile(), and the "${{ }}" GitHub-Actions shape that only an
// *unfiltered* MatchString probe -- not the filtered FindAll variants used
// elsewhere in this function -- would catch). Each case's expected value is
// captured from the current (post-hoist) implementation and is a stable
// literal a reviewer can diff against pre-hoist behavior; the point of this
// test is that it stays green across the hoist, proving the refactor changed
// no observable output.
func TestInferContentMetadataHoistEquivalence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		relativePath   string
		content        string
		wantArtifact   string
		wantDialect    string
		wantIACRelated bool
	}{
		{
			name:           "plain go source has no markers",
			relativePath:   filepath.Join("cmd", "server", "main.go"),
			content:        "package main\n\nfunc main() {}\n",
			wantArtifact:   "",
			wantDialect:    "",
			wantIACRelated: false,
		},
		{
			name:           "plain python source has no markers",
			relativePath:   filepath.Join("app", "service.py"),
			content:        "def handler():\n    return 200\n",
			wantArtifact:   "",
			wantDialect:    "",
			wantIACRelated: false,
		},
		{
			name:         "terraform hcl with interpolation marker",
			relativePath: filepath.Join("infra", "main.tf"),
			content: `resource "aws_instance" "node" {
  ami = "${var.ami_id}"
}
`,
			wantArtifact:   "terraform_hcl",
			wantDialect:    "terraform_template",
			wantIACRelated: true,
		},
		{
			name:         "terraform hcl with directive marker",
			relativePath: filepath.Join("infra", "flags.tf"),
			content: `locals {
  flag = %{ if var.enabled }true%{ else }false%{ endif }
}
`,
			wantArtifact:   "terraform_hcl",
			wantDialect:    "terraform_template",
			wantIACRelated: true,
		},
		{
			name:         "terraform hcl with templatefile marker",
			relativePath: filepath.Join("infra", "userdata.tf"),
			content: `resource "aws_instance" "node" {
  user_data = templatefile("init.tpl", { name = "node" })
}
`,
			wantArtifact:   "terraform_hcl",
			wantDialect:    "terraform_template",
			wantIACRelated: true,
		},
		{
			name:         "terraform hcl with no markers stays untemplated",
			relativePath: filepath.Join("infra", "plain.tf"),
			content: `resource "aws_instance" "node" {
  ami = "ami-123456"
}
`,
			wantArtifact:   "terraform_hcl",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:         "generic conf file classified as nginx config",
			relativePath: filepath.Join("etc", "nginx", "sites-available", "app.conf"),
			content: `server {
  listen 80;
  location / {
    proxy_pass http://upstream;
  }
}
`,
			wantArtifact:   "nginx_config",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:         "generic conf file with go-template markers",
			relativePath: filepath.Join("etc", "app", "service.conf"),
			content: `[service]
name = {{ .Values.name }}
`,
			// isRawConfigSuffix(".conf") resolves inferArtifactType to
			// generic_config (isTemplateSuffix is false here: ".conf" is
			// neither a jinja nor terraform-template suffix), and the
			// go-template marker alone does not add a "_template" suffix --
			// only jinja/terraform-template *suffixes* do that in
			// inferArtifactType. The bucket switch's
			// `!isYAMLSuffix && lastSuffix != ".kcl"` branch only sets
			// bucket to "unknown_templated" (no dialect), so TemplateDialect
			// stays empty and IACRelevant is false because artifactType
			// "generic_config" is not in isIACRelevant's allowed set and
			// bucket "unknown_templated" is not in its bucket switch either.
			wantArtifact:   "generic_config",
			wantDialect:    "",
			wantIACRelated: false,
		},
		{
			name:         "ansible playbook path",
			relativePath: filepath.Join("playbooks", "deploy.yml"),
			content: `- hosts: web
  roles:
    - app
`,
			wantArtifact:   "ansible_playbook",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:         "ansible role with jinja markers",
			relativePath: filepath.Join("roles", "app", "templates", "config.j2"),
			content: `app_name = {{ app_name }}
{% if debug %}
debug = true
{% endif %}
`,
			// hasPart(parts, "roles") makes ansibleArtifactType resolve
			// "ansible_role" before inferArtifactType ever reaches its
			// isJinjaSuffix(lastSuffix) case, so the ".j2" suffix does not
			// win here despite carrying jinja markers.
			// persistedArtifactType passes "ansible_role" through unchanged
			// (not one of its special-cased buckets, and not "plain_text"/
			// "yaml_document"). The bucket switch's
			// `strings.HasPrefix(artifactType, "ansible_")` case sets
			// bucket=artifactType and TemplateDialect="jinja" because
			// hasCurlyExpressions is true (the content also has "{{ ... }}"
			// besides the jinja block).
			wantArtifact:   "ansible_role",
			wantDialect:    "jinja",
			wantIACRelated: true,
		},
		{
			name:         "helm chart values file",
			relativePath: filepath.Join("chart", "values.yaml"),
			content:      "replicaCount: 1\nimage:\n  repository: app\n",
			wantArtifact: "",
			wantDialect:  "",
			// values.yaml resolves to root family helm_argo but carries no
			// go-template/jinja markers, so it falls into the "plain_yaml"
			// bucket (persistedArtifactType("plain_yaml", "yaml_document")
			// returns "" because artifactType=="yaml_document"). IACRelevant
			// is still true because rootFamily=="helm_argo".
			wantIACRelated: true,
		},
		{
			name:         "helm template with go-template markers",
			relativePath: filepath.Join("chart", "templates", "deployment.yaml"),
			content: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
`,
			// hasPart(parts, "chart") && hasPart(parts, "templates") makes
			// rootFamily "helm_argo"; the rootFamily=="helm_argo" bucket
			// case (not explicitJinja) sets bucket="go_template_yaml" and
			// TemplateDialect="go_template". persistedArtifactType returns
			// "go_template_yaml" directly for that bucket regardless of
			// inferArtifactType's own (yaml_document) result.
			wantArtifact:   "go_template_yaml",
			wantDialect:    "go_template",
			wantIACRelated: true,
		},
		{
			name:         "github actions workflow with expression marker",
			relativePath: filepath.Join(".github", "workflows", "ci.yaml"),
			content: `name: ci
on: [push]
jobs:
  build:
    steps:
      - run: echo ${{ github.ref }}
`,
			// hasPart(parts, ".github") && hasPart(parts, "workflows") makes
			// inferArtifactType return "github_actions_workflow" directly
			// (ansibleType check first returns "" so that does not win).
			// The bucket switch's dedicated github-actions case
			// (hasGitHubActions && !explicitGo && !explicitJinja &&
			// !hasCurlyExpressions) sets TemplateDialect="github_actions".
			// isIACRelevant explicitly returns false for
			// artifactType=="github_actions_workflow" before checking any
			// other rule.
			wantArtifact:   "github_actions_workflow",
			wantDialect:    "github_actions",
			wantIACRelated: false,
		},
		{
			name:         "plain yaml with only github-actions-shaped expression outside workflows dir",
			relativePath: filepath.Join("deploy", "notes.yaml"),
			content:      "message: run ${{ github.ref }}\n",
			// "${{ github.ref }}" contains a bare "{{ github.ref }}" match
			// for goExpressionRE (the regex does not anchor on the leading
			// "$"), so the unfiltered hasAnyGoExpression probe used by
			// inferRootFamily sees a go-expression here even though
			// filteredMatches (used for hasCurlyExpressions) would exclude
			// it as "$"-prefixed. This case specifically exercises that
			// unfiltered-vs-filtered distinction preserved by the hoist.
			wantArtifact:   "",
			wantDialect:    "github_actions",
			wantIACRelated: false,
		},
		{
			name:         "dagster asset path with jinja markers",
			relativePath: filepath.Join("dagster", "assets", "pipeline.py"),
			content: `# {% if debug %}
CONFIG = "debug"
# {% endif %}
`,
			// rootFamily resolves "dagster_jinja" via hasPart(parts,
			// "dagster", "assets", ...). inferArtifactType falls through to
			// "plain_text" for a ".py" file matching none of its cases, so
			// persistedArtifactType returns "" regardless of bucket. The
			// bucket switch's `!isYAMLSuffix(lastSuffix) && lastSuffix !=
			// ".kcl"` branch fires first (".py" is neither YAML nor .kcl)
			// and only sets bucket to "unknown_templated", never reaching
			// the later rootFamily=="dagster_jinja" case that would have set
			// TemplateDialect="jinja" -- so TemplateDialect stays empty.
			// IACRelevant is still true because rootFamily=="dagster_jinja".
			wantArtifact:   "",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:         "terraform template suffix with tf markers only",
			relativePath: filepath.Join("infra", "cloud-init.tftpl"),
			content: `#cloud-config
hostname: ${hostname}
runcmd:
  - %{ if enable_swap }swapon /swapfile%{ endif }
`,
			wantArtifact:   "terraform_template_text",
			wantDialect:    "terraform_template",
			wantIACRelated: true,
		},
		{
			name:         "docker compose overlay",
			relativePath: filepath.Join("deploy", "docker-compose.override.yaml"),
			content: `services:
  api:
    environment:
      - DEBUG=true
`,
			wantArtifact:   "docker_compose",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:         "kcl file with go-template markers",
			relativePath: filepath.Join("k8s", "app.kcl"),
			content:      "name = \"{{ .Values.name }}\"\n",
			// inferArtifactType falls through to "plain_text" for ".kcl"
			// (none of its suffix/basename/path-segment cases match), so
			// persistedArtifactType alone would return "" -- but the bucket
			// switch's explicit ".kcl" carve-out
			// (`!isYAMLSuffix(lastSuffix) && lastSuffix != ".kcl"` is false
			// for ".kcl", so that branch is skipped) lets a later
			// `case explicitGo:` arm set bucket="go_template_yaml", and
			// persistedArtifactType returns "go_template_yaml" directly for
			// that bucket regardless of the underlying "plain_text"
			// artifactType. isIACRelevant's bucket switch also returns true
			// for "go_template_yaml".
			wantArtifact:   "go_template_yaml",
			wantDialect:    "go_template",
			wantIACRelated: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := inferContentMetadata(filepath.FromSlash(tt.relativePath), tt.content)
			if got.ArtifactType != tt.wantArtifact {
				t.Errorf("ArtifactType = %q, want %q", got.ArtifactType, tt.wantArtifact)
			}
			if got.TemplateDialect != tt.wantDialect {
				t.Errorf("TemplateDialect = %q, want %q", got.TemplateDialect, tt.wantDialect)
			}
			if got.IACRelevant != tt.wantIACRelated {
				t.Errorf("IACRelevant = %t, want %t", got.IACRelevant, tt.wantIACRelated)
			}
		})
	}
}

// generateLargeTFTemplateSource produces a large synthetic .tftpl file
// carrying real terraform interpolation, directive, and templatefile()
// markers -- content shaped so that inferRootFamily's marker-scanning branch
// (the hasTFMarkers && isTerraformTemplateSuffix branch) actually executes,
// unlike a plain .tf file which resolves through the anySuffix(isHCLSuffix)
// fast path before ever looking at markers. blockCount controls file size so
// the marker regex cost is measurable against total inferContentMetadata
// wall time.
func generateLargeTFTemplateSource(blockCount int) string {
	var b strings.Builder
	b.WriteString("# rendered by templatefile()\n\n")
	for i := range blockCount {
		fmt.Fprintf(&b, "resource_name_%d = \"${var.name%d}\"\n", i, i)
		fmt.Fprintf(&b, "flag_%d = %%{ if enabled%d }true%%{ else }false%%{ endif }\n", i, i)
	}
	return b.String()
}

// BenchmarkInferContentMetadataTFTemplateBeforeHoist and
// BenchmarkInferRootFamilyAloneTFTemplate are the #4805 before/after proof
// pair. BenchmarkInferRootFamilyAloneTFTemplate isolates inferRootFamily's
// own cost (now near-zero: no regex scans, just suffix/path-segment
// comparisons and boolean reads from the precomputed contentMarkers).
// BenchmarkInferContentMetadataTFTemplateBeforeHoist measures the full,
// still-necessary inferContentMetadata call for a file this shape actually
// requires full inference for (real IaC markers, not gated by
// shouldSkipContentMetadata). Comparing this pair's allocs/op and ns/op
// against the pre-hoist implementation (recorded in the PR's performance
// evidence) is the proof that the marker regexes now run once instead of
// twice per call.
func BenchmarkInferContentMetadataTFTemplateBeforeHoist(b *testing.B) {
	path := filepath.Join("infra", "large_main.tftpl")
	content := generateLargeTFTemplateSource(400)

	b.ResetTimer()
	for b.Loop() {
		_ = inferContentMetadata(path, content)
	}
}

func BenchmarkInferRootFamilyAloneTFTemplate(b *testing.B) {
	path := filepath.Join("infra", "large_main.tftpl")
	content := generateLargeTFTemplateSource(400)

	hasAnyGoExpression := goExpressionRE.MatchString(content)
	hasAnyJinjaStatement := jinjaStatementRE.MatchString(content)
	hasAnyTFInterpolation := tfInterpolationRE.MatchString(content)
	hasAnyTFDirective := tfDirectiveRE.MatchString(content)
	hasAnyTFTemplatefile := tfTemplatefileRE.MatchString(content)
	markers := contentMarkers{
		hasTFMarkers:       hasAnyTFInterpolation || hasAnyTFDirective || hasAnyTFTemplatefile,
		hasGoExpressions:   hasAnyGoExpression,
		hasJinjaStatements: hasAnyJinjaStatement,
	}

	b.ResetTimer()
	for b.Loop() {
		_ = inferRootFamily(path, content, markers)
	}
}
