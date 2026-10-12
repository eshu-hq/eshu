// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"path"
	"strings"
)

func extractAnsibleConfigPathRows(repoName string, files []File) []map[string]any {
	rows := make([]map[string]any, 0)
	seen := make(map[string]struct{})

	addRow := func(relativePath, evidenceKind string) {
		cleaned := CleanRelativePath(relativePath)
		if cleaned == "" || evidenceKind == "" {
			return
		}
		key := strings.Join([]string{repoName, cleaned, evidenceKind}, "|")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		rows = append(rows, map[string]any{
			"path":          cleaned,
			"source_repo":   repoName,
			"relative_path": relativePath,
			"evidence_kind": evidenceKind,
		})
	}

	for _, file := range files {
		lowerPath, lowerBase, ok := ansiblePathKeys(file)
		if !ok {
			continue
		}
		relativePath := strings.TrimSpace(file.RelativePath)

		if AnsibleInventoryEvidencePath(lowerPath, lowerBase) {
			addRow(relativePath, "ansible_inventory")
		}
		if AnsibleVarsEvidencePath(lowerPath, lowerBase) {
			addRow(relativePath, "ansible_vars")
		}
		if AnsibleTaskEntrypointEvidencePath(lowerPath, lowerBase) {
			addRow(relativePath, "ansible_task_entrypoint")
		}
		if ansiblePlaybookEvidencePath(lowerPath, lowerBase, file.Content) {
			addRow(relativePath, "ansible_playbook")
		}
		if rolePath := AnsibleRoleEvidencePath(lowerPath); rolePath != "" {
			addRow(rolePath, "ansible_role")
		}
	}

	SortConfigPaths(rows)

	return rows
}

// ansiblePathKeys derives the lower-cased path and base name every Ansible path
// rule matches on. The extractor and the needed-content planner share it.
func ansiblePathKeys(file File) (lowerPath, lowerBase string, ok bool) {
	relativePath := strings.TrimSpace(file.RelativePath)
	if relativePath == "" {
		return "", "", false
	}
	return strings.ToLower(CleanRelativePath(relativePath)), strings.ToLower(path.Base(relativePath)), true
}

// AnsibleInventoryEvidencePath reports whether a lower-cased cleaned path and
// base name denote an Ansible inventory file. The config-artifact extractor and
// the repository controller-artifact reader share it.
func AnsibleInventoryEvidencePath(lowerPath, lowerBase string) bool {
	switch {
	case strings.Contains(lowerPath, "/inventories/"), strings.Contains(lowerPath, "/inventory/"):
		return true
	case strings.Contains(lowerPath, "inventories/"), strings.Contains(lowerPath, "inventory/"):
		return true
	case lowerBase == "inventory.yml", lowerBase == "inventory.yaml", lowerBase == "hosts.yml", lowerBase == "hosts.yaml":
		return true
	default:
		return false
	}
}

// AnsibleVarsEvidencePath reports whether a lower-cased cleaned path and base
// name denote an Ansible variable file (group_vars, host_vars, vars,
// defaults).
func AnsibleVarsEvidencePath(lowerPath, lowerBase string) bool {
	switch {
	case strings.Contains(lowerPath, "/group_vars/"), strings.Contains(lowerPath, "/host_vars/"):
		return true
	case strings.Contains(lowerPath, "group_vars/"), strings.Contains(lowerPath, "host_vars/"):
		return true
	case strings.Contains(lowerPath, "/vars/"), strings.Contains(lowerPath, "/defaults/"):
		return true
	case strings.Contains(lowerPath, "roles/") && (strings.Contains(lowerPath, "/vars/") || strings.Contains(lowerPath, "/defaults/")):
		return true
	case lowerBase == "vars.yml", lowerBase == "vars.yaml", lowerBase == "defaults.yml", lowerBase == "defaults.yaml":
		return true
	default:
		return false
	}
}

// AnsibleTaskEntrypointEvidencePath reports whether a lower-cased cleaned path
// and base name denote a role's tasks/main entry point.
func AnsibleTaskEntrypointEvidencePath(lowerPath, lowerBase string) bool {
	if !strings.Contains(lowerPath, "/roles/") && !strings.HasPrefix(lowerPath, "roles/") {
		return false
	}
	switch {
	case strings.Contains(lowerPath, "/tasks/") && (lowerBase == "main.yml" || lowerBase == "main.yaml"):
		return true
	default:
		return false
	}
}

// ansiblePlaybookVerdict is the path-only decision of the playbook rule.
type ansiblePlaybookVerdict int

const (
	// ansiblePlaybookByContent means the path rules did not decide; the
	// content heuristic does, for conventional playbook names only.
	ansiblePlaybookByContent ansiblePlaybookVerdict = iota
	ansiblePlaybookYes
	ansiblePlaybookNo
)

// ansiblePlaybookPathVerdict applies the path rules of the playbook evidence
// check, in their original order, and reports whether they decide alone.
func ansiblePlaybookPathVerdict(lowerPath, lowerBase string) ansiblePlaybookVerdict {
	switch {
	case strings.Contains(lowerPath, "/playbooks/"), strings.Contains(lowerPath, "playbooks/"):
		return ansiblePlaybookYes
	case strings.Contains(lowerPath, "/site.yml"), strings.Contains(lowerPath, "/site.yaml"):
		return ansiblePlaybookYes
	case lowerBase == "site.yml", lowerBase == "site.yaml":
		return ansiblePlaybookYes
	case strings.Contains(lowerPath, "/inventories/"), strings.Contains(lowerPath, "/inventory/"):
		return ansiblePlaybookNo
	case strings.Contains(lowerPath, "inventories/"), strings.Contains(lowerPath, "inventory/"):
		return ansiblePlaybookNo
	case strings.Contains(lowerPath, "/roles/"), strings.Contains(lowerPath, "/group_vars/"), strings.Contains(lowerPath, "/host_vars/"):
		return ansiblePlaybookNo
	default:
		return ansiblePlaybookByContent
	}
}

func ansiblePlaybookEvidencePath(lowerPath, lowerBase, content string) bool {
	switch ansiblePlaybookPathVerdict(lowerPath, lowerBase) {
	case ansiblePlaybookYes:
		return true
	case ansiblePlaybookNo:
		return false
	default:
		return plausibleAnsiblePlaybookPath(lowerBase) && ansiblePlaybookContent(content)
	}
}

// ansiblePlaybookReadsContent reports whether the playbook rule reads the
// file's content: only when the path rules do not decide and the name is a
// conventional playbook name. It is the one place the extractor consults
// content for an Ansible row.
func ansiblePlaybookReadsContent(lowerPath, lowerBase string) bool {
	return ansiblePlaybookPathVerdict(lowerPath, lowerBase) == ansiblePlaybookByContent &&
		plausibleAnsiblePlaybookPath(lowerBase)
}

// plausibleAnsiblePlaybookPath keeps content heuristics scoped to conventional
// playbook filenames so generic Kubernetes or Helm YAML cannot be promoted.
func plausibleAnsiblePlaybookPath(lowerBase string) bool {
	switch lowerBase {
	case "deploy.yml", "deploy.yaml", "playbook.yml", "playbook.yaml", "main-playbook.yml", "main-playbook.yaml":
		return true
	default:
		return strings.HasSuffix(lowerBase, "-playbook.yml") || strings.HasSuffix(lowerBase, "-playbook.yaml")
	}
}

// AnsibleRoleEvidencePath returns the role directory path of a lower-cased
// cleaned path that sits inside an Ansible role, or the empty string.
func AnsibleRoleEvidencePath(lowerPath string) string {
	if !strings.Contains(lowerPath, "/roles/") && !strings.HasPrefix(lowerPath, "roles/") {
		return ""
	}
	if strings.Contains(lowerPath, "/tasks/") && (strings.HasSuffix(lowerPath, "/main.yml") || strings.HasSuffix(lowerPath, "/main.yaml")) {
		return roleDirectoryPath(lowerPath)
	}
	if strings.Contains(lowerPath, "/vars/") || strings.Contains(lowerPath, "/defaults/") || strings.Contains(lowerPath, "/meta/") {
		return roleDirectoryPath(lowerPath)
	}
	return roleDirectoryPath(lowerPath)
}

func roleDirectoryPath(lowerPath string) string {
	normalized := strings.TrimSuffix(CleanRelativePath(lowerPath), "/")
	parts := strings.Split(normalized, "/")
	for i, part := range parts {
		if part != "roles" || i+1 >= len(parts) {
			continue
		}
		if i+2 >= len(parts) {
			return path.Join(parts[:i+2]...) + "/"
		}
		return path.Join(parts[:i+2]...) + "/"
	}
	return ""
}

func ansiblePlaybookContent(content string) bool {
	lowered := strings.ToLower(content)
	return strings.Contains(lowered, "hosts:") ||
		strings.Contains(lowered, "roles:") ||
		strings.Contains(lowered, "tasks:") ||
		strings.Contains(lowered, "import_playbook:")
}
