// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// coreScanFiles are the config files whose ESHU_* reads must all be declared in
// the registry. This is the CI coverage gate: it keeps the registry from
// drifting away from the code it documents. Container-registry credential
// variables (ESHU_*_OCI_*, ESHU_*_PACKAGE_*) are integration-test gating read
// only from _test.go and are intentionally out of scope.
var coreScanFiles = []string{
	"internal/runtime/data_stores.go",
	"internal/runtime/config.go",
	"internal/runtime/pprof.go",
	"internal/coordinator/config.go",
	"cmd/reducer/code_value_flow_stale_cleanup_config.go",
	"cmd/collector-tempo/config.go",
	"cmd/collector-loki/config.go",
	"cmd/collector-prometheus-mimir/config.go",
	"cmd/collector-grafana/config.go",
	"cmd/collector-aws-cloud/config.go",
	"cmd/collector-jira/config.go",
	"cmd/collector-cicd-run/config.go",
	"cmd/collector-pagerduty/config.go",
	"cmd/collector-security-alerts/config.go",
	"cmd/collector-sbom-attestation/config.go",
	"cmd/collector-vulnerability-intelligence/config.go",
	"cmd/collector-vault-live/config.go",
	"cmd/collector-package-registry/config.go",
	"cmd/collector-oci-registry/config.go",
	"cmd/collector-terraform-state/config.go",
	"cmd/collector-azure-cloud/config.go",
	"cmd/collector-kubernetes-live/config.go",
	"cmd/collector-component-extension/config.go",
	"cmd/api/config_helpers.go",
	"cmd/api/main.go",
	"cmd/api/oidc_login.go",
}

var esuVarPattern = regexp.MustCompile(`ESHU_[A-Z0-9_]+`)

func TestRegistryCoversCoreEnvCallSites(t *testing.T) {
	t.Parallel()
	r := Default()
	goRoot := goModuleRoot(t)

	uncovered := map[string][]string{}
	for _, rel := range coreScanFiles {
		path := filepath.Join(goRoot, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		seen := map[string]struct{}{}
		for _, name := range esuVarPattern.FindAllString(string(data), -1) {
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			if !r.Covers(name) {
				uncovered[name] = append(uncovered[name], rel)
			}
		}
	}

	if len(uncovered) > 0 {
		names := make([]string, 0, len(uncovered))
		for name := range uncovered {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			t.Errorf("%s is read in %v but not declared in the envregistry; add it to coreEntries", name, uncovered[name])
		}
	}
}

// goModuleRoot returns the directory containing go.mod by walking up from the
// test's working directory (the package directory).
func goModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod above the test working directory")
		}
		dir = parent
	}
}
