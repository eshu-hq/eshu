// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func extractAnsibleConfigPathRows(repoName string, files []querycontract.FileContent) []map[string]any {
	rows := make([]map[string]any, 0)
	seen := make(map[string]struct{})

	addRow := func(relativePath, evidenceKind string) {
		cleaned := CleanRepositoryRelativePath(relativePath)
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

		if ansibleInventoryEvidencePath(lowerPath, lowerBase) {
			addRow(relativePath, "ansible_inventory")
		}
		if ansibleVarsEvidencePath(lowerPath, lowerBase) {
			addRow(relativePath, "ansible_vars")
		}
		if ansibleTaskEntrypointEvidencePath(lowerPath, lowerBase) {
			addRow(relativePath, "ansible_task_entrypoint")
		}
		if ansiblePlaybookEvidencePath(lowerPath, lowerBase, file.Content) {
			addRow(relativePath, "ansible_playbook")
		}
		if rolePath := ansibleRoleEvidencePath(lowerPath); rolePath != "" {
			addRow(rolePath, "ansible_role")
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		leftPath := querycontract.StringVal(rows[i], "path")
		rightPath := querycontract.StringVal(rows[j], "path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		leftKind := querycontract.StringVal(rows[i], "evidence_kind")
		rightKind := querycontract.StringVal(rows[j], "evidence_kind")
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		return querycontract.StringVal(rows[i], "relative_path") < querycontract.StringVal(rows[j], "relative_path")
	})

	return rows
}

// ansiblePathKeys derives the lower-cased path and base name every Ansible path
// rule matches on. The extractor and the needed-content planner share it.
func ansiblePathKeys(file querycontract.FileContent) (lowerPath, lowerBase string, ok bool) {
	relativePath := strings.TrimSpace(file.RelativePath)
	if relativePath == "" {
		return "", "", false
	}
	return strings.ToLower(CleanRepositoryRelativePath(relativePath)), strings.ToLower(path.Base(relativePath)), true
}

func ansibleInventoryEvidencePath(lowerPath, lowerBase string) bool {
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

func ansibleVarsEvidencePath(lowerPath, lowerBase string) bool {
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

func ansibleTaskEntrypointEvidencePath(lowerPath, lowerBase string) bool {
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

func ansibleRoleEvidencePath(lowerPath string) string {
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
	normalized := strings.TrimSuffix(CleanRepositoryRelativePath(lowerPath), "/")
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
