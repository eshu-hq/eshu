package parser

import (
	"path/filepath"
	"testing"
)

func TestInferContentMetadata(t *testing.T) {
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
			name:         "jinja config template",
			relativePath: filepath.Join("templates", "config.cfg.j2"),
			content: `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Values.name }}
`,
			wantArtifact:   "generic_config_template",
			wantDialect:    "jinja",
			wantIACRelated: true,
		},
		{
			name:         "terraform hcl with markers",
			relativePath: filepath.Join("infra", "main.tf"),
			content: `terraform {
  required_version = ">= 1.5"
}

value = ${var.name}
`,
			wantArtifact:   "terraform_hcl",
			wantDialect:    "terraform_template",
			wantIACRelated: true,
		},
		{
			name:           "plain yaml stays metadata light",
			relativePath:   filepath.Join("k8s", "deployment.yaml"),
			content:        "apiVersion: v1\nkind: ConfigMap\n",
			wantArtifact:   "",
			wantDialect:    "",
			wantIACRelated: false,
		},
		{
			name:         "ansible playbook is classified",
			relativePath: filepath.Join("playbooks", "site.yml"),
			content: `- hosts: all
  roles:
    - common
`,
			wantArtifact:   "ansible_playbook",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:         "ansible inventory is classified",
			relativePath: filepath.Join("inventories", "prod", "hosts.yml"),
			content: `all:
  children:
    web:
      hosts:
        web-1.example.com:
`,
			wantArtifact:   "ansible_inventory",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:           "ansible role task entrypoint is classified",
			relativePath:   filepath.Join("roles", "web", "tasks", "main.yml"),
			content:        "- debug:\n    msg: hello\n",
			wantArtifact:   "ansible_task_entrypoint",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:           "ansible vars file is classified",
			relativePath:   filepath.Join("group_vars", "all.yml"),
			content:        "app_name: demo\n",
			wantArtifact:   "ansible_vars",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:           "github actions workflow is classified",
			relativePath:   filepath.Join(".github", "workflows", "deploy.yaml"),
			content:        "name: deploy\non:\n  push:\n",
			wantArtifact:   "github_actions_workflow",
			wantDialect:    "",
			wantIACRelated: false,
		},
		{
			name:         "terraform tfvars json stays on terraform artifact path",
			relativePath: filepath.Join("infra", "terraform.tfvars.json"),
			content: `{
  "app_repo": "payments-service"
}`,
			wantArtifact:   "terraform_hcl",
			wantDialect:    "",
			wantIACRelated: true,
		},
		{
			name:         "docker compose overlay file is classified",
			relativePath: filepath.Join("deploy", "docker-compose.neo4j.yml"),
			content: `services:
  api:
    command: ["bundle", "exec", "puma"]
`,
			wantArtifact:   "docker_compose",
			wantDialect:    "",
			wantIACRelated: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := inferContentMetadata(filepath.FromSlash(tt.relativePath), tt.content)
			if got.ArtifactType != tt.wantArtifact {
				t.Fatalf("ArtifactType = %q, want %q", got.ArtifactType, tt.wantArtifact)
			}
			if got.TemplateDialect != tt.wantDialect {
				t.Fatalf("TemplateDialect = %q, want %q", got.TemplateDialect, tt.wantDialect)
			}
			if got.IACRelevant != tt.wantIACRelated {
				t.Fatalf("IACRelevant = %t, want %t", got.IACRelevant, tt.wantIACRelated)
			}
		})
	}
}
