// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"testing"
)

// TestParsePoetryLockEmitsExactVersionsAndDistinguishesSourceProvenance
// proves poetry.lock graduates registered PyPI packages to exact-version
// evidence while keeping git/path sources from being treated as registry
// pins. The reducer needs both signals to compute trustworthy
// vulnerability impact.
func TestParsePoetryLockEmitsExactVersionsAndDistinguishesSourceProvenance(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "poetry.lock", `
[[package]]
name = "requests"
version = "2.31.0"
description = "Python HTTP for Humans."
optional = false
python-versions = ">=3.7"

[[package]]
name = "pytest"
version = "7.4.4"
description = "pytest"
category = "dev"
optional = false

[[package]]
name = "internal-tool"
version = "0.0.0"
description = "internal git dependency"

[package.source]
type = "git"
url = "https://github.com/acme/internal-tool.git"
reference = "main"
resolved_reference = "deadbeef"

[[package]]
name = "local-pkg"
version = "0.1.0"
description = "local directory dependency"

[package.source]
type = "directory"
url = "../local-pkg"
`)

	payload, err := ParsePoetryLock(path)
	if err != nil {
		t.Fatalf("ParsePoetryLock error = %v", err)
	}
	rows := variableRows(t, payload)
	byName := rowsByName(rows)

	requests, ok := byName["requests"]
	if !ok {
		t.Fatalf("requests missing in %#v", rows)
	}
	if got, want := requests["value"], "2.31.0"; got != want {
		t.Fatalf("requests value = %#v, want exact %q", got, want)
	}
	if got, want := requests["config_kind"], "dependency"; got != want {
		t.Fatalf("requests config_kind = %#v, want dependency", got)
	}
	if got, _ := requests["lockfile"].(bool); !got {
		t.Fatalf("requests lockfile = %#v, want true", requests["lockfile"])
	}
	if got, want := requests["package_manager"], "pypi"; got != want {
		t.Fatalf("requests package_manager = %#v, want pypi", got)
	}

	pytest, ok := byName["pytest"]
	if !ok {
		t.Fatalf("pytest missing")
	}
	if dev, _ := pytest["dev_dependency"].(bool); !dev {
		t.Fatalf("pytest dev_dependency = %#v, want true (category=dev)", pytest["dev_dependency"])
	}

	internal, ok := byName["internal-tool"]
	if !ok {
		t.Fatalf("internal-tool missing")
	}
	if got, want := internal["config_kind"], "vcs_dependency"; got != want {
		t.Fatalf("internal-tool config_kind = %#v, want vcs_dependency (lockfile recorded git source)", got)
	}
	if got, want := internal["source_kind"], "vcs"; got != want {
		t.Fatalf("internal-tool source_kind = %#v, want vcs", got)
	}
	// resolved_reference (commit SHA) MUST win over the human-readable
	// reference. Otherwise downstream consumers lose the precise commit
	// pinned by the lockfile.
	if got, want := internal["source_ref"], "deadbeef"; got != want {
		t.Fatalf("internal-tool source_ref = %#v, want resolved_reference %q (not branch %q)", got, want, "main")
	}

	local, ok := byName["local-pkg"]
	if !ok {
		t.Fatalf("local-pkg missing")
	}
	if got, want := local["config_kind"], "path_dependency"; got != want {
		t.Fatalf("local-pkg config_kind = %#v, want path_dependency", got)
	}
}
