package query

import (
	"path"
	"sort"
	"strings"
)

func extractAnsibleConfigPathRows(repoName string, files []FileContent) []map[string]any {
	rows := make([]map[string]any, 0)
	seen := make(map[string]struct{})

	addRow := func(relativePath, evidenceKind string) {
		cleaned := cleanRepositoryRelativePath(relativePath)
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
		relativePath := strings.TrimSpace(file.RelativePath)
		if relativePath == "" {
			continue
		}
		lowerPath := strings.ToLower(cleanRepositoryRelativePath(relativePath))
		lowerBase := strings.ToLower(path.Base(relativePath))

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
		leftPath := StringVal(rows[i], "path")
		rightPath := StringVal(rows[j], "path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		leftKind := StringVal(rows[i], "evidence_kind")
		rightKind := StringVal(rows[j], "evidence_kind")
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		return StringVal(rows[i], "relative_path") < StringVal(rows[j], "relative_path")
	})

	return rows
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

func ansiblePlaybookEvidencePath(lowerPath, lowerBase, content string) bool {
	switch {
	case strings.Contains(lowerPath, "/playbooks/"), strings.Contains(lowerPath, "playbooks/"):
		return true
	case strings.Contains(lowerPath, "/site.yml"), strings.Contains(lowerPath, "/site.yaml"):
		return true
	case lowerBase == "site.yml", lowerBase == "site.yaml":
		return true
	case strings.Contains(lowerPath, "/inventories/"), strings.Contains(lowerPath, "/inventory/"):
		return false
	case strings.Contains(lowerPath, "inventories/"), strings.Contains(lowerPath, "inventory/"):
		return false
	case strings.Contains(lowerPath, "/roles/"), strings.Contains(lowerPath, "/group_vars/"), strings.Contains(lowerPath, "/host_vars/"):
		return false
	default:
		return plausibleAnsiblePlaybookPath(lowerBase) && ansiblePlaybookContent(content)
	}
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
	normalized := strings.TrimSuffix(cleanRepositoryRelativePath(lowerPath), "/")
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
