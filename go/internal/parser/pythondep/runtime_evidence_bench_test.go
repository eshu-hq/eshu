// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkPythonRuntimeTOMLParse measures full dependency parsing for fixed
// manifests that also declare an interpreter version. It runs unchanged on
// both sides of the runtime-evidence change for a paired comparison.
func BenchmarkPythonRuntimeTOMLParse(b *testing.B) {
	for _, tc := range []struct {
		name  string
		body  string
		parse func(string) (map[string]any, error)
	}{
		{"pyproject", "[project]\nname = 'bench'\nrequires-python = '>=3.10'\ndependencies = ['httpx>=0.27']\n\n[tool.poetry.dependencies]\npython = '^3.11'\nrequests = '^2.32'\n", ParsePyProject},
		{"pipfile", "[requires]\npython_version = '3.12'\npython_full_version = '3.12.1'\n\n[packages]\nhttpx = '*'\nrequests = '>=2.32'\n", ParsePipfile},
	} {
		b.Run(tc.name, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), map[string]string{"pyproject": "pyproject.toml", "pipfile": "Pipfile"}[tc.name])
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(len(tc.body)), "source_bytes")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				payload, err := tc.parse(path)
				if err != nil || payload == nil {
					b.Fatalf("parse manifest: %v", err)
				}
			}
		})
	}
}
