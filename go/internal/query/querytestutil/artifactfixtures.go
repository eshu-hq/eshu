// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ReadAnsibleJenkinsAutomationFixture reads one file from the shared
// ansible_jenkins_automation ecosystem fixture corpus. It moved here for #6060
// lane B B3 because its readers span the repositoryartifacts family tests and
// root integration tests; a _test.go declaration in either package is
// unreachable from the other. The corpus path walks up from this package's
// directory exactly as it did from the artifacts tests, so the resolved path
// is unchanged.
func ReadAnsibleJenkinsAutomationFixture(t *testing.T, parts ...string) string {
	t.Helper()

	path := ansibleJenkinsAutomationFixturePath(parts...)
	// #nosec G304 -- path is anchored at this package's directory via runtime.Caller plus test-supplied fixture parts, never external input
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	return string(body)
}

func ansibleJenkinsAutomationFixturePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "tests", "fixtures", "ecosystems", "ansible_jenkins_automation"))
	elems := append([]string{root}, parts...)
	return filepath.Join(elems...)
}

// RequireRepositoryStoryDeliveryFamily returns the delivery-family row for
// family, or nil when no row carries it. It moved here for #6060 lane B B3
// because the cloudformation delivery test moved to the repository family
// package while the delivery-parity tests stay in repositoryartifacts.
func RequireRepositoryStoryDeliveryFamily(rows []map[string]any, family string) map[string]any {
	for _, row := range rows {
		if querycontract.StringVal(row, "family") == family {
			return row
		}
	}
	return nil
}
