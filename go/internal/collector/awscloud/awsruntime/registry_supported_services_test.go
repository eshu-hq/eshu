// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/internal/guardset"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestSupportedServiceKindsBuildScanners exercises every registered service
// builder through DefaultScannerFactory.Scanner, so adding a runtimebind
// import to bindings.go is the only step needed for a new scanner to be
// reachable from the runtime entry point.
func TestSupportedServiceKindsBuildScanners(t *testing.T) {
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	factory := awsruntime.DefaultScannerFactory{RedactionKey: key}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	for _, service := range awsruntime.SupportedServiceKinds() {
		t.Run(service, func(t *testing.T) {
			target := awsruntime.Target{
				AccountID:   "123456789012",
				Region:      "us-east-1",
				ServiceKind: service,
			}
			boundary := awscloud.Boundary{
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
// exactly one builder per services/<service>/runtimebind/ directory. The
// expected count is DERIVED from the filesystem, not a hardcoded want-list, so
// a new scanner adds zero lines to this test. A binding that is imported but
// fails to register at init (so its kind never reaches the registry) still
// fails here because the registered count drops below the directory count.
//
// This test imports the bindings aggregator (blank import above) so every
// production registration runs before the assertion.
func TestSupportedServiceKindsCoversEveryRuntimebind(t *testing.T) {
	dirs, err := guardset.RuntimebindServiceDirs(servicesDir(t))
	if err != nil {
		t.Fatalf("RuntimebindServiceDirs() error = %v", err)
	}
	if len(dirs) == 0 {
		t.Fatalf("RuntimebindServiceDirs() = empty, want the live scanner set")
	}
	if got := len(awsruntime.SupportedServiceKinds()); got != len(dirs) {
		t.Errorf("len(SupportedServiceKinds()) = %d, want %d (one per runtimebind dir %v)", got, len(dirs), dirs)
	}
	if !awsruntime.SupportsServiceKind(awscloud.ServiceIAM) {
		t.Fatalf("SupportsServiceKind(iam) = false, want true")
	}
	if awsruntime.SupportsServiceKind("nonexistent") {
		t.Fatalf("SupportsServiceKind(nonexistent) = true, want false")
	}
}

// servicesDir resolves go/internal/collector/awscloud/services from this test
// file's location so the directory walk does not depend on the go test working
// directory.
func servicesDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// registry_supported_services_test.go lives in awsruntime/; services is a
	// sibling of awsruntime under awscloud/.
	return filepath.Join(filepath.Dir(currentFile), "..", "services")
}
