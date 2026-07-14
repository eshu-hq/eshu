// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildRepresentativeAtlantisConfig returns an atlantis.yaml body with the given
// number of projects and workflows, sized to the issue #4846 "typical" scale
// (~50 projects, ~10 workflows). Every other project references a defined
// workflow so both the projects[] and workflows: paths carry real work.
func buildRepresentativeAtlantisConfig(projects, workflows int) string {
	var b strings.Builder
	b.WriteString("version: 3\nautomerge: true\nparallel_plan: true\nprojects:\n")
	for i := 0; i < projects; i++ {
		fmt.Fprintf(&b, "  - name: service-%02d\n", i)
		fmt.Fprintf(&b, "    dir: terraform/service-%02d\n", i)
		fmt.Fprintf(&b, "    workspace: default\n")
		fmt.Fprintf(&b, "    terraform_version: v1.7.5\n")
		fmt.Fprintf(&b, "    workflow: wf-%02d\n", i%workflows)
		fmt.Fprintf(&b, "    autoplan:\n      enabled: true\n      when_modified:\n        - \"*.tf\"\n        - \"../modules/**/*.tf\"\n")
		fmt.Fprintf(&b, "    apply_requirements:\n      - approved\n      - mergeable\n")
		if i > 0 {
			fmt.Fprintf(&b, "    depends_on:\n      - service-%02d\n", i-1)
			fmt.Fprintf(&b, "    execution_order_group: %d\n", i)
		}
	}
	b.WriteString("workflows:\n")
	for i := 0; i < workflows; i++ {
		fmt.Fprintf(&b, "  wf-%02d:\n", i)
		fmt.Fprintf(&b, "    plan:\n      steps:\n        - init\n        - run: terraform fmt -check\n        - plan\n")
		fmt.Fprintf(&b, "    apply:\n      steps:\n        - apply\n")
	}
	return b.String()
}

// BenchmarkParseAtlantisConfig measures the full Parse() path over a
// representative atlantis.yaml (50 projects, 10 workflows). It is the
// before/after guard for issue #4846: the pre-change path unmarshalled the
// source twice (once for projects, once for workflows); the post-change path
// unmarshals once and extracts both buckets from the shared node tree. The
// benchmark drives the stable public Parse() entrypoint, so the same benchmark
// runs unchanged against origin/main and the single-unmarshal branch.
func BenchmarkParseAtlantisConfig(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "atlantis.yaml")
	source := buildRepresentativeAtlantisConfig(50, 10)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		b.Fatalf("write atlantis.yaml: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload, err := Parse(path, false, Options{})
		if err != nil {
			b.Fatalf("Parse() error = %v", err)
		}
		if len(payload["atlantis_projects"].([]map[string]any)) != 50 {
			b.Fatalf("expected 50 atlantis_projects rows")
		}
	}
}
