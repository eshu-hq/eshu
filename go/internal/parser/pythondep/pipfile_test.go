// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"reflect"
	"testing"
)

// TestParsePipfileDeclaresRuntimeAndDevDependencies binds the Pipenv manifest
// shape into the same content_entity dependency row contract npm and composer
// already produce.
func TestParsePipfileDeclaresRuntimeAndDevDependencies(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t, "Pipfile", `
[[source]]
url = "https://pypi.org/simple"
verify_ssl = true
name = "pypi"

[packages]
requests = "==2.31.0"
flask = { version = "*", extras = ["async"] }
local-app = { path = "./libs/local-app" }
upstream = { git = "https://github.com/acme/upstream.git", ref = "v1.0" }

[dev-packages]
pytest = ">=7.0"
mypy = "*"
`)

	payload, err := ParsePipfile(path)
	if err != nil {
		t.Fatalf("ParsePipfile error = %v", err)
	}
	rows := variableRows(t, payload)
	byName := rowsByName(rows)

	requests, ok := byName["requests"]
	if !ok {
		t.Fatalf("requests missing in %#v", rows)
	}
	if got, want := requests["value"], "==2.31.0"; got != want {
		t.Fatalf("requests value = %#v, want pinned %q", got, want)
	}
	if got, want := requests["section"], "packages"; got != want {
		t.Fatalf("requests section = %#v, want packages", got)
	}
	if got, want := requests["package_manager"], "pypi"; got != want {
		t.Fatalf("requests package_manager = %#v, want pypi", got)
	}
	if dev, _ := requests["dev_dependency"].(bool); dev {
		t.Fatalf("requests dev_dependency = %#v, want false", requests["dev_dependency"])
	}

	flask, ok := byName["flask"]
	if !ok {
		t.Fatalf("flask missing")
	}
	if got, want := flask["value"], "*"; got != want {
		t.Fatalf("flask value = %#v, want %q", got, want)
	}
	if extras, ok := flask["extras"].([]string); !ok || !reflect.DeepEqual(extras, []string{"async"}) {
		t.Fatalf("flask extras = %#v, want [async]", flask["extras"])
	}

	pytest, ok := byName["pytest"]
	if !ok {
		t.Fatalf("pytest missing")
	}
	if dev, _ := pytest["dev_dependency"].(bool); !dev {
		t.Fatalf("pytest dev_dependency = %#v, want true", pytest["dev_dependency"])
	}
	if got, want := pytest["section"], "dev-packages"; got != want {
		t.Fatalf("pytest section = %#v, want dev-packages", got)
	}

	local := findRowByName(rows, "local-app")
	if local == nil {
		t.Fatalf("local-app missing")
	}
	if got, want := local["config_kind"], "path_dependency"; got != want {
		t.Fatalf("local-app config_kind = %#v, want path_dependency", got)
	}

	upstream := findRowByName(rows, "upstream")
	if upstream == nil {
		t.Fatalf("upstream missing")
	}
	if got, want := upstream["config_kind"], "vcs_dependency"; got != want {
		t.Fatalf("upstream config_kind = %#v, want vcs_dependency", got)
	}
}
