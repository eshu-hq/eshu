// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backup

import (
	"reflect"
	"strings"
	"testing"
)

// TestClientInterfaceExcludesMutationAndUnsafeReadAPIs asserts the Client
// surface in this package only exposes the metadata-only reads listed in the
// SDK adapter contract. The acceptance gate in issue #752 requires the
// scanner to never call Create/Update/Delete vault/plan/selection/report
// plan/restore testing plan/framework, StartBackupJob, StartRestoreJob,
// StartCopyJob, DeleteRecoveryPoint, PutBackupVaultAccessPolicy, or
// GetBackupVaultAccessPolicy. The Client interface is the only way the
// scanner reaches the AWS Backup API, so asserting the interface shape is a
// load-bearing proof that those APIs are unreachable from this code path.
func TestClientInterfaceExcludesMutationAndUnsafeReadAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	want := map[string]bool{
		"ListBackupVaults":        true,
		"ListBackupPlans":         true,
		"ListBackupSelections":    true,
		"ListRecoveryPoints":      true,
		"ListReportPlans":         true,
		"ListRestoreTestingPlans": true,
		"ListFrameworks":          true,
	}
	have := map[string]bool{}
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		have[method.Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("Client interface missing required method %q", name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("Client interface exposes unexpected method %q; metadata-only contract violated", name)
		}
	}

	// Defensive check: any method name containing forbidden verbs or APIs is a
	// contract violation. The list mirrors issue #752 acceptance language.
	forbiddenSubstrings := []string{
		"Create",
		"Update",
		"Delete",
		"Put",
		"Start",
		"Copy",
		"Restore",
		"Disassociate",
		"Associate",
		"Cancel",
		"Stop",
		"AccessPolicy",
		"VaultAccessPolicy",
		"RecoveryPointRestoreMetadata",
		"Notifications",
		"LegalHold",
	}
	// Allowed exceptions: methods whose names contain a forbidden substring
	// but are still safe reads. ListRecoveryPoints lists identities only,
	// ListRestoreTestingPlans lists plan summaries only, and
	// ListReportPlans lists report plan summaries only. None of them touch a
	// forbidden API.
	allowed := map[string]bool{
		"ListRecoveryPoints":      true,
		"ListRestoreTestingPlans": true,
		"ListReportPlans":         true,
	}
	for name := range have {
		if allowed[name] {
			continue
		}
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(name, forbidden) {
				t.Errorf("Client method %q contains forbidden substring %q", name, forbidden)
			}
		}
	}
}
