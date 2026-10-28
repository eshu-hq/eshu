// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestSetupPythonRuntimeBoundLiteralCalls(t *testing.T) {
	for _, tc := range []struct{ name, source, value string }{
		{"direct", "from setuptools import setup\nsetup(name='demo', python_requires='>=3.10')\n", ">=3.10"},
		{"aliased", "from setuptools import setup as package_setup\npackage_setup(python_requires='3.12.1')\n", "3.12.1"},
		{"module", "import setuptools as build\nbuild.setup(python_requires='latest')\n", "latest"},
		{"distutils", "from distutils.core import setup\nsetup(python_requires='${PYTHON_VERSION}')\n", "${PYTHON_VERSION}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, source, close := parsePythonForTest(t, tc.source)
			defer close()
			got := pythonSetupRuntimeEvidence(root, source)
			if len(got.Values) != 1 || got.Values[0].Value != tc.value || got.Values[0].SourceKind != "setup_py_python_requires" || got.Values[0].Line != 2 {
				t.Fatalf("evidence = %+v", got)
			}
		})
	}
}

func TestSetupPythonRuntimeRejectsUnboundOrDynamicCalls(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"unbound", "setup(python_requires='3.12')\n"},
		{"shadowed", "from setuptools import setup\nsetup = fake\nsetup(python_requires='3.12')\n"},
		{"dynamic", "from setuptools import setup\nsetup(python_requires=minimum_version)\n"},
		{"fstring", "from setuptools import setup\nsetup(python_requires=f'>={minimum_version}')\n"},
		{"kwargs", "from setuptools import setup\nsetup(python_requires='3.12', **options)\n"},
		{"wrong_module", "from something import setup\nsetup(python_requires='3.12')\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, source, close := parsePythonForTest(t, tc.source)
			defer close()
			got := pythonSetupRuntimeEvidence(root, source)
			if len(got.Values) != 0 {
				t.Fatalf("unexpected evidence = %+v", got)
			}
		})
	}
}

func TestSetupPythonRuntimeEvidenceCountsPastTokenCapWithoutPrefix(t *testing.T) {
	for _, count := range []int{shared.RuntimeVersionTokenCap, shared.RuntimeVersionTokenCap + 1} {
		body := "from setuptools import setup\n" + strings.Repeat("setup(python_requires='3.12')\n", count)
		evidence, err := ExtractSetupPYRuntimeEvidence([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if evidence.TokenCount != count {
			t.Fatalf("token count = %d, want %d", evidence.TokenCount, count)
		}
		if count == shared.RuntimeVersionTokenCap {
			if evidence.State != "parsed_values" || len(evidence.Values) != count {
				t.Fatalf("at cap evidence = %+v", evidence)
			}
		} else if evidence.State != "over_tokens" || len(evidence.Values) != 0 {
			t.Fatalf("over cap retained a prefix: %+v", evidence)
		}
	}
}
