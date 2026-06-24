// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestParsePipfileLockEmitsExactDependencyRowsAcrossDefaultAndDevel proves
// that Pipfile.lock graduates to a covered PyPI lockfile parser. Eshu's
// supply-chain reducer needs the exact installed version from the lock when
// available so vulnerability impact does not stop at a "*" Pipfile range.
func TestParsePipfileLockEmitsExactDependencyRowsAcrossDefaultAndDevel(t *testing.T) {
	t.Parallel()

	path := writeJSONTestFile(t, "Pipfile.lock", `{
  "_meta": {
    "sources": [{"name": "pypi", "url": "https://pypi.org/simple", "verify_ssl": true}]
  },
  "default": {
    "requests": {"version": "==2.31.0", "hashes": ["sha256:deadbeef"]},
    "internal-tool": {"git": "https://github.com/acme/internal-tool.git", "ref": "v1.0"}
  },
  "develop": {
    "pytest": {"version": "==7.4.4"}
  }
}`)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse(Pipfile.lock) error = %v", err)
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	byName := dependencyRowsByName(rows)

	requests, ok := byName["requests"]
	if !ok {
		t.Fatalf("requests missing in %#v", rows)
	}
	if got, want := requests["value"], "2.31.0"; got != want {
		t.Fatalf("requests value = %#v, want exact %q (== stripped)", got, want)
	}
	if got, want := requests["config_kind"], "dependency"; got != want {
		t.Fatalf("requests config_kind = %#v, want dependency", got)
	}
	if got, want := requests["package_manager"], "pypi"; got != want {
		t.Fatalf("requests package_manager = %#v, want pypi", got)
	}
	if got, _ := requests["lockfile"].(bool); !got {
		t.Fatalf("requests lockfile = %#v, want true", requests["lockfile"])
	}
	if got, want := requests["section"], "default"; got != want {
		t.Fatalf("requests section = %#v, want default", got)
	}

	pytest, ok := byName["pytest"]
	if !ok {
		t.Fatalf("pytest missing")
	}
	if got, want := pytest["value"], "7.4.4"; got != want {
		t.Fatalf("pytest value = %#v, want %q", got, want)
	}
	if dev, _ := pytest["dev_dependency"].(bool); !dev {
		t.Fatalf("pytest dev_dependency = %#v, want true (develop section)", pytest["dev_dependency"])
	}

	internal, ok := byName["internal-tool"]
	if !ok {
		t.Fatalf("internal-tool missing")
	}
	if got, want := internal["config_kind"], "vcs_dependency"; got != want {
		t.Fatalf("internal-tool config_kind = %#v, want vcs_dependency", got)
	}
}
