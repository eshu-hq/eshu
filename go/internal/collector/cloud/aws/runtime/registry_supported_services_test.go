// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime_test

import (
	"context"
	"path/filepath"
	stdruntime "runtime"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime/bindings"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime/internal/guardset"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestSupportedServiceKindsBuildScanners exercises every registered service
// builder through DefaultScannerFactory.Scanner, so adding a bind
// import to all.go is the only step needed for a new scanner to be
// reachable from the runtime entry point.
func TestSupportedServiceKindsBuildScanners(t *testing.T) {
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	factory := runtime.DefaultScannerFactory{RedactionKey: key}
	lease := staticAWSConfigLease{config: awsv2.Config{Region: "us-east-1"}}
	for _, service := range runtime.SupportedServiceKinds() {
		t.Run(service, func(t *testing.T) {
			target := runtime.Target{
				AccountID:   "123456789012",
				Region:      "us-east-1",
				ServiceKind: service,
			}
			boundary := aws.Boundary{
				AccountID:   target.AccountID,
				Region:      target.Region,
				ServiceKind: target.ServiceKind,
			}
			scanner, err := factory.Scanner(context.Background(), target, boundary, lease)
			if err != nil {
				t.Fatalf("Scanner(%q) error = %v", service, err)
			}
			if scanner == nil {
				t.Fatalf("Scanner(%q) = nil", service)
			}
		})
	}
}

// TestSupportedServiceKindsCoversEveryRuntimebind asserts the registry holds
// exactly one builder per services/<service>/bind/ directory. The
// expected count is DERIVED from the filesystem, not a hardcoded want-list, so
// a new scanner adds zero lines to this test. A binding that is imported but
// fails to register at init (so its kind never reaches the registry) still
// fails here because the registered count drops below the directory count.
//
// This test imports the bindings aggregator (blank import above) so every
// production registration runs before the assertion.
func TestSupportedServiceKindsCoversEveryRuntimebind(t *testing.T) {
	dirs, err := guardset.BindServiceDirs(serviceDir(t))
	if err != nil {
		t.Fatalf("BindServiceDirs() error = %v", err)
	}
	if len(dirs) == 0 {
		t.Fatalf("BindServiceDirs() = empty, want the live scanner set")
	}
	if got := len(runtime.SupportedServiceKinds()); got != len(dirs) {
		t.Errorf("len(SupportedServiceKinds()) = %d, want %d (one per bind dir %v)", got, len(dirs), dirs)
	}
	if !runtime.SupportsServiceKind(aws.ServiceIAM) {
		t.Fatalf("SupportsServiceKind(iam) = false, want true")
	}
	if runtime.SupportsServiceKind("nonexistent") {
		t.Fatalf("SupportsServiceKind(nonexistent) = true, want false")
	}
}

// serviceDir resolves go/internal/collector/cloud/aws/service from this test
// file's location so the directory walk does not depend on the go test working
// directory.
func serviceDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := stdruntime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// registry_supported_services_test.go lives in runtime/; service is a
	// sibling of runtime under aws/.
	return filepath.Join(filepath.Dir(currentFile), "..", "service")
}
