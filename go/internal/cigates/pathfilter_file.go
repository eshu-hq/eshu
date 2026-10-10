// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DornyFiltersFile resolves a workflow's local filters file from the same
// checkout, then returns the parsed filters. Inline filters also work.
func DornyFiltersFile(path string) (map[string][]string, bool, error) {
	filters, every, _, err := dornyFiltersFile(path)
	return filters, every, err
}

func dornyFiltersFile(path string) (map[string][]string, bool, string, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- caller supplies a repository workflow path.
	if err != nil {
		return nil, false, "", fmt.Errorf("read workflow %s: %w", path, err)
	}
	var wf pathFilterWorkflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return nil, false, "", fmt.Errorf("parse workflow %s: %w", path, err)
	}
	keys := make([]string, 0, len(wf.Jobs))
	for key := range wf.Jobs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		job := wf.Jobs[key]
		for _, step := range job.Steps {
			if !strings.Contains(step.Uses, "dorny/paths-filter") {
				continue
			}
			ref, ok := step.With["filters"].(string)
			if !ok {
				return nil, false, key, fmt.Errorf("workflow %s has no string dorny filters", path)
			}
			var inline map[string][]string
			if err := yaml.Unmarshal([]byte(ref), &inline); err == nil && len(inline) > 0 {
				filters, every, host := dornyFilters(raw)
				return filters, every, host, nil
			}
			if strings.Contains(ref, "\n") || ref == "" || filepath.IsAbs(ref) || filepath.Clean(ref) != ref || strings.HasPrefix(ref, "..") {
				return nil, false, key, fmt.Errorf("workflow %s has invalid dorny filters reference %q", path, ref)
			}
			root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
			root, err = filepath.EvalSymlinks(root)
			if err != nil {
				return nil, false, key, fmt.Errorf("resolve workflow root for %s: %w", path, err)
			}
			resolved, err := filepath.EvalSymlinks(filepath.Join(root, ref))
			if err != nil {
				return nil, false, key, fmt.Errorf("resolve dorny filters %q: %w", ref, err)
			}
			rel, err := filepath.Rel(root, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, false, key, fmt.Errorf("dorny filters %q escapes repository", ref)
			}
			content, err := os.ReadFile(resolved) // #nosec G304 -- path is repository-confined.
			if err != nil {
				return nil, false, key, fmt.Errorf("read dorny filters %q: %w", ref, err)
			}
			var filters map[string][]string
			if err := yaml.Unmarshal(content, &filters); err != nil || len(filters) == 0 {
				return nil, false, key, fmt.Errorf("dorny filters %q are empty or malformed: %v", ref, err)
			}
			q, _ := step.With["predicate-quantifier"].(string)
			return filters, q == "every", key, nil
		}
	}
	return nil, false, "", nil
}
