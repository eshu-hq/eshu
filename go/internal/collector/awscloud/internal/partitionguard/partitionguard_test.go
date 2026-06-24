// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package partitionguard_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/partitionguard"
)

// TestLiveScannerTreeHasNoHardcodedPartitions is the repo-level guard: it walks
// the live AWS scanner tree and fails if any scanner synthesizes an ARN with a
// hardcoded commercial `aws` partition instead of deriving it. This closes the
// partition graph-join bug class by construction — a new scanner that hardcodes
// `arn:aws:<service>:` or `arn:aws:s3:::` fails CI here.
func TestLiveScannerTreeHasNoHardcodedPartitions(t *testing.T) {
	servicesDir := liveServicesDir(t)
	violations, err := partitionguard.ScanForHardcodedPartitions(servicesDir)
	if err != nil {
		t.Fatalf("scan for hardcoded partitions: %v", err)
	}
	if len(violations) > 0 {
		t.Errorf("%d scanner ARN synthesis sites hardcode the commercial partition; derive it instead:", len(violations))
		for _, v := range violations {
			t.Errorf("  %s", v)
		}
	}
}

func liveServicesDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// .../internal/collector/awscloud/internal/partitionguard -> .../services
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "services")
}
