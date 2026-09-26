// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lines

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestMigrationTriggersSkipExactlyTheDeferredSession binds the two Go constants
// bootstrap-index sets on its connections to the WHEN clause of migration 131's
// content_files triggers: a rename on either side alone would make the gate
// silently never (or always) fire.
func TestMigrationTriggersSkipExactlyTheDeferredSession(t *testing.T) {
	t.Parallel()

	migration := postgres.MigrationSQL("content_file_secret_lines")
	when := "WHEN (current_setting('" + SessionSetting + "', true) IS DISTINCT FROM '" + deferredValue + "')"
	for _, trigger := range []string{"content_files_secret_lines_insert", "content_files_secret_lines_update"} {
		start := strings.Index(migration, "CREATE TRIGGER "+trigger)
		if start < 0 {
			t.Fatalf("migration 131 does not create %s", trigger)
		}
		end := strings.Index(migration[start:], "EXECUTE FUNCTION")
		if end < 0 || !strings.Contains(migration[start:start+end], when) {
			t.Fatalf("trigger %s lacks %q before EXECUTE FUNCTION", trigger, when)
		}
	}
	if want := "SET eshu.secret_lines_derive = 'deferred'"; DeferredSessionSQL != want {
		t.Fatalf("DeferredSessionSQL = %q, want %q", DeferredSessionSQL, want)
	}
}
