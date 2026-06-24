// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	// Blank import installs the full AWS scanner registry via init side
	// effects so the derived redaction set reflects production registrations.
	// main.go imports the same aggregator; this keeps the test honest even if
	// the command's import is refactored.
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
)

// TestRedactionKeySetDerivesFromRuntimebindRegistrations proves the command
// derives the redaction-key requirement from the registry rather than a
// hand-maintained switch. The expected set is computed from the
// services/<svc>/runtimebind/bind.go files that set RequiresRedactionKey:
// true, so adding a redaction scanner needs no change here or in config.go.
// A scanner that declares the flag in its binding but is missing from the
// registry-derived set (or vice versa) fails this test.
func TestRedactionKeySetDerivesFromRuntimebindRegistrations(t *testing.T) {
	want := redactionRequiringServiceDirs(t)
	if len(want) == 0 {
		t.Fatalf("redactionRequiringServiceDirs() = empty, want the live redaction scanner set")
	}
	got := awsruntime.ServiceKindsRequiringRedactionKey()
	if !equalStringSets(got, want) {
		t.Fatalf("ServiceKindsRequiringRedactionKey() = %v, want %v (derived from runtimebind RequiresRedactionKey flags)", got, want)
	}
}

// TestRedactionKeyErrorListsDerivedServices proves the missing-key error
// message lists exactly the registry-derived set, joined as "a, b, ..., or z".
// The expectation is recomputed from the registry the same way the production
// code builds the phrase, so the assertion stays meaningful as scanners change
// instead of pinning a brittle literal.
func TestRedactionKeyErrorListsDerivedServices(t *testing.T) {
	kinds := awsruntime.ServiceKindsRequiringRedactionKey()
	if len(kinds) < 3 {
		t.Fatalf("ServiceKindsRequiringRedactionKey() = %v, want at least three redaction scanners for the join check", kinds)
	}

	// Drive the real error path with the first redaction-requiring service and
	// no ESHU_AWS_REDACTION_KEY set.
	getenv := mapEnv(map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": `[{
			"instance_id":"collector-aws-1",
			"collector_kind":"aws",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"target_scopes":[{
					"account_id":"123456789012",
					"allowed_regions":["us-east-1"],
					"allowed_services":["` + kinds[0] + `"],
					"credentials":{
						"mode":"local_workload_identity"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
	})

	_, err := loadRuntimeConfig(getenv)
	if err == nil {
		t.Fatalf("loadRuntimeConfig() error = nil, want missing redaction key rejection")
	}

	wantPhrase := expectedRedactionPhrase(kinds)
	wantMsg := "ESHU_AWS_REDACTION_KEY is required when " + wantPhrase + " service scans are enabled"
	if err.Error() != wantMsg {
		t.Fatalf("loadRuntimeConfig() error = %q, want %q", err.Error(), wantMsg)
	}
	// Every redaction-requiring kind must appear in the message so an operator
	// can see which scanner forced the requirement.
	for _, kind := range kinds {
		if !strings.Contains(err.Error(), kind) {
			t.Fatalf("error %q missing redaction service %q", err.Error(), kind)
		}
	}
}

// expectedRedactionPhrase mirrors redactionKeyServicesPhrase so the test
// recomputes the expected list instead of hardcoding it. Both must render the
// sorted set as "a, b, ..., or z".
func expectedRedactionPhrase(kinds []string) string {
	switch len(kinds) {
	case 0:
		return "a redaction-requiring"
	case 1:
		return kinds[0]
	case 2:
		return kinds[0] + " or " + kinds[1]
	default:
		return strings.Join(kinds[:len(kinds)-1], ", ") + ", or " + kinds[len(kinds)-1]
	}
}

// redactionRequiringServiceDirs returns the sorted set of service tokens whose
// services/<svc>/runtimebind/bind.go declares RequiresRedactionKey: true. It
// reads the source on disk so the expected set is not a hand-maintained list:
// the registry-derived set must match what the bindings actually register.
func redactionRequiringServiceDirs(t *testing.T) []string {
	t.Helper()
	servicesDir := awsServicesDir(t)
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("read services dir %q: %v", servicesDir, err)
	}
	var services []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		bindFile := filepath.Join(servicesDir, entry.Name(), "runtimebind", "bind.go")
		data, readErr := os.ReadFile(bindFile)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			t.Fatalf("read bind file %q: %v", bindFile, readErr)
		}
		if strings.Contains(string(data), "RequiresRedactionKey: true") {
			services = append(services, entry.Name())
		}
	}
	sort.Strings(services)
	return services
}

// awsServicesDir resolves go/internal/collector/awscloud/services from this
// test file's location so the walk does not depend on the working directory.
func awsServicesDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// This file lives in go/cmd/collector-aws-cloud/; services is under
	// go/internal/collector/awscloud/services.
	return filepath.Join(
		filepath.Dir(currentFile),
		"..", "..",
		"internal", "collector", "awscloud", "services",
	)
}

// equalStringSets reports whether two sorted slices hold the same elements.
func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
