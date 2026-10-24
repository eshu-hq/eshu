// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"regexp"
	"testing"
)

// TestContentStorePathPatternMatchesGovernedPaths pins the path predicate the
// eshu_dp_content_policy_raw_rows gauge runs in SQL (#8833): it matches a path
// exactly when ContentStoreForPath binds a non-store policy, case-insensitively,
// as LookupByPath does.
func TestContentStorePathPatternMatchesGovernedPaths(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	pattern := registry.ContentStorePathPattern()
	if pattern == "" {
		t.Fatal("the default registry binds tfvars and yarnrc, so the pattern must not be empty")
	}
	re := regexp.MustCompile("(?i)" + pattern)
	for _, path := range []string{
		"terraform.tfvars", "env/prod.auto.tfvars", "infra/terraform.tfvars.json", "ENV/STAGE.TFVARS",
		".yarnrc.yml", "packages/web/.yarnrc.yml", ".YARNRC.YML",
		"main.tf", "live/terragrunt.hcl", "config.yml", "app.yaml", "yarnrc.yml", "x.yarnrc.yml",
		"a.tfvars.bak", "tfvars", "notes/tfvars.md", "src/main.go",
	} {
		governed := registry.ContentStoreForPath(path).Policy != ContentStoreStore
		if got := re.MatchString(path); got != governed {
			t.Errorf("pattern matches %q = %v, ContentStoreForPath governed = %v", path, got, governed)
		}
	}
	if got := contentStoreRegistry(t, ContentStore{}).ContentStorePathPattern(); got != "" {
		t.Errorf("an all-store registry pattern = %q, want empty", got)
	}
}
